package harness_test

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

const (
	journeyID       = "csv-delivery"
	journeyRequest  = "Fix the existing export patch so CSV fields with quotes round-trip. Verify and commit export.sh only."
	journeyCriteria = "A CSV field containing a quote with or without a comma must round-trip as one field. Commit only export.sh."
	journeyExpect   = "A field containing a quote and no comma round-trips as one CSV field"
	journeyFinding  = `major export.sh:2: sh export.sh 'x"y' emits a bare quote because only comma-bearing fields are quoted`
	journeyCommit   = "Fix CSV field quoting"
	journeyHostEnv  = "KITSU_TEST_JOURNEY_HOST_HISTORY"

	journeyExistingPatch = `case $1 in
  *,*) printf '"%s"\n' "$1" ;;
  *) printf '%s\n' "$1" ;;
esac
`
	journeyPartialRepair = `case $1 in
  *,*) escaped=$(printf '%s' "$1" | sed 's/"/""/g')
       printf '"%s"\n' "$escaped" ;;
  *) printf '%s\n' "$1" ;;
esac
`
	journeyFixedExport = `case $1 in
  *,*|*\"*) escaped=$(printf '%s' "$1" | sed 's/"/""/g')
            printf '"%s"\n' "$escaped" ;;
  *) printf '%s\n' "$1" ;;
esac
`
	journeyAssertion = `value=$(sh export.sh 'x"y') || exit
printf 'observed export: %s\n' "$value"
test "$value" = '"x""y"'
`
	journeyReadField = `IFS= read -r field || exit
exec sh export.sh "$field"
`
)

type journeyCandidate struct{ Tree, Raw, Index, Head string }

type journeyEvent struct {
	Action, Actor, Ticket, Note string
	Findings                    []string
	Candidate                   journeyCandidate
}

type journeyClaim struct {
	Owner, Ticket, Worker string
	Candidate             journeyCandidate
}

type journeyProof struct {
	Run       string `json:"run_id"`
	Expect    string `json:"expected_behavior"`
	Outcome   string
	Argv      []string
	Candidate journeyCandidate
}

type journeyLog struct {
	Run    string `json:"run_id"`
	Exit   int    `json:"exit_status"`
	Path   string `json:"path"`
	Digest string `json:"log_sha256"`
}

type journeyView struct {
	Task, Request, Next, Ticket, Delivery string
	Verified                              bool
	Scope                                 struct {
		Entry, Until   string
		Repair, Commit bool
	}
	Candidate    journeyCandidate
	Claim        *journeyClaim
	ClaimCreated bool              `json:"claim_created"`
	ReviewStarts int               `json:"review_starts"`
	Review       *journeyEvent     `json:"latest_review"`
	Last         *journeyEvent     `json:"last_result"`
	PriorNotes   map[string]string `json:"prior_notes"`
	Proof        *journeyProof     `json:"behavioral_assertion"`
	Log          *journeyLog       `json:"verification_log"`
	CommitTarget string            `json:"commit_target"`
}

type journeyResume struct {
	LastAction string
	HostWorker string
	View       journeyView
}

type journeyDelivery struct{ Head, Tree, Parents string }

type interruptedJourney struct {
	t                           *testing.T
	repo                        *kitsutest.Repo
	actor, history, testProgram string
	restarts                    int
}

func TestExistingPatchJourneySurvivesFreshCoordinator(t *testing.T) {
	var deliveries [2]journeyDelivery
	for i, name := range []string{"uninterrupted", "restarted"} {
		t.Run(name, func(t *testing.T) {
			journey, checkDeveloperWork := newInterruptedJourney(t, i == 1)
			deliveries[i] = journey.run()
			checkDeveloperWork()
		})
	}
	if !t.Failed() && deliveries[0] != deliveries[1] {
		t.Fatalf("delivery changed across restart: %+v", deliveries)
	}
}

func newInterruptedJourney(t *testing.T, restarted bool) (*interruptedJourney, func()) {
	t.Helper()
	developer := kitsutest.NewRepo(t)
	developer.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:00Z")
	developer.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")
	developer.Write(".gitignore", "build/\n")
	developer.Write("README.md", "# CSV export\n")
	developer.Write("export.sh", "printf '%s\\n' \"$1\"\n")
	developer.Write("assert-quote.sh", journeyAssertion)
	developer.Write("read-field.sh", journeyReadField)
	developer.Write("unit.sh", "test \"$(sh export.sh plain)\" = plain\n")
	developer.Write("lint.sh", "sh -n export.sh\n")
	developer.Write("kitsu.toml", strings.Replace(kitsutest.DemoDeclaration, "src/**", "export.sh", 1))
	developer.Git("add", "-A")
	developer.Git("commit", "-qm", "Start the CSV exporter")
	base := developer.Git("rev-parse", "HEAD")
	developer.Write("export.sh", journeyExistingPatch)
	developer.Write("README.md", "staged developer notes\n")
	developer.Git("add", "export.sh", "README.md")
	developer.Write("README.md", "unstaged developer notes\n")
	developerIndex := developer.Git("write-tree")
	developerStatus := developer.Git("status", "--porcelain")

	deliveryPath := filepath.Join(t.TempDir(), "delivery")
	developer.Git("worktree", "add", "-qb", "deliver-csv", deliveryPath)
	delivery := developer.In(deliveryPath)
	delivery.Write("export.sh", developer.Read("export.sh"))
	delivery.Git("add", "export.sh")
	delivery.Write("build/draft.txt", "unrelated local draft\n")
	j := &interruptedJourney{t: t, repo: delivery, actor: "initial-coordinator", history: t.TempDir()}
	if restarted {
		program, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		j.testProgram = program
		delivery.Setenv(journeyHostEnv, j.history)
		delivery.Setenv("GORACE", "atexit_sleep_ms=0")
	}
	return j, func() {
		if developer.Git("rev-parse", "HEAD") != base || developer.Git("write-tree") != developerIndex ||
			developer.Git("status", "--porcelain") != developerStatus || developer.Read("README.md") != "unstaged developer notes\n" ||
			developer.Read("export.sh") != journeyExistingPatch || delivery.Read("build/draft.txt") != "unrelated local draft\n" {
			t.Fatal("delivery changed the developer's branch, index or working files")
		}
	}
}

func (j *interruptedJourney) run() journeyDelivery {
	j.t.Helper()
	j.checkPublicCSV(`x"y`, `x,"y"`)
	if result := j.repo.Run("", "sh", "unit.sh"); result.Code != 0 {
		j.t.Fatal("the original repository check should miss the seeded defect", result.Output())
	}
	base := j.repo.Git("rev-parse", "HEAD")
	initial := j.call(0, "open", "--require-review", "--request", journeyRequest, "--entry", "verify", "--until", "deliver", "--repair", "--commit", "--review-limit", "2")
	if decoy := j.repo.Kitsu("task", "open", "another-task", "--until", "plan", "--request", "Plan the next export format"); decoy.Code != 0 {
		j.t.Fatal(decoy.Output())
	}
	intake := j.restart("intake", "", initial).View
	j.want(intake, "intake")
	verify := j.call(0, "record", "intake", "--actor", j.actor, "--ticket", intake.Ticket, "--note", journeyCriteria)
	j.want(verify, "verify")

	argv := []string{"sh", "assert-quote.sh"}
	failed := j.call(5, "test", append([]string{"--ticket", verify.Ticket, "--expect", journeyExpect, "--"}, argv...)...)
	failed = j.restart("failing assertion", "test", failed).View
	j.want(failed, "repair")
	if failed.Verified || failed.Proof == nil || failed.Proof.Outcome != "failed" || failed.Proof.Expect != journeyExpect ||
		!slices.Equal(failed.Proof.Argv, argv) || failed.Proof.Candidate != initial.Candidate || failed.Log == nil ||
		failed.Log.Run != failed.Proof.Run || failed.Log.Exit != 1 {
		j.t.Fatalf("the expected behavior failure was not saved: %+v", failed)
	}
	j.checkFailureLog(*failed.Log)

	repair := j.firstReview(failed)
	approved := j.secondReview(repair, failed.Proof.Run)
	j.want(approved, "verify")
	if approved.Proof == nil || approved.Proof.Run != failed.Proof.Run || approved.Proof.Candidate.Tree == approved.Candidate.Tree {
		j.t.Fatal("the repaired candidate lost its original failing assertion")
	}
	j.repo.Git("add", "export.sh")
	approved = j.call(0, "next")
	j.want(approved, "verify")
	retry := []string{"--ticket", approved.Ticket, "--retry"}
	verified := j.call(0, "test", retry...)
	verified = j.restart("accepted evidence", "test", verified).View
	j.want(verified, "commit")
	if !verified.Verified || verified.CommitTarget != "refs/heads/deliver-csv" || verified.Proof == nil ||
		verified.Proof.Run == failed.Proof.Run || verified.Proof.Outcome != "passed" || verified.Proof.Expect != failed.Proof.Expect ||
		!slices.Equal(verified.Proof.Argv, failed.Proof.Argv) || verified.Proof.Candidate != verified.Candidate {
		j.t.Fatalf("retry did not verify the saved assertion on the repaired candidate: %+v", verified)
	}
	if j.testProgram != "" {
		j.want(j.call(0, "test", retry...), "commit")
	}

	commit := []string{"--ticket", verified.Ticket, "--message", journeyCommit}
	j.call(1, "commit", commit...)
	if j.repo.Git("rev-parse", "HEAD") != base || j.repo.Git("write-tree") != verified.Candidate.Tree {
		j.t.Fatal("the first commit must require explicit inclusion of the patch and leave Git unchanged")
	}
	commit = append(commit, "--include", "export.sh")
	completed := j.call(0, "commit", commit...)
	// The restarted coordinator discards the commit response and rediscovers delivery from disk.
	completed = j.restart("commit before acknowledgement", "commit", completed).View
	j.want(completed, "done")
	if completed.Ticket != "" || !completed.Verified || completed.Delivery != "committed" {
		j.t.Fatalf("delivery did not survive: %+v", completed)
	}
	if j.testProgram != "" {
		j.want(j.call(0, "commit", commit...), "done")
	}
	j.checkFailureLog(*failed.Log)
	j.checkHistory(failed.Proof.Run)
	return j.checkDelivery(base, verified.Candidate.Tree)
}

func (j *interruptedJourney) firstReview(failed journeyView) journeyView {
	j.t.Helper()
	j.repo.Write("export.sh", journeyPartialRepair)
	j.checkPublicCSV(`x"y`)
	partial := []string{
		"repair", "--actor", j.actor, "--ticket", failed.Ticket,
		"--note", "Escape embedded quotes in comma-bearing fields", "--claim-review", "codex",
	}
	reserved := j.call(0, "record", partial...)
	worker := j.launchWorker(reserved)
	// Native dispatch succeeded, but the coordinator has yet to link its ID.
	recovered := j.restart("review reserved before worker link", "repair", reserved)
	reserved = recovered.View
	if j.testProgram != "" {
		if recovered.HostWorker != worker {
			j.t.Fatal("unlinked claim needs the original native host history", recovered.HostWorker)
		}
		worker = recovered.HostWorker
		replayed := j.call(0, "record", partial...)
		if replayed.ClaimCreated || !reflect.DeepEqual(replayed.Claim, reserved.Claim) {
			j.t.Fatal("replayed repair started or replaced a reservation")
		}
		j.call(1, "claim", "--ticket", reserved.Ticket, "--actor", j.actor, "--host", "codex")
	}
	linked := j.linkWorker(reserved, worker)
	linked = j.restart("linked review worker", "repair", linked).View
	if linked.Claim == nil || linked.Claim.Worker != worker {
		j.t.Fatal("fresh coordinator lost the linked worker", linked.Claim)
	}
	j.checkReviewInput(linked, "Escape embedded quotes in comma-bearing fields", failed.Proof.Run, nil)
	finishNativeFixture(j.t, j.repo, journeyID, linked.Ticket, "codex", linked.Claim.Owner, worker,
		"Comma fields round-trip; an isolated quote still fails", journeyFinding)
	review := []string{"review", "--ticket", linked.Ticket, "--observed-review"}
	result := j.call(0, "record", review...)
	result = j.restart("recorded review finding", "review", result).View
	j.want(result, "repair")
	if result.Review == nil || result.Review.Actor != worker || !slices.Equal(result.Review.Findings, []string{journeyFinding}) {
		j.t.Fatal("repair lost the independent review finding", result.Review)
	}
	if j.testProgram != "" {
		if replay := j.call(0, "record", review...); replay.Ticket != result.Ticket {
			j.t.Fatal("replaying review changed the repair ticket")
		}
	}
	return result
}

func (j *interruptedJourney) secondReview(repair journeyView, failedRun string) journeyView {
	j.t.Helper()
	const note = "Quote fields with an embedded quote even when there is no comma"
	j.repo.Write("export.sh", journeyFixedExport)
	j.checkPublicCSV()
	// The repair ticket survives a restart after editing and before recording the result.
	edited := j.call(0, "next")
	edited = j.restart("working repair before record", "review", edited).View
	j.want(edited, "repair")
	if edited.Ticket != repair.Ticket {
		j.t.Fatal("editing the repair lost its ticket")
	}
	changed := j.call(0, "record", "repair", "--actor", j.actor, "--ticket", edited.Ticket, "--note", note)
	changed = j.restart("recorded repair", "repair", changed).View
	j.want(changed, "review")
	if changed.Review == nil || changed.Candidate.Tree == changed.Review.Candidate.Tree ||
		changed.Candidate.Index == changed.Candidate.Tree || changed.Claim != nil {
		j.t.Fatal("the worktree repair should need a new review and remain unstaged")
	}
	reserved := j.call(0, "claim", "--ticket", changed.Ticket, "--actor", j.actor, "--host", "codex")
	linked := j.linkWorker(reserved, j.launchWorker(reserved))
	j.checkReviewInput(linked, note, failedRun, []string{journeyFinding})
	j.call(1, "record", "review", "--ticket", linked.Ticket, "--actor", "reviewer-1", "--note", "Stale worker")
	finishNativeFixture(j.t, j.repo, journeyID, linked.Ticket, "codex", j.actor, linked.Claim.Worker, "All CSV round-trip cases passed")
	review := []string{"review", "--ticket", linked.Ticket, "--observed-review"}
	approved := j.call(0, "record", review...)
	if j.testProgram != "" {
		if replay := j.call(0, "record", review...); replay.Ticket != approved.Ticket {
			j.t.Fatal("replaying approval changed the verification ticket")
		}
	}
	return approved
}

func (j *interruptedJourney) checkPublicCSV(wantFailures ...string) {
	j.t.Helper()
	var failures []string
	for _, value := range []string{"plain", "x,y", `x"y`, `x,"y"`} {
		// Let the shell pass quoted fields to the exporter, including on Windows.
		result := j.repo.Run(value+"\n", "sh", "read-field.sh")
		if result.Code != 0 {
			j.t.Fatalf("public export command for %q exited %d: %s", value, result.Code, result.Output())
		}
		rows, err := csv.NewReader(strings.NewReader(result.Stdout)).ReadAll()
		if err != nil || len(rows) != 1 || !slices.Equal(rows[0], []string{value}) {
			failures = append(failures, value)
		}
	}
	if !slices.Equal(failures, wantFailures) {
		j.t.Fatalf("public CSV round-trip failed on %q, want %q", failures, wantFailures)
	}
}

func (j *interruptedJourney) checkReviewInput(view journeyView, repair, failedRun string, findings []string) {
	j.t.Helper()
	var input struct {
		Task, Request, Intake string
		Round                 int              `json:"review_round"`
		Repair                string           `json:"reported_repair"`
		Candidate             journeyCandidate `json:"reserved_candidate"`
		Findings              []string         `json:"prior_findings"`
		Failure               journeyProof     `json:"prior_assertion"`
	}
	result := j.repo.Kitsu("task", "show", journeyID, "--review-ticket", view.Ticket)
	if err := json.Unmarshal([]byte(result.Stdout), &input); err != nil || result.Code != 0 || input.Task != journeyID ||
		input.Request != journeyRequest || input.Intake != journeyCriteria || input.Repair != repair || input.Round != view.ReviewStarts ||
		input.Candidate != view.Candidate || input.Failure.Run != failedRun || !slices.Equal(input.Findings, findings) {
		j.t.Fatalf("reviewer cannot recover its input: %v\n%s", err, result.Output())
	}
}

func (j *interruptedJourney) checkFailureLog(log journeyLog) {
	j.t.Helper()
	data, err := os.ReadFile(log.Path)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != log.Digest || !strings.Contains(string(data), `observed export: x"y`) {
		j.t.Fatalf("original failing log changed: %v\n%s", err, data)
	}
}

func (j *interruptedJourney) checkHistory(failedRun string) {
	j.t.Helper()
	var history struct {
		Harness journeyView
		Events  []journeyEvent
	}
	result := j.repo.Kitsu("task", "show", journeyID, "--json")
	if err := json.Unmarshal([]byte(result.Stdout), &history); result.Code != 0 || err != nil {
		j.t.Fatalf("task history unavailable: %v\n%s", err, result.Output())
	}
	var actions []string
	for _, event := range history.Events {
		actions = append(actions, event.Action)
	}
	want := []string{"intake", "test", "repair", "review", "repair", "review", "test", "commit"}
	if !slices.Equal(actions, want) || history.Harness.ReviewStarts != 2 || history.Events[3].Actor != "reviewer-1" ||
		history.Events[5].Actor != "reviewer-2" || !slices.Equal(history.Events[3].Findings, []string{journeyFinding}) ||
		!strings.Contains(history.Events[1].Note, journeyExpect+" (failed), run "+failedRun) {
		j.t.Fatalf("recovery lost evidence or duplicated actions: %s", result.Output())
	}
	workers, err := os.ReadDir(j.history)
	if err != nil || len(workers) != 2 || len(j.repo.RecordFiles("task-"+journeyID)) != 2 ||
		len(j.repo.RecordFiles("unit")) != 1 || len(j.repo.RecordFiles("lint")) != 1 {
		j.t.Fatal("expected two native worker launches, two assertions and one run of each declared check", err)
	}
}

func (j *interruptedJourney) checkDelivery(base, tree string) journeyDelivery {
	j.t.Helper()
	r := j.repo
	delivered := journeyDelivery{
		Head: r.Git("rev-parse", "HEAD"), Tree: r.Git("rev-parse", "HEAD^{tree}"), Parents: r.Git("show", "-s", "--format=%P"),
	}
	if delivered.Parents != base || delivered.Tree != tree || r.Git("rev-list", "--count", base+"..HEAD") != "1" ||
		r.Git("diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD") != "export.sh" ||
		r.Git("write-tree") != tree || r.Git("status", "--porcelain") != "" {
		j.t.Fatalf("unexpected delivered Git state: %+v", delivered)
	}
	j.checkPublicCSV()
	return delivered
}

func (j *interruptedJourney) call(code int, command string, args ...string) journeyView {
	j.t.Helper()
	argv := append([]string{"task", command, journeyID, "--json"}, args...)
	result := j.repo.Kitsu(argv...)
	if result.Code != code {
		j.t.Fatalf("kitsu %v exited %d, want %d\n%s", argv, result.Code, code, result.Output())
	}
	var view journeyView
	if code == 0 || code == 5 {
		if err := json.Unmarshal([]byte(result.Stdout), &view); err != nil {
			j.t.Fatalf("kitsu %v returned invalid JSON: %v\n%s", argv, err, result.Output())
		}
	}
	return view
}

func (j *interruptedJourney) want(view journeyView, next string) {
	j.t.Helper()
	if view.Next != next || (view.Ticket == "") != (next == "done") {
		j.t.Fatalf("next is %q with ticket %q, want %q", view.Next, view.Ticket, next)
	}
}

func (j *interruptedJourney) launchWorker(view journeyView) string {
	j.t.Helper()
	j.want(view, "review")
	if !view.ClaimCreated || view.Claim == nil || view.Claim.Owner != j.actor || view.Claim.Ticket != view.Ticket || view.Claim.Worker != "" {
		j.t.Fatal("a worker can start only on a new reservation by its coordinator", view.Claim)
	}
	worker := fmt.Sprintf("reviewer-%d", view.ReviewStarts)
	path := journeyWorkerHistory(j.history, view.Task, view.ReviewStarts)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		j.t.Fatal("duplicate native worker launch", err)
	}
	_, writeErr := file.WriteString(worker)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		j.t.Fatal(writeErr, closeErr)
	}
	sendNativeReviewEvent(j.t, j.repo, "codex", "SubagentStart", j.actor, worker, "")
	return worker
}

func (j *interruptedJourney) linkWorker(view journeyView, worker string) journeyView {
	j.t.Helper()
	linked := j.call(0, "claim", "--ticket", view.Ticket, "--actor", view.Claim.Owner, "--worker", worker)
	if linked.ClaimCreated || linked.Claim == nil || linked.Claim.Owner != view.Claim.Owner ||
		linked.Claim.Ticket != view.Ticket || linked.Claim.Worker != worker || linked.ReviewStarts != view.ReviewStarts {
		j.t.Fatal("linking the worker must preserve its existing reservation", linked.Claim)
	}
	return linked
}

func (j *interruptedJourney) restart(boundary, lastAction string, want journeyView) journeyResume {
	j.t.Helper()
	if j.testProgram == "" {
		return journeyResume{View: want}
	}
	result := j.repo.Run("", j.testProgram, "-test.run=^TestInterruptedJourneyCoordinator$", "-test.timeout=30s")
	var recovered journeyResume
	if err := json.Unmarshal([]byte(result.Stdout), &recovered); err != nil || result.Code != 0 {
		j.t.Fatalf("restart at %s failed: %v\n%s", boundary, err, result.Output())
	}
	got := recovered.View
	if recovered.LastAction != lastAction || got.Task != journeyID || got.Request != journeyRequest || got.Scope != want.Scope ||
		got.Next != want.Next || got.Ticket != want.Ticket || got.Candidate != want.Candidate || got.Verified != want.Verified ||
		got.Delivery != want.Delivery || got.ClaimCreated || got.ReviewStarts != want.ReviewStarts ||
		!reflect.DeepEqual(got.Claim, want.Claim) || !reflect.DeepEqual(got.Last, want.Last) ||
		!reflect.DeepEqual(got.Review, want.Review) || !reflect.DeepEqual(got.Proof, want.Proof) {
		j.t.Fatalf("task state changed at restart after %s: %s", boundary, result.Output())
	}
	if lastAction != "" && got.PriorNotes["intake"] != journeyCriteria {
		j.t.Fatalf("original criteria missing at restart after %s: %s", boundary, result.Output())
	}
	j.restarts++
	j.actor = fmt.Sprintf("resumed-coordinator-%d", j.restarts)
	return recovered
}

func journeyWorkerHistory(root, task string, round int) string {
	return filepath.Join(root, fmt.Sprintf("%s-review-%d", task, round))
}

// The restarted coordinator gets only its checkout and the simulated host history.
// It rediscovers the task by request without any previous tickets or receipts.
func TestInterruptedJourneyCoordinator(t *testing.T) {
	history, ok := os.LookupEnv(journeyHostEnv)
	if !ok {
		return
	}
	call := func(target any, args ...string) {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "kitsu", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("kitsu %v: %v\n%s", args, err, out)
		}
		if err := json.Unmarshal(out, target); err != nil {
			t.Fatalf("kitsu %v: %v\n%s", args, err, out)
		}
	}
	var list struct {
		Tasks []struct {
			Task  string
			Daily struct {
				Request    string
				LastAction string `json:"last_action"`
			}
		}
	}
	call(&list, "task", "list", "--json")
	for _, task := range list.Tasks {
		if task.Daily.Request != journeyRequest {
			continue
		}
		resume := journeyResume{LastAction: task.Daily.LastAction}
		call(&resume.View, "task", "next", task.Task, "--json")
		if claim := resume.View.Claim; claim != nil && claim.Worker == "" {
			// A native session may exist even when Kitsu has only the reservation.
			path := journeyWorkerHistory(history, task.Task, resume.View.ReviewStarts)
			worker, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			resume.HostWorker = string(worker)
		}
		if err := json.NewEncoder(os.Stdout).Encode(resume); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	t.Fatal("could not rediscover the task by the original request")
}
