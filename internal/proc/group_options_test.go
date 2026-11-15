//go:build linux || darwin

package proc_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/proc"
	"github.com/belevtsev/gatecub/v3/internal/proc/proctest"
)

// Oracle: after the program exits 0, a member it left in its group is killed
// before Run returns. The program prints its pid, which is the group id.
func TestRunKillsWhatTheProgramLeftInItsGroup(t *testing.T) {
	const script = `/bin/sleep 60 </dev/null >/dev/null 2>&1 &
echo "$$"`
	got := proc.Run(t.Context(), proc.Spec{Path: "/bin/sh", Args: []string{"-c", script}, StdoutLimit: 64})
	pgid, err := strconv.Atoi(strings.TrimSpace(string(got.Stdout)))
	if err != nil || pgid <= 1 || got.Code != 0 || got.Err != nil || got.Cleanup != nil {
		t.Fatalf("output %q, Code=%d Err=%v Cleanup=%v, want the pid and exit 0", got.Stdout, got.Code, got.Err, got.Cleanup)
	}
	t.Cleanup(func() { _ = proc.KillGroup(pgid) })
	if live, err := proctest.GroupHasLiveMember(pgid); err != nil || live {
		t.Errorf("group %d has a live member = %v (%v), want none", pgid, live, err)
	}
}
