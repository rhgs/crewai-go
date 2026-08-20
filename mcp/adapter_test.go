package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
)

func TestToolAdapter_NameAndDescription(t *testing.T) {
	a := NewToolAdapter(New("http://x"), Tool{Name: "calc", Description: "computes"})
	if a.Name() != "calc" {
		t.Fatalf("Name: %s", a.Name())
	}
	if a.Description() != "computes" {
		t.Fatalf("Description: %s", a.Description())
	}
}

func TestToolAdapter_Schema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`)
	a := NewToolAdapter(New("http://x"), Tool{Name: "calc", InputSchema: schema})
	if got := a.Schema(); string(got) != string(schema) {
		t.Fatalf("Schema returned %s, want %s", got, schema)
	}
}

func TestToolAdapter_Schema_NilWhenEmpty(t *testing.T) {
	a := NewToolAdapter(New("http://x"), Tool{Name: "calc"})
	if got := a.Schema(); got != nil {
		t.Fatalf("expected nil schema, got %s", got)
	}
}

func TestToolAdapter_Call_HappyPath(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"42"}]}}`))
	})
	defer cleanup()
	a := NewToolAdapter(New(srv.URL), Tool{Name: "calc"})
	out, err := a.Call(context.Background(), `{"x":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "42" {
		t.Fatalf("got %q, want 42", out)
	}
}

func TestToolAdapter_ImplementsCrewaiTool(t *testing.T) {
	var _ crewai.Tool = (*ToolAdapter)(nil)
	var _ crewai.SchemaProvider = (*ToolAdapter)(nil)
}

func TestToolAdapter_AdapterSatisfiesCrewaiTool(t *testing.T) {
	a := &ToolAdapter{
		client: New("http://x"),
		tool:   Tool{Name: "n", Description: "d"},
	}
	// Verify interface satisfaction via direct method calls.
	if a.Name() != "n" || a.Description() != "d" {
		t.Fatal("adapter methods inconsistent with tool descriptor")
	}
}

func TestToolAdapter_Call_IsErrorReturnsTextNotError(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"db down"}],"isError":true}}`))
	})
	defer cleanup()

	a := NewToolAdapter(New(srv.URL), Tool{Name: "lookup"})
	out, err := a.Call(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("isError must not be a Go error; got %v", err)
	}
	if !strings.HasPrefix(out, "[tool error]") {
		t.Fatalf("expected error prefix, got %q", out)
	}
	if !strings.Contains(out, "db down") {
		t.Fatalf("expected underlying text to be preserved, got %q", out)
	}
}

func TestToolAdapter_Call_NoTextBlocks(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"empty_content",
			`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`,
			"tool returned no text content",
		},
		{
			"only_image_block",
			`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"image","text":""}]}}`,
			"tool returned no text content",
		},
		{
			"isError_no_text",
			`{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":true}}`,
			"tool reported an error without text content",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			})
			defer cleanup()
			a := NewToolAdapter(New(srv.URL), Tool{Name: "x"})
			out, err := a.Call(context.Background(), "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out != tc.want {
				t.Fatalf("got %q, want %q", out, tc.want)
			}
		})
	}
}

func TestToolAdapter_Call_ProtocolErrorPropagates(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()
	a := NewToolAdapter(New(srv.URL), Tool{Name: "x"})
	if _, err := a.Call(context.Background(), ""); err == nil {
		t.Fatal("HTTP 500 must propagate as Go error")
	}
}

func TestToolAdapter_Call_ConcatenatesMultipleTextBlocks(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"a"},{"type":"text","text":"b"},{"type":"image","text":""},{"type":"text","text":"c"}]}}`))
	})
	defer cleanup()
	a := NewToolAdapter(New(srv.URL), Tool{Name: "x"})
	out, err := a.Call(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "a\nb\nc" {
		t.Fatalf("got %q", out)
	}
}

func TestFilterTools_EmptyInputs(t *testing.T) {
	if got := FilterTools(nil, map[string]struct{}{"a": {}}); got != nil {
		t.Fatalf("nil tools: %v", got)
	}
	if got := FilterTools([]Tool{{Name: "a"}}, nil); got != nil {
		t.Fatalf("nil allow: %v", got)
	}
	if got := FilterTools([]Tool{{Name: "a"}}, map[string]struct{}{}); got != nil {
		t.Fatalf("empty allow: %v", got)
	}
}

func TestFilterTools_KeepsAllowedInOrder(t *testing.T) {
	in := []Tool{
		{Name: "z"},
		{Name: "a"},
		{Name: "b"},
		{Name: ""},
		{Name: "a"},
	}
	allow := map[string]struct{}{"a": {}, "b": {}}
	got := FilterTools(in, allow)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(got), got)
	}
	if got[0].Name != "a" || got[1].Name != "b" || got[2].Name != "a" {
		t.Fatalf("order/names: %+v", got)
	}
}

func TestFilterTools_DropsUnknown(t *testing.T) {
	in := []Tool{{Name: "keep"}, {Name: "drop"}}
	got := FilterTools(in, map[string]struct{}{"keep": {}})
	if len(got) != 1 || got[0].Name != "keep" {
		t.Fatalf("got %+v", got)
	}
}

func TestWithDescriptionLimit_NoOpWhenZeroOrNegative(t *testing.T) {
	raw := "hello\x00world"
	for _, n := range []int{0, -1} {
		a := NewToolAdapter(New("http://x"), Tool{Name: "t", Description: raw}, WithDescriptionLimit(n))
		if a.Description() != raw {
			t.Fatalf("n=%d: got %q, want unchanged", n, a.Description())
		}
	}
}

func TestWithDescriptionLimit_StripsControlsAndTruncates(t *testing.T) {
	raw := "abcdefghij\x00\x07"
	a := NewToolAdapter(New("http://x"), Tool{Name: "t", Description: raw}, WithDescriptionLimit(5))
	got := a.Description()
	if got != "abcde" {
		t.Fatalf("got %q, want abcde", got)
	}
}

func TestWithDescriptionLimit_UnicodeRunes(t *testing.T) {
	raw := "你好世界" // 4 runes
	a := NewToolAdapter(New("http://x"), Tool{Name: "t", Description: raw}, WithDescriptionLimit(2))
	got := a.Description()
	if got != "你好" {
		t.Fatalf("got %q, want 你好", got)
	}
	a2 := NewToolAdapter(New("http://x"), Tool{Name: "t", Description: raw}, WithDescriptionLimit(10))
	if a2.Description() != raw {
		t.Fatalf("under limit: got %q", a2.Description())
	}
}

func TestWithDescriptionLimit_KeepsTab(t *testing.T) {
	raw := "a\tb\nc" // newline stripped, tab kept
	a := NewToolAdapter(New("http://x"), Tool{Name: "t", Description: raw}, WithDescriptionLimit(10))
	got := a.Description()
	if got != "a\tbc" {
		t.Fatalf("got %q, want a\\tbc", got)
	}
}

func TestNewToolAdapter_NilOptionIgnored(t *testing.T) {
	a := NewToolAdapter(New("http://x"), Tool{Name: "t", Description: "d"}, nil, WithDescriptionLimit(1))
	if a.descLimit != 1 {
		t.Fatalf("descLimit=%d", a.descLimit)
	}
	if a.Description() != "d" {
		t.Fatalf("got %q", a.Description())
	}
}

func TestNewToolAdapter_NoOptionsUnchanged(t *testing.T) {
	raw := "x\x01y"
	a := NewToolAdapter(New("http://x"), Tool{Name: "t", Description: raw})
	if a.Description() != raw {
		t.Fatalf("default must not strip: got %q", a.Description())
	}
}
