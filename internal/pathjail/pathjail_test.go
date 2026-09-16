package pathjail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve_InsideAndEscape(t *testing.T) {
	jail := t.TempDir()
	inside := filepath.Join(jail, "ok.txt")
	got, err := Resolve(inside, jail)
	if err != nil {
		t.Fatal(err)
	}
	if got != inside && !strings.HasPrefix(got, jail) {
		t.Fatalf("got %q jail %q", got, jail)
	}

	if _, err := Resolve(filepath.Join(jail, "..", "secret"), jail); err == nil {
		t.Fatal("expected escape reject")
	}
	if _, err := Resolve("/etc/passwd", jail); err == nil {
		t.Fatal("expected abs escape reject")
	}
	if _, err := Resolve("", jail); err == nil {
		t.Fatal("empty path")
	}
	if _, err := Resolve(inside, ""); err == nil {
		t.Fatal("empty jail")
	}
	if _, err := Resolve(inside, filepath.Join(t.TempDir(), "missing-jail")); err == nil {
		t.Fatal("missing jail")
	}
}

func TestResolve_ExactJail(t *testing.T) {
	jail := t.TempDir()
	if _, err := Resolve(jail, jail); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_SymlinkEscape(t *testing.T) {
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
		t.Skipf("symlink: %v", err)
	}
	if _, err := Resolve(filepath.Join(link, "evil.txt"), jail); err == nil {
		t.Fatal("symlink escape")
	}
}

func TestEvalExisting_MissingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "no", "such", "file.txt")
	got, err := EvalExisting(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "file.txt") {
		t.Fatalf("got=%q", got)
	}
}

func TestEvalExisting_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := EvalExisting(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != p && !strings.Contains(got, "f.txt") {
		t.Fatalf("got=%q", got)
	}
}

func TestResolve_DotAndBrokenJail(t *testing.T) {
	if _, err := Resolve(".", t.TempDir()); err == nil {
		t.Fatal("dot path")
	}
	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	if err := os.Symlink(filepath.Join(root, "missing-target"), broken); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if _, err := Resolve(filepath.Join(broken, "x"), broken); err == nil {
		t.Fatal("broken jail")
	}
}

func TestResolve_WhitespacePath(t *testing.T) {
	jail := t.TempDir()
	if _, err := Resolve("   ", jail); err == nil {
		t.Fatal("whitespace path")
	}
}

func TestEvalExisting_BrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if _, err := EvalExisting(link); err == nil {
		t.Fatal("broken symlink")
	}
}

func TestEvalExisting_BrokenParent(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "missing"), parent); err != nil {
		t.Skipf("symlink: %v", err)
	}
	child := filepath.Join(parent, "child.txt")
	if _, err := EvalExisting(child); err == nil {
		t.Fatal("broken parent")
	}
}

func TestResolve_DeletedCwdAbs(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("x.txt", "jail"); err == nil {
		t.Fatal("abs after deleted cwd")
	}
}
