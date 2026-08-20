package tools

import (
	"net"
	"testing"
)

func TestIsBlockedURLAndExtractHost(t *testing.T) {
	if extractHost("://bad") != "" {
		t.Fatal("bad url host")
	}
	if extractHost("https://example.com:8443/x") != "example.com" {
		t.Fatal("host parse")
	}

	blocked := []string{
		"not a url",
		"ftp://example.com",
		"http://localhost/a",
		"http://localhost.localdomain/a",
		"http://127.0.0.1/a",
		"http://[::1]/a",
		"http://192.168.0.1/a",
		"http://10.0.0.5/a",
		"http://172.16.1.1/a",
		"http://169.254.169.254/latest",
		"http://0.0.0.0/",
		"http://[::]/",
		"http://metadata.google.internal/",
		"http://metadata/",
		"http://100.64.0.1/",            // CGNAT
		"http://224.0.0.1/",             // multicast
		"http://user:pass@example.com/", // userinfo
		"http:///",                      // empty host
	}
	for _, u := range blocked {
		if !isBlockedURL(u) {
			t.Errorf("expected blocked: %s", u)
		}
	}
	// public domain should resolve; if DNS fails in sandbox, fail-closed is ok.
	if isBlockedURL("https://example.com") {
		// environment without DNS: acceptable fail-closed
		t.Log("example.com blocked (fail-closed DNS) — ok in restricted env")
	}
}

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"100.64.0.1", true},
		{"100.127.255.255", true},
		{"100.63.255.255", false}, // just below CGNAT
		{"224.0.0.1", true},
		{"::1", true},
		{"::", true},
		{"2001:db8::1", false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if got := isBlockedIP(ip); got != tc.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", tc.ip, got, tc.blocked)
		}
	}
	if !isBlockedIP(nil) {
		t.Error("nil IP should be blocked")
	}
}
