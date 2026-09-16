// Offline HTTPFetch demo: httptest server + deny-by-default allowlist.
//
// Run:
//
//	go run ./examples/tools_http
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/rhgs/crewai-go/tools"
)

func main() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true,"path":"`+r.URL.Path+`"}`)
	}))
	defer srv.Close()
	host := hostOnly(srv.URL)

	denied := tools.NewHTTPFetch()
	if _, err := denied.Call(context.Background(), srv.URL); err != nil {
		fmt.Println("deny-by-default:", err)
	}

	fetch := tools.NewHTTPFetch(tools.WithHTTPAllowlist(host))
	body, err := fetch.Call(context.Background(), srv.URL+"/status")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("allowlisted GET:", body)

	if _, err := fetch.Call(context.Background(), "http://169.254.169.254/latest"); err != nil {
		fmt.Println("metadata blocked:", err)
	}
}

func hostOnly(raw string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
