package tools

import (
	"context"
	"testing"
)

func TestCalculator(t *testing.T) {
	calc := Calculator()
	cases := []struct {
		in   string
		want string
	}{
		{"2 + 2", "4"},
		{"2 + 2 * 3", "8"},
		{"(2 + 2) * 3", "12"},
		{"10 / 4", "2.5"},
		{"-5 + 3", "-2"},
		{"2.5 * 2", "5"},
	}
	for _, c := range cases {
		got, err := calc.Call(context.Background(), c.in)
		if err != nil {
			t.Errorf("Call(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Call(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCalculatorErrors(t *testing.T) {
	calc := Calculator()
	for _, in := range []string{"1 / 0", "2 +", "(1 + 2", "abc", ""} {
		if _, err := calc.Call(context.Background(), in); err == nil {
			t.Errorf("Call(%q) should fail", in)
		}
	}
}

func TestWordCount(t *testing.T) {
	wc := WordCount()
	got, err := wc.Call(context.Background(), "hello world go")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != "words=3 characters=14" {
		t.Errorf("WordCount = %q", got)
	}
}

func TestCurrentTime(t *testing.T) {
	ct := CurrentTime("2006")
	got, err := ct.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("CurrentTime = %q, expected a 4-digit year", got)
	}
}
