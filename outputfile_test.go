package crewai

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTaskOutputFile_EmptyRejected(t *testing.T) {
	for _, p := range []string{"", "   ", ".", "  .  "} {
		err := writeTaskOutputFile(p, "", []byte("x"))
		if !errors.Is(err, ErrOutputPathRejected) {
			t.Fatalf("path %q: got %v, want ErrOutputPathRejected", p, err)
		}
	}
}

func TestWriteTaskOutputFile_CleanAndMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "..", "out.txt")
	if err := writeTaskOutputFile(target, "", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "out.txt")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read cleaned path: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}
	st, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", st.Mode().Perm())
	}
}

func TestWriteTaskOutputFile_JailAllowsInside(t *testing.T) {
	jail := t.TempDir()
	path := filepath.Join(jail, "report.md")
	if err := writeTaskOutputFile(path, jail, []byte("ok")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "ok" {
		t.Fatalf("read: %v %q", err, data)
	}
}

func TestWriteTaskOutputFile_JailRejectsEscape(t *testing.T) {
	jail := t.TempDir()
	escape := filepath.Join(jail, "..", "secret.txt")
	err := writeTaskOutputFile(escape, jail, []byte("nope"))
	if !errors.Is(err, ErrOutputPathRejected) {
		t.Fatalf("got %v, want ErrOutputPathRejected", err)
	}
	out := filepath.Join(t.TempDir(), "outside.txt")
	err = writeTaskOutputFile(out, jail, []byte("nope"))
	if !errors.Is(err, ErrOutputPathRejected) {
		t.Fatalf("outside abs: got %v", err)
	}
}

func TestWriteTaskOutputFile_JailSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	jail := filepath.Join(root, "jail")
	if err := os.Mkdir(jail, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jail, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	target := filepath.Join(link, "evil.txt")
	err := writeTaskOutputFile(target, jail, []byte("pwn"))
	if !errors.Is(err, ErrOutputPathRejected) {
		t.Fatalf("symlink escape: got %v, want rejected", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); err == nil {
		t.Fatal("symlink escape wrote outside jail")
	}
}

func TestWriteTaskOutputFile_JailMissingJailFailsClosed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-jail")
	err := writeTaskOutputFile(filepath.Join(missing, "a.txt"), missing, []byte("x"))
	if !errors.Is(err, ErrOutputPathRejected) {
		t.Fatalf("got %v, want ErrOutputPathRejected", err)
	}
}

func TestWriteTaskOutputFile_NewFileInsideJail(t *testing.T) {
	jail := t.TempDir()
	path := filepath.Join(jail, "nested", "new.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeTaskOutputFile(path, jail, []byte("fresh")); err != nil {
		t.Fatal(err)
	}
}

func TestSetOutputWithJail_TaskAndCrewPrecedence(t *testing.T) {
	crewJail := t.TempDir()
	taskJail := t.TempDir()
	tk := &Task{OutputFile: filepath.Join(taskJail, "t.txt"), OutputDir: taskJail}
	if err := tk.setOutputWithJail("body", taskJail); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(taskJail, "t.txt")); err != nil {
		t.Fatal(err)
	}
	tk2 := &Task{OutputFile: filepath.Join(crewJail, "c.txt")}
	if err := tk2.setOutputWithJail("crew", crewJail); err != nil {
		t.Fatal(err)
	}
	tk3 := &Task{OutputFile: filepath.Join(t.TempDir(), "x.txt")}
	err := tk3.setOutputWithJail("x", crewJail)
	if !errors.Is(err, ErrOutputPathRejected) {
		t.Fatalf("got %v", err)
	}
	if tk3.Output() != "x" {
		t.Fatalf("output should still be stored, got %q", tk3.Output())
	}
}

func TestSetOutput_NoFile(t *testing.T) {
	tk := &Task{}
	if err := tk.setOutput("only-mem"); err != nil {
		t.Fatal(err)
	}
	if tk.Output() != "only-mem" {
		t.Fatal(tk.Output())
	}
}

func TestWithOutputDir_Fluent(t *testing.T) {
	tk := NewTask("d", "e", nil).WithOutputDir("/tmp/out")
	if tk.OutputDir != "/tmp/out" {
		t.Fatal(tk.OutputDir)
	}
}

func TestResolveOutputPathInJail_ExactJailOK(t *testing.T) {
	jail := t.TempDir()
	if _, err := resolveOutputPathInJail(jail, jail); err != nil {
		t.Fatalf("exact jail path should resolve: %v", err)
	}
}

func TestWriteTaskOutputFile_ErrorMessageSentinel(t *testing.T) {
	jail := t.TempDir()
	err := writeTaskOutputFile("/etc/passwd", jail, []byte("x"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrOutputPathRejected) {
		t.Fatal(err)
	}
	if !strings.Contains(err.Error(), "output path rejected") {
		t.Fatalf("error: %v", err)
	}
}
