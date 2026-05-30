package server

import (
	"net/http"
	"testing"
)

// TestClientIP covers AUTH-09: X-Forwarded-For is honored only behind the
// configured number of trusted proxies, and the chosen entry is the
// proxy-attested client address (not a client-spoofable left entry).
func TestClientIP(t *testing.T) {
	prev := trustedProxyDepth
	defer func() { trustedProxyDepth = prev }()

	mk := func(remote, xff string) *http.Request {
		r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}

	cases := []struct {
		name   string
		depth  int
		remote string
		xff    string
		want   string
	}{
		{"no trust ignores xff", 0, "203.0.113.9:443", "1.1.1.1, 2.2.2.2", "203.0.113.9"},
		{"ipv6 remote unwrapped", 0, "[::1]:54321", "", "::1"},
		{"depth1 takes proxy-attested rightmost", 1, "10.0.0.1:8080", "9.9.9.9, 8.8.8.8", "8.8.8.8"},
		{"depth1 ignores spoofed left entry", 1, "10.0.0.1:8080", "1.2.3.4, 203.0.113.7", "203.0.113.7"},
		{"depth2 steps one further left", 2, "10.0.0.1:8080", "7.7.7.7, 8.8.8.8, 9.9.9.9", "8.8.8.8"},
		{"depth over chain clamps to leftmost", 5, "10.0.0.1:8080", "4.4.4.4, 5.5.5.5", "4.4.4.4"},
		{"depth1 no xff falls back to remote", 1, "203.0.113.9:443", "", "203.0.113.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			trustedProxyDepth = c.depth
			if got := clientIP(mk(c.remote, c.xff)); got != c.want {
				t.Fatalf("clientIP(depth=%d, remote=%q, xff=%q) = %q, want %q", c.depth, c.remote, c.xff, got, c.want)
			}
		})
	}
}
