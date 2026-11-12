package taskflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Amendment is one accepted, append-only addition of checks to a started
// substantial task, tied to a merge from its base branch.
type Amendment struct {
	Checks        []Check             `json:"checks"`
	MergeCommit   string              `json:"merge_commit"`
	BaseRef       string              `json:"base_ref"`
	BaseCommit    string              `json:"base_commit"`
	Reason        string              `json:"reason"`
	CoordinatorID string              `json:"coordinator_id"`
	Snapshot      projectctx.Snapshot `json:"snapshot"`
	At            time.Time           `json:"at"`
}

// AmendmentInput is the JSON that cw task add-checks reads.
type AmendmentInput struct {
	MergeCommit string  `json:"merge_commit"`
	BaseRef     string  `json:"base_ref"`
	Reason      string  `json:"reason"`
	Checks      []Check `json:"checks"`
}

// mergeHint ends the refusals that a base merge without a merge commit
// usually causes.
const mergeHint = "; if the base merge fast-forwarded or was rebased, check that the branch is not pushed, undo it with git reset --keep ORIG_HEAD (never --hard) and merge again with git merge --no-ff %s; an unfinished merge is concluded with git merge --continue"

// errNoGoRule refuses a task whose evidence rules do not include the Go
// package-test rule (rows B3 and C2).
var errNoGoRule = errors.New("checks can be added only to a task whose evidence rules include the Go package-test rule")

// amendmentRequest is an AmendmentInput that passed every check that needs no
// record (rows A1-A7), with timeouts defaulted.
type amendmentRequest struct {
	mergeCommit, baseRef, reason string
	checks                       []Check
}

// parseAmendmentInput applies rows A1-A7 in order. Each row about input
// checks examines every check before the next row runs.
func parseAmendmentInput(input AmendmentInput) (amendmentRequest, error) {
	if len(input.Checks) == 0 {
		return amendmentRequest{}, errors.New("add at least one check")
	}
	if !boundedText(input.Reason) {
		return amendmentRequest{}, errors.New("an amendment reason of 1 to 16384 bytes is required")
	}
	if !revisionShape(input.MergeCommit) {
		return amendmentRequest{}, errors.New("merge_commit must name a Git revision")
	}
	if !revisionShape(input.BaseRef) {
		return amendmentRequest{}, errors.New("base_ref must name a Git revision")
	}
	checks := make([]Check, 0, len(input.Checks))
	for _, check := range input.Checks {
		if check.TimeoutSeconds == 0 {
			check.TimeoutSeconds = 300
		}
		checks = append(checks, Check{ID: check.ID, Argv: slices.Clone(check.Argv), TimeoutSeconds: check.TimeoutSeconds})
	}
	seen := map[string]bool{}
	for _, check := range checks {
		if !identifier.MatchString(check.ID) || seen[check.ID] {
			return amendmentRequest{}, errors.New("added check IDs must be distinct identifiers")
		}
		seen[check.ID] = true
	}
	for _, check := range checks {
		if check.TimeoutSeconds < 1 || check.TimeoutSeconds > 3600 {
			return amendmentRequest{}, errors.New("check timeout must be 1..3600 seconds")
		}
	}
	for _, check := range checks {
		if err := validateCommand(check.Argv); err != nil {
			return amendmentRequest{}, fmt.Errorf("check %s: %w", check.ID, err)
		}
	}
	return amendmentRequest{mergeCommit: input.MergeCommit, baseRef: input.BaseRef, reason: input.Reason, checks: checks}, nil
}

// AddChecks appends one amendment to the task at path, or refuses and writes
// nothing. On success it returns the new record.
func AddChecks(ctx context.Context, path string, input AmendmentInput) (Record, error) {
	request, err := parseAmendmentInput(input)
	if err != nil {
		return Record{}, err
	}
	return update(path, func(root *os.Root, record *Record) error {
		if record.Plan.Kind != "substantial" {
			return errors.New("checks can be added only to a substantial task")
		}
		if !slices.ContainsFunc(record.Packs.Applied, func(pack PackBinding) bool {
			return pack.EvidenceRule == langpack.GoPackageTestV2
		}) {
			return errNoGoRule
		}
		existing := allChecks(*record)
		for _, check := range request.checks {
			if _, found := findCheck(*record, check.ID); found {
				return fmt.Errorf("check %s is already a planned or added check; checks cannot be replaced, removed or narrowed", check.ID)
			}
		}
		if len(existing)+len(request.checks) > 256 {
			return errors.New("planned and added checks would exceed 256")
		}
		for _, check := range request.checks {
			if !langpack.CreditsGoTest(check.Argv) {
				return fmt.Errorf("check %s is not a direct go test that the Go evidence rule credits", check.ID)
			}
		}
		snapshot, files, err := projectctx.CaptureManifest(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		source, err := evidenceSource(root, *record, snapshot, files)
		if err != nil {
			return err
		}
		if snapshot.Head == "" {
			return errors.New("adding checks needs a Git repository with a committed HEAD")
		}
		facts, err := projectctx.InspectMerge(ctx, snapshot.Root, projectctx.MergeQuery{Commit: request.mergeCommit, BaseRef: request.baseRef, Head: snapshot.Head, BaselineHead: record.Baseline.Head})
		if err != nil {
			return fmt.Errorf("inspect merge commit: %w", err)
		}
		if err := admitMerge(facts, request); err != nil {
			return err
		}
		relative, err := selectedRelative(snapshot)
		if err != nil {
			return err
		}
		merged := []string{}
		for _, name := range facts.Changed {
			if name, ok := relative(name); ok {
				merged = append(merged, name)
			}
		}
		prior := [][]string{}
		for _, check := range existing {
			prior = append(prior, check.Argv)
		}
		added := [][]string{}
		for _, check := range request.checks {
			added = append(added, check.Argv)
		}
		for i, work := range langpack.AddedGoCoverage(source, merged, prior, added) {
			if len(work.Packages) == 0 && len(work.ModuleFiles) == 0 {
				return fmt.Errorf("check %s covers no package or module file that the merge changed, that is still changed since the baseline, and that no earlier check covers", request.checks[i].ID)
			}
		}
		after, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		if !equalSnapshot(snapshot, after) {
			return errors.New("source changed while adding checks")
		}
		record.Amendments = append(record.Amendments, Amendment{Checks: request.checks, MergeCommit: facts.Commit, BaseRef: request.baseRef, BaseCommit: facts.BaseCommit, Reason: request.reason, CoordinatorID: effectiveCoordinator(record.Plan), Snapshot: snapshot, At: time.Now().UTC()})
		record.Verification = nil
		record.EvidenceRevision++
		return nil
	})
}

// admitMerge applies rows C5-C11 to what Git reported.
func admitMerge(facts projectctx.MergeFacts, request amendmentRequest) error {
	switch {
	case facts.Commit == "":
		return fmt.Errorf("merge_commit %s does not name a commit", request.mergeCommit)
	case facts.BaseCommit == "":
		return fmt.Errorf("base_ref %s does not name a commit", request.baseRef)
	case len(facts.Parents) < 2:
		return fmt.Errorf("commit %s is not a merge commit"+mergeHint, facts.Commit, request.baseRef)
	case !facts.FromHead:
		return fmt.Errorf("merge commit %s is not reachable from HEAD", facts.Commit)
	case facts.FromBaseline:
		return fmt.Errorf("merge commit %s is reachable from the task baseline HEAD", facts.Commit)
	case facts.FromBase:
		return fmt.Errorf("merge commit %s is already reachable from base_ref %s"+mergeHint, facts.Commit, request.baseRef, request.baseRef)
	case !facts.ParentFromBase:
		return fmt.Errorf("merge commit %s has no parent after the first that is reachable from base_ref %s", facts.Commit, request.baseRef)
	}
	return nil
}

// effectiveCoordinator is the plan's coordinator, or /root when it names none.
func effectiveCoordinator(plan Plan) string {
	if plan.CoordinatorID == "" {
		return "/root"
	}
	return plan.CoordinatorID
}

// allChecks returns the plan checks, then each amendment's checks, in order.
func allChecks(record Record) []Check {
	checks := slices.Clone(record.Plan.Checks)
	for _, amendment := range record.Amendments {
		checks = append(checks, amendment.Checks...)
	}
	return checks
}

// findCheck returns the plan or added check with id.
func findCheck(record Record, id string) (Check, bool) {
	for _, check := range allChecks(record) {
		if check.ID == id {
			return check, true
		}
	}
	return Check{}, false
}

// revisionShape reports whether value may name a Git revision: 1 to 256
// bytes, not starting with -, without whitespace or control characters.
func revisionShape(value string) bool {
	return value != "" && len(value) <= 256 && !strings.HasPrefix(value, "-") && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r < 0x20 || r == 0x7f })
}

// fullCommitID reports whether value is a full lowercase SHA-1 or SHA-256
// object ID.
func fullCommitID(value string) bool {
	return (len(value) == 40 || len(value) == 64) && !strings.ContainsFunc(value, func(r rune) bool { return !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') })
}

// validateAmendments checks the shape of stored amendments. It never
// re-applies the check admission policy, the credit rule or the merge
// conditions, so stored history keeps loading under later rules.
func validateAmendments(record Record) error {
	invalid := errors.New("invalid stored check amendment")
	ids := map[string]bool{}
	for _, check := range record.Plan.Checks {
		ids[check.ID] = true
	}
	total := len(record.Plan.Checks)
	for _, amendment := range record.Amendments {
		total += len(amendment.Checks)
		if len(amendment.Checks) == 0 || len(amendment.Checks) > 256 || total > 256 {
			return invalid
		}
		for _, check := range amendment.Checks {
			if !identifier.MatchString(check.ID) || ids[check.ID] || check.TimeoutSeconds < 1 || check.TimeoutSeconds > 3600 || validateCommandShape(check.Argv) != nil {
				return invalid
			}
			ids[check.ID] = true
		}
		snapshot := amendment.Snapshot
		if !fullCommitID(amendment.MergeCommit) || !fullCommitID(amendment.BaseCommit) || !revisionShape(amendment.BaseRef) || !boundedText(amendment.Reason) || amendment.CoordinatorID != effectiveCoordinator(record.Plan) {
			return invalid
		}
		if snapshot.Root != record.Baseline.Root || snapshot.SelectedPath != record.Baseline.SelectedPath || snapshot.Head == "" || amendment.At.Before(record.CreatedAt) || record.UpdatedAt.Before(amendment.At) {
			return invalid
		}
	}
	return nil
}

// validateAmendmentTransition requires current to keep every amendment of
// previous unchanged and in order, and to add at most one.
func validateAmendmentTransition(previous, current Record) error {
	appendOnly := errors.New("check amendments are append-only")
	if len(current.Amendments) < len(previous.Amendments) || len(current.Amendments) > len(previous.Amendments)+1 {
		return appendOnly
	}
	for i, amendment := range previous.Amendments {
		if !sameEncoded(amendment, current.Amendments[i]) {
			return appendOnly
		}
	}
	return nil
}
