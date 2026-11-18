package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

// Actor IDs alone identify session receipts. They do not partition the host's
// counters across tasks. Keep these receipts separate from task-attributed sums.
func actorSessionUsage(events []harness.Event, sessions []tokens.Session) []tokens.Session {
	actors := make(map[string]bool)
	for _, e := range events {
		if e.Actor != "" {
			actors[e.Actor] = true
		}
	}
	var receipts []tokens.Session
	for _, s := range sessions {
		if actors[s.Session] {
			receipts = append(receipts, s)
		}
	}
	return receipts
}

// Read a single saved task without creating a ledger or observing the Git tree.
func savedUsageTask(repo *gitx.Repo, id string) (*harness.Task, error) {
	if !taskName.MatchString(id) {
		return nil, nil
	}
	if _, err := os.Lstat(filepath.Join(repo.GitDir, "kitsu-stages")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	ledger, err := readOnlyStageLedger(repo.GitDir)
	if err != nil {
		return nil, err
	}
	defer ledger.close()
	var task harness.Task
	if err := ledger.read(harnessFile(id), &task); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if !task.Valid(id) {
		return nil, fmt.Errorf("invalid saved task %s", id)
	}
	return &task, nil
}

func printDailySessionUsage(cfg Config, id string, sessions []tokens.Session, advisers *adviserTokenUsage, json bool) error {
	if json {
		return printJSON(cfg, struct {
			Task          string             `json:"task"`
			Total         *int64             `json:"total"`
			Complete      bool               `json:"complete"`
			ActorSessions []tokens.Session   `json:"actor_session_usage,omitempty"`
			Advisers      *adviserTokenUsage `json:"advisers,omitempty"`
		}{Task: id, ActorSessions: sessions, Advisers: advisers})
	}
	fmt.Fprintf(cfg.Out, "%s: task token usage is unknown\n", id)
	for _, s := range sessions {
		fmt.Fprintf(cfg.Out, "%s %s: %s tokens for the whole session, which may include other work\n", decl.Plain(s.Host), decl.Plain(s.Session), atLeast(s.Entry))
	}
	printAdviserTokenUsage(cfg.Out, advisers)
	return nil
}
