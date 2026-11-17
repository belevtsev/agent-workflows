package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

func verificationTaskView(t *testing.T, r *kitsutest.Repo) uxTaskView {
	t.Helper()
	result := r.Kitsu("task", "show", "application", "--json")
	if result.Code != 0 {
		t.Fatal(result.Output())
	}
	var view uxTaskView
	if err := json.Unmarshal([]byte(result.Stdout), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestTaskVerificationFreshness(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*kitsutest.Repo, taskVerificationReport)
	}{
		{"source", func(r *kitsutest.Repo, _ taskVerificationReport) {
			r.Write("app/config.json", `{"defect":"","revision":2}`)
		}},
		{"policy", func(r *kitsutest.Repo, _ taskVerificationReport) {
			r.Write("kitsu.toml", strings.ReplaceAll(r.Read("kitsu.toml"), "preserve HTTP response version 1", "preserve the original response contract"))
		}},
		{"missing log", func(r *kitsutest.Repo, report taskVerificationReport) {
			r.Remove(".git/kitsu/logs/" + report.Verification.Results[0].RunID + ".log")
		}},
		{"changed log", func(r *kitsutest.Repo, report taskVerificationReport) {
			r.Write(".git/kitsu/logs/"+report.Verification.Results[0].RunID+".log", "forged pass\n")
		}},
		{"missing observation", func(r *kitsutest.Repo, _ taskVerificationReport) {
			r.Remove(".git/kitsu-stages/verification-application.json")
		}},
		{"corrupt observation", func(r *kitsutest.Repo, _ taskVerificationReport) {
			r.Write(".git/kitsu-stages/verification-application.json", `{"version":1,"version":1}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := applicationFixture(t, "")
			report := applicationReport(t, applicationVerify(r, "--from", "HEAD"), 0)
			page := compactResult(t, r.Kitsu("task", "verify", "application", "--compact"), 0)
			tc.change(r, report)
			view := verificationTaskView(t, r)
			if view.Verification == nil || view.Verification.Ready || len(view.Verification.Missing) == 0 {
				t.Fatal("stale or missing evidence reported ready", view)
			}
			if stale := r.Kitsu("context", "show", page.ID); stale.Code == 0 {
				t.Fatal("stale compact verification accepted", stale.Output())
			}
			if structural := r.Kitsu("task", "check", "application"); structural.Code != 0 {
				t.Fatal("structural check required live local evidence", structural.Output())
			}
			refreshed := applicationReport(t, applicationVerify(r), 0)
			if !refreshed.Verification.Ready || refreshed.Executed == 0 {
				t.Fatal("missing binding was not re-established through execution", refreshed)
			}
		})
	}
}

func TestTaskVerificationOriginalSkillFreshness(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	skill := filepath.Join(r.Home, ".agents", "skills", "verify", "SKILL.md")
	writeSkillFixture(t, skill, "verify")
	if result := r.Kitsu("skill", "attach", "codex:user:verify", "--role", "quality-engineer", "--host", "codex"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	first := applicationReport(t, applicationVerify(r, "--from", "HEAD"), 0)
	before := r.Git("status", "--porcelain")
	writeSkillFixture(t, skill, "verify-updated")
	if view := verificationTaskView(t, r); view.Verification.Ready {
		t.Fatal("changed original personal skill was invisible")
	}
	if r.Git("status", "--porcelain") != before {
		t.Fatal("reading skill freshness modified tracked state")
	}
	second := applicationReport(t, applicationVerify(r), 0)
	if !second.Verification.Ready || second.Executed == 0 || second.Verification.AttemptID == first.Verification.AttemptID {
		t.Fatal("original skill was not re-observed", second)
	}
	history, err := verification.Load(r.Dir, "application")
	if err != nil || len(history.Attempt.Selections) != 1 {
		t.Fatal("selected original skill receipt missing", history, err)
	}
}

func TestTaskVerificationRecordReplacement(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	first := applicationReport(t, applicationVerify(r, "--from", "HEAD"), 0)
	if result := r.Kitsu("check", "run", "http-contract", "--force"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	view := verificationTaskView(t, r)
	if view.Verification.Ready || view.Verification.Results[0].RunID == first.Verification.Results[0].RunID {
		t.Fatal("replacement Git-only run inherited private raw observation", view)
	}
	replacement := view.Verification.Results[0].RunID
	refreshed := applicationReport(t, applicationVerify(r), 0)
	if !refreshed.Verification.Ready || refreshed.Executed == 0 || refreshed.Verification.Results[0].RunID == replacement {
		t.Fatal("replacement record was certified without execution", refreshed)
	}
}

func TestVerificationCleanFilterChild(t *testing.T) {
	if os.Getenv("KITSU_TEST_VERIFICATION_FILTER") != "1" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		os.Exit(1)
	}
	fmt.Println("stable filtered fixture")
	os.Exit(0)
}

func TestTaskVerificationMixedHiddenSourceChange(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r.Setenv("KITSU_TEST_VERIFICATION_FILTER", "1")
	filter := "'" + strings.ReplaceAll(filepath.ToSlash(program), "'", "'\\''") + "' -test.run=^TestVerificationCleanFilterChild$"
	r.Git("config", "filter.verify.clean", filter)
	r.Write(".gitattributes", "app/config.json filter=verify\n")
	r.Git("add", "-A")
	r.Git("commit", "-qm", "pin normalization filter")
	r.Write("app/config.json", `{"defect":"","revision":2}`)
	applicationReport(t, applicationVerify(r, "--from", "HEAD"), 0)
	before := len(applicationRecords(t, r))
	r.Write("app/config.json", `{"defect":"accept-empty","revision":2}`)
	r.Write("visible.txt", "separate visible source edit\n")
	if view := verificationTaskView(t, r); view.Verification.Ready {
		t.Fatal("mixed hidden and visible change remained ready")
	}
	failed := applicationReport(t, applicationVerify(r), ExitNotVerified)
	if failed.Verification.Ready || len(applicationRecords(t, r)) <= before || failed.Checks[0].State != "failed" {
		t.Fatal("hidden semantic defect reused prior passing run", failed)
	}
}

func TestTaskVerificationCloneAndCorruption(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	applicationReport(t, applicationVerify(r, "--from", "HEAD"), 0)
	r.Git("add", "-A")
	r.Git("commit", "-qm", "share verification history")
	clone := r.Path("../clone")
	r.Git("clone", "-q", r.Dir, clone)
	cloned := *r
	cloned.Dir = clone
	if view := verificationTaskView(t, &cloned); view.Verification.Ready {
		t.Fatal("shared history substituted for unavailable private evidence")
	}
	if result := cloned.Kitsu("task", "check", "application"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	if len(applicationRecords(t, &cloned)) != 0 {
		t.Fatal("clone inherited private execution evidence")
	}
	verified := applicationReport(t, applicationVerify(&cloned), 0)
	if !verified.Verification.Ready || verified.Executed != 2 {
		t.Fatal("clone did not establish its own application verification", verified)
	}
	cloned.Remove("openspec/changes/.verification/application/binding.json")
	for _, args := range [][]string{{"task", "show", "application", "--json"}, {"task", "check", "application"}, {"task", "verify", "application"}} {
		if result := cloned.Kitsu(args...); result.Code == 0 {
			t.Fatal("corrupt history became an absent task", result.Output())
		}
	}
}

func TestTaskVerificationSnapshotDelta(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	applicationReport(t, applicationVerify(r, "--from", "HEAD"), 0)
	first := coordinatorJSON(t, r, "application", "--compact")
	sections := first["sections"].(map[string]any)
	r.Write("app/config.json", `{"defect":"","revision":2}`)
	delta := coordinatorJSON(t, r, "application", "--since", first["snapshot_id"].(string))
	for key, value := range delta["sections"].(map[string]any) {
		sections[key] = value
	}
	full := coordinatorJSON(t, r, "application", "--compact")
	if !reflect.DeepEqual(sections, full["sections"]) {
		t.Fatal("verification delta did not reconstruct current facts")
	}
	v := coordinatorSection(t, full, "task")["verification"].(map[string]any)
	if v["ready"] != false || len(sections) != 8 || !strings.Contains(stringMustJSON(coordinatorSection(t, full, "prerequisites")), "behavioral_verification") {
		t.Fatal("missing verification prerequisite or changed section contract", full)
	}
	r.Write("kitsu.toml", strings.ReplaceAll(r.Read("kitsu.toml"), "verify=true", "verify=false"))
	cleared := coordinatorJSON(t, r, "application", "--since", delta["snapshot_id"].(string))
	value := coordinatorSection(t, cleared, "task")["verification"].(map[string]any)
	if value["status"] != "not_required" || value["ready"] != true {
		t.Fatal("explicit opt-out retained stale verification requirements", cleared)
	}
}

func TestTaskVerificationActiveInterval(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	stagePrepare(t, r, "reviewer", "review")
	before := r.Git("status", "--porcelain")
	if result := applicationVerify(r, "--from", "HEAD"); result.Code == 0 || !strings.Contains(result.Output(), "active managed interval") {
		t.Fatal(result.Output())
	}
	if before != r.Git("status", "--porcelain") || len(applicationRecords(t, r)) != 0 {
		t.Fatal("verification changed an active worker's interval")
	}
}

func TestTaskVerificationReadinessRequirements(t *testing.T) {
	for _, status := range []string{"pending", "failed", "stale", "unavailable"} {
		t.Run(status, func(t *testing.T) {
			report := change.Report{Task: "app", Stage: change.StageDone}
			applyTaskVerification(&report, &taskVerificationView{Status: status})
			if report.Stage == change.StageDone || len(report.Missing) == 0 {
				t.Fatal("opted-in full task remained done without current verification", report)
			}
		})
	}
	unchanged := change.Report{Task: "app", Stage: change.StageDone}
	applyTaskVerification(&unchanged, nil)
	if unchanged.Stage != change.StageDone {
		t.Fatal("unconfigured task behavior changed")
	}
}

func TestTaskVerificationPublicationCommand(t *testing.T) {
	if os.Getenv("KITSU_TEST_PUBLICATION_COMMAND") != "1" {
		return
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	history, err := verification.Load(root, "application")
	if err != nil || history.ID == "" || len(history.Attempt.Results) == 0 {
		t.Fatal("final check started before shared verification publication", history, err)
	}
	fmt.Println("final check observed immutable attempt " + history.ID)
}

func TestTaskVerificationPublicationOrdering(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	r.Setenv("KITSU_TEST_PUBLICATION_COMMAND", "1")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := []string{filepath.ToSlash(program), "-test.run=^TestTaskVerificationPublicationCommand$", "-test.count=1"}
	r.Write("kitsu.toml", r.Read("kitsu.toml")+"\n[[check]]\nid='publication'\nrun="+decl.QuoteList(command)+"\n")
	r.Git("add", "-A")
	r.Git("commit", "-qm", "declare final publication assertion")
	r.Write("app/config.json", `{"defect":"","revision":2}`)
	report := applicationReport(t, applicationVerify(r, "--from", "HEAD"), 0)
	var checkedTree string
	for _, record := range applicationRecords(t, r) {
		if record.ID == "publication" {
			checkedTree = record.Tree
		}
	}
	if checkedTree == "" || !report.Verification.Ready {
		t.Fatal("final check or current verification missing", report)
	}
	r.Git("cat-file", "-e", checkedTree+":openspec/changes/.verification/application/binding.json")
	r.Git("add", "-A")
	if current := r.Git("write-tree"); current != checkedTree {
		t.Fatal("tracked state changed after the final declared check", current, checkedTree)
	}
}

func TestTaskVerificationMissingFullCheckLog(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	first := applicationReport(t, applicationVerify(r, "--from", "HEAD"), 0)
	r.Remove(".git/kitsu/logs/" + first.Checks[1].RunID + ".log")
	second := applicationReport(t, applicationVerify(r), 0)
	if second.Executed != 1 || second.Checks[1].RunID == first.Checks[1].RunID || !second.ChecksVerified {
		t.Fatal("missing full-check log reused as available evidence", second)
	}
}

func TestTaskVerificationPublicationRejectsSourceDrift(t *testing.T) {
	for _, tc := range []struct {
		name    string
		hidden  bool
		path    string
		allowed bool
	}{
		{name: "owned bookkeeping", path: "openspec/changes/.verification/application/binding.json", allowed: true},
		{name: "visible source", path: "app/config.json"},
		{name: "hidden source", hidden: true, path: "app/config.json"},
		{name: "another task history", path: "openspec/changes/.verification/another-task/binding.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := applicationFixture(t, "")
			if tc.hidden {
				program, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				filter := "'" + strings.ReplaceAll(filepath.ToSlash(program), "'", "'\\''") + "' -test.run=^TestVerificationCleanFilterChild$"
				r.Git("config", "filter.verify.clean", filter)
				r.Write(".gitattributes", "app/config.json filter=verify\n")
			}
			repo, err := gitx.Open(t.Context(), r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			s := &session{repo: repo, store: store.Open(repo.CommonDir)}
			before, err := repo.Snapshot(t.Context(), s.store.Tmp())
			if err != nil {
				t.Fatal(err)
			}
			sources, err := stageRawSources(t.Context(), s, before)
			if err != nil {
				t.Fatal(err)
			}
			r.Write(tc.path, `{"defect":"accept-empty","revision":7}`)
			after, err := repo.Snapshot(t.Context(), s.store.Tmp())
			if err != nil {
				t.Fatal(err)
			}
			if tc.hidden && before != after {
				t.Fatal("fixture failed to hide the original byte change")
			}
			err = verifyPublicationSources(t.Context(), s, "application", before, after, sources)
			if (err == nil) != tc.allowed {
				t.Fatal("publication adopted non-bookkeeping source or rejected owned summary", tc.name, err)
			}
		})
	}
}
