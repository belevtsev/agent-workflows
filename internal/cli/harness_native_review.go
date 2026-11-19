package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

var nativeSessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type nativeReviewReport struct {
	Task      string   `json:"task"`
	ReviewID  string   `json:"review_id"`
	Candidate string   `json:"candidate"`
	Decision  string   `json:"decision"`
	Note      string   `json:"note"`
	Findings  []string `json:"findings"`
}

type nativeReviewObservation struct {
	Model    string                `json:"observed_model,omitempty"`
	Effort   string                `json:"observed_effort,omitempty"`
	Started  []string              `json:"started"`
	Source   *harness.NativeReview `json:"source,omitempty"`
	Result   *nativeReviewReport   `json:"result,omitempty"`
	Conflict bool                  `json:"conflict,omitempty"`
}

func addNativeReviewInput(input map[string]any, task *harness.Task, candidate harness.Snapshot, assignment string) {
	if task.Claim.ReviewID == "" {
		return
	}
	input["native_review_result"] = nativeReviewReport{Task: task.ID, ReviewID: task.Claim.ReviewID, Candidate: contextpack.Key(candidate), Findings: []string{}}
	input["assignment"] = assignment + " Finish with only the native_review_result JSON populated with a note and a decision of approve or findings. Include the unresolved findings in the findings array."
}

func (r nativeReviewReport) valid() bool {
	decision := (r.Decision == "approve" && len(r.Findings) == 0) ||
		(r.Decision == "findings" && len(r.Findings) > 0)
	return decision && len(r.Findings) <= 32 && strings.TrimSpace(r.Note) != "" &&
		!slices.ContainsFunc(r.Findings, func(line string) bool { return strings.TrimSpace(line) == "" })
}

func nativeReviewFile(reviewID string) string { return "native-review-" + reviewID + ".json" }

func observeNativeReview(ctx context.Context, repo *gitx.Repo, host string, notice tokens.Notice) error {
	if !nativeSessionID.MatchString(notice.Session) || !nativeSessionID.MatchString(notice.AgentID) || notice.Session == notice.AgentID {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(repo.GitDir, "kitsu-stages")); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	ledger, err := openStageLedger(ctx, &session{repo: repo})
	if err != nil {
		return err
	}
	defer ledger.close()
	if notice.Event == "SubagentStart" {
		return observeReviewStart(ledger, host, notice)
	}
	var report nativeReviewReport
	if len(notice.LastAssistant) == 0 || len(notice.LastAssistant) > 16<<10 {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(notice.LastAssistant))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&report) != nil || decoder.Decode(new(any)) != io.EOF || !report.valid() {
		return nil
	}
	return observeReviewStop(ledger, host, notice, report)
}

func nativeClaimFor(t *harness.Task, host string, notice tokens.Notice) bool {
	c := t.Claim
	return t.Workflow == nil && c != nil && c.Candidate != nil && nativeSessionID.MatchString(c.ReviewID) && c.Reviewer != nil &&
		c.Reviewer.Host == host && c.Owner == notice.Session &&
		checkHarnessActor(t, "review", notice.AgentID) == nil
}

func workflowNoticeSettings(notice tokens.Notice) (model, effort string) {
	if json.Unmarshal(notice.Effort, &effort) != nil {
		var value struct {
			Level string `json:"level"`
		}
		_ = json.Unmarshal(notice.Effort, &value)
		effort = value.Level
	}
	return notice.Model, effort
}

func observeReviewStart(l *stageLedger, host string, notice tokens.Notice) error {
	files, err := fs.ReadDir(l.root.FS(), ".")
	if err != nil {
		return err
	}
	for _, file := range files {
		id, daily := strings.CutPrefix(file.Name(), "task-harness-")
		if !daily || !strings.HasSuffix(id, ".json") {
			continue
		}
		var task harness.Task
		if l.read(file.Name(), &task) != nil || !task.Valid(strings.TrimSuffix(id, ".json")) || !nativeClaimFor(&task, host, notice) {
			continue
		}
		name := nativeReviewFile(task.Claim.ReviewID)
		var observed nativeReviewObservation
		if err := l.read(name, &observed); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if task.Workflow != nil && len(observed.Started) != 0 && !slices.Contains(observed.Started, notice.AgentID) {
			observed.Conflict = true
			return l.write(name, observed)
		}
		if observed.Result == nil && len(observed.Started) < 32 && !slices.Contains(observed.Started, notice.AgentID) {
			observed.Started = append(observed.Started, notice.AgentID)
			if err := l.write(name, observed); err != nil {
				return err
			}
		}
	}
	return nil
}

func observeReviewStop(l *stageLedger, host string, notice tokens.Notice, report nativeReviewReport) error {
	var task harness.Task
	if !validHarnessID(report.Task) || l.read(harnessFile(report.Task), &task) != nil ||
		!task.Valid(report.Task) || !nativeClaimFor(&task, host, notice) {
		return nil
	}
	claim := task.Claim
	if report.ReviewID != claim.ReviewID || report.Candidate != contextpack.Key(*claim.Candidate) {
		return nil
	}
	name := nativeReviewFile(claim.ReviewID)
	var observed nativeReviewObservation
	if err := l.read(name, &observed); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if observed.Conflict || !slices.Contains(observed.Started, notice.AgentID) || observed.Result != nil {
		return nil
	}
	observed.Source = &harness.NativeReview{Host: host, Parent: notice.Session, Worker: notice.AgentID}
	observed.Model, observed.Effort = workflowNoticeSettings(notice)
	observed.Result = &report
	return l.write(name, observed)
}

func recordObservedReview(l *stageLedger, task *harness.Task, o *harnessOptions, view harnessView) (*harness.NativeReview, error) {
	required := task.Scope.RequireReview || view.RepositoryReview || o.observedReview
	claim := task.Claim
	var observed nativeReviewObservation
	if claim != nil && nativeSessionID.MatchString(claim.ReviewID) {
		if err := l.read(nativeReviewFile(claim.ReviewID), &observed); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if observed.Conflict || observed.Result == nil || observed.Source == nil {
		if required {
			return nil, failf("review is pending. Check the installed SubagentStart and SubagentStop hooks or resume the reserved worker")
		}
		return nil, nil
	}
	result := observed.Result
	if !o.observedReview && (o.actor != observed.Source.Worker || o.note != result.Note || !slices.Equal(o.findings, result.Findings)) {
		return nil, failf("review result differs from the host-observed result. Use --observed-review")
	}
	o.actor, o.note, o.findings = observed.Source.Worker, result.Note, result.Findings
	return observed.Source, nil
}

func (v *harnessView) setReviewStatus(required bool) {
	switch {
	case v.WorkflowReview:
		v.ReviewStatus = "runner_signed"
	case v.Review != nil && v.ReviewCurrent && v.Review.NativeReview != nil:
		v.ReviewStatus = "host_observed"
	case required:
		v.ReviewStatus, v.ReviewCurrent = "pending", false
	case v.Review != nil && v.ReviewCurrent:
		v.ReviewStatus = "agent_reported"
	case v.Review != nil:
		v.ReviewStatus = "stale"
	}
}
