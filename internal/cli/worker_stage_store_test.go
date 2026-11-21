package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

func TestLedgerWaitsForItsHolderAndNamesABusyOne(t *testing.T) {
	t.Parallel()
	s := &session{repo: &gitx.Repo{GitDir: t.TempDir()}}
	held, err := openStageLedger(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openStageLedger(t.Context(), s); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("a held ledger opened without waiting: %v", err)
	}
	// A holder that ends inside the wait is waited for.
	go func() {
		time.Sleep(100 * time.Millisecond)
		held.close()
	}()
	waited, err := openStageLedgerWait(t.Context(), s, 30*time.Second)
	if err != nil {
		t.Fatalf("the ledger did not wait for its holder: %v", err)
	}
	defer waited.close()
	// A holder that outlasts the wait is named, with what to do.
	_, err = openStageLedgerWait(t.Context(), s, 50*time.Millisecond)
	if ExitCode(err) != ExitFailure || !strings.Contains(err.Error(), "running assertion") || !strings.Contains(err.Error(), "Run this command again") {
		t.Fatalf("a busy ledger is not explained: %v", err)
	}
}
