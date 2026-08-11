package crewai

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactError_Nil(t *testing.T) {
	if redactError(nil) != nil {
		t.Fatal("redactError(nil) must be nil")
	}
}

func TestRedactError_APIKey_OpenAI(t *testing.T) {
	// Real-world shape: OpenAI auth error includes a partial API key.
	in := errors.New("openai: 401 Incorrect API key provided: sk-proj-AbCdEfGhIjKlMnOpQrStUvWxYz1234567890")
	got := redactError(in).Error()

	if strings.Contains(got, "sk-proj-AbCdEfGhIjKlMn") {
		t.Fatalf("expected API key redacted, got: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker, got: %q", got)
	}
	// Keep the start/end: "sk-p…[REDACTED]…1234" shape.
	if !strings.Contains(got, "sk-p") {
		t.Fatalf("expected start prefix preserved, got: %q", got)
	}
}

func TestRedactError_APIKey_Anthropic(t *testing.T) {
	in := errors.New("anthropic: 401 x-anthropic-api-key sk-ant-api03-abcdefghij1234567890xyzABCDEFGHIJ")
	got := redactError(in).Error()
	if strings.Contains(got, "abcdefghij1234567890") {
		t.Fatalf("anthropic key should be redacted, got: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction marker, got: %q", got)
	}
}

func TestRedactError_BearerToken(t *testing.T) {
	in := errors.New("authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789")
	got := redactError(in).Error()
	if strings.Contains(got, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("bearer token should be redacted, got: %q", got)
	}
	if !strings.Contains(got, "Bearer [REDACTED]") {
		t.Fatalf("expected 'Bearer [REDACTED]', got: %q", got)
	}
}

func TestRedactError_QueryStringKey(t *testing.T) {
	in := errors.New(`upstream: GET https://api.example.com/v1/search?api_key=sksecret1234567890abcdef&q=test`)
	got := redactError(in).Error()
	if strings.Contains(got, "sksecret1234567890") {
		t.Fatalf("query string api_key value should be redacted, got: %q", got)
	}
	if !strings.Contains(got, "api_key=[REDACTED]") {
		t.Fatalf("expected 'api_key=[REDACTED]' in output, got: %q", got)
	}
}

func TestRedactError_NewlineTruncation(t *testing.T) {
	in := errors.New("first line with secret sk-1234567890abcdefghij\nsecond line that should be ignored xyz\nthird")
	got := redactError(in).Error()
	if strings.Contains(got, "second line") {
		t.Fatalf("multiline error should be truncated to first line, got: %q", got)
	}
	if !strings.Contains(got, "first line") {
		t.Fatalf("first line should be preserved, got: %q", got)
	}
}

func TestRedactError_LengthCap(t *testing.T) {
	// Long text with no long alphanumeric tokens (so redaction doesn't
	// shorten the message first) — exercises pure length cap.
	long := strings.Repeat("x y ", 100)
	in := errors.New("prefix " + strings.ReplaceAll(long, "x", strings.Repeat("a", 1)))
	got := redactError(in).Error()
	if len(got) > 200 {
		t.Fatalf("redacted error should be length-capped, got len=%d value=%q", len(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("truncated error should end with '...', got: %q", got)
	}
}

func TestRedactError_ShortTextIntact(t *testing.T) {
	in := errors.New("short and friendly")
	got := redactError(in).Error()
	if got != "short and friendly" {
		t.Fatalf("short text should not be modified, got: %q", got)
	}
}

func TestRedactError_PreservesFirstAndLastFour(t *testing.T) {
	in := errors.New("token=ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	got := redactError(in).Error()
	// Token length is 36 ≥ 24, so first-4 + "…[REDACTED]…" + last-4.
	if !strings.Contains(got, "ABCD") || !strings.Contains(got, "6789") {
		t.Fatalf("expected first and last 4 chars preserved, got: %q", got)
	}
	if !strings.Contains(got, "…[REDACTED]…") {
		t.Fatalf("expected partial-visibility marker, got: %q", got)
	}
	// The middle should NOT leak.
	if strings.Contains(got, "MNOPQRSTUVWXYZ") {
		t.Fatalf("middle of token must not leak, got: %q", got)
	}
}

func TestRedactString_Empty(t *testing.T) {
	if redactString("") != "" {
		t.Fatal("empty input should stay empty")
	}
}

func TestRedactString_MultipleSecrets(t *testing.T) {
	in := "sk-proj-AbCdEfGhIjKlMnOpQrStUvWxYz0123 and sk-ant-api03-LmnOpQrStUvWxYz9876543210abc"
	got := redactString(in)
	if strings.Count(got, "[REDACTED]") != 2 {
		t.Fatalf("expected two redactions, got: %q", got)
	}
}
