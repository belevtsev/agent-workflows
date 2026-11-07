//go:build unix

package projectctx

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMetadataDoctorRejectsFIFOConfigWithoutOpeningIt(t *testing.T) {
	root, codex := t.TempDir(), t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(codex, "config.toml"), 0600); err != nil {
		t.Fatal(err)
	}
	// A blocking open of a FIFO for reading waits for a writer, and none ever
	// appears. Neither the doctor's Timeout nor its context can interrupt that
	// syscall, so a doctor that opens the path never returns at all. The
	// budgets only have to separate "returned" from "never returns": the
	// doctor gets the largest Timeout it accepts so a loaded runner can finish
	// project inspection, and the guard waits longer than that, capped so it
	// still reports before the test binary's own deadline.
	const doctorTimeout = time.Minute
	guard := doctorTimeout + 30*time.Second
	if deadline, ok := t.Deadline(); ok {
		guard = min(guard, time.Until(deadline)-5*time.Second)
	}
	type result struct {
		report DoctorReport
		err    error
	}
	done := make(chan result, 1)
	go func() {
		report, err := Doctor(t.Context(), DoctorOptions{Repo: root, Home: t.TempDir(), CodexHome: codex, Timeout: doctorTimeout, LookPath: func(string) (string, error) { return "", os.ErrNotExist }})
		done <- result{report, err}
	}()
	select {
	case got := <-done:
		// Only the pre-open rejection leaves the path unopened; a non-blocking
		// open that is rejected afterwards reports a different detail.
		want := Check{Status: "degraded", Detail: "Selected Codex config must be a regular file; non-regular paths were not opened", Source: "selected_codex_home"}
		if got.err != nil || got.report.Checks["codex_config"] != want {
			t.Fatalf("FIFO was not rejected before opening: %+v %v", got.report.Checks["codex_config"], got.err)
		}
	case <-time.After(guard):
		t.Fatalf("doctor did not return within %v: it blocked opening a non-regular configuration path", guard)
	}
}
