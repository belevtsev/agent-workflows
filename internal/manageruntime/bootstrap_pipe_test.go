package manageruntime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// PS1-PS3: the documented installer form `curl -fsSL .../install.sh | sh -s -- <args>`
// delivers the launcher bytes on the shell's standard input. Run the checkout
// launcher that way under every available shell and require the staged child's
// exact exit status, its output, and full cleanup.
func TestBootstrapPipedScriptForwardsChildExitStatus(t *testing.T) {
	for _, shell := range []string{"/bin/sh", "/bin/dash", "/bin/bash"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := os.Stat(shell); err != nil {
				t.Skipf("shell %s is not available: %v", shell, err)
			}
			bootstrapPipedScript(t, shell)
		})
	}
}

func bootstrapPipedScript(t *testing.T, shell string) {
	t.Helper()
	launcher, _ := bootstrapFixture(t)
	root := filepath.Dir(filepath.Dir(launcher))
	// CW_MOCK_STREAMS and CW_MOCK_HOLD stay at the fixture default "0": the
	// child must not read stdin, which may be the remainder of the script pipe.
	t.Setenv("CW_MOCK_CHILD_EXIT", "37")
	script, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	args := []string{"install"}
	// Reuse the launcher's exact Dir, Env, SysProcAttr, Cancel and WaitDelay,
	// but run the shell with the script arriving on its standard input.
	command := bootstrapCommand(ctx, launcher, args...)
	command.Path = shell
	command.Args = append([]string{shell, "-s", "--"}, args...)
	command.Stdin = bytes.NewReader(script)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()

	// PS1: normal exit with exactly the child's status, never a signal.
	if err == nil {
		t.Fatalf("%s -s: piped launcher exited 0, want child status 37; stdout=%q stderr=%q", shell, stdout.String(), stderr.String())
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("%s -s: piped launcher did not run to an exit status: %v; stdout=%q stderr=%q", shell, err, stdout.String(), stderr.String())
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("%s -s: unexpected wait status type %T: %v; stdout=%q stderr=%q", shell, exit.Sys(), err, stdout.String(), stderr.String())
	}
	if status.Signaled() {
		t.Fatalf("%s -s: piped launcher killed by signal %v (%v), want normal exit 37; stdout=%q stderr=%q", shell, status.Signal(), err, stdout.String(), stderr.String())
	}
	if !status.Exited() || status.ExitStatus() != 37 {
		t.Fatalf("%s -s: piped launcher exit status %d (%v), want 37; stdout=%q stderr=%q", shell, status.ExitStatus(), err, stdout.String(), stderr.String())
	}

	// PS2: the staged child actually executed and produced its output.
	if strings.TrimSpace(stdout.String()) != `{"forwarded":true}` {
		t.Fatalf("%s -s: child output missing or changed (%v); stdout=%q stderr=%q", shell, err, stdout.String(), stderr.String())
	}
	steps, readErr := os.ReadFile(filepath.Join(root, "child-calls"))
	if readErr != nil || !strings.Contains(string(steps), "executed\n") {
		t.Fatalf("%s -s: child-calls lacks executed: %q %v; exit=%v stdout=%q stderr=%q", shell, steps, readErr, err, stdout.String(), stderr.String())
	}

	// PS3: no private stage remains and Git/Go were not invoked.
	assertBootstrapCleaned(t, launcher)
}
