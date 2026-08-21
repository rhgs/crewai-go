package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
)

func TestCallStream_NDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["stream"] != true {
			t.Errorf("stream %#v", req["stream"])
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Hel"},"done":false}` + "\n"))
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"lo"},"done":true}` + "\n"))
	}))
	defer srv.Close()

	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), []crewai.Message{
		crewai.UserMessage("hi"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello" {
		t.Fatalf("got %q", out)
	}
}

func TestCallStream_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("got %v", err)
	}
}

func TestCallStream_APIErrorField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"nope","done":true}` + "\n"))
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("got %v", err)
	}
}

func TestCallStream_CloudMissingKey(t *testing.T) {
	c := NewCloud("m")
	c.apiKey = ""
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("got %v", err)
	}
}

func TestCallStream_EOFWithoutDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"x"},"done":false}` + "\n"))
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil || out != "x" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{bad\n"))
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestCountingLimitReader(t *testing.T) {
	r := &countingLimitReader{r: strings.NewReader(strings.Repeat("a", 100)), limit: 50}
	buf := make([]byte, 40)
	if _, err := r.Read(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(buf); !errors.Is(err, crewai.ErrStreamResponseTooLarge) {
		t.Fatalf("%v", err)
	}
	if _, err := r.Read(buf); !errors.Is(err, crewai.ErrStreamResponseTooLarge) {
		t.Fatalf("%v", err)
	}
}

func TestCallStream_InvalidURL(t *testing.T) {
	c := New("m", WithBaseURL("http://["))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestCallStream_EmptyLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\n\n"))
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done":true}` + "\n"))
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil || out != "ok" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_WithAPIKeyHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"k"},"done":true}` + "\n"))
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithAPIKey("tok"), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth %q", gotAuth)
	}
}

func TestCallStream_DialError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := New("m", WithBaseURL(url), WithHTTPClient(&http.Client{}))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected dial err")
	}
}

func TestCallStream_BodyTooLargeComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pad := strings.Repeat("x", 64*1024)
		written := 0
		for written <= crewai.MaxProviderResponseBytes {
			// NDJSON-ish junk lines (invalid JSON will error first OR body cap)
			n, err := w.Write([]byte(pad + "\n"))
			written += n
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	// either too large or decode error — must fail
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestCallStream_ConnectionClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"a"},"done":false}` + "\n"))
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
			}
		}
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if out != "a" && err == nil {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_CancelMid(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"a"},"done":false}` + "\n"))
		if flusher != nil {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	ch := c.CallStream(ctx, nil)
	<-started
	cancel()
	_, _ = crewai.CollectStream(context.Background(), ch)
}

func TestCallStream_PartialLineThenClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"a"},"done":false}` + "\n"))
		_, _ = w.Write([]byte(`{"message":`)) // incomplete JSON line
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
			}
		}
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	// got some text then error or done-from-eof
	if out != "a" && err == nil {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_DoneTrueEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":false}` + "\n"))
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":""},"done":true}` + "\n"))
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil || out != "hi" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_BodyCapValidJSON(t *testing.T) {
	// Valid tiny NDJSON lines until body byte cap trips on Read.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		line := []byte(`{"message":{"role":"assistant","content":""},"done":false}` + "\n")
		// pad with spaces inside a comment? NDJSON has no comments — use large empty content chunks under scanner limit.
		pad := strings.Repeat(" ", 32*1024)
		line = []byte(`{"message":{"role":"assistant","content":"` + pad + `"},"done":false}` + "\n")
		written := 0
		for written <= crewai.MaxProviderResponseBytes {
			n, err := w.Write(line)
			written += n
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	c := New("m", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if !errors.Is(err, crewai.ErrStreamResponseTooLarge) {
		t.Fatalf("got %v", err)
	}
}
