package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
)

func TestFileRead_InsideAndEscape(t *testing.T) {
	jail := t.TempDir()
	path := filepath.Join(jail, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewFileRead(jail)
	if r.Name() != "file_read" || r.Description() == "" {
		t.Fatal("name")
	}
	out, err := r.Call(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" {
		t.Fatalf("%q", out)
	}
	if _, err := r.Call(context.Background(), filepath.Join(jail, "..", "secret")); err == nil {
		t.Fatal("escape")
	}
	if _, err := r.Call(context.Background(), "/etc/passwd"); err == nil {
		t.Fatal("abs escape")
	}
}

func TestFileRead_BinaryRejected(t *testing.T) {
	jail := t.TempDir()
	path := filepath.Join(jail, "bin")
	if err := os.WriteFile(path, []byte("a\x00b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileRead(jail).Call(context.Background(), path); err == nil {
		t.Fatal("binary")
	}
	out, err := NewFileRead(jail, WithAllowBinary()).Call(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(out), []byte{0}) {
		t.Fatal("want NUL")
	}
}

func TestFileRead_Cap(t *testing.T) {
	jail := t.TempDir()
	path := filepath.Join(jail, "big")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := NewFileRead(jail, WithFileMaxBytes(8)).Call(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 8 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestFileRead_EmptyFile(t *testing.T) {
	jail := t.TempDir()
	path := filepath.Join(jail, "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := NewFileRead(jail).Call(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("%q", out)
	}
}

func TestFileRead_Missing(t *testing.T) {
	jail := t.TempDir()
	if _, err := NewFileRead(jail).Call(context.Background(), filepath.Join(jail, "nope")); err == nil {
		t.Fatal("missing")
	}
}

func TestFileWrite_DisabledByDefault(t *testing.T) {
	jail := t.TempDir()
	w := NewFileWrite(jail)
	if w.Name() != "file_write" || w.Description() == "" {
		t.Fatal("name")
	}
	if _, err := w.Call(context.Background(), `{"path":"a.txt","content":"x"}`); err == nil {
		t.Fatal("write must be off")
	}
}

func TestFileWrite_AllowAndJail(t *testing.T) {
	jail := t.TempDir()
	w := NewFileWrite(jail, WithAllowWrite())
	target := filepath.Join(jail, "out.txt")
	out, err := w.Call(context.Background(), `{"path":"`+target+`","content":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "wrote") {
		t.Fatalf("%q", out)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "hi" {
		t.Fatalf("%v %q", err, data)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}

	if _, err := w.Call(context.Background(), `{"path":"/etc/passwd","content":"x"}`); err == nil {
		t.Fatal("escape")
	}
}

func TestFileWrite_BadJSONAndEmptyPath(t *testing.T) {
	jail := t.TempDir()
	w := NewFileWrite(jail, WithAllowWrite())
	if _, err := w.Call(context.Background(), "not-json"); err == nil {
		t.Fatal("json")
	}
	if _, err := w.Call(context.Background(), `{"path":"","content":"x"}`); err == nil {
		t.Fatal("empty path")
	}
}

func TestFileWrite_Cap(t *testing.T) {
	jail := t.TempDir()
	w := NewFileWrite(jail, WithAllowWrite(), WithFileMaxBytes(4))
	if _, err := w.Call(context.Background(), `{"path":"a","content":"12345"}`); err == nil {
		t.Fatal("cap")
	}
}

func TestApplyFileOpts_NonPositiveCap(t *testing.T) {
	o := applyFileOpts([]FileOption{func(o *fileOpts) { o.maxBytes = 0 }})
	if o.maxBytes != crewai.MaxToolOutputBytes {
		t.Fatalf("%d", o.maxBytes)
	}
}

func TestFileOpts_DefaultCap(t *testing.T) {
	r := NewFileRead(t.TempDir(), WithFileMaxBytes(0))
	if r.maxBytes != crewai.MaxToolOutputBytes {
		t.Fatalf("%d", r.maxBytes)
	}
}

func TestFileRead_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	jail := filepath.Join(root, "jail")
	if err := os.Mkdir(jail, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(outside, []byte("pwn"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jail, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if _, err := NewFileRead(jail).Call(context.Background(), link); err == nil {
		t.Fatal("symlink escape")
	}
}
