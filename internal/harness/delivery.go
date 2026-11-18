package harness

import "time"

// CommitAttempt keeps the original authority and passing assertion even when
// Git advances the ref before Kitsu can save its final result.
type CommitAttempt struct {
	Before    Snapshot `json:"before_commit"`
	Assertion CheckRun `json:"before_commit_assertion"`
	Commit    string   `json:"commit,omitempty"`
	Tree      string   `json:"tree,omitempty"`
}

// Completion is an observation about a particular delivery. Its candidate
// stays fixed when the worktree moves, or a later publication recheck fails.
type Completion struct {
	Candidate        Snapshot  `json:"candidate"`
	Target           string    `json:"target,omitempty"`
	At               time.Time `json:"observed_at"`
	CommitEvidence   string    `json:"commit_evidence"`
	RepositoryChecks string    `json:"repository_checks"`
	Reviewed         bool      `json:"reviewed,omitempty"`
}

// CompletedDelivery also recognizes prior versions' completed operations.
// Older local commit events can identify the verified tree without recording
// a commit hash. Never substitute the current checkout's HEAD for that fact.
func (t *Task) CompletedDelivery() *Completion {
	if t.Completion != nil {
		return t.Completion
	}
	if t.Scope.Until != "deliver" {
		return nil
	}
	target := t.AuthorizedCommitTarget()
	if t.Scope.Publish != "" {
		if e := t.Latest("publish"); e != nil && e.Note == t.Scope.Publish && e.Candidate.Head != "" && (t.Scope.PRBase == "" || e.Link != "") {
			return &Completion{Candidate: e.Candidate, Target: target, At: e.At, CommitEvidence: "recorded_publication", RepositoryChecks: "recorded"}
		}
		return nil
	}
	if t.Bug != nil {
		if r := t.Bug.Receipt; r != nil {
			evidence := "commit_matches_verified_tree"
			if r.BeforeCommitTree != "" {
				evidence = "verified_existing_commit"
			}
			return &Completion{Candidate: Snapshot{Head: r.Commit, Tree: r.Tree, Index: r.Tree}, Target: r.Target, At: r.At, CommitEvidence: evidence, RepositoryChecks: r.RepositoryChecks}
		}
		return nil
	}
	if t.Scope.Commit {
		if e := t.recordedCommit(); e != nil {
			candidate := e.Candidate
			candidate.Head, candidate.MergeHeads = e.ObservedCommit, ""
			evidence := "recorded_commit"
			if candidate.Head != "" {
				evidence = "commit_matches_verified_tree"
			}
			return &Completion{Candidate: candidate, Target: target, At: e.At, CommitEvidence: evidence, RepositoryChecks: "recorded"}
		}
	}
	return nil
}

// AttemptedCommit returns the intended candidate and whether Git's result is
// recorded. Earlier versions sometimes recorded the tree and parents only.
func (t *Task) AttemptedCommit() (*CommitAttempt, bool) {
	if t.CommitAttempt != nil {
		return t.CommitAttempt, t.CommitAttempt.Commit != ""
	}
	if !t.Scope.Commit {
		return nil, false
	}
	if e := t.recordedCommit(); e != nil {
		return &CommitAttempt{Before: e.Candidate, Commit: e.ObservedCommit, Tree: e.Candidate.Tree}, true
	}
	return nil, false
}

func (t *Task) recordedCommit() *Event {
	if e := t.Latest("commit"); e != nil {
		return e
	}
	for i := len(t.Events) - 1; i >= 0; i-- {
		e := &t.Events[i]
		if e.Action == "test" && e.ObservedCommit != "" {
			return e
		}
	}
	return nil
}
