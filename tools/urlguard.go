package tools

import (
	"net"
	"net/url"
	"strings"
)

// extractHost returns the hostname from a URL.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// isNeverAllowIP reports IPs that must never be fetched, even when the
// caller put the literal in an allowlist: link-local (cloud metadata),
// unspecified, multicast, and CGNAT/shared-address space (RFC 6598).
func isNeverAllowIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
	}
	return false
}

// isBlockedIP reports whether ip is not safe to present as a crawlable
// URL. Covers private, loopback, link-local (unicast and multicast),
// unspecified (0.0.0.0 / ::), and CGNAT/shared-address space
// (100.64.0.0/10, RFC 6598) which cloud metadata and internal meshes
// sometimes use.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsPrivate() || ip.IsLoopback() || isNeverAllowIP(ip) {
		return true
	}
	return false
}

// isBlockedURL checks whether a URL points to a private, loopback,
// link-local, unspecified, multicast, or CGNAT address. This prevents
// SSRF attacks where a malicious search result could direct the agent
// to internal endpoints (e.g. cloud metadata at 169.254.169.254).
//
// Checks:
//   - Non-http(s) schemes are blocked.
//   - Userinfo (user:pass@host) is blocked — never useful for public
//     search results and a common SSRF smuggling vector.
//   - Hostnames "localhost", "127.0.0.1", "::1", and bare metadata
//     aliases are blocked by name before DNS.
//   - If the host is an IP literal, it is checked via isBlockedIP.
//   - If the host is a domain name, it is resolved via DNS and all
//     resolved IPs are checked. This prevents DNS rebinding attacks
//     where a domain resolves to an internal IP. Fail-closed: an
//     unresolvable host is blocked.
func isBlockedURL(rawURL string) bool {
	return blockedURL(rawURL, nil)
}

// blockedURL is the shared SSRF predicate. exactAllow is a set of
// lowercase hosts that may bypass loopback/private IP-literal blocks
// (httptest, explicit internal APIs). Link-local, metadata names,
// CGNAT, multicast, and unspecified are never bypassed. Domain names
// always go through DNS rebind checks (no bypass).
func blockedURL(rawURL string, exactAllow map[string]bool) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return true
	}
	if u.User != nil {
		return true
	}
	host := u.Hostname()
	if host == "" {
		return true
	}
	lower := strings.ToLower(host)
	switch lower {
	case "metadata", "metadata.google.internal":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if isNeverAllowIP(ip) {
			return true
		}
		if exactAllow[lower] {
			return false
		}
		return isBlockedIP(ip)
	}
	switch lower {
	case "localhost", "localhost.localdomain":
		if exactAllow[lower] {
			return false
		}
		return true
	}
	// Host is a domain name. Resolve it to prevent DNS rebinding.
	// If resolution fails, block by default (fail-closed).
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return true
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return true
		}
	}
	return false
}
