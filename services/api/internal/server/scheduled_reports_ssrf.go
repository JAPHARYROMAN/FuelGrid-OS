package server

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Webhook SSRF guard for the per-tenant Scheduled Reports webhook channel
// (Reports Center Phase 12 — blueprint §8).
//
// A tenant-supplied webhook_url is an SSRF vector: left unchecked, a tenant could
// point a schedule at http://169.254.169.254/ (cloud metadata), http://localhost/
// (the API's own internal surface), or an RFC 1918 host on the cluster network and
// have the SERVER fetch it. The guard fails CLOSED:
//
//   - scheme MUST be https (no http, file, gopher, …);
//   - the host MUST resolve, and EVERY resolved IP must be a GLOBAL UNICAST public
//     address — any loopback / link-local / private / unique-local / unspecified /
//     multicast IP rejects the whole URL (so a DNS name that resolves to a private
//     IP is rejected just like a literal private IP);
//   - an OPTIONAL exact-hostname allowlist (SCHEDULED_REPORTS_WEBHOOK_ALLOW_HOSTS)
//     further restricts delivery to known integration endpoints when configured.
//
// validateWebhookURL is called BOTH at write time (CRUD create/update — a fast
// reject before a bad URL is ever stored) AND again at delivery (the worker, so a
// host whose DNS now resolves to a private IP is caught at the moment of the POST).

// validateWebhookURL parses + SSRF-checks a webhook URL against the optional host
// allowlist. It returns a descriptive error suitable for a 400 (write time) or a
// run failure reason (delivery time). lookupHost is injectable for tests; nil uses
// net.LookupIP.
func validateWebhookURL(raw string, allowHosts []string, lookupIP func(host string) ([]net.IP, error)) error {
	if lookupIP == nil {
		lookupIP = net.LookupIP
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("webhook_url is required for the webhook channel")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webhook_url is not a valid URL")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("webhook_url must use https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook_url has no host")
	}

	// Optional exact-hostname allowlist (case-insensitive).
	if len(allowHosts) > 0 {
		ok := false
		for _, h := range allowHosts {
			if strings.EqualFold(strings.TrimSpace(h), host) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("webhook host %q is not in the allowed-hosts list", host)
		}
	}

	// Resolve the host (or use the literal IP) and reject any non-public address.
	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		resolved, lerr := lookupIP(host)
		if lerr != nil {
			return fmt.Errorf("webhook host %q does not resolve", host)
		}
		ips = resolved
	}
	if len(ips) == 0 {
		return fmt.Errorf("webhook host %q does not resolve to any address", host)
	}
	for _, ip := range ips {
		if !isPublicUnicast(ip) {
			return fmt.Errorf("webhook host %q resolves to a non-public address (private, loopback or link-local) and is blocked", host)
		}
	}
	return nil
}

// isPublicUnicast reports whether ip is a globally-routable public unicast
// address: NOT loopback, link-local (unicast or multicast), multicast,
// unspecified, private (RFC 1918 / RFC 4193 unique-local), or in the
// 100.64.0.0/10 carrier-grade-NAT / 169.254.0.0/16 ranges. Fails closed for
// anything it does not positively recognise as global unicast.
func isPublicUnicast(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsPrivate() {
		return false
	}
	// 100.64.0.0/10 — RFC 6598 shared CGN space (not caught by IsPrivate).
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
	}
	// Go's IsGlobalUnicast returns true for private too; combined with the explicit
	// rejections above, requiring it here is the final positive gate.
	return ip.IsGlobalUnicast()
}
