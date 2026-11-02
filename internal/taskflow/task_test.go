package taskflow

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCheckSubprocess(t *testing.T) {
	if os.Getenv("CW_TASKFLOW_HELPER") != "1" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	if len(args) == 0 {
		os.Exit(12)
	}
	switch args[0] {
	case "pass":
		fmt.Print(strings.Join(args[1:], "|"))
		for _, key := range []string{"OPENAI_API_KEY", "SUBQ_API_KEY", "TYPESAFE_API_KEY", "EXAMPLE_SECRET"} {
			if os.Getenv(key) != "" {
				os.Exit(13)
			}
		}
	case "fail":
		os.Exit(9)
	case "marker":
		if _, err := os.Stat(args[1]); err != nil {
			os.Exit(9)
		}
	case "wait":
		time.Sleep(20 * time.Second)
	case "mutate":
		os.WriteFile(args[1], []byte("changed\n"), 0600)
	case "output":
		fmt.Print(strings.Repeat("x", MaxLogBytes+2048))
	case "barrier":
		connection, err := net.DialTimeout("tcp", args[1], 10*time.Second)
		if err != nil {
			os.Exit(21)
		}
		defer connection.Close()
		if err := connection.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			os.Exit(22)
		}
		if _, err := io.WriteString(connection, args[2]); err != nil {
			os.Exit(23)
		}
		var release [1]byte
		if _, err := io.ReadFull(connection, release[:]); err != nil {
			os.Exit(24)
		}
	case "wait-marker":
		if err := os.WriteFile(args[1], []byte("ready"), 0600); err != nil {
			os.Exit(15)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(args[2]); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(16)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if len(args) > 3 && args[3] == "fail" {
			os.Exit(9)
		}
	case "spawn-background":
		executable, err := os.Executable()
		if err != nil {
			os.Exit(17)
		}
		null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if err != nil {
			os.Exit(18)
		}
		defer null.Close()
		child := exec.Command(executable, "-test.run=^TestCheckSubprocess$", "--", "delayed-marker", args[1])
		child.Env = os.Environ()
		child.Stdin = null
		child.Stdout = null
		child.Stderr = null
		if err := child.Start(); err != nil {
			os.Exit(19)
		}
		child.Process.Release()
	case "delayed-marker":
		time.Sleep(2 * time.Second)
		if err := os.WriteFile(args[1], []byte("escaped check descendant"), 0600); err != nil {
			os.Exit(20)
		}
	default:
		os.Exit(14)
	}
	os.Exit(0)
}

func helperPlan(t *testing.T, kind string, args ...string) Plan {
	t.Helper()
	t.Setenv("CW_TASKFLOW_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argv := append([]string{executable, "-test.run=^TestCheckSubprocess$", "--"}, args...)
	return Plan{Title: "Source-bound example", Kind: kind, Requirements: []Requirement{{ID: "behavior", Description: "Verified source behavior", CheckIDs: []string{"focused"}}}, Checks: []Check{{ID: "focused", Argv: argv, TimeoutSeconds: 10}}}
}

func fixtureTask(t *testing.T, plan Plan) (Record, SourceRef) {
	t.Helper()
	repo := t.TempDir()
	data := []byte("first line\nsecond line\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	record, err := Start(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), plan)
	if err != nil {
		t.Fatal(err)
	}
	return record, SourceRef{Path: "source.txt", StartLine: 1, EndLine: 2, SHA256: digest(data)}
}

func assessment(ref SourceRef, checks ...string) AssessmentInput {
	return AssessmentInput{Requirements: []Assessment{{ID: "behavior", Status: "satisfied", Evidence: []SourceRef{ref}, CheckIDs: checks}}, Decision: &Decision{Status: "resolved", Reason: "Actual source and current planned checks establish the result.", Evidence: []SourceRef{ref}, CheckIDs: checks}}
}

func mustAssess(t *testing.T, record Record, ref SourceRef) {
	t.Helper()
	if _, err := Run(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	if _, err := Assess(t.Context(), record.Directory, assessment(ref, "focused")); err != nil {
		t.Fatal(err)
	}
}

func TestSmallTaskCompletionAndDrift(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "private-key-must-not-be-in-child")
	t.Setenv("EXAMPLE_SECRET", "private-secret-must-not-be-in-child")
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass", "literal; $(touch should-not-exist)", "path with spaces"))
	mustAssess(t, record, ref)
	verified, err := Verify(t.Context(), record.Directory)
	if err != nil || !verified.Complete {
		t.Fatalf("completion: %+v %v", verified, err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Verification == nil || !stored.Verification.Complete {
		t.Fatalf("stored completion: %+v %v", stored, err)
	}
	log, err := os.ReadFile(filepath.Join(record.Directory, stored.Checks[0].Log))
	if err != nil || !bytes.Contains(log, []byte("$(touch should-not-exist)")) {
		t.Fatalf("argv was not literal: %s %v", log, err)
	}
	if _, err := os.Stat(filepath.Join(record.Baseline.Root, "should-not-exist")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("argv was interpreted as shell")
	}
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "untracked.txt"), []byte("new source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	verified, err = Verify(t.Context(), record.Directory)
	if err == nil || verified.Complete || !strings.Contains(strings.Join(verified.Problems, " "), "stale") {
		t.Fatalf("drift accepted: %+v %v", verified, err)
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "before"); err == nil {
		t.Fatal("baseline refresh accepted after drift")
	}
}

func TestNonExecutionMustBeExplicitlyPermitted(t *testing.T) {
	plan := Plan{Title: "Read-only source check", Kind: "small", Requirements: []Requirement{{ID: "behavior", Description: "Read actual source", AllowNonExecution: true}}}
	record, ref := fixtureTask(t, plan)
	input := assessment(ref)
	if _, err := Assess(t.Context(), record.Directory, input); err == nil {
		t.Fatal("empty rationale accepted")
	}
	input.Requirements[0].NonExecutionReason = "Read-only change; inspected the complete referenced file."
	input.Decision.NonExecutionReason = input.Requirements[0].NonExecutionReason
	if _, err := Assess(t.Context(), record.Directory, input); err != nil {
		t.Fatal(err)
	}
	if result, err := Verify(t.Context(), record.Directory); err != nil || !result.Complete {
		t.Fatalf("explicit non-execution rejected: %+v %v", result, err)
	}
}

func TestCheckOutcomesNeverBecomePasses(t *testing.T) {
	for _, mode := range []string{"fail", "wait", "mutate", "output"} {
		t.Run(mode, func(t *testing.T) {
			plan := helperPlan(t, "small", mode)
			if mode == "wait" {
				plan.Checks[0].TimeoutSeconds = 1
			}
			record, _ := fixtureTask(t, plan)
			if mode == "mutate" {
				// A fresh plan records the exact mutation argv; the manager never
				// accepts a runtime argv override.
				plan.Checks[0].Argv = append(plan.Checks[0].Argv, filepath.Join(record.Baseline.Root, "source.txt"))
				var err error
				record, err = Start(t.Context(), record.Baseline.Root, filepath.Dir(record.Directory), plan)
				if err != nil {
					t.Fatal(err)
				}
			}
			run, err := Run(t.Context(), record.Directory, "focused")
			if mode == "output" {
				if err != nil || run.Status != "passed" || !run.Truncated || run.OutputBytes != MaxLogBytes+2048 {
					t.Fatalf("bounded output: %+v %v", run, err)
				}
				data, err := os.ReadFile(filepath.Join(record.Directory, run.Log))
				if err != nil || len(data) != MaxLogBytes || digest(data) != run.LogSHA256 {
					t.Fatal("invalid bounded output hash")
				}
			} else {
				expected := map[string]string{"fail": "failed", "wait": "timed_out", "mutate": "source_changed"}[mode]
				if err == nil || run.Status != expected {
					t.Fatalf("outcome: %+v %v", run, err)
				}
				stored, loadErr := Load(record.Directory)
				if loadErr != nil || stored.Checks[0].Status != expected {
					t.Fatalf("terminal result not preserved: %+v %v", stored, loadErr)
				}
			}
		})
	}
}

func TestCancellationPreservesTerminalRecord(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "wait"))
	ctx, cancel := context.WithCancel(t.Context())
	timer := time.AfterFunc(200*time.Millisecond, cancel)
	defer timer.Stop()
	run, err := Run(ctx, record.Directory, "focused")
	if !errors.Is(err, context.Canceled) || run.Status != "cancelled" {
		t.Fatalf("cancellation: %+v %v", run, err)
	}
	stored, err := Load(record.Directory)
	if err != nil || stored.Checks[0].Status != "cancelled" || stored.Checks[0].FinishedAt == nil {
		t.Fatalf("cancelled record lost: %+v %v", stored, err)
	}
}

func TestNormalCheckExitStopsRedirectedBackgroundDescendants(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("owned process-group cleanup is available on native Linux and macOS builds")
	}
	marker := filepath.Join(t.TempDir(), "descendant-marker")
	record, _ := fixtureTask(t, helperPlan(t, "small", "spawn-background", marker))
	if run, err := Run(t.Context(), record.Directory, "focused"); err != nil || run.Status != "passed" {
		t.Fatalf("normal check exit: %+v %v", run, err)
	}
	// The redirected child would outlive command.Run without explicit cleanup;
	// it must never reach its delayed write after a passed terminal record.
	time.Sleep(2200 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("normally exiting check left an owned descendant running: %v", err)
	}
}

func TestInvalidEvidenceCannotBeMarkedSatisfied(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	if _, err := Run(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*AssessmentInput){
		func(input *AssessmentInput) { input.Requirements[0].Evidence[0].SHA256 = strings.Repeat("0", 64) },
		func(input *AssessmentInput) { input.Requirements[0].Evidence[0].EndLine = 999 },
		func(input *AssessmentInput) { input.Requirements[0].Evidence[0].Path = "../escape" },
		func(input *AssessmentInput) { input.Requirements[0].CheckIDs = []string{"unknown"} },
		func(input *AssessmentInput) { input.Requirements[0].CheckIDs = nil },
	} {
		input := assessment(ref, "focused")
		change(&input)
		if _, err := Assess(t.Context(), record.Directory, input); err == nil {
			t.Fatal("invalid evidence accepted")
		}
	}
	if _, err := Run(t.Context(), record.Directory, "not-approved"); err == nil {
		t.Fatal("unplanned check accepted")
	}
}

func writeConsultation(t *testing.T, context Context, mutate func(*Consultation, *SharedRequest)) string {
	t.Helper()
	request := SharedRequest{State: mustJSON(t, map[string]any{"task_context": context, "note": "дані 👩 & <source>"}), Questions: map[string]Question{"supported": {Type: "choice", Instructions: jsontext.Value(`"Assess the prepared source"`), Criteria: map[string]jsontext.Value{"yes": jsontext.Value(`"Supported"`), "no": jsontext.Value(`"Unsupported"`)}}}}
	zero := 0.0
	choice := "yes"
	record := Consultation{Version: 3, Status: "unavailable", Providers: map[string]Provider{}, Comparison: map[string]Comparison{"supported": {Status: "unavailable", Choices: map[string]string{}, Missing: map[string]string{"jev": "unavailable", "subq_snap": "unavailable", "openai_decisions": "unavailable"}}}, FinalDecision: FinalDecision{Status: "resolved", Questions: map[string]QuestionDecision{"supported": {Status: "resolved", Choice: &choice, Reason: "Codex verified the prepared source and focused check independently.", Evidence: []string{"source.txt:1 at exact snapshot"}, Verification: []string{"focused check at exact snapshot"}, RejectedJudgments: map[string]string{}}}}}
	for _, name := range adviserIDs {
		model := map[string]string{"jev": "jev-1.14.0", "subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}[name]
		record.Providers[name] = Provider{Status: "unavailable", RequestedModel: model, DurationSeconds: &zero, Error: &ProviderError{Category: "missing_credential"}}
	}
	if mutate != nil {
		mutate(&record, &request)
	}
	record.Request = mustJSON(t, request)
	canonical, err := canonicalJSON(record.Request)
	if err != nil {
		t.Fatal(err)
	}
	record.RequestSHA256 = digest(canonical)
	for name, provider := range record.Providers {
		provider.RequestSHA256 = record.RequestSHA256
		record.Providers[name] = provider
	}
	path := filepath.Join(t.TempDir(), "consultation.json")
	if err := os.WriteFile(path, mustJSON(t, record), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustJSON(t *testing.T, value any) jsontext.Value {
	t.Helper()
	data, err := encode(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSubstantialTaskNeedsTwoSourceBoundAdjudications(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	before, err := PrepareContext(t.Context(), record.Directory, "before")
	if err != nil {
		t.Fatal(err)
	}
	input := writeConsultation(t, before, nil)
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", input); err != nil {
		t.Fatal(err)
	}
	attached, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	copiedBytes, err := os.ReadFile(filepath.Join(record.Directory, attached.Consultations["before"].File))
	if err != nil || !bytes.Equal(originalBytes, copiedBytes) {
		t.Fatal("original consultation bytes were not preserved")
	}
	mustAssess(t, record, ref)
	if result, err := Verify(t.Context(), record.Directory); err == nil || result.Complete {
		t.Fatal("missing final consultation accepted")
	}
	final, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", input); err == nil {
		t.Fatal("same before/final artifact accepted")
	}
	hardlink := filepath.Join(t.TempDir(), "linked-final.json")
	if err := os.Link(input, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", hardlink); err == nil {
		t.Fatal("hard-linked before/final artifact accepted")
	}
	finalInput := writeConsultation(t, final, nil)
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", finalInput); err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(t.Context(), record.Directory)
	if err != nil || !verified.Complete || len(verified.Limitations) < 8 {
		t.Fatalf("unavailability fallback: %+v %v", verified, err)
	}
}

func TestFinalCheckpointBindsCompletedEvidenceAtUnchangedSource(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	before := record.Contexts["before"]
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", writeConsultation(t, before, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil {
		t.Fatal("final context emitted before checks, assessments and Coordinator decision")
	}
	// Guessing a final request against the original source cannot bypass the
	// missing prepared checkpoint and its evidence binding.
	premature := before
	premature.Phase = "final"
	premature.EvidenceSHA256 = strings.Repeat("0", 64)
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", writeConsultation(t, premature, nil)); err == nil {
		t.Fatal("premature final adjudication accepted")
	}
	mustAssess(t, record, ref)
	final, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil || !validSHA256(final.EvidenceSHA256) {
		t.Fatalf("completed evidence context: %+v %v", final, err)
	}
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", writeConsultation(t, final, nil)); err != nil {
		t.Fatal(err)
	}
	if result, err := Verify(t.Context(), record.Directory); err != nil || !result.Complete {
		t.Fatalf("final completion: %+v %v", result, err)
	}
	// A new assessment revision at identical source invalidates the adviser
	// checkpoint, even when its assertion text and passed check IDs are identical.
	if _, err := Assess(t.Context(), record.Directory, assessment(ref, "focused")); err != nil {
		t.Fatal(err)
	}
	if result, err := Verify(t.Context(), record.Directory); err == nil || result.Complete {
		t.Fatal("new assessment reused stale final adjudication")
	}
	refreshed, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.EvidenceSHA256 == final.EvidenceSHA256 {
		t.Fatal("assessment revision did not invalidate evidence binding")
	}
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", writeConsultation(t, refreshed, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	if _, err := Assess(t.Context(), record.Directory, assessment(ref, "focused")); err != nil {
		t.Fatal(err)
	}
	if result, err := Verify(t.Context(), record.Directory); err == nil || result.Complete {
		t.Fatal("new check run reused stale final adjudication")
	}
	stored, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Contexts["before"] != before || stored.Consultations["before"].Context != before {
		t.Fatal("later evidence invalidated immutable before binding")
	}
}

func TestFinalCheckpointInvalidAfterLaterFailedRun(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "allowed-marker")
	if err := os.WriteFile(marker, []byte("present"), 0600); err != nil {
		t.Fatal(err)
	}
	plan := helperPlan(t, "substantial", "marker", marker)
	record, ref := fixtureTask(t, plan)
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", writeConsultation(t, record.Contexts["before"], nil)); err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	final, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", writeConsultation(t, final, nil)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if run, err := Run(t.Context(), record.Directory, "focused"); err == nil || run.Status != "failed" {
		t.Fatalf("later failure: %+v %v", run, err)
	}
	if result, err := Verify(t.Context(), record.Directory); err == nil || result.Complete {
		t.Fatal("failed rerun accepted with prior final adjudication")
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil {
		t.Fatal("final context emitted over failed current checks")
	}
}

func TestCompletionWaitsForEveryOwnedRunAndRefusesSameCheckConcurrency(t *testing.T) {
	control := t.TempDir()
	ready := filepath.Join(control, "ready")
	release := filepath.Join(control, "release")
	plan := helperPlan(t, "small", "pass")
	background := plan.Checks[0]
	background.ID = "background"
	background.Argv = append(append([]string(nil), background.Argv[:3]...), "wait-marker", ready, release, "fail")
	plan.Checks = append(plan.Checks, background)
	record, ref := fixtureTask(t, plan)
	done := make(chan error, 1)
	go func() { _, err := Run(t.Context(), record.Directory, "background"); done <- err }()
	t.Cleanup(func() {
		os.WriteFile(release, []byte("release"), 0600)
		select {
		case <-done:
		case <-time.After(12 * time.Second):
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background child did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := Run(t.Context(), record.Directory, "background"); err == nil || !strings.Contains(err.Error(), "already has an owned running") {
		t.Fatalf("same-check concurrency was not refused: %v", err)
	}
	mustAssess(t, record, ref)
	if result, err := Verify(t.Context(), record.Directory); err == nil || result.Complete || !strings.Contains(strings.Join(result.Problems, " "), "nonterminal") {
		t.Fatalf("completion ignored a running owned check: %+v %v", result, err)
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil {
		t.Fatal("final context ignored a running owned check")
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("background check unexpectedly passed")
	}
	// Cleanup must not wait again after this terminal result was consumed.
	done <- nil
}

func TestOlderNonterminalRunCannotBeHiddenByLatestPass(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	mustAssess(t, record, ref)
	if _, err := update(record.Directory, func(_ *os.Root, record *Record) error {
		older := record.Checks[0]
		id, err := randomID()
		if err != nil {
			return err
		}
		older.ID = id
		older.Status = "running"
		older.FinishedAt = nil
		older.After = nil
		older.ExitCode = nil
		record.Checks = append([]CheckRun{older}, record.Checks...)
		record.EvidenceRevision++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if result, err := Verify(t.Context(), record.Directory); err == nil || result.Complete || !strings.Contains(strings.Join(result.Problems, " "), "nonterminal") {
		t.Fatalf("older running attempt was hidden: %+v %v", result, err)
	}
}

func TestConsultationRejectsDisabledMismatchedAndPendingRecords(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	bound := record.Contexts["before"]
	cases := map[string]func(*Consultation, *SharedRequest){
		"missing provider": func(record *Consultation, _ *SharedRequest) { delete(record.Providers, "jev") },
		"disabled provider": func(record *Consultation, _ *SharedRequest) {
			value := record.Providers["openai_decisions"]
			value.Status = "disabled"
			record.Providers["openai_decisions"] = value
		},
		"unsupported model": func(record *Consultation, _ *SharedRequest) {
			value := record.Providers["openai_decisions"]
			value.RequestedModel = "gpt-unknown"
			record.Providers["openai_decisions"] = value
		},
		"cross task": func(_ *Consultation, request *SharedRequest) {
			context := bound
			context.TaskID = "different"
			request.State = mustJSON(t, map[string]any{"task_context": context})
		},
		"cross phase": func(_ *Consultation, request *SharedRequest) {
			context := bound
			context.Phase = "final"
			request.State = mustJSON(t, map[string]any{"task_context": context})
		},
		"stale source": func(_ *Consultation, request *SharedRequest) {
			context := bound
			context.Snapshot.Fingerprint = "different"
			request.State = mustJSON(t, map[string]any{"task_context": context})
		},
		"forged comparison": func(record *Consultation, _ *SharedRequest) { record.Status = "success" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeConsultation(t, bound, change)
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
				t.Fatal("invalid consultation accepted")
			}
		})
	}
	pending := writeConsultation(t, bound, func(record *Consultation, _ *SharedRequest) {
		record.FinalDecision = FinalDecision{Status: "pending", Questions: map[string]QuestionDecision{}}
	})
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", pending); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil {
		t.Fatal("pending before decision accepted")
	}
}

func TestMultiContextBatchHasExactlyOneMatchingBinding(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	bound := record.Contexts["before"]
	other := bound
	other.TaskID = "another-task"
	for _, duplicate := range []bool{false, true} {
		input := writeConsultation(t, bound, func(_ *Consultation, request *SharedRequest) {
			contexts := []Context{other, bound}
			if duplicate {
				contexts = append(contexts, bound)
			}
			request.State = mustJSON(t, map[string]any{"task_contexts": contexts})
		})
		_, err := AttachConsultation(t.Context(), record.Directory, "before", input)
		if duplicate && err == nil {
			t.Fatal("duplicate context accepted")
		}
		if !duplicate && err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndependentSuccessPartialAndRejectedJudgments(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	bound := record.Contexts["before"]
	build := func(invalid string) string {
		return writeConsultation(t, bound, func(record *Consultation, request *SharedRequest) {
			request.Questions["second"] = request.Questions["supported"]
			yes, no := 0.9, 0.1
			confidence := 0.9
			for _, name := range adviserIDs {
				provider := record.Providers[name]
				provider.Status = "success"
				provider.Error = nil
				provider.ResolvedModel = provider.RequestedModel
				choice := "yes"
				probabilities := map[string]float64{"yes": yes, "no": no}
				if name == "subq_snap" {
					choice = "no"
					probabilities = map[string]float64{"yes": no, "no": yes}
				}
				answer := Answer{Type: "choice", Choice: choice, Probabilities: probabilities, Confidence: &confidence}
				usage := jsontext.Value(`{"input_tokens":1,"output_tokens":1}`)
				if name == "openai_decisions" {
					usage = jsontext.Value(`{"input_tokens":1,"output_tokens":1,"total_tokens":2,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}`)
				}
				provider.Usage = usage
				provider.Result = &ProviderResult{Model: provider.RequestedModel, Answers: map[string]Answer{"supported": answer, "second": answer}, Usage: usage}
				if name == "openai_decisions" {
					provider.Status = "partial"
					provider.Result.Answers["second"] = Answer{Type: "refusal"}
				}
				record.Providers[name] = provider
			}
			record.Status = "partial"
			record.Comparison = map[string]Comparison{
				"supported": {Status: "disagreement", Choices: map[string]string{"jev": "yes", "subq_snap": "no", "openai_decisions": "yes"}, Missing: map[string]string{}},
				"second":    {Status: "partial", Choices: map[string]string{"jev": "yes", "subq_snap": "no"}, Missing: map[string]string{"openai_decisions": "refusal"}},
			}
			decision := record.FinalDecision.Questions["supported"]
			decision.RejectedJudgments = map[string]string{"subq_snap": "The contrasting choice omits the verified source condition."}
			record.FinalDecision.Questions["supported"] = decision
			record.FinalDecision.Questions["second"] = decision
			switch invalid {
			case "rejection":
				decision.RejectedJudgments = map[string]string{}
				record.FinalDecision.Questions["second"] = decision
			case "resolved-model":
				provider := record.Providers["jev"]
				provider.ResolvedModel = "jev-9.9.9"
				record.Providers["jev"] = provider
			case "distribution":
				provider := record.Providers["jev"]
				answer := provider.Result.Answers["supported"]
				answer.Probabilities = map[string]float64{"yes": 0.5, "no": 0.1}
				provider.Result.Answers["supported"] = answer
				record.Providers["jev"] = provider
			case "refusal-choice":
				provider := record.Providers["openai_decisions"]
				provider.Result.Answers["second"] = Answer{Type: "refusal", Choice: "no"}
				record.Providers["openai_decisions"] = provider
			}
		})
	}
	for _, invalid := range []string{"", "rejection", "resolved-model", "distribution", "refusal-choice"} {
		path := build(invalid)
		_, err := AttachConsultation(t.Context(), record.Directory, "before", path)
		if invalid == "" && err != nil {
			t.Fatal(err)
		}
		if invalid != "" && err == nil {
			t.Fatalf("invalid %s accepted", invalid)
		}
	}
}

func TestPrivateStorageAndInterruptedWritesPreserveOriginals(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	initial, err := os.ReadFile(filepath.Join(record.Directory, "record-000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Directory, ".write-interrupted"), []byte(`{"partial":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "before"); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(record.Directory, "record-000001.json"))
	if err != nil || !bytes.Equal(initial, current) {
		t.Fatal("original revision modified")
	}
	if err := os.Mkdir(filepath.Join(record.Directory, ".lock"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "before"); err == nil {
		t.Fatal("concurrent/interrupted lock ignored")
	}
	if _, err := Load(record.Directory); err != nil {
		t.Fatal("lock hid prior valid record")
	}
	if err := os.Remove(filepath.Join(record.Directory, ".lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Directory, "record-000003.json"), []byte(`{"version":1`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(record.Directory); err == nil {
		t.Fatal("truncated published revision ignored")
	}
}

func TestTerminalPublicationWaitsForShortMetadataWriter(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	lock := filepath.Join(record.Directory, ".lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(lock) })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := updateWhenUnlocked(ctx, record.Directory, "", func(_ *os.Root, record *Record) error { record.EvidenceRevision++; return nil })
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("terminal update did not wait for writer: %v", err)
	case <-time.After(60 * time.Millisecond):
	}
	before, err := Load(record.Directory)
	if err != nil || before.Revision != 1 {
		t.Fatalf("waiting writer changed existing record: %+v %v", before, err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after, err := Load(record.Directory)
	if err != nil || after.Revision != 2 || after.EvidenceRevision != 1 {
		t.Fatalf("terminal update was lost: %+v %v", after, err)
	}
}

func TestStorageRejectsRepoAndSymlinkEscapes(t *testing.T) {
	plan := helperPlan(t, "small", "pass")
	repo := t.TempDir()
	if _, err := Start(t.Context(), repo, filepath.Join(repo, "records"), plan); err == nil {
		t.Fatal("in-repo storage accepted")
	}
	parent := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := Start(t.Context(), repo, link, plan); err == nil {
		t.Fatal("symlink storage accepted")
	}
	record, _ := fixtureTask(t, plan)
	if err := os.Remove(filepath.Join(record.Directory, "record-000001.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(target, "stolen"), filepath.Join(record.Directory, "record-000001.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(record.Directory); err == nil {
		t.Fatal("symlink record accepted")
	}
	if _, err := os.Stat(filepath.Join(target, "stolen")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("escaped record read/write occurred")
	}
}

func TestInitialRevisionNeverPublishesAboveReadableLimit(t *testing.T) {
	plan := Plan{Title: "Bounded task plan", Kind: "small", Requirements: []Requirement{{ID: "behavior", Description: "One planned check", CheckIDs: []string{"check0"}}}}
	for i := 0; i < 129; i++ {
		plan.Checks = append(plan.Checks, Check{ID: fmt.Sprintf("check%d", i), Argv: []string{"go", "test", strings.Repeat("x", 65500)}, TimeoutSeconds: 1})
	}
	store := filepath.Join(t.TempDir(), "tasks")
	if _, err := Start(t.Context(), t.TempDir(), store, plan); err == nil || !strings.Contains(err.Error(), "initial task revision exceeds") {
		t.Fatalf("oversized initial revision accepted: %v", err)
	}
	entries, err := os.ReadDir(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if files, err := os.ReadDir(filepath.Join(store, entry.Name())); err != nil || len(files) != 0 {
			t.Fatalf("unreadable initial revision published: %v %v", files, err)
		}
	}
}

func TestJSONIsStrictAndCommandsAreExplicit(t *testing.T) {
	for _, input := range []string{`{"title":"x","kind":"small","unexpected":true}`, `{"title":"x","title":"y"}`, `{"title":`, `{} {}`} {
		var plan Plan
		if err := Decode(strings.NewReader(input), &plan); err == nil {
			t.Fatalf("malformed JSON accepted: %s", input)
		}
	}
	for _, argv := range [][]string{{"sh", "-c", "true"}, {"terraform", "apply"}, {"git", "push"}, {"npm", "publish"}, {"git", "-C", "path with spaces", "push"}, {"git", "-c", "core.pager=cat", "--git-dir=repository", "push"}, {"terraform", "-chdir=path with spaces", "apply"}, {"tofu", "-chdir", "path with spaces", "destroy"}} {
		if err := validateCommand(argv); err == nil {
			t.Fatalf("unsafe check accepted: %v", argv)
		}
	}
	for _, argv := range [][]string{{"terraform", "fmt", "-check"}, {"terraform", "validate"}, {"rojo", "build", "file with spaces.project.json"}, {"lune", "run", "validate"}, {"go", "test", "-run", "deploy"}, {"git", "-C", "push", "status", "--short"}, {"git", "-c", "core.pager=cat", "diff", "--", "push"}, {"terraform", "-chdir=path with spaces", "validate"}, {"terraform", "-chdir", "apply", "fmt", "-check"}, {"terraform", "test"}, {"terraform", "-version"}} {
		if err := validateCommand(argv); err != nil {
			t.Fatalf("normal local validation denied: %v %v", argv, err)
		}
	}
}

func TestExecutablePathWithSpaces(t *testing.T) {
	plan := helperPlan(t, "small", "pass", "an argument with spaces")
	source, err := os.Open(plan.Checks[0].Argv[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	path := filepath.Join(t.TempDir(), "test executable with spaces")
	target, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	plan.Checks[0].Argv[0] = path
	record, _ := fixtureTask(t, plan)
	if run, err := Run(t.Context(), record.Directory, "focused"); err != nil || run.Status != "passed" {
		t.Fatalf("spaced executable: %+v %v", run, err)
	}
}

func TestBranchChangeInvalidatesBoundEvidence(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	plan := helperPlan(t, "small", "pass")
	record, ref := fixtureTask(t, plan)
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", record.Baseline.Root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, output)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("add", ".")
	git("commit", "-qm", "baseline")
	var err error
	record, err = Start(t.Context(), record.Baseline.Root, filepath.Dir(record.Directory), plan)
	if err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	git("checkout", "-qb", "different")
	if result, err := Verify(t.Context(), record.Directory); err == nil || result.Complete {
		t.Fatalf("branch drift accepted: %+v %v", result, err)
	}
}
