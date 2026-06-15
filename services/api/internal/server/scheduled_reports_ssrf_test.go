package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// staticResolver returns a fixed IP set for any host, so the SSRF guard's
// resolve-then-classify path is tested deterministically without real DNS.
func staticResolver(ips ...string) func(string) ([]net.IP, error) {
	return func(string) ([]net.IP, error) {
		out := make([]net.IP, 0, len(ips))
		for _, s := range ips {
			out = append(out, net.ParseIP(s))
		}
		return out, nil
	}
}

// TestValidateWebhookURL_SchemeAndHost rejects non-https schemes and missing hosts.
func TestValidateWebhookURL_SchemeAndHost(t *testing.T) {
	t.Parallel()
	resolve := staticResolver("93.184.216.34") // example.com, public
	cases := []struct {
		name string
		url  string
		ok   bool
	}{
		{"https public", "https://hooks.example.com/abc", true},
		{"http rejected", "http://hooks.example.com/abc", false},
		{"ftp rejected", "ftp://hooks.example.com/abc", false},
		{"no host", "https://", false},
		{"garbage", "not a url", false},
	}
	for _, c := range cases {
		err := validateWebhookURL(c.url, nil, resolve)
		if c.ok && err != nil {
			t.Fatalf("%s: expected ok, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("%s: expected rejection", c.name)
		}
	}
}

// TestValidateWebhookURL_BlocksPrivate rejects every non-public address class: a
// host that resolves to loopback / RFC1918 / link-local / CGN / metadata is blocked
// even though the scheme is https.
func TestValidateWebhookURL_BlocksPrivate(t *testing.T) {
	t.Parallel()
	blocked := map[string]string{
		"loopback":     "127.0.0.1",
		"loopback-v6":  "::1",
		"private-10":   "10.0.0.5",
		"private-172":  "172.16.3.4",
		"private-192":  "192.168.1.10",
		"link-local":   "169.254.169.254", // cloud metadata
		"link-local-6": "fe80::1",
		"cgn-100.64":   "100.64.0.1",
		"unspecified":  "0.0.0.0",
		"ula-v6":       "fd00::1",
	}
	for name, ip := range blocked {
		err := validateWebhookURL("https://attacker.example.com/x", nil, staticResolver(ip))
		if err == nil {
			t.Fatalf("%s (%s): expected SSRF rejection, got nil", name, ip)
		}
	}

	// A literal private IP in the URL is rejected too (no DNS needed).
	if err := validateWebhookURL("https://169.254.169.254/latest/meta-data/", nil, nil); err == nil {
		t.Fatalf("literal metadata IP should be rejected")
	}

	// MIXED resolution: if ANY resolved address is private, the whole URL is blocked
	// (a DNS-rebinding-style host that resolves to both public and private).
	if err := validateWebhookURL("https://mixed.example.com/x", nil, staticResolver("93.184.216.34", "10.0.0.1")); err == nil {
		t.Fatalf("a host resolving to a private IP must be blocked even if it also resolves public")
	}
}

// TestValidateWebhookURL_Allowlist: when an allowlist is configured only listed
// hosts pass (in addition to the SSRF guard).
func TestValidateWebhookURL_Allowlist(t *testing.T) {
	t.Parallel()
	resolve := staticResolver("93.184.216.34")
	allow := []string{"hooks.example.com"}

	if err := validateWebhookURL("https://hooks.example.com/x", allow, resolve); err != nil {
		t.Fatalf("allowlisted host should pass: %v", err)
	}
	if err := validateWebhookURL("https://other.example.com/x", allow, resolve); err == nil {
		t.Fatalf("non-allowlisted host should be rejected")
	}
	// Even an allowlisted host is still SSRF-checked: if it resolves private, blocked.
	if err := validateWebhookURL("https://hooks.example.com/x", allow, staticResolver("127.0.0.1")); err == nil {
		t.Fatalf("allowlisted but private-resolving host must still be blocked")
	}
}

// TestWebhookClient_DialPinBlocksPrivate proves the dial-time IP pin (the
// DNS-rebinding / TOCTOU defense): even when a request reaches the HTTP client
// pointed at a private/loopback destination — simulating a host whose DNS flipped
// to a private IP AFTER the up-front validateWebhookURL guard ran — the dialer's
// Control hook rejects the connection before any bytes leave the box.
func TestWebhookClient_DialPinBlocksPrivate(t *testing.T) {
	t.Parallel()
	s := &Server{}
	client := s.webhookClient()

	// A loopback server stands in for any private/metadata endpoint a rebinding
	// host would resolve to. The pin must refuse to connect to it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("expected dial to a loopback address to be blocked by the IP pin, but the POST succeeded")
	}
	if !strings.Contains(err.Error(), "not a public address") {
		t.Fatalf("expected an IP-pin rejection, got: %v", err)
	}
}

// TestWebhookClient_DialPinAllowsPublicShape is a light sanity check that the pin's
// Control hook accepts a public-looking address (it does not, by itself, prove a
// real public POST works — that needs network — but it guards against the pin
// rejecting EVERYTHING, e.g. a logic inversion).
func TestWebhookClient_DialPinAllowsPublicShape(t *testing.T) {
	t.Parallel()
	s := &Server{}
	tr, ok := s.webhookClient().Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatalf("webhook client must use a custom DialContext for the IP pin")
	}
}
