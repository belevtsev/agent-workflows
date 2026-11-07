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
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCheckSubprocess acts as the check helper only in a re-executed helper
// child, whose arguments hold "--" followed by a mode. TestMain sets
// CW_TASKFLOW_HELPER for the whole test process, so in a normal test run (no
// "--") the variable alone must not turn this test into the helper.
func TestCheckSubprocess(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if os.Getenv("CW_TASKFLOW_HELPER") != "1" || separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	args := os.Args[separator+1:]
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
		if len(args) != 4 {
			os.Exit(25)
		}
		lifetimeDeadline, err := time.Parse(time.RFC3339Nano, args[3])
		if err != nil || !lifetimeDeadline.After(time.Now()) {
			os.Exit(25)
		}
		dialer := net.Dialer{Timeout: 10 * time.Second, Deadline: lifetimeDeadline}
		connection, err := dialer.Dial("tcp", args[1])
		if err != nil {
			os.Exit(21)
		}
		defer connection.Close()
		readinessDeadline := time.Now().Add(15 * time.Second)
		if lifetimeDeadline.Before(readinessDeadline) {
			readinessDeadline = lifetimeDeadline
		}
		if err := connection.SetWriteDeadline(readinessDeadline); err != nil {
			connection.Close()
			os.Exit(22)
		}
		if _, err := io.WriteString(connection, args[2]); err != nil {
			connection.Close()
			os.Exit(23)
		}
		if err := connection.SetReadDeadline(lifetimeDeadline); err != nil {
			connection.Close()
			os.Exit(22)
		}
		var release [1]byte
		if _, err := io.ReadFull(connection, release[:]); err != nil {
			connection.Close()
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
	case "storm-leader":
		os.Exit(pgStormLeader(args[1:]))
	case "storm-forker":
		os.Exit(pgStormForker(args[1:]))
	default:
		os.Exit(14)
	}
	os.Exit(0)
}

const (
	// pgStormDescendantScript is one short-lived storm-forker descendant. If
	// its forker ($1) is still alive after a short wait it exits at once.
	// Otherwise it waits up to three seconds for the test to report that Run
	// returned ($2) and then creates its unique marker ($3). Only a process
	// that outlived the return of Run can create a marker.
	pgStormDescendantScript = `sleep 0.2
kill -0 "$1" 2>/dev/null && exit 0
i=0
while [ "$i" -lt 30 ]; do
	if [ -e "$2" ]; then : > "$3"; exit 0; fi
	sleep 0.1
	i=$((i + 1))
done`
	// The storm is bounded: at most pgStormForks descendants, at most
	// pgStormConcurrency alive at once, forking for at most pgStormDuration.
	pgStormForks       = 1000
	pgStormConcurrency = 64
	pgStormDuration    = 3 * time.Second
	// pgStormRunningAfter descendants started confirm the storm is running.
	pgStormRunningAfter = 8
	// pgStormReadiness bounds how long the leader waits for the forker.
	pgStormReadiness = 8 * time.Second
)

// pgStormLeader is the check command of a storm trial. Its arguments are the
// trial directory and the survivor-marker directory. It records its pid in
// the trial directory, starts one storm forker in the background with all
// standard streams on /dev/null, waits until the forker reports it is forking
// and exits 0 while the forker keeps forking.
func pgStormLeader(args []string) int {
	if len(args) != 2 {
		return 30
	}
	trial, survivors := args[0], args[1]
	if err := os.WriteFile(filepath.Join(trial, "leader-pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		return 31
	}
	executable, err := os.Executable()
	if err != nil {
		return 32
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 33
	}
	defer null.Close()
	forker := exec.Command(executable, "-test.run=^TestCheckSubprocess$", "--", "storm-forker", trial, survivors)
	forker.Env = os.Environ()
	forker.Stdin = null
	forker.Stdout = null
	forker.Stderr = null
	if err := forker.Start(); err != nil {
		return 34
	}
	forker.Process.Release()
	running := filepath.Join(trial, "running")
	deadline := time.Now().Add(pgStormReadiness)
	for {
		if _, err := os.Stat(running); err == nil {
			return 0
		}
		if time.Now().After(deadline) {
			return 35
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pgStormForker keeps forking short-lived descendants within fixed bounds, so
// the end of its check is likely to arrive while a fork is in progress. It
// reports itself running after pgStormRunningAfter descendants have started.
// Left alone it stops forking after pgStormDuration and reaps every
// descendant.
func pgStormForker(args []string) int {
	if len(args) != 2 {
		return 36
	}
	trial, survivors := args[0], args[1]
	self := strconv.Itoa(os.Getpid())
	returned := filepath.Join(trial, "returned")
	// The check environment may omit PATH; the descendant script needs sleep.
	env := append(os.Environ(), "PATH=/usr/bin:/bin")
	slots := make(chan struct{}, pgStormConcurrency)
	var running sync.WaitGroup
	defer running.Wait()
	stop := time.Now().Add(pgStormDuration)
	for index := 0; index < pgStormForks && time.Now().Before(stop); index++ {
		slots <- struct{}{}
		marker := filepath.Join(survivors, fmt.Sprintf("%s-%d", filepath.Base(trial), index))
		child := exec.Command("/bin/sh", "-c", pgStormDescendantScript, "sh", self, returned, marker)
		child.Env = env
		if err := child.Start(); err != nil {
			<-slots
			break
		}
		running.Add(1)
		go func() {
			defer running.Done()
			_ = child.Wait()
			<-slots
		}()
		if index+1 == pgStormRunningAfter {
			if err := os.WriteFile(filepath.Join(trial, "running"), []byte("forking\n"), 0600); err != nil {
				return 37
			}
		}
	}
	return 0
}

// helperPlan needs no per-test environment: TestMain sets the helper
// variables once for the whole test process, so parallel tests may use it.
func helperPlan(t *testing.T, kind string, args ...string) Plan {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argv := append([]string{executable, "-test.run=^TestCheckSubprocess$", "--"}, args...)
	return Plan{Title: "Source-bound example", Kind: kind, Requirements: []Requirement{{ID: "behavior", Description: "Verified source behavior", CheckIDs: []string{"focused"}}}, Checks: []Check{{ID: "focused", Argv: argv, TimeoutSeconds: 10}}}
}

// fixtureTask starts a task bound to the shipped default advisers (D5).
func fixtureTask(t *testing.T, plan Plan) (Record, SourceRef) {
	t.Helper()
	return fixtureTaskBound(t, plan, fixtureBinding())
}

// fixtureTaskBound starts a task with an explicit adviser binding (D5).
func fixtureTaskBound(t *testing.T, plan Plan, advisers map[string]string) (Record, SourceRef) {
	t.Helper()
	repo := t.TempDir()
	data := []byte("first line\nsecond line\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	record, err := Start(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), plan, advisers)
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
				record, err = Start(t.Context(), record.Baseline.Root, filepath.Dir(record.Directory), plan, record.Advisers)
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
	if err != nil || !verified.Complete {
		t.Fatalf("unavailability fallback: %+v %v", verified, err)
	}
	// D5 verify limitations: every bound provider was unavailable at both checkpoints.
	for _, provider := range []string{"subq_snap", "openai_decisions"} {
		if !limitationNaming(verified.Limitations, provider, "unavailable") {
			t.Fatalf("limitations omit %s unavailability: %q", provider, verified.Limitations)
		}
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
	if !reflect.DeepEqual(stored.Contexts["before"], before) || !reflect.DeepEqual(stored.Consultations["before"].Context, before) {
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

// D3/D4 via consult.ValidateRecord at attach: removed v3 formats and forged
// records are rejected; valid v4 records bound to another task, phase or
// source are rejected by the context binding; a pending record attaches but
// cannot open the final context.
func TestConsultationRejectsRemovedForgedAndCrossBoundRecords(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	bound := record.Contexts["before"]
	forge := func(edit func(*testing.T, *fixtureObject)) func(*checkpointDraft) {
		return func(draft *checkpointDraft) { draft.Forge = edit }
	}
	invalid := map[string]func(*checkpointDraft){
		"disabled_status": forge(func(t *testing.T, record *fixtureObject) {
			record.object(t, "providers", "subq_snap").set("status", "disabled")
		}),
		"missing_credential_category": withOutcomes(map[string]adviserOutcome{"subq_snap": {Status: "unavailable", Category: "missing_credential"}, "openai_decisions": {Status: "unavailable"}}),
		"version_3_record":            forge(func(_ *testing.T, record *fixtureObject) { record.set("version", 3) }),
		"forged_record_status":        forge(func(_ *testing.T, record *fixtureObject) { record.set("status", "success") }),
		"forged_comparison": forge(func(t *testing.T, record *fixtureObject) {
			record.object(t, "comparison", "claim").set("status", "agreement")
		}),
		"request_changed_after_fingerprint": forge(func(t *testing.T, record *fixtureObject) {
			record.object(t, "request", "questions", "claim").set("instructions", "Does the bound source refute the claim?")
		}),
		"unsupported_model": withOutcomes(map[string]adviserOutcome{"subq_snap": {Status: "unavailable"}, "openai_decisions": {Status: "unavailable", Model: "gpt-unknown"}}),
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			path, verr := writeUncheckedConsultation(t, bound, mutate)
			if verr == nil {
				t.Errorf("consult.ValidateRecord accepted %s", name)
			}
			if name == "version_3_record" && (verr == nil || !strings.Contains(verr.Error(), "consultation record version 3 is not supported; this cw reads version 4")) {
				t.Errorf("version 3 record not refused with the D3 message: %v", verr)
			}
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
				t.Fatal("invalid consultation accepted")
			}
		})
	}
	crossBound := map[string]func(Context) Context{
		"cross_task":   func(context Context) Context { context.TaskID = "different"; return context },
		"cross_phase":  func(context Context) Context { context.Phase = "final"; return context },
		"stale_source": func(context Context) Context { context.Snapshot.Fingerprint = "different"; return context },
	}
	for name, change := range crossBound {
		t.Run(name, func(t *testing.T) {
			path := writeConsultation(t, change(bound), nil)
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
				t.Fatal("record bound to another context accepted")
			}
		})
	}
	pending := writeConsultation(t, bound, func(draft *checkpointDraft) { draft.Pending = true })
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", pending); err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil {
		t.Fatal("pending before decision accepted")
	}
}

// Revision 2: a batched request attaches only when exactly one context matches
// the task and every context carries the same adviser binding.
func TestMultiContextBatchHasExactlyOneMatchingBinding(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	bound := record.Contexts["before"]
	other := bound
	other.TaskID = "another-task"
	batch := func(contexts ...map[string]jsontext.Value) func(*checkpointDraft) {
		items := make([]any, 0, len(contexts))
		for _, context := range contexts {
			items = append(items, context)
		}
		return func(draft *checkpointDraft) { draft.State = fixtureObj("task_contexts", items) }
	}
	differentBinding := fixtureContextFields(t, other)
	differentBinding["advisers"] = fixtureMarshal(t, map[string]string{"jev": "jev-1.14.0"})
	unbound := fixtureContextFields(t, other)
	delete(unbound, "advisers")
	rejected := map[string]func(*checkpointDraft){
		"duplicate_matching_context": batch(fixtureContextFields(t, other), fixtureContextFields(t, bound), fixtureContextFields(t, bound)),
		"different_bindings":         batch(differentBinding, fixtureContextFields(t, bound)),
		"context_without_binding":    batch(unbound, fixtureContextFields(t, bound)),
		"no_matching_context":        batch(fixtureContextFields(t, other)),
	}
	for name, mutate := range rejected {
		t.Run(name, func(t *testing.T) {
			path, _ := writeUncheckedConsultation(t, bound, mutate)
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
				t.Fatal("invalid batch accepted")
			}
		})
	}
	// Accepted last, so no refusal above is explained by an attached checkpoint.
	attachCheckpoint(t, record, "before", bound, batch(fixtureContextFields(t, other), fixtureContextFields(t, bound)))
}

// D3/D4 via consult.ValidateRecord: an answered record (success with a
// disagreement, OpenAI partial with a refused question) attaches only when
// provider details, comparison and the decision's judgments all revalidate.
func TestAttachRevalidatesAnsweredRecords(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	bound := record.Contexts["before"]
	answered := func(forge func(*testing.T, *fixtureObject), item func(*decisionItem)) func(*checkpointDraft) {
		return func(draft *checkpointDraft) {
			draft.Questions = []string{"claim", "other"}
			draft.Outcomes = map[string]adviserOutcome{
				"subq_snap":        {Status: "success", Choices: map[string]string{"claim": "contradicted", "other": "supported"}},
				"openai_decisions": {Status: "partial", Choices: map[string]string{"claim": "supported"}},
			}
			claim := fixtureResolvedItem()
			claim.Rejected = map[string]string{"subq_snap": "The contrasting choice omits the storage step on line 1."}
			claim.Investigations = map[string]string{"subq_snap": "Re-read lines 1-2; storage precedes the acknowledgement."}
			if item != nil {
				item(&claim)
			}
			draft.Items = map[string]decisionItem{"claim": claim, "other": fixtureResolvedItem()}
			draft.Forge = forge
		}
	}
	invalid := map[string]func(*checkpointDraft){
		"missing_rejection": answered(nil, func(item *decisionItem) {
			item.Rejected = map[string]string{}
			item.Investigations = map[string]string{}
		}),
		"missing_investigation": answered(nil, func(item *decisionItem) { item.Investigations = map[string]string{} }),
		"resolved_model": answered(func(t *testing.T, record *fixtureObject) {
			record.object(t, "providers", "subq_snap").set("resolved_model", "subq-snap-other")
		}, nil),
		"distribution": answered(func(t *testing.T, record *fixtureObject) {
			record.object(t, "providers", "subq_snap", "result", "answers", "claim").set("probabilities", fixtureObj("supported", 0.5, "contradicted", 0.1, "insufficient_evidence", 0))
		}, nil),
		"refusal_with_choice": answered(func(t *testing.T, record *fixtureObject) {
			record.object(t, "providers", "openai_decisions", "result", "answers").set("other", fixtureObj("type", "refusal", "choice", "supported"))
		}, nil),
		"forged_comparison": answered(func(t *testing.T, record *fixtureObject) {
			record.object(t, "comparison", "claim").set("status", "agreement")
		}, nil),
		"forged_status": answered(func(_ *testing.T, record *fixtureObject) { record.set("status", "success") }, nil),
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			path, verr := writeUncheckedConsultation(t, bound, mutate)
			if verr == nil {
				t.Errorf("consult.ValidateRecord accepted %s", name)
			}
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
				t.Fatalf("invalid %s accepted", name)
			}
		})
	}
	attachCheckpoint(t, record, "before", bound, answered(nil, nil))
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
	// The truncated bytes begin with a version-1 member but never decode, so
	// this is malformed history rather than an unsupported version (D5).
	if _, err := Load(record.Directory); err == nil || strings.Contains(err.Error(), "this cw reads version 3 records") {
		t.Fatalf("truncated published revision ignored or misreported as an unsupported version: %v", err)
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
	if _, err := Start(t.Context(), repo, filepath.Join(repo, "records"), plan, fixtureBinding()); err == nil {
		t.Fatal("in-repo storage accepted")
	}
	parent := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := Start(t.Context(), repo, link, plan, fixtureBinding()); err == nil {
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
	if _, err := Start(t.Context(), t.TempDir(), store, plan, fixtureBinding()); err == nil || !strings.Contains(err.Error(), "initial task revision exceeds") {
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
	for _, argv := range [][]string{{"sh", "-c", "true"}, {"terraform", "apply"}, {"git", "push"}, {"npm", "publish"}, {"git", "-C", "path with spaces", "push"}, {"git", "-c", "core.pager=cat", "--git-dir=repository", "push"}, {"git", "-c", "core.pager=cat", "diff", "--", "push"}, {"terraform", "-chdir=path with spaces", "apply"}, {"tofu", "-chdir", "path with spaces", "destroy"}} {
		if err := validateCommand(argv); err == nil {
			t.Fatalf("unsafe check accepted: %v", argv)
		}
	}
	for _, argv := range [][]string{{"terraform", "fmt", "-check"}, {"terraform", "validate"}, {"rojo", "build", "file with spaces.project.json"}, {"lune", "run", "validate"}, {"go", "test", "-run", "deploy"}, {"git", "-C", "push", "status", "--short"}, {"git", "-c", "core.quotePath=false", "diff", "--", "push"}, {"terraform", "-chdir=path with spaces", "validate"}, {"terraform", "-chdir", "apply", "fmt", "-check"}, {"terraform", "test"}, {"terraform", "-version"}} {
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
	record, err = Start(t.Context(), record.Baseline.Root, filepath.Dir(record.Directory), plan, record.Advisers)
	if err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	git("checkout", "-qb", "different")
	if result, err := Verify(t.Context(), record.Directory); err == nil || result.Complete {
		t.Fatalf("branch drift accepted: %+v %v", result, err)
	}
}
