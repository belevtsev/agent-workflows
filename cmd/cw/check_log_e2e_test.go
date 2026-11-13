package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A check that passes but whose output cw cannot write to its log is cw
// failing, not an answer: cw task run exits 1 (failure), not 5 (negative
// result), and the run is recorded as failed with log_write_failed. Oracle:
// the exit table in docs/operations.md#exit-statuses.
//
// The log write fails through a file-size limit that sh's ulimit sets on the
// cw process alone: the check prints 96 KiB, more than the limit of 64 blocks
// (32 or 64 KiB, by the shell's block size) and less than
// taskflow.MaxLogBytes (128 KiB), while each record revision stays far below
// the limit.
func TestTaskRunExitsFailureWhenTheCheckLogCannotBeWritten(t *testing.T) {
	t.Parallel()
	e := tle2eNewEnv(t, buildPlainCW(t))
	tle2eWrite(t, filepath.Join(e.repo, "big.txt"), strings.Repeat(strings.Repeat("x", 127)+"\n", 768))
	// The check is named ok because the shared start helper expects that ID.
	task, _ := e.start(t, `{"title":"t","kind":"small","requirements":[{"id":"r1","description":"d","check_ids":["ok"]}],`+
		`"checks":[{"id":"ok","argv":["cat","big.txt"],"timeout_seconds":60}]}`)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `ulimit -f 64 && exec "$0" "$@"`,
		e.bin, "task", "run", "--task", task, "--check", "ok", "--summary")
	cmd.Dir = e.work
	cmd.Env = []string{e.pathEnv(), "HOME=" + e.home}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run cw under a file-size limit: %v", err)
		}
		code = exitErr.ExitCode()
	}
	if code != 1 {
		t.Fatalf("task run with an unwritable log: exit %d, want 1 (failure)\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	run := tle2eDecodeOne(t, stdout.String())
	if status, failure := tle2eString(t, run, "status"), tle2eString(t, run, "failure"); status != "failed" || failure != "log_write_failed" {
		t.Fatalf("recorded run status %q failure %q, want failed and log_write_failed", status, failure)
	}
	var checkExit json.Number
	if err := json.Unmarshal(run["exit_code"], &checkExit); err != nil || checkExit.String() != "0" {
		t.Fatalf("precondition: the check itself did not pass: exit_code %s", run["exit_code"])
	}
}
