package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	environmentDevelopment = "development"
	environmentTest        = "test"

	// The development defaults. They match the local infrastructure stack and are refused outside development and test.
	devAuthSecret       = "fupi-secret"
	devDatabasePassword = "fupi-pass"
	devRedisPassword    = "fupi-svc"
	devShortCodeKey     = "fupi-short-code-key-for-development-only"
)

// Lookup reads one environment variable. os.LookupEnv satisfies it; tests pass a map.
type Lookup func(key string) (string, bool)

// reader reads typed values through a Lookup and remembers every problem, so a bad environment is reported in full
// instead of one variable at a time. A value that is set but empty counts as unset, because `FOO=` in a .env file
// means "not set".
type reader struct {
	lookup Lookup
	errs   []error
}

func newReader(lookup Lookup) *reader { return &reader{lookup: lookup} }

func (r *reader) raw(key string) (string, bool) {
	value, ok := r.lookup(key)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func (r *reader) str(key, def string) string {
	if value, ok := r.raw(key); ok {
		return value
	}
	return def
}

func (r *reader) integer(key string, def int) int {
	value, ok := r.raw(key)
	if !ok {
		return def
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		r.fail("%s must be an integer", key)
		return def
	}
	return number
}

func (r *reader) int32(key string, def int32) int32 {
	value, ok := r.raw(key)
	if !ok {
		return def
	}
	number, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		r.fail("%s must be a 32-bit integer", key)
		return def
	}
	return int32(number)
}

func (r *reader) boolean(key string, def bool) bool {
	value, ok := r.raw(key)
	if !ok {
		return def
	}
	flag, err := strconv.ParseBool(value)
	if err != nil {
		r.fail("%s must be true or false", key)
		return def
	}
	return flag
}

// units reads a whole number of unit, so DATABASE_CONN_TIMEOUT=30 with time.Second is thirty seconds.
func (r *reader) units(key string, def int, unit time.Duration) time.Duration {
	return time.Duration(r.integer(key, def)) * unit
}

// port records a failure unless value is a TCP port number.
func (r *reader) port(key, value string) {
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 || number > 65535 {
		r.fail("%s must be a port between 1 and 65535", key)
	}
}

// enforceSecrets is true for every environment except development and test (spec D6), so a forgotten variable in
// a real deployment fails at boot instead of running with a development default.
func (r *reader) enforceSecrets() bool {
	environment := r.str("ENVIRONMENT", environmentDevelopment)
	return environment != environmentDevelopment && environment != environmentTest
}

func (r *reader) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf(format, args...))
}

func (r *reader) err() error { return errors.Join(r.errs...) }

// splitList turns "a:1, b:2" into ["a:1", "b:2"], dropping empty entries.
func splitList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// parseProxies turns "10.0.0.1, 192.168.0.0/16" into prefixes; a bare address becomes a /32 or /128. It returns the
// first entry that is neither.
func parseProxies(entries []string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, entry := range entries {
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			prefixes = append(prefixes, prefix)
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR range", entry)
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

// validAddress reports whether entry is host:port with a usable port.
func validAddress(entry string) bool {
	host, port, err := net.SplitHostPort(entry)
	if err != nil || host == "" {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 1 && number <= 65535
}
