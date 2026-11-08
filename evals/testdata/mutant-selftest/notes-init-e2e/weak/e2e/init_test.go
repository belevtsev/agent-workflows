package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var notesBin string

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "notes-e2e-weak-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(tmp)
	moduleRoot, err := filepath.Abs("..")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	notesBin = filepath.Join(tmp, "notes")
	build := exec.Command("go", "build", "-o", notesBin, "./cmd/notes")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build ./cmd/notes: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// Deliberately weak: only checks that init exits 0 in an empty directory.
func TestInitExitsZero(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, notesBin, "init")
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + dir, "TMPDIR=" + dir, "TERM=dumb", "LC_ALL=C", "PATH=/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("notes init: %v\n%s", err, out)
	}
}
