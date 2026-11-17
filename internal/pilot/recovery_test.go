package pilot

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var pilotStopEvidence = flag.String("pilot-stop-evidence", "", "operator cancellation evidence; offline recovery only")

type cancellationEvidence struct {
	Version                  int       `json:"version"`
	StoppedAt                time.Time `json:"stopped_at"`
	ProcessesVerifiedStopped bool      `json:"processes_verified_stopped"`
	Reason                   string    `json:"reason"`
}

// TestPilotRecoverCancelled only captures existing work and invokes the frozen
// local grader. It never invokes a solver, retries a task, or changes inputs.
func TestPilotRecoverCancelled(t *testing.T) {
	if *pilotOutput == "" || *pilotStopEvidence == "" {
		t.Skip("explicit output and operator stop evidence required")
	}
	var evidence cancellationEvidence
	if err := readJSON(*pilotStopEvidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Version != 1 || evidence.StoppedAt.IsZero() || !evidence.ProcessesVerifiedStopped || evidence.Reason == "" {
		t.Fatal("recovery requires evidence that all solver processes stopped")
	}
	digest, err := hashFile(*pilotStopEvidence)
	if err != nil {
		t.Fatal(err)
	}
	m, p, err := loadFrozen(t.Context(), *pilotOutput)
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range p.Runs {
		runDir := filepath.Join(*pilotOutput, "runs", selected.ID)
		if _, err := os.Stat(filepath.Join(runDir, "result.json")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(runDir, "running.json")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		if err := recoverCancelled(t.Context(), m, p, selected, *pilotOutput, evidence, asset{*pilotStopEvidence, digest}); err != nil {
			t.Errorf("%s: %v; recovered evidence retained", selected.ID, err)
		} else {
			t.Log("recovered cancelled claim:", selected.ID)
		}
	}
}

func recoverCancelled(ctx context.Context, m manifest, p plan, selected cell, output string, evidence cancellationEvidence, source asset) (returned error) {
	runDir := filepath.Join(output, "runs", selected.ID)
	if _, err := os.Stat(filepath.Join(runDir, "result.json")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("recovery refuses an existing result or inaccessible result path")
	}
	var result runResult
	if err := readJSON(filepath.Join(runDir, "running.json"), &result); err != nil {
		return err
	}
	if result.Run != selected || result.ManifestSHA256 != p.ManifestSHA256 || result.Status != "running" || !result.Measured || result.BaselineTree == "" {
		return errors.New("running claim does not match the frozen assignment")
	}
	index := slices.IndexFunc(m.Cases, func(c pilotCase) bool { return c.ID == result.Run.CaseID })
	if index < 0 {
		return errors.New("claimed case is absent from frozen inputs")
	}
	chosen := m.Cases[index]
	recoveryDir := filepath.Join(runDir, "cancellation-recovery")
	if err := os.Mkdir(recoveryDir, 0o700); err != nil {
		return fmt.Errorf("recovery already claimed or unavailable: %w", err)
	}
	result.Status, result.Error = "operator_cancelled", evidence.Reason
	result.Usage = usageReport{Complete: false, Unknown: []string{"usage capture has not completed; original invocation receipt is missing"}}
	// The killed controller did not retain an invocation receipt. Its start,
	// duration, exit status, and end time cannot be reconstructed reliably.
	result.Execution = nil
	defer func() {
		if returned != nil {
			result.Error += "; recovery: " + returned.Error()
		}
		returned = errors.Join(returned, writeJSON(filepath.Join(runDir, "result.json"), result, true))
	}()
	if err := writeJSON(filepath.Join(runDir, "cancellation.json"), map[string]any{
		"version": 1, "status": result.Status, "reason": evidence.Reason,
		"operator_stop_evidence": source, "operator_stopped_at": evidence.StoppedAt,
		"processes_verified_stopped": evidence.ProcessesVerifiedStopped,
		"execution_metadata":         "unknown; controller exited before retaining invocation receipt",
		"usage_scope":                "captured cumulative rollout counts are lower bounds; in-flight or unflushed usage is unknown",
	}, true); err != nil {
		return err
	}
	home := filepath.Join(runDir, "home")
	usage, threads, usageErr := collectUsage(home, m.Host.Rollouts, recoveryDir, stdoutThreadID(filepath.Join(runDir, "host.stdout")))
	usage.Complete = false
	usage.Unknown = append(usage.Unknown, "operator cancellation: captured counts are lower bounds; final in-flight or unflushed usage is unknown")
	result.Usage, result.Method = usage, auditMethod(threads, selected.Arm, m.Model, m.Effort)
	result.Method.Facts["operator_cancelled"] = true
	result.Method.Facts["execution_metadata"] = "unknown"
	result.Method.Facts["usage_lower_bound"] = true
	if err := recoverCandidate(ctx, chosen, output, runDir, recoveryDir, &result); err != nil {
		return errors.Join(usageErr, err)
	}
	return errors.Join(usageErr, recoverGrade(ctx, m, runDir, recoveryDir, &result))
}

func recoverCandidate(ctx context.Context, chosen pilotCase, output, runDir, recoveryDir string, result *runResult) error {
	trustedRoot := filepath.Join(output, "trusted-candidates")
	trusted := filepath.Join(trustedRoot, result.Run.ID)
	receiptPath := trusted + ".json"
	if _, err := os.Stat(receiptPath); err == nil {
		var receipt exportReceipt
		if err := readJSON(receiptPath, &receipt); err != nil {
			return err
		}
		request := gradeRequest{CandidateWorktree: trusted, BaselineTree: result.BaselineTree, CandidateTree: receipt.CandidateTree}
		if receipt.SourceWorktree != filepath.Join(runDir, "worktree") || receipt.PublicSnapshotSHA256 != chosen.SnapshotSHA256 {
			return errors.New("existing candidate receipt differs from frozen source")
		}
		if err := trustedGradeCandidate(trustedRoot, request); err != nil {
			return err
		}
		digest, err := hashFile(receiptPath)
		if err != nil {
			return err
		}
		result.TrustedCandidate, result.CandidateTree, result.ExportAttestation = trusted, receipt.CandidateTree, asset{receiptPath, digest}
		call, err := invoke(ctx, trusted, filepath.Join(recoveryDir, "retained-candidate-diff"), "",
			[]string{"git", "-c", "core.hooksPath=" + os.DevNull, "diff", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", result.BaselineTree, result.CandidateTree}, trustedGitEnv(recoveryDir), 60)
		result.Patch = call.Stdout
		if err != nil || call.ExitCode != 0 {
			return fmt.Errorf("retained candidate diff: exit %d: %v", call.ExitCode, err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Retain an unfinished export as evidence before creating the canonical
	// host-owned export. No previous files or Git objects are overwritten.
	if _, err := os.Lstat(trusted); err == nil {
		if err := os.Rename(trusted, filepath.Join(recoveryDir, "preserved-partial-trusted-candidate")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var err error
	result.TrustedCandidate, result.CandidateTree, result.Patch, result.ExportAttestation, err = exportCandidate(ctx, chosen, result.Run, output, recoveryDir, filepath.Join(runDir, "worktree"), result.BaselineTree)
	return err
}

func recoverGrade(ctx context.Context, m manifest, runDir, recoveryDir string, result *runResult) error {
	gradePath := filepath.Join(runDir, "grade.json")
	if data, err := os.ReadFile(gradePath); err == nil {
		if !json.Valid(data) {
			return errors.New("existing grade is invalid JSON; retained without overwrite")
		}
		result.Grade = data
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	vars := map[string]string{"worktree": result.TrustedCandidate, "home": filepath.Join(runDir, "home"), "run_dir": runDir,
		"task": result.Run.CaseID, "case_id": result.Run.CaseID, "grade_request": filepath.Join(recoveryDir, "grade-request.json")}
	request := gradeRequest{Version: 1, CaseID: result.Run.CaseID, CandidateWorktree: result.TrustedCandidate, OutputPath: gradePath,
		BaselineTree: result.BaselineTree, CandidateTree: result.CandidateTree, VisiblePromptSHA256: result.Prompt.SHA256}
	if err := writeJSON(vars["grade_request"], request, true); err != nil {
		return err
	}
	settings := pilotHost{Env: m.Grader.Env, PassEnv: m.Grader.PassEnv}
	if settings.PassEnv == nil {
		settings.PassEnv = m.Host.PassEnv
	}
	graded, gradeErr := invoke(ctx, runDir, filepath.Join(recoveryDir, "grader"), "", expandedArgv(m.Grader.Argv, vars), executionEnv(settings, vars), 240)
	result.Grading = &graded
	data, readErr := os.ReadFile(gradePath)
	if readErr == nil && json.Valid(data) {
		result.Grade = data
	} else {
		readErr = fmt.Errorf("recovery grader left no valid JSON: %v", readErr)
	}
	if graded.ExitCode != 0 {
		gradeErr = errors.Join(gradeErr, fmt.Errorf("recovery grader exit %d", graded.ExitCode))
	}
	return errors.Join(gradeErr, readErr)
}

func TestPilotRecoveryPreservesCancellationAndExistingEvidence(t *testing.T) {
	path, output := unitManifest(t)
	if err := freeze(t.Context(), path, output); err != nil {
		t.Fatal(err)
	}
	m, p, err := loadFrozen(t.Context(), output)
	if err != nil {
		t.Fatal(err)
	}
	selected := p.Runs[0]
	if err := executeCell(t.Context(), m, p, selected, output); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(output, "runs", selected.ID)
	gradeBefore, err := os.ReadFile(filepath.Join(runDir, "grade.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(runDir, "result.json"), filepath.Join(runDir, "offline-completed-result.json")); err != nil {
		t.Fatal(err)
	}
	// A solver call would fail immediately: recovery must use retained files.
	m.Host.Argv = []string{"/no/solver/may/run"}
	evidence := cancellationEvidence{1, time.Date(2026, 11, 16, 20, 2, 42, 0, time.UTC), true, "user budget change"}
	if err := recoverCancelled(t.Context(), m, p, selected, output, evidence, asset{}); err != nil {
		t.Fatal(err)
	}
	var got runResult
	if err := readJSON(filepath.Join(runDir, "result.json"), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "operator_cancelled" || got.Error != evidence.Reason || got.Execution != nil || got.Grading != nil || got.Usage.Complete || got.Usage.Total != 618 || len(got.Usage.Threads) != 5 || got.CandidateTree == "" || !functionalPass(&got) {
		t.Fatalf("cancellation evidence lost or invented: %+v", got)
	}
	gradeAfter, err := os.ReadFile(filepath.Join(runDir, "grade.json"))
	if err != nil || string(gradeAfter) != string(gradeBefore) {
		t.Fatal("existing grade changed", err)
	}
	if err := recoverCancelled(t.Context(), m, p, selected, output, evidence, asset{}); err == nil {
		t.Fatal("recovery overwrote an existing result")
	}
}

func TestPilotRecoveryExportsPartialCandidateAndGradesOffline(t *testing.T) {
	path, output := unitManifest(t)
	if err := freeze(t.Context(), path, output); err != nil {
		t.Fatal(err)
	}
	m, p, err := loadFrozen(t.Context(), output)
	if err != nil {
		t.Fatal(err)
	}
	selected := p.Runs[0]
	if err := executeCell(t.Context(), m, p, selected, output); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(output, "runs", selected.ID)
	for _, path := range []string{filepath.Join(runDir, "result.json"), filepath.Join(runDir, "grade.json"), filepath.Join(output, "trusted-candidates", selected.ID+".json")} {
		if err := os.Rename(path, path+".offline-before-recovery"); err != nil {
			t.Fatal(err)
		}
	}
	m.Host.Argv = []string{"/no/solver/may/run"}
	evidence := cancellationEvidence{1, time.Date(2026, 11, 16, 20, 2, 42, 0, time.UTC), true, "user budget change"}
	if err := recoverCancelled(t.Context(), m, p, selected, output, evidence, asset{}); err != nil {
		t.Fatal(err)
	}
	var got runResult
	if err := readJSON(filepath.Join(runDir, "result.json"), &got); err != nil {
		t.Fatal(err)
	}
	if got.Execution != nil || got.Grading == nil || !functionalPass(&got) || got.CandidateTree == "" || got.Status != "operator_cancelled" {
		t.Fatalf("retained candidate was not independently graded: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(runDir, "cancellation-recovery", "preserved-partial-trusted-candidate", ".git")); err != nil {
		t.Fatal("partial trusted export was not preserved", err)
	}
}
