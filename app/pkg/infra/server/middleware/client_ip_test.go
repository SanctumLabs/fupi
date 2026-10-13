package middleware

import (
	"net"
	"net/netip"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
)

// clientIPFor runs ClientIP inside a fiber handler for a request arriving from the given peer address.
func clientIPFor(t *testing.T, peer string, cfg ClientIPConfig, headers map[string]string) string {
	t.Helper()

	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(ClientIP(c, cfg))
	})

	var req fasthttp.Request
	req.SetRequestURI("/")
	req.Header.SetMethod(fiber.MethodGet)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	var ctx fasthttp.RequestCtx
	ctx.Init(&req, &net.TCPAddr{IP: net.ParseIP(peer), Port: 4321}, nil)
	app.Handler()(&ctx)

	return string(ctx.Response.Body())
}

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	tests := []struct {
		name    string
		peer    string
		header  string
		value   string
		trusted []netip.Prefix
		want    string
	}{
		{name: "no header configured", peer: "203.0.113.7", header: "", trusted: trusted, want: "203.0.113.7"},
		{name: "untrusted peer ignores header", peer: "203.0.113.7", header: "X-Forwarded-For", value: "1.2.3.4", trusted: trusted, want: "203.0.113.7"},
		{name: "trusted peer single value", peer: "10.0.0.2", header: "X-Real-IP", value: "198.51.100.9", trusted: trusted, want: "198.51.100.9"},
		{name: "XFF rightmost untrusted", peer: "10.0.0.2", header: "X-Forwarded-For", value: "6.6.6.6, 198.51.100.9, 10.0.0.3", trusted: trusted, want: "198.51.100.9"},
		{name: "spoofed leftmost ignored", peer: "10.0.0.2", header: "X-Forwarded-For", value: "1.1.1.1, 198.51.100.9", trusted: trusted, want: "198.51.100.9"},
		{name: "garbage entry returns peer", peer: "10.0.0.2", header: "X-Forwarded-For", value: "198.51.100.9, nonsense", trusted: trusted, want: "10.0.0.2"},
		{name: "all trusted returns peer", peer: "10.0.0.2", header: "X-Forwarded-For", value: "10.0.0.5", trusted: trusted, want: "10.0.0.2"},
		{name: "header absent returns peer", peer: "10.0.0.2", header: "X-Forwarded-For", trusted: trusted, want: "10.0.0.2"},
		{name: "IPv6 client", peer: "10.0.0.2", header: "X-Forwarded-For", value: "2001:db8::1", trusted: trusted, want: "2001:db8::1"},
		{name: "IPv4-mapped peer is trusted", peer: "::ffff:10.0.0.2", header: "X-Forwarded-For", value: "198.51.100.9", trusted: trusted, want: "198.51.100.9"},
		{name: "IPv4-mapped peer is unmapped", peer: "::ffff:203.0.113.7", header: "", trusted: trusted, want: "203.0.113.7"},
		{name: "IPv4-mapped header entry is unmapped", peer: "10.0.0.2", header: "X-Forwarded-For", value: "198.51.100.9, ::ffff:10.0.0.3", trusted: trusted, want: "198.51.100.9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			if tt.value != "" {
				headers[tt.header] = tt.value
			}
			got := clientIPFor(t, tt.peer, ClientIPConfig{Header: tt.header, TrustedProxies: tt.trusted}, headers)
			assert.Equal(t, tt.want, got)
		})
	}
}
