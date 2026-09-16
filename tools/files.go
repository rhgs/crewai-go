package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/internal/pathjail"
)

// FileRead reads a text file inside a directory jail (D-T3, D-T7).
// Binary (NUL) content is rejected unless WithAllowBinary is set.
type FileRead struct {
	root        string
	maxBytes    int
	allowBinary bool
}

// FileWrite writes a text file inside a directory jail. Writes are disabled
// until WithAllowWrite is set (D-T5).
type FileWrite struct {
	root       string
	maxBytes   int
	allowWrite bool
}

// FileOption configures FileRead / FileWrite.
type FileOption func(*fileOpts)

type fileOpts struct {
	maxBytes    int
	allowBinary bool
	allowWrite  bool
}

// WithAllowWrite enables FileWrite.Call. Without it every write is rejected.
func WithAllowWrite() FileOption {
	return func(o *fileOpts) { o.allowWrite = true }
}

// WithAllowBinary allows NUL bytes on FileRead (off by default, D-T7).
func WithAllowBinary() FileOption {
	return func(o *fileOpts) { o.allowBinary = true }
}

// WithFileMaxBytes caps read/write payload size. Zero keeps
// crewai.MaxToolOutputBytes (D-T6).
func WithFileMaxBytes(n int) FileOption {
	return func(o *fileOpts) {
		if n > 0 {
			o.maxBytes = n
		}
	}
}

func applyFileOpts(opts []FileOption) fileOpts {
	o := fileOpts{maxBytes: crewai.MaxToolOutputBytes}
	for _, fn := range opts {
		fn(&o)
	}
	if o.maxBytes <= 0 {
		o.maxBytes = crewai.MaxToolOutputBytes
	}
	return o
}

// NewFileRead builds a jailed file-read tool. root is required and is
// evaluated as the jail (must exist).
func NewFileRead(root string, opts ...FileOption) *FileRead {
	o := applyFileOpts(opts)
	return &FileRead{root: root, maxBytes: o.maxBytes, allowBinary: o.allowBinary}
}

// NewFileWrite builds a jailed file-write tool. Call still fails unless
// WithAllowWrite is passed.
func NewFileWrite(root string, opts ...FileOption) *FileWrite {
	o := applyFileOpts(opts)
	return &FileWrite{root: root, maxBytes: o.maxBytes, allowWrite: o.allowWrite}
}

// Name implements crewai.Tool.
func (t *FileRead) Name() string { return "file_read" }

// Description implements crewai.Tool.
func (t *FileRead) Description() string {
	return "Reads a UTF-8 text file inside a jailed directory. Input: the file path."
}

// Call implements crewai.Tool.
func (t *FileRead) Call(_ context.Context, input string) (string, error) {
	resolved, err := pathjail.Resolve(input, t.root)
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(t.maxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}
	if len(data) > t.maxBytes {
		data = data[:t.maxBytes]
	}
	if !t.allowBinary && bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("file_read: binary content rejected")
	}
	return string(data), nil
}

// Name implements crewai.Tool.
func (t *FileWrite) Name() string { return "file_write" }

// Description implements crewai.Tool.
func (t *FileWrite) Description() string {
	return `Writes a UTF-8 text file inside a jailed directory. Input: JSON object {"path":"...","content":"..."}.`
}

type fileWriteInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Call implements crewai.Tool.
func (t *FileWrite) Call(_ context.Context, input string) (string, error) {
	if !t.allowWrite {
		return "", fmt.Errorf("file_write: writes disabled (pass tools.WithAllowWrite)")
	}
	var in fileWriteInput
	if err := json.Unmarshal([]byte(strings.TrimSpace(input)), &in); err != nil {
		return "", fmt.Errorf("file_write: expected JSON {\"path\",\"content\"}: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("file_write: empty path")
	}
	if len(in.Content) > t.maxBytes {
		return "", fmt.Errorf("file_write: content exceeds cap (%d bytes)", t.maxBytes)
	}
	resolved, err := pathjail.Resolve(in.Path, t.root)
	if err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(in.Content), 0o600); err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}
	return "wrote " + resolved, nil
}

var _ crewai.Tool = (*FileRead)(nil)
var _ crewai.Tool = (*FileWrite)(nil)
