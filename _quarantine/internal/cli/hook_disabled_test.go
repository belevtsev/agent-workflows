package cli

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// TestHookPrePublishDisabledDoesNotReadInput: with enforce_verify and
// small_task_prompt both false the gate is off, so `cw hook pre-publish`
// returns before it reads stdin. Oracle: the hook's documented off switches
// (docs/hooks.md, Disabling the gate) and the hook's 20 second budget, which
// does not cover the stdin read: Execute returns nil with no output while
// stdin is still open, well inside the deadline.
func TestHookPrePublishDisabledDoesNotReadInput(t *testing.T) {
	h := newHKHarness(t)
	h.env[enforceVerifyOption] = "false"
	h.env[smallTaskPromptOption] = "false"
	stdin, writer := io.Pipe()
	// The writer stays open until the test ends, as a host that never closes
	// stdin would leave it.
	t.Cleanup(func() { writer.CloseWithError(errors.New("test ended")) })
	h.config.In = stdin
	h.out.Reset()

	done := make(chan error, 1)
	go func() { done <- Execute(t.Context(), []string{"hook", "pre-publish"}, h.config) }()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("hook pre-publish returned %v; it must always return nil", err)
		}
		hkExpectAllow(t, h.out.String())
	case <-ctx.Done():
		t.Fatal("hook pre-publish with the gate off was still waiting on an open stdin after 10s")
	}
}
