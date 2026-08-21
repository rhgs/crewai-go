package mock

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
)

func TestCallStream_SplitsResponses(t *testing.T) {
	m := New("abcdefgh") // 8 runes → 2 chunks of 4 + Done
	out, err := crewai.CollectStream(context.Background(), m.CallStream(context.Background(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if out != "abcdefgh" {
		t.Fatalf("%q", out)
	}
	if m.Calls() != 1 {
		t.Fatalf("calls %d", m.Calls())
	}
}

func TestCallStream_Scripted(t *testing.T) {
	m := New("ignored")
	m.StreamChunks = [][]crewai.StreamChunk{
		{{Delta: "A"}, {Delta: "B", Done: true}},
	}
	out, err := crewai.CollectStream(context.Background(), m.CallStream(context.Background(), nil))
	if err != nil || out != "AB" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCallStream_HandlerError(t *testing.T) {
	m := New()
	m.Handler = func(ctx context.Context, messages []crewai.Message) (string, error) {
		return "", errors.New("fail")
	}
	_, err := crewai.CollectStream(context.Background(), m.CallStream(context.Background(), nil))
	if err == nil || !strings.Contains(err.Error(), "fail") {
		t.Fatalf("%v", err)
	}
}

func TestCallStream_CustomRuneSize(t *testing.T) {
	m := New("abcdef")
	m.StreamChunkRunes = 2
	var parts []string
	for c := range m.CallStream(context.Background(), nil) {
		if c.Delta != "" {
			parts = append(parts, c.Delta)
		}
	}
	if strings.Join(parts, "") != "abcdef" {
		t.Fatalf("%v", parts)
	}
	if len(parts) != 3 {
		t.Fatalf("want 3 parts, got %v", parts)
	}
}

func TestSplitRunes_Empty(t *testing.T) {
	if splitRunes("", 4) != nil {
		t.Fatal("want nil")
	}
	if got := splitRunes("ab", 0); len(got) != 1 || got[0] != "ab" {
		t.Fatalf("%v", got)
	}
}
