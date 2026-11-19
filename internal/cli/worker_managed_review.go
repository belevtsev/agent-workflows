package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

type managedReviewReport struct {
	Candidate string          `json:"candidate"`
	Result    json.RawMessage `json:"result"`
}

type managedReviewObservation struct {
	Candidate string          `json:"candidate"`
	Host      string          `json:"host"`
	Parent    string          `json:"parent"`
	Started   string          `json:"started"`
	Worker    string          `json:"worker,omitempty"`
	Conflict  bool            `json:"conflict,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

func managedReviewFile(id string) string { return id + ".review.json" }

func managedReviewCandidate(r stageRecord) string {
	return contextpack.Key([]any{r.Prepared.State, r.Managed})
}

func managedReviewPrompt(d nativeDispatch, r stageRecord) string {
	return fmt.Sprintf("%s\nFor this native review, put that handback object in the result of exactly one JSON object "+
		"with the fields candidate and result. Set candidate to %q. This is the native_review_result for this assignment.",
		d.Native.Prompt, managedReviewCandidate(r),
	)
}

func validateManagedReviewIdentity(run managedWorkerRun) error {
	if run.Role == "reviewer" {
		if nativeSessionID.MatchString(run.Reviewer) && run.Reviewer != run.Session {
			return nil
		}
		return errors.New("configured review has no distinct native worker")
	}
	if run.Reviewer != "" {
		return errors.New("configured non-review work contains a reviewer identity")
	}
	return nil
}

func observeManagedReview(ctx context.Context, repo *gitx.Repo, host string, notice tokens.Notice) error {
	if !managedReviewNotice(notice) {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(repo.GitDir, "kitsu-stages", "active.json")); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	ledger, err := openStageLedgerWait(ctx, &session{repo: repo}, time.Second)
	if err != nil {
		return err
	}
	defer ledger.close()
	return recordManagedReviewNotice(ledger, repo.Root, host, notice)
}

func managedReviewNotice(notice tokens.Notice) bool {
	event := notice.Event == "SubagentStart" || notice.Event == "SubagentStop"
	return event && nativeSessionID.MatchString(notice.Session) &&
		nativeSessionID.MatchString(notice.AgentID) && notice.Session != notice.AgentID
}

func recordManagedReviewNotice(l *stageLedger, worktree, host string, notice tokens.Notice) error {
	id, err := l.active()
	if err != nil || id == "" {
		return err
	}
	r, err := l.record(id, worktree)
	if err != nil {
		return err
	}
	if r.Managed == nil || r.Prepared.Brief.Role != "reviewer" || r.Prepared.Brief.Host != host {
		return nil
	}
	run, err := readManagedRun(l, id, r)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if run.Status != "running" || run.Session != notice.Session {
		return nil
	}
	return recordManagedReviewEvent(l, r, run, notice)
}

func recordManagedReviewEvent(l *stageLedger, r stageRecord, run managedWorkerRun, notice tokens.Notice) error {
	obs := managedReviewObservation{Candidate: managedReviewCandidate(r), Host: run.Host, Parent: run.Session}
	name := managedReviewFile(run.Dispatch)
	err := l.read(name, &obs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && !managedReviewMatches(obs, r, run) {
		return errors.New("native review observation does not match its reservation")
	}
	if notice.Event == "SubagentStart" {
		if obs.Started == "" {
			obs.Started = notice.AgentID
		} else if obs.Started != notice.AgentID {
			obs.Conflict = true
		}
		return l.write(name, obs)
	}
	if obs.Started != notice.AgentID || obs.Conflict || obs.Worker != "" {
		return nil
	}
	payload := readManagedReviewReport(r, run, notice)
	if payload == nil {
		return nil
	}
	obs.Worker, obs.Result = notice.AgentID, payload
	return l.write(name, obs)
}

func readManagedReviewReport(r stageRecord, run managedWorkerRun, notice tokens.Notice) json.RawMessage {
	if len(notice.LastAssistant) == 0 || len(notice.LastAssistant) > maxWorkerReturn {
		return nil
	}
	if err := uniqueReturnKeys(json.NewDecoder(strings.NewReader(notice.LastAssistant)), 0); err != nil {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(notice.LastAssistant))
	decoder.DisallowUnknownFields()
	var report managedReviewReport
	if decoder.Decode(&report) != nil || decoder.Decode(new(any)) != io.EOF || report.Candidate != managedReviewCandidate(r) {
		return nil
	}
	request, err := readWorkerReturn(bytes.NewReader(report.Result), "-")
	if err != nil || matchWorkerReturn(r, request) != nil {
		return nil
	}
	run.Reviewer = notice.AgentID
	if !managedHandbackMatchesRun(request, run) || len(request.ChangedPaths) != 0 {
		return nil
	}
	return report.Result
}

func completedManagedReview(ctx context.Context, s *session, r stageRecord, run managedWorkerRun) (managedReviewObservation, error) {
	l, err := openStageLedgerWait(ctx, s, time.Second)
	if err != nil {
		return managedReviewObservation{}, err
	}
	defer l.close()
	return readManagedReview(l, r, run)
}

func readManagedReview(l *stageLedger, r stageRecord, run managedWorkerRun) (managedReviewObservation, error) {
	var obs managedReviewObservation
	if err := l.read(managedReviewFile(run.Dispatch), &obs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = errors.New("no matching native reviewer hook observation; inspect SubagentStart and SubagentStop")
		}
		return obs, err
	}
	if !managedReviewMatches(obs, r, run) || obs.Conflict || obs.Worker != obs.Started ||
		!nativeSessionID.MatchString(obs.Worker) || obs.Worker == obs.Parent || len(obs.Result) == 0 {
		return obs, errors.New("no completed independent native reviewer on the reserved candidate")
	}
	if run.Reviewer != "" && (run.Reviewer != obs.Worker || run.SHA256 != workerBriefHash(obs.Result)) {
		return obs, errors.New("native reviewer evidence changed after completion")
	}
	return obs, nil
}

func managedReviewMatches(obs managedReviewObservation, r stageRecord, run managedWorkerRun) bool {
	return obs.Candidate == managedReviewCandidate(r) && obs.Host == run.Host && obs.Parent == run.Session
}
