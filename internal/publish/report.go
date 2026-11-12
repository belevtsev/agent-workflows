package publish

import "github.com/belevtsev/agent-workflows/v3/internal/taskflow"

// Statuses.
const (
	StatusPublished = "published"
	StatusFailed    = "failed"
	StatusDenied    = "denied"
	StatusRefused   = "refused"
	StatusUnknown   = "unknown"
	StatusMismatch  = "mismatch"
	StatusCancelled = "cancelled"
)

// reasonStatus assigns every reason its status (contract section 5).
var reasonStatus = map[string]string{
	"git-missing": StatusFailed, "locked": StatusFailed, "task-unreadable": StatusFailed, "record-invalid": StatusFailed,
	"capture-failed": StatusFailed, "read-failed": StatusFailed, "record-write-failed": StatusFailed, "commit-failed": StatusFailed,

	"task-abandoned": StatusDenied, "task-not-verified": StatusDenied,
	"source-changed": StatusDenied, "open-record": StatusDenied,

	"not-a-repository": StatusRefused, "nothing-to-publish": StatusRefused, "path-not-verified": StatusRefused,
	"branch-mismatch": StatusRefused, "head-moved": StatusRefused, "hunk-scope": StatusRefused,
	"content-conversion": StatusRefused, "git-locked": StatusRefused, "remote-unknown": StatusRefused,
	"remote-split": StatusRefused, "remote-moved": StatusRefused, "not-fast-forward": StatusRefused,
	"gh-missing": StatusRefused, "repo-mismatch": StatusRefused, "pr-exists": StatusRefused,
	"pr-missing": StatusRefused, "pr-ambiguous": StatusRefused,

	"commit-unknown": StatusUnknown, "push-unknown": StatusUnknown, "pr-unknown": StatusUnknown,

	"commit-mismatch": StatusMismatch, "push-mismatch": StatusMismatch, "pr-mismatch": StatusMismatch,

	"cancelled": StatusCancelled,
}

// statusExit is the exit status of each status, on cw's exit table in
// internal/cli/exit.go; a test there checks that the two agree.
var statusExit = map[string]int{
	StatusPublished: 0, StatusFailed: 1, StatusDenied: 3, StatusRefused: 4,
	StatusUnknown: 6, StatusMismatch: 7, StatusCancelled: 130,
}

// Report is what cw publish prints (contract section 6).
type Report struct {
	Status          string                    `json:"status"`
	Reason          string                    `json:"reason"`
	Detail          string                    `json:"detail"`
	Task            string                    `json:"task"`
	TaskID          string                    `json:"task_id"`
	Worktree        string                    `json:"worktree"`
	SourceSHA256    string                    `json:"source_sha256"`
	Paths           []string                  `json:"paths"`
	CommittedPaths  []string                  `json:"committed_paths"`
	IntentToAdd     []string                  `json:"intent_to_add"`
	LocksLeft       []string                  `json:"locks_left"`
	ExpectHead      string                    `json:"expect_head"`
	Branch          string                    `json:"branch"`
	Remote          string                    `json:"remote"`
	ExpectRemote    string                    `json:"expect_remote"`
	OpenRecords     []OpenRecord              `json:"open_records"`
	Steps           taskflow.PublicationSteps `json:"steps"`
	PreviousOutcome string                    `json:"previous_outcome"`
	Record          string                    `json:"record"`
}

// OpenRecord is another task record that gates the repository.
type OpenRecord struct {
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Problem   string `json:"problem"`
}

// Summary is printed instead of the Report with --summary.
type Summary struct {
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	TaskID    string `json:"task_id"`
	Commit    string `json:"commit"`
	RemoteRef string `json:"remote_ref"`
	PRNumber  int    `json:"pr_number"`
	PRURL     string `json:"pr_url"`
	Record    string `json:"record"`
}

// ExitCode is the process exit status for the report's status; a status
// outside section 5 cannot be a success, so it is 1.
func (r Report) ExitCode() int {
	if code, ok := statusExit[r.Status]; ok {
		return code
	}
	return 1
}

// Summary condenses the report.
func (r Report) Summary() Summary {
	return Summary{Status: r.Status, Reason: r.Reason, TaskID: r.TaskID, Commit: r.Steps.Commit.Commit,
		RemoteRef: r.Steps.Push.RemoteRef, PRNumber: r.Steps.PR.Number, PRURL: r.Steps.PR.URL, Record: r.Record}
}
