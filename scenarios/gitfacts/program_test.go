package gitfacts_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// writeProgram writes a file that a test will run. While the file is open
// for writing, no other test of this process may start a program: its child
// would inherit the open file, and the system refuses to run a file that
// anyone still has open for writing ("text file busy").
func writeProgram(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	syscall.ForkLock.RLock()
	err := os.WriteFile(path, []byte(content), 0o755) //nolint:gosec // the program must be executable
	syscall.ForkLock.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // the program must be executable
		t.Fatal(err)
	}
}
