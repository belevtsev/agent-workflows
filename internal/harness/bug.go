package harness

import "time"

// TicketSource identifies the immutable ticket text saved as the task request.
type TicketSource struct {
	Kind      string            `json:"kind"`
	URL       string            `json:"url"`
	Title     string            `json:"title"`
	SHA256    string            `json:"sha256"`
	UpdatedAt string            `json:"updated_at,omitempty"`
	Check     *PullRequestCheck `json:"check,omitempty"`
}

// PullRequestCheck identifies the observed failed check and its local reproduction.
type PullRequestCheck struct {
	Name           string `json:"name"`
	Head           string `json:"head"`
	LocalCheck     string `json:"local_check"`
	ApprovalSHA256 string `json:"approval_sha256,omitempty"`
	Supplied       bool   `json:"supplied,omitempty"` // The caller supplied the check metadata; GitHub was not asked.
}

type BugDelivery struct {
	Source       TicketSource     `json:"source"`
	Reproduction *Proof           `json:"reproduction,omitempty"`
	Receipt      *DeliveryReceipt `json:"receipt,omitempty"`
}

// FailureEvidence identifies the failing candidate, including an uncommitted reproducer.
type FailureEvidence struct {
	CheckRun
	Tree       string `json:"tree"`
	Head       string `json:"head,omitempty"`
	ExitStatus int    `json:"exit_status"`
}

// DeliveryReceipt is a historical observation about one local commit.
type DeliveryReceipt struct {
	Task             string              `json:"task"`
	Source           TicketSource        `json:"source"`
	Commit           string              `json:"commit"`
	Tree             string              `json:"tree"`
	Target           string              `json:"target"`
	Expect           string              `json:"expected_behavior"`
	Argv             []string            `json:"argv"`
	Reproduction     FailureEvidence     `json:"reproduction"`
	BeforeCommit     CheckRun            `json:"before_commit"`
	BeforeCommitTree string              `json:"before_commit_tree,omitempty"`
	OnCommit         CheckRun            `json:"on_commit"`
	RepositoryChecks string              `json:"repository_checks"`
	RepositoryRuns   map[string]CheckRun `json:"repository_runs,omitempty"`
	At               time.Time           `json:"verified_at"`
}

// A bug task first needs an executable failure and a recorded correction.
func (t *Task) bugAction() string {
	if t.Bug != nil && t.Bug.Receipt != nil {
		return "done"
	}
	if t.NeedsReproduction() {
		return "verify"
	}
	if t.Bug != nil && t.Latest("repair") == nil {
		return "repair"
	}
	return ""
}

func (t *Task) NeedsReproduction() bool { return t.Bug != nil && t.Bug.Reproduction == nil }
