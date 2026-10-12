package url

import (
	"fmt"
	neturl "net/url"
	"strings"

	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// OriginalURL is a value object that encapsulates validation.
// Construction fails if the URL is too short, too long, filtered, or malformed.
//
// The input is normalized before it is validated, so that the same target spelled differently
// collapses onto one value. urls.original_url is globally unique, and without normalization the rule
// would be trivial to bypass with a trailing slash or a tracking parameter.
type OriginalURL struct {
	value string
}

// NewOriginalURL creates a new OriginalURL value object after normalizing and validating the input.
func NewOriginalURL(url string) (OriginalURL, error) {
	normalized := normalizeURL(url)

	if l := len(normalized); l < MinLength || l > MaxLength {
		return OriginalURL{}, fmt.Errorf("%w: must be between %d and %d characters", errdefs.ErrInvalidURLLen, MinLength, MaxLength)
	}

	if filterRe.MatchString(normalized) {
		return OriginalURL{}, fmt.Errorf("%w: %q", errdefs.ErrFilteredURL, url)
	}

	if !urlRe.MatchString(normalized) {
		return OriginalURL{}, fmt.Errorf("%w: %q", errdefs.ErrInvalidURL, url)
	}

	return OriginalURL{value: normalized}, nil
}

// Value returns the original URL string.
func (o OriginalURL) Value() string {
	return o.value
}

// normalizeURL rewrites a URL into the canonical form used for storage and uniqueness:
// the scheme and host are lowercased, a default port for the scheme is dropped, a trailing slash is
// removed, and utm_* tracking parameters are stripped.
//
// It only rewrites the parts that actually need it, so a URL that is already canonical is returned
// byte for byte unchanged. Anything net/url cannot parse is returned untouched for the validation in
// NewOriginalURL to reject.
func normalizeURL(raw string) string {
	parsed, parseErr := neturl.Parse(raw)
	if parseErr != nil {
		return raw
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = stripDefaultPort(parsed.Scheme, strings.ToLower(parsed.Host))

	// Only re-encode the query when a tracking parameter is actually present: Encode() reorders and
	// re-escapes everything, which would otherwise change URLs that needed no change.
	if hasTrackingParam(parsed.Query()) {
		query := parsed.Query()
		for key := range query {
			if isTrackingParam(key) {
				query.Del(key)
			}
		}
		parsed.RawQuery = query.Encode()
	}

	// A trailing slash addresses the same resource, so "/path/" and "/path" must not be two rows.
	if strings.HasSuffix(parsed.Path, "/") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}

	return parsed.String()
}

// stripDefaultPort removes the port when it is the default for the scheme, since including it
// addresses the same host.
func stripDefaultPort(scheme, host string) string {
	switch {
	case scheme == "http" && strings.HasSuffix(host, ":80"):
		return strings.TrimSuffix(host, ":80")
	case scheme == "https" && strings.HasSuffix(host, ":443"):
		return strings.TrimSuffix(host, ":443")
	case scheme == "ftp" && strings.HasSuffix(host, ":21"):
		return strings.TrimSuffix(host, ":21")
	default:
		return host
	}
}

// isTrackingParam reports whether a query parameter is campaign tracking rather than part of the
// resource's identity.
func isTrackingParam(key string) bool {
	return strings.HasPrefix(strings.ToLower(key), "utm_")
}

func hasTrackingParam(query neturl.Values) bool {
	for key := range query {
		if isTrackingParam(key) {
			return true
		}
	}
	return false
}
