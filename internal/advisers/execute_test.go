package advisers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

const adviserHelperFlag = "--kitsu-adviser-test-helper"

type adviserInspection struct {
	Args      []string
	Input     string
	Directory string
	Marker    string
	Inherited string
	HasSecret bool
}

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == adviserHelperFlag {
		os.Exit(runAdviserHelper(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// The test binary is the adviser CLI; no shell or installed provider is needed.
func runAdviserHelper(args []string) int {
	if len(args) < 2 {
		return 90
	}
	switch args[0] {
	case "inspect":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			return 91
		}
		directory, err := os.Getwd()
		if err != nil {
			return 92
		}
		fmt.Fprint(os.Stderr, "fixture-only-diagnostic")
		err = json.NewEncoder(os.Stdout).Encode(adviserInspection{
			Args: args[1:], Input: string(input), Directory: directory,
			Marker: os.Getenv("KITSU_ADVISER_CALL"), Inherited: os.Getenv("KITSU_ADVISER_TEST_INHERITED"),
			HasSecret: os.Getenv("KITSU_ADVISER_TEST_SECRET") == "not-a-real-key",
		})
		if err != nil {
			return 93
		}
	case "answer", "answer-empty", "answer-invalid", "answer-large", "answer-directory", "answer-symlink":
		if len(args) != 3 {
			return 94
		}
		answer := []byte("answer from file\n")
		switch args[0] {
		case "answer-empty":
			answer = []byte{}
		case "answer-invalid":
			answer = []byte("prefix\xffsuffix")
		case "answer-large":
			answer = []byte(strings.Repeat("x", MaxAnswer+1))
		case "answer-directory":
			if err := os.Mkdir(args[2], 0o700); err != nil {
				return 95
			}
			fmt.Fprint(os.Stdout, "stdout is not an answer-file fallback")
			return 0
		case "answer-symlink":
			target := filepath.Join(filepath.Dir(args[2]), "symlink-target")
			if err := os.WriteFile(target, answer, 0o600); err != nil {
				return 96
			}
			if err := os.Symlink("symlink-target", args[2]); err != nil {
				return 97
			}
			fmt.Fprint(os.Stdout, "stdout is not an answer-file fallback")
			return 0
		}
		if err := os.WriteFile(args[2], answer, 0o600); err != nil {
			return 98
		}
		fmt.Fprint(os.Stdout, "stdout is not an answer-file fallback")
	case "answer-missing":
		fmt.Fprint(os.Stdout, "stdout is not an answer-file fallback")
	case "bytes":
		if len(args) != 3 {
			return 99
		}
		count, err := strconv.Atoi(args[2])
		if err != nil {
			return 100
		}
		fmt.Fprint(os.Stdout, strings.Repeat("x", count))
	case "unicode-overflow":
		fmt.Fprint(os.Stdout, strings.Repeat("x", MaxAnswer-1)+"界")
	case "empty":
		fmt.Fprint(os.Stdout, " \t\n")
	case "invalid":
		fmt.Fprint(os.Stdout, "valid prefix\xfftail")
	case "diagnostics":
		fmt.Fprint(os.Stderr, strings.Repeat("\xffa", MaxDiagnostic)+"diagnostic-tail")
		fmt.Fprint(os.Stdout, "answer")
	case "nonzero":
		if len(args) != 3 {
			return 101
		}
		count, err := os.OpenFile(args[2], os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			return 102
		}
		_, writeErr := count.WriteString("called\n")
		closeErr := count.Close()
		if writeErr != nil || closeErr != nil {
			return 103
		}
		fmt.Fprint(os.Stdout, "partial answer")
		fmt.Fprint(os.Stderr, "failure-only-diagnostic")
		return 23
	case "sleep":
		if len(args) != 3 {
			return 104
		}
		fmt.Fprint(os.Stdout, "answer before stop")
		if err := os.WriteFile(args[2], []byte("ready"), 0o600); err != nil {
			return 105
		}
		time.Sleep(30 * time.Second)
	default:
		return 106
	}
	return 0
}

func fakeAdviserDefinition(t *testing.T, mode string, args ...string) Definition {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Race-instrumented helper processes need no one-second exit delay.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	command := append([]string{binary, adviserHelperFlag, mode, "{model}"}, args...)
	return Definition{Model: "configured-model", Command: command, Timeout: 5 * time.Second}
}

func TestExecuteUsesLiteralArgvStdinDirectoryAndInheritedAuthentication(t *testing.T) {
	t.Setenv("KITSU_ADVISER_CALL", "old-value")
	t.Setenv("KITSU_ADVISER_TEST_INHERITED", "inherited value")
	t.Setenv("KITSU_ADVISER_TEST_SECRET", "not-a-real-key")
	root := filepath.Join(t.TempDir(), "repository with spaces")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	args := []string{"; touch injected", "$(touch substitution)", "`touch backtick`", "$HOME", "quoted ' \" value", ""}
	definition := fakeAdviserDefinition(t, "inspect", args...)
	definition.Model = "model ; $(touch model-substitution)"
	prompt := "Question with Unicode: Привіт\nand a literal $(no-command)\n"
	got := Execute(t.Context(), root, definition, prompt, "")
	if got.Outcome != "success" || got.ExitStatus == nil || *got.ExitStatus != 0 {
		t.Fatalf("execution = %#v", got)
	}
	var inspection adviserInspection
	if err := json.Unmarshal([]byte(got.Answer), &inspection); err != nil {
		t.Fatal(err)
	}
	wantArgs := append([]string{definition.Model}, args...)
	if !reflect.DeepEqual(inspection.Args, wantArgs) || inspection.Input != prompt {
		t.Fatalf("CLI saw args=%q input=%q; want args=%q input=%q", inspection.Args, inspection.Input, wantArgs, prompt)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	gotRoot, err := filepath.EvalSymlinks(inspection.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != wantRoot || inspection.Marker != "1" || inspection.Inherited != "inherited value" || !inspection.HasSecret {
		t.Fatalf("CLI environment and directory = %#v", inspection)
	}
	for _, name := range []string{"injected", "substitution", "backtick", "model-substitution"} {
		if _, err := os.Stat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("shell marker %q exists or cannot be inspected: %v", name, err)
		}
	}
	if got.Diagnostics != "fixture-only-diagnostic" {
		t.Fatalf("diagnostics = %q", got.Diagnostics)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "fixture-only-diagnostic") || strings.Contains(string(data), "not-a-real-key") {
		t.Fatal("execution JSON recorded diagnostics or an inherited secret")
	}
	if got.ActualModel != "unknown" || got.UsageKnown || got.StartedAt.IsZero() || got.EndedAt.Before(got.StartedAt) {
		t.Fatalf("execution metadata = %#v", got)
	}
}

func TestExecuteAnswerFileIsAuthoritativeAndBounded(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		outcome string
		answer  string
	}{
		{name: "file answer", mode: "answer", outcome: "success", answer: "answer from file\n"},
		{name: "missing file", mode: "answer-missing", outcome: "failed"},
		{name: "empty file", mode: "answer-empty", outcome: "failed"},
		{name: "invalid UTF-8", mode: "answer-invalid", outcome: "failed", answer: "prefix"},
		{name: "oversize file", mode: "answer-large", outcome: "overflow", answer: strings.Repeat("x", MaxAnswer)},
		{name: "directory", mode: "answer-directory", outcome: "failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			answer := filepath.Join(root, "answer")
			definition := fakeAdviserDefinition(t, test.mode, "{answer_file}")
			got := Execute(t.Context(), root, definition, "question", answer)
			if got.Outcome != test.outcome || got.Answer != test.answer {
				t.Fatalf("outcome=%q answer bytes=%d detail=%q, want %q with %d answer bytes",
					got.Outcome, len(got.Answer), got.Detail, test.outcome, len(test.answer))
			}
			if strings.Contains(got.Answer, "stdout") {
				t.Fatal("stdout was used when an answer file was configured")
			}
		})
	}
}

func TestExecuteRejectsExistingRelativeAndSymlinkAnswerFiles(t *testing.T) {
	root := t.TempDir()
	definition := fakeAdviserDefinition(t, "answer", "{answer_file}")
	path := filepath.Join(root, "existing")
	writeAdviserTestFile(t, path, "retain existing answer")
	for _, candidate := range []string{path, "relative-answer", filepath.Join(root, "missing-dir", "answer")} {
		got := Execute(t.Context(), root, definition, "question", candidate)
		if got.Outcome != "failed" || got.ExitStatus != nil {
			t.Fatalf("invalid output path %q ran a program: %#v", candidate, got)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "retain existing answer" {
		t.Fatalf("existing answer changed: %q, %v", data, err)
	}
	if err := os.Symlink("existing", filepath.Join(root, "link-probe")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	definition = fakeAdviserDefinition(t, "answer-symlink", "{answer_file}")
	got := Execute(t.Context(), root, definition, "question", filepath.Join(root, "symlink-answer"))
	if got.Outcome != "failed" || got.Answer != "" || got.ExitStatus == nil || *got.ExitStatus != 0 {
		t.Fatalf("symlink answer was not rejected: %#v", got)
	}
}

func TestExecuteHandlesEmptyInvalidAndOversizeStdout(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		extra   []string
		outcome string
		size    int
	}{
		{name: "empty", mode: "empty", outcome: "failed", size: 3},
		{name: "invalid UTF-8", mode: "invalid", outcome: "failed", size: len("valid prefix")},
		{name: "exact byte limit", mode: "bytes", extra: []string{strconv.Itoa(MaxAnswer)}, outcome: "success", size: MaxAnswer},
		{name: "overflow", mode: "bytes", extra: []string{strconv.Itoa(MaxAnswer + 1)}, outcome: "overflow", size: MaxAnswer},
		{name: "split UTF-8 rune", mode: "unicode-overflow", outcome: "overflow", size: MaxAnswer - 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := fakeAdviserDefinition(t, test.mode, test.extra...)
			got := Execute(t.Context(), t.TempDir(), definition, "question", "")
			if got.Outcome != test.outcome || len(got.Answer) != test.size || !utf8.ValidString(got.Answer) {
				t.Fatalf("outcome=%q answer bytes=%d valid UTF-8=%v detail=%q; want %q, %d, true",
					got.Outcome, len(got.Answer), utf8.ValidString(got.Answer), got.Detail, test.outcome, test.size)
			}
		})
	}
}

func TestExecuteKeepsBoundedDiagnosticsOutOfRecords(t *testing.T) {
	definition := fakeAdviserDefinition(t, "diagnostics")
	got := Execute(t.Context(), t.TempDir(), definition, "question", "")
	if got.Outcome != "success" || got.Answer != "answer" {
		t.Fatalf("execution = %#v", got)
	}
	if len(got.Diagnostics) > MaxDiagnostic || !utf8.ValidString(got.Diagnostics) || !strings.HasSuffix(got.Diagnostics, "diagnostic-tail") {
		t.Fatalf("diagnostics bytes=%d valid UTF-8=%v tail retained=%v",
			len(got.Diagnostics), utf8.ValidString(got.Diagnostics), strings.HasSuffix(got.Diagnostics, "diagnostic-tail"))
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "diagnostic-tail") {
		t.Fatal("diagnostics were included in a portable record")
	}
}

func TestExecuteReportsNonzeroWithoutRetryOrSubstitution(t *testing.T) {
	root := t.TempDir()
	count := filepath.Join(root, "invocations")
	definition := fakeAdviserDefinition(t, "nonzero", count)
	got := Execute(t.Context(), root, definition, "question", "")
	if got.Outcome != "failed" || got.ExitStatus == nil || *got.ExitStatus != 23 || got.Answer != "partial answer" {
		t.Fatalf("execution = %#v", got)
	}
	data, err := os.ReadFile(count)
	if err != nil || string(data) != "called\n" {
		t.Fatalf("invocations = %q, %v; want one call", data, err)
	}
	if strings.Contains(got.Detail, "failure-only-diagnostic") || got.ActualModel != "unknown" || got.UsageKnown {
		t.Fatalf("unverified metadata or diagnostics were recorded: %#v", got)
	}
}

// Windows job termination reports status 1; Unix signal termination has no
// ordinary exit status. These calls have started, unlike pre-start cancellation.
func assertStoppedExitStatus(t *testing.T, got Execution) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if got.ExitStatus == nil {
			t.Fatal("stopped Windows execution has no exit status, want 1")
		}
		if *got.ExitStatus != 1 {
			t.Fatalf("stopped Windows execution exit status = %d, want 1", *got.ExitStatus)
		}
		return
	}
	if got.ExitStatus != nil {
		t.Fatalf("stopped execution exit status = %d, want none after signal termination", *got.ExitStatus)
	}
}

func TestExecuteCancellationBeforeAndDuringRun(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(root, "ready")
	definition := fakeAdviserDefinition(t, "sleep", ready)
	before, cancelBefore := context.WithCancel(t.Context())
	cancelBefore()
	got := Execute(before, root, definition, "question", "")
	if got.Outcome != "cancelled" || got.ExitStatus != nil {
		t.Fatalf("pre-cancelled execution = %#v", got)
	}
	if _, err := os.Stat(ready); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled command started: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan Execution, 1)
	go func() { done <- Execute(ctx, root, definition, "question", "") }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-tick.C:
		case result := <-done:
			t.Fatalf("CLI exited before cancellation: %#v", result)
		case <-deadline.C:
			t.Fatal("CLI did not report readiness")
		}
	}
	cancel()
	select {
	case got = <-done:
		if got.Outcome != "cancelled" || got.Answer != "answer before stop" {
			t.Fatalf("cancelled execution = %#v", got)
		}
		assertStoppedExitStatus(t, got)
	case <-deadline.C:
		t.Fatal("cancelled CLI did not stop")
	}
}

func TestExecuteTimeoutAndInvalidInputs(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(root, "ready")
	definition := fakeAdviserDefinition(t, "sleep", ready)
	definition.Timeout = time.Second
	guard, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	got := Execute(guard, root, definition, "question", "")
	if got.Outcome != "timed_out" || got.Answer != "answer before stop" || guard.Err() != nil {
		t.Fatalf("timed-out execution = %#v, guard=%v", got, guard.Err())
	}
	assertStoppedExitStatus(t, got)
	for _, test := range []struct {
		name       string
		definition Definition
		prompt     string
	}{
		{name: "bad command", definition: Definition{Model: "model", Command: []string{"missing-placeholder"}, Timeout: time.Second}, prompt: "question"},
		{name: "bad timeout", definition: Definition{Model: "model", Command: definition.Command, Timeout: time.Millisecond}, prompt: "question"},
		{name: "bad model", definition: Definition{Model: "\xff", Command: definition.Command, Timeout: time.Second}, prompt: "question"},
		{name: "bad UTF-8", definition: definition, prompt: "\xff"},
		{name: "oversize prompt", definition: definition, prompt: strings.Repeat("x", MaxInput+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Execute(t.Context(), root, test.definition, test.prompt, "")
			if result.Outcome != "failed" || result.ExitStatus != nil {
				t.Fatalf("invalid call was executed: %#v", result)
			}
		})
	}
}

func TestExecuteResolvesRelativeExecutableAgainstRelativeRoot(t *testing.T) {
	definition := fakeAdviserDefinition(t, "bytes", "6")
	base := t.TempDir()
	t.Chdir(base)
	if err := os.Mkdir("repository", 0o700); err != nil {
		t.Fatal(err)
	}
	name := "adviser"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	destination := filepath.Join("repository", name)
	input, err := os.Open(definition.Command[0])
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy executable: %v, close: %v", copyErr, closeErr)
	}
	definition.Command[0] = "./" + name
	definition.Timeout = 0
	got := Execute(t.Context(), "repository", definition, "question", "")
	if got.Outcome != "success" || got.Answer != "xxxxxx" {
		t.Fatalf("relative execution = %#v", got)
	}
}

func TestExecuteDoesNotCallAMissingProgramAndCleanupCannotSucceed(t *testing.T) {
	definition := Definition{
		Model: "model", Command: []string{filepath.Join(t.TempDir(), "missing-program"), "{model}"},
		Timeout: time.Second,
	}
	got := Execute(t.Context(), t.TempDir(), definition, "question", "")
	if got.Outcome != "failed" || got.ExitStatus != nil || strings.Contains(got.Detail, definition.Command[0]) {
		t.Fatalf("missing program result = %#v", got)
	}
	// A cleanup error is difficult to force on the host; the mapping must fail closed.
	outcome, detail := executionOutcome(t.Context(), proc.Result{Code: 0, Cleanup: errors.New("still alive")}, []byte("answer"), false, nil)
	if outcome != "failed" || !strings.Contains(detail, "cleanup") {
		t.Fatalf("cleanup failure outcome=%q detail=%q", outcome, detail)
	}
}
