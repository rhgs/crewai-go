// Package pathjail resolves filesystem paths inside a directory jail.
// EvalSymlinks is applied to the jail and to the deepest existing ancestor
// of the target (so new files can still be created). Any resolution error
// fails closed.
package pathjail

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Resolve returns an absolute, symlink-resolved path that must equal jail
// or live under jail+separator. An empty jail is rejected.
func Resolve(path, jail string) (string, error) {
	jail = strings.TrimSpace(jail)
	if jail == "" {
		return "", fmt.Errorf("empty jail")
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return "", fmt.Errorf("empty path")
	}

	absJail, err := filepath.Abs(jail)
	if err != nil {
		return "", fmt.Errorf("jail abs: %v", err)
	}
	absJail, err = filepath.EvalSymlinks(absJail)
	if err != nil {
		return "", fmt.Errorf("jail symlinks: %v", err)
	}
	absJail = filepath.Clean(absJail)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("path abs: %v", err)
	}
	resolved, err := EvalExisting(absPath)
	if err != nil {
		return "", fmt.Errorf("path symlinks: %v", err)
	}
	resolved = filepath.Clean(resolved)

	sep := string(filepath.Separator)
	if resolved == absJail || strings.HasPrefix(resolved, absJail+sep) {
		return resolved, nil
	}
	return "", fmt.Errorf("%q escapes jail", path)
}

// EvalExisting resolves symlinks for path. If path does not exist yet, it
// resolves the deepest existing ancestor and rejoins the missing trailing
// components (so a new file can still be created inside the jail).
func EvalExisting(path string) (string, error) {
	if _, err := os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path)
	}
	var missing []string
	cur := path
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no existing ancestor for %s", path)
		}
		missing = append([]string{filepath.Base(cur)}, missing...)
		if _, err := os.Lstat(parent); err == nil {
			resolvedParent, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", err
			}
			return filepath.Join(append([]string{resolvedParent}, missing...)...), nil
		}
		cur = parent
	}
}
