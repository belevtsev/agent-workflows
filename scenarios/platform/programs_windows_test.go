//go:build windows

package platform_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Requirement "Programs on Windows", scenario "A program is found without its
// extension".
func TestProgramIsFoundWithoutItsExtension(t *testing.T) {
	t.Parallel()
	repo := portable(t, "[[check]]\nid = \"hello\"\nrun = [\"hello\"]\n")
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "hello-ran")
	script := "@echo off\r\necho ran> \"" + marker + "\"\r\n"
	if err := os.WriteFile(filepath.Join(bin, "hello.cmd"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	prependPath(repo, bin)

	result := repo.CW("run")

	if !exists(marker) {
		t.Errorf("hello.cmd was not executed\ncw run (exit status %d):\n%s", result.Code, result.Output())
	}
}

// printArgs is a program that writes its arguments, one per line, to a file.
const printArgs = `package main

import (
	"os"
	"strings"
)

func main() {
	var b strings.Builder
	for _, arg := range os.Args[1:] {
		b.WriteString(arg + "\n")
	}
	_ = os.WriteFile(%s, []byte(b.String()), 0o644)
}
`

// leaveMarker is a program that creates a file to show that it was started.
const leaveMarker = `package main

import "os"

func main() { _ = os.WriteFile(%s, []byte("started\n"), 0o644) }
`

// Requirement "Programs on Windows", scenario "Arguments reach a batch file as
// they were written".
func TestArgumentsReachABatchFileAsTheyWereWritten(t *testing.T) {
	t.Parallel()
	repo := portable(t, "[[check]]\nid = \"hello\"\nrun = [\"hello\", \"a b\", \"c&d\"]\n")
	bin := t.TempDir()
	results := t.TempDir()
	received := filepath.Join(results, "received")
	dStarted := filepath.Join(results, "d-started")
	printer := buildProgram(t, bin, "printargs", fmt.Sprintf(printArgs, strconv.Quote(received)))
	buildProgram(t, bin, "d", fmt.Sprintf(leaveMarker, strconv.Quote(dStarted)))
	script := "@echo off\r\n\"" + printer + "\" %*\r\n"
	if err := os.WriteFile(filepath.Join(bin, "hello.cmd"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	prependPath(repo, bin)

	result := repo.CW("run")

	if exists(dStarted) {
		t.Errorf("a program named d was started: an argument was read as a second command")
	}
	data, err := os.ReadFile(received)
	switch {
	case err == nil:
		if got := string(data); got != "a b\nc&d\n" {
			t.Errorf("the program received %q, want exactly the two arguments \"a b\" and \"c&d\"", got)
		}
	case result.Code == 1:
		if !strings.Contains(result.Output(), "c&d") {
			t.Errorf("cw run exited 1 but does not name c&d")
		}
	default:
		t.Errorf("the program received nothing (%v) and cw run exit status is %d, want the arguments or exit status 1", err, result.Code)
	}
	if t.Failed() {
		t.Logf("cw run (exit status %d):\n%s", result.Code, result.Output())
	}
}
