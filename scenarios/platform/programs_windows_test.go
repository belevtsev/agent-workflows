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

// Requirement "Programs on Windows": an argument of plain characters reaches
// a batch file unchanged, and a program that is not a batch file gets every
// argument unchanged, also one that cw refuses for a batch file.
func TestPlainArgumentsReachABatchFileAndEveryArgumentReachesAProgram(t *testing.T) {
	t.Parallel()
	repo := portable(t, "[[check]]\nid = \"batch\"\nrun = [\"hello\", \"--name=value\", \"a.b/c:d\", \"x+y,z@w\", \"back\\\\slash-1_2\"]\n\n"+
		"[[check]]\nid = \"program\"\nrun = [\"printdirect\", \"a b\", \"c&d\", \"100%\", \"e^f\"]\n")
	bin := t.TempDir()
	results := t.TempDir()
	viaBatch := filepath.Join(results, "via-batch")
	direct := filepath.Join(results, "direct")
	printer := buildProgram(t, bin, "printargs", fmt.Sprintf(printArgs, strconv.Quote(viaBatch)))
	buildProgram(t, bin, "printdirect", fmt.Sprintf(printArgs, strconv.Quote(direct)))
	script := "@echo off\r\n\"" + printer + "\" %*\r\n"
	if err := os.WriteFile(filepath.Join(bin, "hello.cmd"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	prependPath(repo, bin)

	result := repo.CW("run", "batch", "program")

	for file, want := range map[string]string{
		viaBatch: "--name=value\na.b/c:d\nx+y,z@w\nback\\slash-1_2\n",
		direct:   "a b\nc&d\n100%\ne^f\n",
	} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("%s: the program was not run: %v", filepath.Base(file), err)
			continue
		}
		if got := string(data); got != want {
			t.Errorf("%s: the program received %q, want %q", filepath.Base(file), got, want)
		}
	}
	if result.Code != 0 {
		t.Errorf("cw run exit status %d, want 0", result.Code)
	}
	if t.Failed() {
		t.Logf("cw run batch program (exit status %d):\n%s", result.Code, result.Output())
	}
}

// Requirement "Programs on Windows", scenario "A batch file named from a
// subfolder".
func TestBatchFileRuleHoldsFromASubfolder(t *testing.T) {
	t.Parallel()
	repo := portable(t, "[[check]]\nid = \"batch\"\nrun = [\"tools\\\\check.bat\", \"a&b\"]\n")
	marker := filepath.Join(t.TempDir(), "check-ran")
	repo.Write("tools/check.bat", "@echo off\r\necho ran> \""+marker+"\"\r\n")
	repo.Write("sub/README.md", "# sub\n")

	result := repo.In("sub").CW("run", "batch")

	if exists(marker) {
		t.Errorf("tools\\check.bat was started")
	}
	if !strings.Contains(result.Output(), "was not started") || !strings.Contains(result.Output(), "is a batch file") || !strings.Contains(result.Output(), "a&b") {
		t.Errorf("the output does not say that the batch file was not started and name a&b")
	}
	if result.Code == 0 {
		t.Errorf("cw run exited 0, want a refusal")
	}
	if t.Failed() {
		t.Logf("cw run batch in sub (exit status %d):\n%s", result.Code, result.Output())
	}
}

// Requirement "Programs on Windows", scenario "A batch file of the same name
// in the repository".
func TestCheckIsStartedByThePathCWFound(t *testing.T) {
	t.Parallel()
	repo := portable(t, "[[check]]\nid = \"which\"\nrun = [\"check.bat\"]\n")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "check.bat"), []byte("@echo off\r\necho from path\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Write("check.bat", "@echo off\r\necho from root\r\n")
	prependPath(repo, bin)

	result := repo.CW("run", "which")

	if !strings.Contains(result.Output(), "from path") || strings.Contains(result.Output(), "from root") {
		t.Errorf("the check did not run check.bat of PATH alone\ncw run which (exit status %d):\n%s", result.Code, result.Output())
	}
}
