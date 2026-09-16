// Offline FileRead / FileWrite demo: tempdir jail, write opt-in.
//
// Run:
//
//	go run ./examples/tools_files
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rhgs/crewai-go/tools"
)

func main() {
	jail, err := os.MkdirTemp("", "crewai-files-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(jail)

	note := filepath.Join(jail, "note.txt")
	if err := os.WriteFile(note, []byte("hello from jail"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	read := tools.NewFileRead(jail)
	out, err := read.Call(context.Background(), note)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("read:", out)

	if _, err := read.Call(context.Background(), filepath.Join(jail, "..", "secret")); err != nil {
		fmt.Println("escape rejected:", err)
	}

	locked := tools.NewFileWrite(jail)
	if _, err := locked.Call(context.Background(), `{"path":"`+filepath.Join(jail, "x.txt")+`","content":"nope"}`); err != nil {
		fmt.Println("write off by default:", err)
	}

	write := tools.NewFileWrite(jail, tools.WithAllowWrite())
	msg, err := write.Call(context.Background(), `{"path":"`+filepath.Join(jail, "out.txt")+`","content":"saved"}`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(msg)
}
