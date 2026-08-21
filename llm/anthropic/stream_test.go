package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
)

func TestCallStream_SSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		payload := "" +
			"event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
			"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"!\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := New("claude-test", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), []crewai.Message{
		crewai.SystemMessage("sys"),
		crewai.UserMessage("u"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hi!" {
		t.Fatalf("got %q", out)
	}
}

func TestCallStream_MissingKey(t *testing.T) {
	c := New("m", WithAPIKey(""))
	c.apiKey = ""
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("got %v", err)
	}
}

func TestCallStream_ErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: error\ndata: {\"error\":{\"type\":\"api\",\"message\":\"boom\"}}\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
}

func TestCallStream_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("got %v", err)
	}
}

func TestCallStream_EOFWithText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil || out != "x" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_BadDeltaJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {bad\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
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
	c := New("m", WithAPIKey("k"), WithBaseURL("http://["))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestCallStream_PingAndEmptyDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		payload := "" +
			": ping\n\n" +
			"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"\"}}\n\n" +
			"event: content_block_start\ndata: {}\n\n" +
			"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err != nil || out != "ok" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_ErrorEventEmptyMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: error\ndata: {}\n\n"))
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "stream error") {
		t.Fatalf("%v", err)
	}
}

func TestCallStream_DialError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(url), WithHTTPClient(&http.Client{}))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if err == nil {
		t.Fatal("expected dial err")
	}
}

func TestCallStream_BodyCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		pad := strings.Repeat("x", 64*1024)
		written := 0
		for written <= crewai.MaxProviderResponseBytes {
			line := ":" + pad + "\n"
			n, err := w.Write([]byte(line))
			written += n
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if !errors.Is(err, crewai.ErrStreamResponseTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestCallStream_ConnectionClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"a\"}}\n\n"))
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
			}
		}
	}))
	defer srv.Close()
	c := New("m", WithAPIKey("k"), WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	out, err := crewai.CollectStream(context.Background(), c.CallStream(context.Background(), nil))
	if out != "a" && err == nil {
		t.Fatalf("%q %v", out, err)
	}
}
