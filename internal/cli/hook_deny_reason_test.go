//go:build unix

package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

func TestHookDenyReasonNamesAtMostThreeTasksOnEveryHost(t *testing.T) {
	// Oracle: with four open records, the deny reason Claude Code gets names
	// the three newest by ID, and counts the oldest, pointing at
	// `cw task list`, which lists it. Codex gets the same reason followed by where its
	// options are, and git's pre-commit hook writes it on stderr followed by
	// where a git hook reads its options.
	repo := hkRepo(t)
	h := newHKHarness(t)
	store := hkEmptyStore(t)
	h.env["CW_TASK_STORE"] = store
	var records []taskflow.Record
	for i := range 4 {
		records = append(records, hkSubstantialRecord(t, repo, store, fmt.Sprintf("Part %d", i+1)))
	}
	oldest, newest := records[0], records[1:]

	got := h.prePublish(t, repo, hkCodexCommand)
	if _, reason := hkDecision(t, got); strings.Contains(reason, oldest.ID) || strings.Count(reason, " And 1 more open task (`cw task list` lists it);") != 1 {
		t.Fatalf("deny reason %q names the oldest record %s, or does not count it once with the command that lists it", reason, oldest.ID)
	}
	claude := hkExpectDenyOnly(t, got, newest, oldest)
	if codex := hkExpectDeny(t, h.runCodexPrePublish(t, hkCodexInput(t, repo)), newest...); codex != claude+hkCodexOptions {
		t.Fatalf("Codex deny reason\n%q\nwant Claude Code's\n%q\nfollowed by %q", codex, claude, hkCodexOptions)
	}
	h.hgExpect(t, "pre-commit", repo, ExitDenied, claude+hgOptions+"\n")
}
