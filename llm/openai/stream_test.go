package openai

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

func TestCallStream_SSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["stream"] != true {
			t.Errorf("stream flag %#v", req["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":null}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := New("gpt-test", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	ch := c.CallStream(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	out, err := crewai.CollectStream(context.Background(), ch)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello" {
		t.Fatalf("got %q", out)
	}
}

func TestCallStream_APIErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want status err, got %v", err)
	}
}

func TestCallStream_MissingKey(t *testing.T) {
	c := New("m", WithAPIKey(""))
	c.apiKey = ""
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallStream_MidStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"rate\"}}\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "rate") {
		t.Fatalf("got %v", err)
	}
}

func TestCallStream_BodyByteCap(t *testing.T) {
	// Emit one huge SSE data line exceeding MaxProviderResponseBytes.
	// Use a modest oversize to keep the test fast: patch via many chunks that
	// accumulate body size. 10MiB is heavy — write exactly limit+1 of padding
	// in one write after a small valid prefix using chunked-style body.
	// Simpler: flood body with non-data lines until cap trips.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Write junk comments larger than the cap.
		pad := strings.Repeat("x", 64*1024)
		// Prefix each as SSE comment lines.
		written := 0
		for written <= crewai.MaxProviderResponseBytes {
			line := ":" + pad + "\n"
			n, err := w.Write([]byte(line))
			written += n
			if err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if !errors.Is(err, crewai.ErrStreamResponseTooLarge) {
		t.Fatalf("want ErrStreamResponseTooLarge, got %v", err)
	}
}

func TestCallStream_DoneWithoutBracket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"))
		// EOF without [DONE]
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if out != "x" {
		t.Fatalf("%q", out)
	}
}

func TestCallStream_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {not-json\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestCallStream_Cancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"))
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	ch := c.CallStream(ctx, nil)
	<-started
	cancel()
	// Drain; should not hang.
	_, _ = crewai.CollectStream(context.Background(), ch)
}

func TestCallStream_EmptyChoicesAndComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": keep-alive\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"z\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil || out != "z" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCountingLimitReader(t *testing.T) {
	r := &countingLimitReader{r: strings.NewReader(strings.Repeat("a", 100)), limit: 50}
	buf := make([]byte, 40)
	if _, err := r.Read(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(buf); !errors.Is(err, crewai.ErrStreamResponseTooLarge) {
		t.Fatalf("want too large, got %v", err)
	}
	// subsequent reads keep the error
	if _, err := r.Read(buf); !errors.Is(err, crewai.ErrStreamResponseTooLarge) {
		t.Fatalf("sticky err %v", err)
	}
}

func TestCallStream_InvalidURL(t *testing.T) {
	c := New("m", WithAPIKey("k"), WithBaseURL("http://["))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestCallStream_EmptyContentDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil || out != "ok" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_NonDataLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: thread.started\n"))
		_, _ = w.Write([]byte("id: 1\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"z\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil || out != "z" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_ConnectionCloseMidStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"))
		flusher.Flush()
		// close without DONE — hijack and close
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	// may succeed with Done-from-EOF or incomplete/read error depending on timing
	if out != "a" && err == nil {
		t.Fatalf("unexpected clean empty: %q %v", out, err)
	}
}

func TestCallStream_DialError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // force dial error
	c := New("m", WithAPIKey("k"), WithBaseURL(url), WithHTTPClient(&http.Client{}))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected dial err")
	}
}
