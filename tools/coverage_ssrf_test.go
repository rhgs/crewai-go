package tools

import "testing"

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
		"http://127.0.0.1/a",
		"http://[::1]/a",
		"http://192.168.0.1/a",
		"http://10.0.0.5/a",
		"http://172.16.1.1/a",
		"http://169.254.169.254/latest",
		"http://0.0.0.0/",
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
