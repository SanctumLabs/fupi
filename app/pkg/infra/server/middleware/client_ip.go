package middleware

import (
	"net/netip"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// ClientIPConfig controls how ClientIP resolves the originating client address.
type ClientIPConfig struct {
	// Header is the request header carrying the client address, set by a trusted proxy (e.g. X-Forwarded-For).
	// When empty, the header is never consulted.
	Header string
	// TrustedProxies are the networks whose peers are allowed to set Header.
	TrustedProxies []netip.Prefix
}

// ClientIP returns the address of the client that sent the request. The TCP peer is returned unless cfg.Header is set
// and the peer is a trusted proxy, in which case the header is walked from the right and the first address that is
// not itself a trusted proxy is returned. Repeated header lines are read as one list, in order (RFC 9110), since a proxy
// may add its own line after the client's. Entries further left are client-controlled and are never consulted. If the
// header yields no usable address, or an entry on the way is not an IP, the peer is returned.
func ClientIP(c *fiber.Ctx, cfg ClientIPConfig) string {
	remote := c.Context().RemoteIP()
	peer, ok := netip.AddrFromSlice(remote)
	if !ok {
		return remote.String()
	}
	peer = peer.Unmap()

	if cfg.Header == "" || !cfg.isTrusted(peer) {
		return peer.String()
	}

	var entries []string
	for _, line := range c.Request().Header.PeekAll(cfg.Header) {
		entries = append(entries, strings.Split(string(line), ",")...)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(entries[i]))
		if err != nil {
			return peer.String()
		}
		addr = addr.Unmap()
		if !cfg.isTrusted(addr) {
			return addr.String()
		}
	}
	return peer.String()
}

func (cfg ClientIPConfig) isTrusted(addr netip.Addr) bool {
	for _, prefix := range cfg.TrustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
