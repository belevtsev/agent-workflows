package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Tests for `cw hook pre-publish` (R-PRE, R-CMD) and `cw hook session-start`
// (R-SESSION), driven through Execute.

// hkSubstantialPlan is a substantial plan whose verify can pass at unchanged
// source without running its check or adviser checkpoints.
const hkSubstantialPlan = `{"title":"T","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`

// hkSmallPlan is the same plan with kind small.
const hkSmallPlan = `{"title":"S","kind":"small","requirements":[{"id":"source","description":"Inspect actual source","check_ids":[],"allow_non_execution":true}],"checks":[]}`

// hkCanonicalTempDir returns a fresh temporary directory in canonical form.
func hkCanonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// hkGit runs git in dir with no global or system configuration.
func hkGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// hkRepo creates a Git repository (no commit) and returns its canonical path.
func hkRepo(t *testing.T) string {
	t.Helper()
	repo := hkCanonicalTempDir(t)
	hkGit(t, repo, "init", "-q")
	return repo
}

// hkHarness is a CLI config with a private home and a mutable environment
// seen through config.Getenv.
type hkHarness struct {
	config Config
	out    *bytes.Buffer
	home   string
	env    map[string]string
}

func newHKHarness(t *testing.T) *hkHarness {
	t.Helper()
	config, out := testConfig(t)
	harness := &hkHarness{out: out, home: hkCanonicalTempDir(t), env: map[string]string{}}
	config.HomeDir = func() (string, error) { return harness.home, nil }
	config.Getenv = func(name string) string { return harness.env[name] }
	config.Environ = func() []string {
		environ := make([]string, 0, len(harness.env))
		for name, value := range harness.env {
			environ = append(environ, name+"="+value)
		}
		return environ
	}
	harness.config = config
	return harness
}

// defaultStore is the task store used when no store variable is set.
func (h *hkHarness) defaultStore() string {
	return filepath.Join(h.home, ".local", "state", "agent-workflows", "workflow-tasks")
}

// start runs `cw task start` for repo with plan and returns the printed record.
func (h *hkHarness) start(t *testing.T, repo, plan string) taskflow.Record {
	t.Helper()
	h.config.WorkingDir = func() (string, error) { return repo, nil }
	h.config.In = strings.NewReader(plan)
	h.out.Reset()
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, h.config); err != nil {
		t.Fatalf("task start: %v", err)
	}
	var record taskflow.Record
	if err := json.Unmarshal(h.out.Bytes(), &record); err != nil {
		t.Fatalf("task start output %q: %v", h.out.String(), err)
	}
	if record.ID == "" || record.Directory == "" {
		t.Fatalf("task start printed an incomplete record: %s", h.out.String())
	}
	return record
}

// prePublishInput is a realistic PreToolUse payload for a Bash command.
func hkPrePublishInput(t *testing.T, cwd, command string) []byte {
	t.Helper()
	payload := map[string]any{
		"session_id":      "session-1",
		"transcript_path": "/nonexistent/transcript.jsonl",
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"cwd":             cwd,
		"tool_input":      map[string]any{"command": command, "description": "publish"},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// runPrePublish runs `cw hook pre-publish` with stdin, requires a nil error
// (R-PRE: the hook exits 0 in every case) and returns what it wrote.
func (h *hkHarness) runPrePublish(t *testing.T, stdin []byte) string {
	t.Helper()
	h.config.In = bytes.NewReader(stdin)
	h.out.Reset()
	if err := Execute(t.Context(), []string{"hook", "pre-publish"}, h.config); err != nil {
		t.Fatalf("hook pre-publish returned %v; it must always return nil", err)
	}
	return h.out.String()
}

func (h *hkHarness) prePublish(t *testing.T, cwd, command string) string {
	t.Helper()
	return h.runPrePublish(t, hkPrePublishInput(t, cwd, command))
}

// hkExpectAllow requires that the hook wrote nothing.
func hkExpectAllow(t *testing.T, got string) {
	t.Helper()
	if got != "" {
		t.Fatalf("hook wrote %q, want nothing (allow)", got)
	}
}

// hkDecision requires exactly one newline-terminated JSON line of the shape
// {"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":D,
// "permissionDecisionReason":R}} and returns D and R.
func hkDecision(t *testing.T, got string) (decision, reason string) {
	t.Helper()
	if !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Fatalf("hook wrote %q, want exactly one newline-terminated line", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("decision line %q is not JSON: %v", got, err)
	}
	output, ok := decoded["hookSpecificOutput"].(map[string]any)
	if len(decoded) != 1 || !ok || len(output) != 3 {
		t.Fatalf("decision %v, want only hookSpecificOutput with three members", decoded)
	}
	if output["hookEventName"] != "PreToolUse" {
		t.Fatalf("hookEventName %v, want PreToolUse", output["hookEventName"])
	}
	decision, okDecision := output["permissionDecision"].(string)
	reason, okReason := output["permissionDecisionReason"].(string)
	if !okDecision || !okReason {
		t.Fatalf("decision %v has a non-string permissionDecision or permissionDecisionReason", output)
	}
	return decision, reason
}

// hkExpectDeny requires a deny whose reason names every record in gating, at
// most three: its ID, its plan title and the verify command for its
// directory, which it names once (H1); and the abandon command once for
// them all.
func hkExpectDeny(t *testing.T, got string, gating ...taskflow.Record) string {
	t.Helper()
	if len(gating) == 0 || len(gating) > 3 {
		t.Fatal("hkExpectDeny needs one to three gating records")
	}
	decision, reason := hkDecision(t, got)
	if decision != "deny" {
		t.Fatalf("permissionDecision %q, want deny; reason %q", decision, reason)
	}
	// H1 wording (rev 5): the reason says to close a finished task with
	// cw task abandon, which needs the user's confirmation (D2).
	if !strings.Contains(reason, hkFinishedPhrase) {
		t.Fatalf("deny reason %q does not contain %q", reason, hkFinishedPhrase)
	}
	for _, record := range gating {
		for _, want := range []string{
			record.ID,
			record.Plan.Title,
			fmt.Sprintf("cw task verify --task %s --summary", record.Directory),
		} {
			if !strings.Contains(reason, want) {
				t.Fatalf("deny reason %q does not contain %q", reason, want)
			}
		}
		if n := strings.Count(reason, record.Directory); n != 1 {
			t.Fatalf("deny reason %q names %s %d times, want once", reason, record.Directory, n)
		}
	}
	return reason
}

// hkExpectDenyOnly is hkExpectDeny that also requires the reason not to name
// any record in current (H1: non-gating records are not named).
func hkExpectDenyOnly(t *testing.T, got string, gating []taskflow.Record, current ...taskflow.Record) string {
	t.Helper()
	reason := hkExpectDeny(t, got, gating...)
	for _, record := range current {
		if strings.Contains(reason, record.ID) {
			t.Fatalf("deny reason %q names the non-gating record %s", reason, record.ID)
		}
	}
	return reason
}

// hkFinishedPhrase is the exact H1 wording (rev 5), as D2 amended it, that
// every deny reason holds.
const hkFinishedPhrase = "For a task that is finished or was given up, ask the user before abandoning it: " +
	"`cw task abandon --task <task-dir> --reason TEXT` needs their confirmation."

// H15 (rev 4): the commit and push ask reason wording.
const (
	hkCommitAskWording = "this commit looks like"
	hkPushAskWording   = "the commits being pushed look like"
)

// hkExpectAsk requires an ask whose reason contains every string in contains.
func hkExpectAsk(t *testing.T, got string, contains ...string) string {
	t.Helper()
	decision, reason := hkDecision(t, got)
	if decision != "ask" {
		t.Fatalf("permissionDecision %q, want ask; reason %q", decision, reason)
	}
	for _, want := range contains {
		if !strings.Contains(reason, want) {
			t.Fatalf("ask reason %q does not contain %q", reason, want)
		}
	}
	return reason
}

// hkSourceRef is the evidence reference for the whole one-line source.txt.
func hkSourceRef(data []byte) taskflow.SourceRef {
	hash := sha256.Sum256(data)
	return taskflow.SourceRef{Path: "source.txt", StartLine: 1, EndLine: 1, SHA256: hex.EncodeToString(hash[:])}
}

// assessAndVerify records a satisfying assessment and requires a passing
// `cw task verify`.
func (h *hkHarness) assessAndVerify(t *testing.T, record taskflow.Record, source []byte) {
	t.Helper()
	h.assess(t, record, source)
	h.verify(t, record)
}

// assess records a satisfying assessment citing source.txt with content
// source, without verifying.
func (h *hkHarness) assess(t *testing.T, record taskflow.Record, source []byte) {
	t.Helper()
	ref := hkSourceRef(source)
	reason := "Read-only source inspection; no executable behavior changed."
	input := taskflow.AssessmentInput{
		Requirements: []taskflow.Assessment{{ID: "source", Status: "satisfied", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: reason}},
		Decision:     &taskflow.Decision{Status: "resolved", Reason: "Actual source reference establishes this result.", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: reason},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	h.config.In = bytes.NewReader(encoded)
	h.out.Reset()
	if err := Execute(t.Context(), []string{"task", "assess", "--task", record.Directory, "--input", "-"}, h.config); err != nil {
		t.Fatalf("task assess: %v\n%s", err, h.out.String())
	}
}

// verify requires a passing `cw task verify` of record.
func (h *hkHarness) verify(t *testing.T, record taskflow.Record) {
	t.Helper()
	h.out.Reset()
	if err := Execute(t.Context(), []string{"task", "verify", "--task", record.Directory}, h.config); err != nil {
		t.Fatalf("task verify: %v\n%s", err, h.out.String())
	}
	var gate taskflow.Verification
	if err := json.Unmarshal(h.out.Bytes(), &gate); err != nil || !gate.Complete {
		t.Fatalf("verify did not pass: %s %v", h.out.String(), err)
	}
}

// hkSubstantialRecord creates a store record directly with taskflow.Start.
func hkSubstantialRecord(t *testing.T, repo, store, title string) taskflow.Record {
	t.Helper()
	plan := taskflow.Plan{
		Title:        title,
		Kind:         "substantial",
		Checkpoints:  &taskflow.CheckpointPolicy{Required: false, Reason: "test"},
		Requirements: []taskflow.Requirement{{ID: "source", Description: "Inspect actual source", CheckIDs: []string{"unit"}, AllowNonExecution: true}},
		Checks:       []taskflow.Check{{ID: "unit", Argv: []string{"go", "test", "./..."}}},
	}
	record, err := taskflow.Env{}.StartWithPacks(t.Context(), repo, store, plan, nil, taskflow.SelectionExplicit, nil)
	if err != nil {
		t.Fatalf("taskflow.Start: %v", err)
	}
	return record
}

func TestHookPrePublishFollowsTaskVerification(t *testing.T) {
	repo := hkRepo(t)
	source := []byte("verified source\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), source, 0600); err != nil {
		t.Fatal(err)
	}
	// Created before task start so the verified source already holds it.
	sub := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	h := newHKHarness(t)

	t.Run("allow without record", func(t *testing.T) {
		// R-PRE: a git commit in a repository with no record is allowed.
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})

	record := h.start(t, repo, hkSubstantialPlan)
	if record.Baseline.Root != repo {
		t.Fatalf("record root %q, want %q", record.Baseline.Root, repo)
	}

	t.Run("deny after start", func(t *testing.T) {
		// R-PRE: an open substantial record denies with the exact reason.
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), record)
	})

	t.Run("deny from a subdirectory", func(t *testing.T) {
		// R-PRE: any cwd inside the worktree selects the worktree root.
		hkExpectDeny(t, h.prePublish(t, sub, "git commit -m x"), record)
	})

	h.assessAndVerify(t, record, source)

	t.Run("allow after passing verify", func(t *testing.T) {
		// R-PRE: a passing verify on the current source closes the record.
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})

	hkGit(t, repo, "add", "source.txt")

	t.Run("allow after git add of the verified bytes", func(t *testing.T) {
		// R-PRE: staging the verified bytes as its own command does not
		// reopen the record.
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})

	hkGit(t, repo, "commit", "-q", "-m", "x")

	t.Run("allow push after committing the verified bytes", func(t *testing.T) {
		// R-PRE: committing the verified bytes (a HEAD change) does not
		// reopen the record, so the following push is allowed.
		hkExpectAllow(t, h.prePublish(t, repo, "git push origin main"))
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})

	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("edited source\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("deny after source drift", func(t *testing.T) {
		// R-PRE: editing a file after a passing verify reopens the record.
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), record)
		hkExpectDeny(t, h.prePublish(t, repo, "git push origin main"), record)
	})
}

// hkTitled is hkSubstantialPlan with another title.
func hkTitled(title string) string {
	return strings.Replace(hkSubstantialPlan, `"title":"T"`, `"title":"`+title+`"`, 1)
}

// abandon runs `cw task abandon --task DIR --reason REASON` and requires success.
func (h *hkHarness) abandon(t *testing.T, record taskflow.Record, reason string) {
	t.Helper()
	h.out.Reset()
	if err := Execute(t.Context(), []string{"task", "abandon", "--task", record.Directory, "--reason", reason}, h.config); err != nil {
		t.Fatalf("task abandon: %v\n%s", err, h.out.String())
	}
}

func TestHookPrePublishEveryOpenRecordGates(t *testing.T) {
	t.Run("deny names every open record", func(t *testing.T) {
		// H1: with two open records the deny reason names both, each with
		// its verify command.
		repo := hkRepo(t)
		h := newHKHarness(t)
		older := h.start(t, repo, hkTitled("Older"))
		newer := h.start(t, repo, hkTitled("Newer"))
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), older, newer)
		hkExpectDeny(t, h.prePublish(t, repo, "git push origin main"), older, newer)
	})

	t.Run("older open record beside a newer verified one", func(t *testing.T) {
		// H2, H1: a newer verified record no longer hides an older open one;
		// the deny names the older record and not the verified one. H3: after
		// `cw task abandon` of the older record the command is allowed.
		repo := hkRepo(t)
		source := []byte("verified source\n")
		if err := os.WriteFile(filepath.Join(repo, "source.txt"), source, 0600); err != nil {
			t.Fatal(err)
		}
		h := newHKHarness(t)
		older := h.start(t, repo, hkTitled("Older"))
		newer := h.start(t, repo, hkTitled("Newer"))
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), older, newer)
		h.assessAndVerify(t, newer, source)
		hkExpectDenyOnly(t, h.prePublish(t, repo, "git commit -m x"), []taskflow.Record{older}, newer)

		h.abandon(t, older, "x")
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})

	t.Run("newer open record beside an older verified one", func(t *testing.T) {
		// H1: the deny names the newer open record and not the older
		// verified one.
		repo := hkRepo(t)
		source := []byte("verified source\n")
		if err := os.WriteFile(filepath.Join(repo, "source.txt"), source, 0600); err != nil {
			t.Fatal(err)
		}
		h := newHKHarness(t)
		older := h.start(t, repo, hkTitled("Older"))
		h.assessAndVerify(t, older, source)
		newer := h.start(t, repo, hkTitled("Newer"))
		hkExpectDenyOnly(t, h.prePublish(t, repo, "git commit -m x"), []taskflow.Record{newer}, older)
	})

	t.Run("abandoning the only open record allows", func(t *testing.T) {
		// H3: an abandoned record no longer gates.
		repo := hkRepo(t)
		h := newHKHarness(t)
		record := h.start(t, repo, hkSubstantialPlan)
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), record)
		h.abandon(t, record, "superseded")
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})
}

func TestHookPrePublishAllowsWithoutGatingRecord(t *testing.T) {
	t.Run("small plan record", func(t *testing.T) {
		// R-PRE: a record whose plan kind is small never gates.
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.start(t, repo, hkSmallPlan)
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})

	t.Run("enforce_verify option", func(t *testing.T) {
		// R-PRE: CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false disables the gate;
		// any other value keeps it.
		repo := hkRepo(t)
		h := newHKHarness(t)
		record := h.start(t, repo, hkSubstantialPlan)
		tests := []struct {
			value string
			deny  bool
		}{
			{"false", false},
			{"true", true},
			{"", true},
		}
		for _, test := range tests {
			t.Run(fmt.Sprintf("value %q", test.value), func(t *testing.T) {
				h.env["CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY"] = test.value
				got := h.prePublish(t, repo, "git commit -m x")
				if test.deny {
					hkExpectDeny(t, got, record)
				} else {
					hkExpectAllow(t, got)
				}
			})
		}
	})

	t.Run("non-git cwd", func(t *testing.T) {
		// R-PRE: a cwd outside any Git worktree is allowed even while another
		// repository has an open record.
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.start(t, repo, hkSubstantialPlan)
		hkExpectAllow(t, h.prePublish(t, hkCanonicalTempDir(t), "git commit -m x"))
	})
}

func TestHookPrePublishCommandRecognition(t *testing.T) {
	// The repository is created by the top-level test so its path holds no
	// spaces or shell separators from subtest names.
	repo := hkRepo(t)
	if err := os.Mkdir(filepath.Join(repo, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	h := newHKHarness(t)
	record := h.start(t, repo, hkSubstantialPlan)
	tests := []struct {
		command string
		deny    bool
	}{
		// H4 (F8a): wrapper and launcher forms that run git commit or push
		// (`command git push` is also listed below under R-PRE).
		{"env X=y git commit -m x", true},
		{"env -i git commit -m x", true},
		{"env -u HOME git push", true},
		{`\git commit -m x`, true},
		{"timeout 30 git push", true},
		{"nice git commit -m x", true},
		{"nice -n 5 git push", true},
		{"stdbuf -oL git push", true},
		{"noglob git push", true},
		{"sh -c 'git commit -m x'", true},
		{`bash -c "git push origin main"`, true},
		{"bash -lc 'git push'", true},
		{`eval "git commit -m x"`, true},
		{"zsh -c 'cd sub && git commit -m x'", true},
		// H4 (F8a): forms that must stay allowed.
		{"command -v git", false},
		{"sh -c 'git status'", false},
		{"bash script.sh", false},
		{"env -i git status", false},
		{"timeout 30 git fetch", false},
		// H11 (rev 4): command substitutions before git, launchers and the
		// command inside a substitution.
		{`GIT_COMMITTER_DATE="$(date -R)" git commit -m x`, true},
		{`env FOO="$(id -u)" git push`, true},
		{`git -C "$(pwd)" commit -m x`, true},
		{`git -c core.hooksPath="$(mktemp -d)" commit -m x`, true},
		{`echo "$(git push)"`, true},
		{"command -p git push", true},
		{"sudo git push", true},
		{"sudo -u root git commit -m x", true},
		// H11 (rev 4): must stay allowed.
		{`echo "$(date)"`, false},
		// R-PRE required non-publish commands.
		{"git status", false},
		{"echo git commit", false},
		{"git -C x log", false},
		// R-PRE required publish commands.
		{"git commit -m x", true},
		{"git push origin main", true},
		{"git -C " + repo + " commit -m x", true},
		{"GIT_AUTHOR_NAME=x git commit -m x", true},
		{"go test ./... && git commit -m x", true},
		{"git push --force-with-lease", true},
		{"if ! git diff --quiet; then git commit -am x; fi", true},
		{"for r in a b; do git push $r; done", true},
		{"{ git commit -m x; }", true},
		{"! git push", true},
		{"time git push", true},
		{"command git push", true},
		{"env git commit -m x", true},
		{"env X=1 git commit -m x", true},
		{`"git" commit -m x`, true},
		{"git -c core.pager=cat commit -m x", true},
		{"git commit -F - <<'EOF'\nmsg\nEOF", true},
		// R-CMD: separators split simple commands.
		{"true; git commit -m x", true},
		{"echo x | git push", true},
		{"true\ngit push origin main", true},
		{"(git commit -m x)", true},
		{"echo `git push`", true},
		{"true & git commit -m x", true},
		// R-CMD: leading assignments, prefix words in any order, the base
		// name and git's global options are skipped.
		{"A=1 B=2 git push", true},
		{"exec git commit -m x", true},
		{"nohup git push", true},
		{"builtin git push", true},
		{"time env X=1 git push", true},
		{"X=1 command git push", true},
		{"while true; do git push; done", true},
		{"until false; do git push; done", true},
		{"if true; then true; elif true; then true; else git commit -m x; fi", true},
		{"'git' push", true},
		{"/usr/bin/git commit -m x", true},
		{"git -c user.name=x commit -m x", true},
		{"git --git-dir .git --work-tree . push", true},
		{"git --namespace ns --exec-path /tmp --super-prefix p/ --config-env a=B push", true},
		{"git --no-pager commit -m x", true},
		{"git --namespace=x push", true},
		{"git -p commit -m x", true},
		// R-CMD: anything else is not a publish command.
		{"git commit-tree HEAD^{tree}", false},
		{"git -C commit status", false},
		{"git -c commit.gpgsign=false status", false},
		{"gitx commit -m x", false},
		{"echo git push; git status", false},
		{"git log --grep commit", false},
		{"", false},
		{"git status commit", false},
		{"git --git-dir push status", false},
		{"git --work-tree commit status", false},
		{"git --namespace push status", false},
		{"git --exec-path commit status", false},
		{"git --super-prefix push status", false},
		{"git --config-env commit status", false},
		{"git pushd", false},
		{"echo 'git push'", false},
		{"git", false},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%q", test.command), func(t *testing.T) {
			got := h.prePublish(t, repo, test.command)
			if test.deny {
				hkExpectDeny(t, got, record)
			} else {
				hkExpectAllow(t, got)
			}
		})
	}
}

func TestHookPrePublishTargetDirectory(t *testing.T) {
	// R-CMD, R-PRE: a publish command's target directory starts at the
	// hook's cwd and moves with each preceding `cd DIR` and each `-C DIR`.
	// All directories come from the top-level test so their paths hold no
	// spaces or shell separators.
	repo := hkRepo(t)
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	elsewhere := hkCanonicalTempDir(t)
	// H17 (rev 7): a non-repository directory with the same relative name
	// as the repository's subdirectory.
	if err := os.Mkdir(filepath.Join(elsewhere, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	parent, base := filepath.Dir(repo), filepath.Base(repo)
	h := newHKHarness(t)
	record := h.start(t, repo, hkSubstantialPlan)
	tests := []struct {
		name    string
		cwd     string
		command string
		deny    bool
	}{
		// R-PRE required cases.
		{"cd into the repository from a non-git cwd", elsewhere, "cd " + repo + " && git commit -m x", true},
		{"git -C the repository from a non-git cwd", elsewhere, "git -C " + repo + " push", true},
		// H4 (F8a): git -C REPO commit from an unrelated non-repository cwd,
		// and git -C SUB commit from the repository root.
		{"git -C the repository commit from a non-git cwd", elsewhere, "git -C " + repo + " commit -m x", true},
		{"git -C a subdirectory commit from the repository root", repo, "git -C nested commit -m x", true},
		// H11 (rev 4): env -C/--chdir and sudo -D move the target from a
		// non-repository cwd.
		{"env -C the repository", elsewhere, "env -C " + repo + " git commit -m x", true},
		{"env --chdir= the repository", elsewhere, "env --chdir=" + repo + " git commit -m x", true},
		{"env -u HOME -C the repository", elsewhere, "env -u HOME -C " + repo + " git push", true},
		{"sudo -D the repository", elsewhere, "sudo -D " + repo + " git commit -m x", true},
		{"cd the repository then env -C a subdirectory", elsewhere, "cd " + repo + " && env -C nested git commit -m x", true},
		// H11 (rev 4): env -C to a non-repository directory from the
		// repository runs the command outside it.
		{"env -C out of the repository", repo, "env -C " + elsewhere + " git commit -m x", false},
		// H16 (rev 6): a command inside $(...) or backticks runs in the
		// directory in effect where the substitution appears.
		{"substitution after cd into the repository", elsewhere, "cd " + repo + " && out=$(git commit -m x)", true},
		{"quoted substitution after cd into the repository", elsewhere, "cd " + repo + ` && echo "$(git push)"`, true},
		{"backticks after cd into the repository", elsewhere, "cd " + repo + " && x=`git push`", true},
		{"substitution after cd out of the repository", repo, "cd " + elsewhere + " && x=$(git commit -m x)", false},
		{"git -C backtick pwd from the repository", repo, "git -C `pwd` commit -m x", true},
		// H17 (rev 6): attached and clustered wrapper options, from a
		// non-repository cwd.
		{"env -C attached", elsewhere, "env -C" + repo + " git commit -m x", true},
		{"sudo -D attached", elsewhere, "sudo -D" + repo + " git commit -m x", true},
		{"env -iC clustered", elsewhere, "env -iC " + repo + " git commit -m x", true},
		{"env --chdir separate value", elsewhere, "env --chdir " + repo + " git push", true},
		// H17 (rev 7, supersedes the rev 6 chained case): within one
		// wrapper a repeated -C/--chdir (env) or -D/--chdir (sudo) is
		// last-wins, each value resolved from the directory in effect (the
		// cwd), not from the earlier option. elsewhere/nested exists and is
		// not a repository, so chaining and last-wins give different results.
		{"env -C repeated, last is the repository", elsewhere, "env -C " + elsewhere + " -C " + repo + " git commit -m x", true},
		{"env -C repeated, last is outside the repository", elsewhere, "env -C " + repo + " -C " + elsewhere + " git commit -m x", false},
		{"env -C repeated, last is relative from the repository", repo, "env -C " + elsewhere + " -C nested git commit -m x", true},
		{"env -C repeated, last is relative from a non-repository cwd", elsewhere, "env -C " + repo + " -C nested git commit -m x", false},
		{"env --chdir= then -C, last is the repository", elsewhere, "env --chdir=" + elsewhere + " -C " + repo + " git commit -m x", true},
		{"env -C then --chdir, last is outside the repository", elsewhere, "env -C " + repo + " --chdir " + elsewhere + " git push", false},
		{"sudo -D repeated, last is the repository", elsewhere, "sudo -D " + elsewhere + " -D " + repo + " git commit -m x", true},
		{"sudo -D repeated, last is outside the repository", elsewhere, "sudo -D " + repo + " -D " + elsewhere + " git commit -m x", false},
		// H17 (rev 7): separate nested wrappers still chain.
		{"nested env -C wrappers chain", elsewhere, "env -C " + repo + " env -C nested git commit -m x", true},
		// H17 (rev 6): an attached option value is not the program.
		{"stdbuf -oL from the repository", repo, "stdbuf -oL git push", true},
		{"cd out of the repository", repo, "cd " + elsewhere + " && git commit -m x", false},
		// R-CMD: relative and repeated moves.
		{"relative cd", parent, "cd " + base + " && git commit -m x", true},
		{"relative git -C", parent, "git -C " + base + " commit -m x", true},
		{"relative cd inside the repository", repo, "cd nested && git commit -m x", true},
		{"relative cd out of the repository", nested, "cd ../.. && git push", false},
		{"quoted cd target", elsewhere, `cd "` + repo + `" && git commit -m x`, true},
		{"cd in a subshell", elsewhere, "(cd " + repo + " && git commit -m x)", true},
		{"cd persists across later commands", elsewhere, "cd " + repo + "; git status; git push", true},
		{"a later cd moves the target again", elsewhere, "cd " + repo + "; cd " + elsewhere + "; git push", false},
		{"a later absolute -C wins", elsewhere, "git -C " + repo + " -C " + elsewhere + " push", false},
		{"-C relative to an earlier -C", elsewhere, "git -C " + parent + " -C " + base + " push", true},
		{"cd then relative -C", elsewhere, "cd " + parent + " && git -C " + base + " commit -m x", true},
		{"cd without a publish command", elsewhere, "cd " + repo + " && git status", false},
		{"any publish target may deny", elsewhere, "git -C " + elsewhere + " commit -m x; git -C " + repo + " push", true},
		{"cwd elsewhere without a move", elsewhere, "git commit -m x", false},
		{"cd with extra words is not a move", elsewhere, "cd " + repo + " extra && git commit -m x", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := h.prePublish(t, test.cwd, test.command)
			if test.deny {
				hkExpectDeny(t, got, record)
			} else {
				hkExpectAllow(t, got)
			}
		})
	}
}

func TestHookPrePublishDirectoryChangeScope(t *testing.T) {
	// R-CMD rev 3, R-PRE rev 3: after a directory change the hook cannot
	// follow, a commit is checked in the session's directory, `pushd DIR`
	// moves like `cd DIR`, and parentheses and backticks scope a directory
	// change. gated (A) has an open record;
	// ungated (B) is a Git repository without one. All directories come from
	// the top-level test so their paths hold no spaces or shell separators,
	// except the deliberate `sp ace` directory inside B.
	gated := hkRepo(t)
	ungated := hkRepo(t)
	for _, dir := range []string{"sp ace", "sub"} {
		if err := os.Mkdir(filepath.Join(ungated, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	parent, base := filepath.Dir(gated), filepath.Base(gated)
	h := newHKHarness(t)
	record := h.start(t, gated, hkSubstantialPlan)
	// Controls: the gated repository denies, the ungated one allows.
	hkExpectDeny(t, h.prePublish(t, gated, "git commit -m x"), record)
	hkExpectAllow(t, h.prePublish(t, ungated, "git commit -m x"))
	tests := []struct {
		name    string
		cwd     string
		command string
		deny    bool
	}{
		// G3: from A, cd -P and cd -- move to B like cd; pushd -n adds B to
		// the directory stack and stays in A.
		{"cd -P moves to the ungated repository", gated, "cd -P " + ungated + " && git commit -m x", false},
		{"cd -- moves to the ungated repository", gated, "cd -- " + ungated + " && git commit -m x", false},
		{"pushd -n stays in the gated repository", gated, "pushd -n " + ungated + " && git commit -m x", true},
		{"quoted cd path with a space makes the target unknown", gated, `cd "` + ungated + `/sp ace" && git commit -m x`, false},
		// R-PRE rev 3 required: from B, an absolute cd or pushd, or an
		// absolute -C, gives an unknown target a directory again.
		{"absolute cd after an unknown target", ungated, "cd - && cd " + gated + " && git commit -m x", true},
		{"pushd moves like cd", ungated, "pushd " + gated + " && git commit -m x", true},
		{"relative cd keeps an unknown target unknown", ungated, "cd - && cd sub && git commit -m x", false},
		{"absolute -C after an unknown target", ungated, "cd - && git -C " + gated + " commit -m x", true},
		// R-PRE rev 3 required: parentheses, $( ) and backticks scope a
		// directory change.
		{"cd in a subshell does not leak", ungated, "(cd " + gated + " && git status) && git commit -m x", false},
		{"cd in a command substitution does not leak", ungated, "echo $(cd " + gated + ") && git commit -m x", false},
		{"cd in backticks does not leak", ungated, "echo `cd " + gated + "`; git commit -m x", false},
		{"publish inside the subshell uses its cd", ungated, "(cd " + gated + " && git commit -m x)", true},
		{"subshell cd away does not leak", gated, "(cd " + ungated + " && make) && git commit -m x", true},
		{"parentheses inside a quoted message", gated, `git commit -m "fix (scope) a)"`, true},
		{"nested subshell cd back into the gated repository", gated, "cd " + ungated + " && (cd " + gated + " && git push)", true},
		// R-CMD rev 3 (beyond the required list): a relative cd while the
		// target is unknown stays unknown even where it would reach A from
		// the cwd.
		{"relative cd keeps unknown even toward the gated repository", parent, "cd - && cd " + base + " && git commit -m x", false},
		// R-CMD rev 3: a relative pushd is joined to the current directory.
		{"relative pushd", parent, "pushd " + base + " && git commit -m x", true},
		// R-CMD rev 3: popd after a pushd makes the target unknown.
		{"popd after pushd makes the target unknown", ungated, "pushd " + gated + " && popd && git commit -m x", false},
		// R-CMD rev 3: an unmatched `)` restores nothing.
		{"unmatched closing parenthesis restores nothing", ungated, "cd " + gated + ") && git commit -m x", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := h.prePublish(t, test.cwd, test.command)
			if test.deny {
				hkExpectDeny(t, got, record)
			} else {
				hkExpectAllow(t, got)
			}
		})
	}
	// G3: from A, after a directory change the hook does not follow, the
	// commit is checked in A, the session's directory, and asks, naming the
	// change and the gating task. R-PRE rev 3 required allow for these.
	for _, change := range []string{"cd -", "popd"} {
		t.Run(change+" asks", func(t *testing.T) {
			got := h.prePublish(t, gated, change+" && git commit -m x")
			hkExpectAsk(t, got, "cannot tell which directory `"+change+"` leads to", record.ID)
		})
	}
}

func TestHookPrePublishFollowsHomeAndAsksAboutAnUnresolvedDirectory(t *testing.T) {
	// A gated repository under the harness's home directory, and a session
	// directory outside it.
	h := newHKHarness(t)
	repo := filepath.Join(h.home, "proj")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	hkGit(t, repo, "init", "-q")
	record := h.start(t, repo, hkSubstantialPlan)
	elsewhere := hkCanonicalTempDir(t)

	// Oracle: the hook gives the gate its home directory (Config.HomeDir,
	// $HOME in the shell), so a bare cd, `~` and `$HOME` reach the
	// repository.
	for _, command := range []string{
		"cd ~/proj && git commit -m x",
		`git -C "$HOME/proj" push`,
		"cd && cd proj && git commit -m x",
		"cd ~ && cd proj && git commit -m x",
		"cd $HOME && cd proj && git push",
	} {
		t.Run(command, func(t *testing.T) {
			hkExpectDeny(t, h.prePublish(t, elsewhere, command), record)
		})
	}

	// Oracle: after a cd the gate cannot resolve, a commit from the gated
	// session directory asks, naming the argument and the task, and Codex,
	// which cannot ask, denies it.
	unresolved := "cd $OTHER && git commit -m x"
	hkExpectAsk(t, h.prePublish(t, repo, unresolved), "$OTHER", record.ID)
	codex := hkRecordedInput(t, "codex-pre-tool-use.json", repo, map[string]any{"tool_input": map[string]any{"command": unresolved}})
	if decision, reason := hkDecision(t, h.runPrePublish(t, codex)); decision != "deny" || !strings.Contains(reason, "$OTHER") {
		t.Fatalf("Codex decision %q with reason %q, want a deny naming $OTHER", decision, reason)
	}
}

func TestHookPrePublishDirectoryChangeRedirectionAndToplevel(t *testing.T) {
	// R-CMD rev 4, R-PRE rev 4: redirection words are removed before a
	// cd/pushd/popd is judged, `$(git rev-parse --show-toplevel)` keeps the
	// directory, and a relative cd from an unknown directory never resolves
	// against the process working directory. gated (A) has an open record
	// and a subdirectory `sub` created before `task start`; ungated (B) is a
	// Git repository without one. These cases do not call t.Parallel because
	// the H2 cases use t.Chdir.
	gated := hkRepo(t)
	ungated := hkRepo(t)
	gatedSub := filepath.Join(gated, "sub")
	if err := os.Mkdir(gatedSub, 0700); err != nil {
		t.Fatal(err)
	}
	parent, base := filepath.Dir(gated), filepath.Base(gated)
	h := newHKHarness(t)
	record := h.start(t, gated, hkSubstantialPlan)
	// Controls: the gated repository and its subdirectory deny, the ungated
	// repository allows.
	hkExpectDeny(t, h.prePublish(t, gated, "git push"), record)
	hkExpectDeny(t, h.prePublish(t, gatedSub, "git push"), record)
	hkExpectAllow(t, h.prePublish(t, ungated, "git push"))
	tests := []struct {
		name    string
		cwd     string
		command string
		deny    bool
	}{
		// R-PRE rev 4 required (R-CMD rev 4 redirection removal): from B, a
		// cd or pushd to A with a redirection still moves to A.
		{"cd with 2>/dev/null moves", ungated, "cd " + gated + " 2>/dev/null && git push", true},
		{"cd with >/dev/null moves", ungated, "cd " + gated + " >/dev/null && git push", true},
		{"cd with > /dev/null 2>&1 moves", ungated, "cd " + gated + " > /dev/null 2>&1 && git push", true},
		{"pushd with >/dev/null moves", ungated, "pushd " + gated + " >/dev/null && git push", true},
		{"cd with 2> /dev/null moves", ungated, "cd " + gated + " 2> /dev/null && git push", true},
		// R-PRE rev 4 required: from A, a redirected cd to B leaves A.
		{"cd away with 2>/dev/null leaves the gated repository", gated, "cd " + ungated + " 2>/dev/null && git push", false},
		// R-PRE rev 4 required (R-CMD rev 4 toplevel substitution): from
		// A/sub, cd to the repository top level stays in A.
		{"quoted toplevel substitution from a subdirectory", gatedSub, `cd "$(git rev-parse --show-toplevel)" && git push`, true},
		{"unquoted toplevel substitution with inner spaces", gatedSub, "cd $( git rev-parse --show-toplevel ) && git commit -m x", true},
		// R-PRE rev 4 required: from B, the toplevel substitution stays in B.
		{"toplevel substitution from the ungated repository", ungated, `cd "$(git rev-parse --show-toplevel)" && git push`, false},
		// G3 (was R-PRE rev 4: an option makes the directory unknown): cd -P
		// moves to A when a redirection follows, as cd does.
		{"cd -P with a redirection moves to the gated repository", ungated, "cd -P " + gated + " 2>/dev/null && git push", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := h.prePublish(t, test.cwd, test.command)
			if test.deny {
				hkExpectDeny(t, got, record)
			} else {
				hkExpectAllow(t, got)
			}
		})
	}
	// R-PRE rev 4 required (H2), R-CMD rev 3/rev 4: with the process working
	// directory at the parent of A, where `<base name of A>` WOULD reach A if
	// it were resolved against the process working directory, a relative
	// path after `cd -` stays unknown.
	t.Run("H2 control: process working directory reaches the gated repository", func(t *testing.T) {
		// Sanity check for the H2 setup: from the parent of A, the relative
		// path names A, so a known relative -C target denies.
		t.Chdir(parent)
		if info, err := os.Stat(base); err != nil || !info.IsDir() {
			t.Fatalf("relative %q from the process working directory is not a directory: %v", base, err)
		}
		hkExpectDeny(t, h.prePublish(t, parent, "git -C "+base+" commit -m x"), record)
	})
	t.Run("H2 relative cd after cd - ignores the process working directory", func(t *testing.T) {
		// R-PRE rev 4 required (H2): from B, allow.
		t.Chdir(parent)
		hkExpectAllow(t, h.prePublish(t, ungated, "cd - && cd "+base+" && git commit -m x"))
	})
	t.Run("H2 relative -C after cd - ignores the process working directory", func(t *testing.T) {
		// R-PRE rev 4 required (H2): from B, allow.
		t.Chdir(parent)
		hkExpectAllow(t, h.prePublish(t, ungated, "cd - && git -C "+base+" commit -m x"))
	})
}

func TestHookPrePublishFailsOpenOnBadInput(t *testing.T) {
	// R-PRE: malformed, empty, oversized or incomplete stdin, and a
	// non-existent cwd, write nothing and return nil, even while the
	// repository (also the working directory) has an open record.
	repo := hkRepo(t)
	h := newHKHarness(t)
	record := h.start(t, repo, hkSubstantialPlan)
	h.config.WorkingDir = func() (string, error) { return repo, nil }
	// Control: the well-formed request denies.
	hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), record)

	oversized, err := json.Marshal(map[string]any{
		"cwd":        repo,
		"tool_input": map[string]any{"command": "git commit -m x"},
		"padding":    strings.Repeat("a", 1<<20),
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		stdin []byte
	}{
		{"malformed JSON", []byte(`{"cwd": "` + repo + `", "tool_input": {"command": "git commit -m x"`)},
		{"not JSON", []byte("git commit -m x")},
		{"empty stdin", nil},
		{"JSON array", []byte(`["git commit -m x"]`)},
		{"missing cwd", []byte(`{"tool_input":{"command":"git commit -m x"}}`)},
		{"non-existent cwd", hkPrePublishInput(t, filepath.Join(repo, "does-not-exist"), "git commit -m x")},
		{"stdin larger than 1 MiB", oversized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hkExpectAllow(t, h.runPrePublish(t, test.stdin))
		})
	}
}

func TestHookPrePublishFailsOpenOnNUL(t *testing.T) {
	// H18 (rev 7): a Bash command holding NUL characters (JSON \u0000)
	// never crashes the hook: Execute returns nil without panicking, and
	// any output is one valid decision line (deny or no output are both
	// acceptable while the repository has an open record).
	repo := hkRepo(t)
	h := newHKHarness(t)
	record := h.start(t, repo, hkSubstantialPlan)
	h.config.WorkingDir = func() (string, error) { return repo, nil }
	// Control: the same repository and record deny a plain commit.
	hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), record)

	tests := []struct {
		name    string
		command string
	}{
		// The two H18 examples: `echo $(echo \u00000\u0000 git commit -m x)`
		// and NUL characters around `git commit -m x`.
		{"NUL inside a substitution", "echo $(echo \x00" + "0\x00 git commit -m x)"},
		{"NUL around a commit", "\x00\x00\x00git commit -m x\x00\x00\x00"},
		// Further NUL placements.
		{"NUL separated from a commit by spaces", "\x00 git commit -m x \x00"},
		{"NUL only", "\x00"},
		{"NUL between git and commit", "git\x00commit -m x"},
		{"NUL in a commit message", "git commit -m x\x00y"},
		{"NUL in a cd target", "cd \x00 && git commit -m x"},
		{"NUL in a git -C target", "git -C " + repo + "\x00 commit -m x"},
		{"NUL in an env -C target", "env -C \x00" + repo + " git push"},
		{"NUL in backticks", "x=`\x00git push`"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdin := hkPrePublishInput(t, repo, test.command)
			if !bytes.Contains(stdin, []byte(`\u0000`)) {
				t.Fatalf("payload %q does not carry the NUL as \\u0000", stdin)
			}
			got := hkRunPrePublishNoPanic(t, h, stdin)
			if got == "" {
				return
			}
			if decision, reason := hkDecision(t, got); decision != "deny" && decision != "ask" {
				t.Fatalf("permissionDecision %q, want deny or ask; reason %q", decision, reason)
			}
		})
	}
}

// hkRunPrePublishNoPanic is runPrePublish (which already requires a nil
// error) that reports a panic in the hook as a test failure instead of
// crashing the test binary (H18).
func hkRunPrePublishNoPanic(t *testing.T, h *hkHarness, stdin []byte) (got string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("hook pre-publish panicked: %v", r)
		}
	}()
	return h.runPrePublish(t, stdin)
}

func TestHookPrePublishTaskStoreSelection(t *testing.T) {
	t.Run("CW_TASK_STORE with an open record denies", func(t *testing.T) {
		// R-PRE: CW_TASK_STORE names the store; the default store is empty.
		repo := hkRepo(t)
		h := newHKHarness(t)
		store := filepath.Join(hkCanonicalTempDir(t), "store")
		record := hkSubstantialRecord(t, repo, store, "From CW_TASK_STORE")
		h.env["CW_TASK_STORE"] = store
		if _, err := os.Stat(h.defaultStore()); !os.IsNotExist(err) {
			t.Fatalf("default store should not exist: %v", err)
		}
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), record)
	})

	t.Run("CW_TASK_STORE replaces the default store", func(t *testing.T) {
		// R-PRE: when CW_TASK_STORE is set, records in the default store are
		// not consulted.
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.start(t, repo, hkSubstantialPlan)
		empty := filepath.Join(hkCanonicalTempDir(t), "store")
		if err := os.Mkdir(empty, 0700); err != nil {
			t.Fatal(err)
		}
		h.env["CW_TASK_STORE"] = empty
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
	})

	t.Run("missing CW_TASK_STORE fails open", func(t *testing.T) {
		// H10: a store that does not exist writes nothing, even while the
		// default store has an open record for the repository.
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.start(t, repo, hkSubstantialPlan)
		h.env["CW_TASK_STORE"] = filepath.Join(hkCanonicalTempDir(t), "missing")
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
		hkExpectAllow(t, h.prePublish(t, repo, "git push origin main"))
	})

	t.Run("XDG_STATE_HOME selects the store", func(t *testing.T) {
		// R-PRE: without CW_TASK_STORE, XDG_STATE_HOME/agent-workflows/workflow-tasks is the store.
		repo := hkRepo(t)
		h := newHKHarness(t)
		state := hkCanonicalTempDir(t)
		if err := os.Mkdir(filepath.Join(state, "agent-workflows"), 0700); err != nil {
			t.Fatal(err)
		}
		record := hkSubstantialRecord(t, repo, filepath.Join(state, "agent-workflows", "workflow-tasks"), "From XDG")
		h.env["XDG_STATE_HOME"] = state
		hkExpectDeny(t, h.prePublish(t, repo, "git push origin main"), record)
	})
}

func TestHookPrePublishLinkedWorktree(t *testing.T) {
	// R-PRE: a record started for the main worktree denies a commit whose cwd
	// is a linked worktree of the same repository.
	main := hkRepo(t)
	hkGit(t, main, "commit", "--allow-empty", "-q", "-m", "x")
	h := newHKHarness(t)
	linked := filepath.Join(hkCanonicalTempDir(t), "linked")
	hkGit(t, main, "worktree", "add", "-q", linked)
	t.Cleanup(func() {
		command := exec.Command("git", "-C", main, "worktree", "remove", "--force", linked)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("git worktree remove: %v\n%s", err, output)
		}
	})
	record := h.start(t, main, hkSubstantialPlan)
	if record.Baseline.Root != main {
		t.Fatalf("record root %q, want main worktree %q", record.Baseline.Root, main)
	}
	hkExpectDeny(t, h.prePublish(t, linked, "git commit -m x"), record)
}

// hkLinkedWorktree adds a linked worktree of main (which needs a commit) in a
// fresh directory and returns its canonical path.
func hkLinkedWorktree(t *testing.T, main string) string {
	t.Helper()
	linked := filepath.Join(hkCanonicalTempDir(t), "linked")
	hkGit(t, main, "worktree", "add", "-q", linked)
	t.Cleanup(func() {
		command := exec.Command("git", "-C", main, "worktree", "remove", "--force", linked)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("git worktree remove: %v\n%s", err, output)
		}
	})
	return linked
}

// hkWriteFile writes content to dir/rel, creating parent directories.
func hkWriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestHookPrePublishLinkedWorktreeDigest(t *testing.T) {
	// H9 (F8c): a record started and verified in the main checkout gates a
	// commit from a linked worktree whose bytes differ, naming the linked
	// worktree, and stops gating once the bytes are identical.
	main := hkRepo(t)
	source := []byte("verified source\n")
	hkWriteFile(t, main, "source.txt", string(source))
	hkGit(t, main, "add", "source.txt")
	hkGit(t, main, "commit", "-q", "-m", "initial")
	linked := hkLinkedWorktree(t, main)
	// Untracked, non-ignored bytes present only in the main checkout.
	hkWriteFile(t, main, "extra.txt", "only in main\n")
	h := newHKHarness(t)
	record := h.start(t, main, hkSubstantialPlan)
	if record.Baseline.Root != main {
		t.Fatalf("record root %q, want main worktree %q", record.Baseline.Root, main)
	}
	h.assessAndVerify(t, record, source)

	// Control: the verified record allows in the main checkout.
	hkExpectAllow(t, h.prePublish(t, main, "git commit -m x"))

	reason := hkExpectDeny(t, h.prePublish(t, linked, "git commit -m x"), record)
	if !strings.Contains(reason, linked) {
		t.Fatalf("deny reason %q does not name the linked worktree %s", reason, linked)
	}

	hkWriteFile(t, linked, "extra.txt", "only in main\n")
	hkExpectAllow(t, h.prePublish(t, linked, "git commit -m x"))
}

// hkLines returns n distinct newline-terminated lines.
func hkLines(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// hkEmptyStore returns an existing, empty, private task store.
func hkEmptyStore(t *testing.T) string {
	t.Helper()
	store := filepath.Join(hkCanonicalTempDir(t), "store")
	if err := os.Mkdir(store, 0700); err != nil {
		t.Fatal(err)
	}
	return store
}

// hkCommittedRepo creates a repository whose one initial commit holds files
// (an empty commit when files is empty).
func hkCommittedRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := hkRepo(t)
	for rel, content := range files {
		hkWriteFile(t, repo, rel, content)
	}
	hkGit(t, repo, "add", "-A")
	hkGit(t, repo, "commit", "-q", "--allow-empty", "-m", "initial")
	return repo
}

// hkNoRecordHarness is a harness whose task store exists and is empty, so the
// repository has no record at all.
func hkNoRecordHarness(t *testing.T) *hkHarness {
	t.Helper()
	h := newHKHarness(t)
	h.env["CW_TASK_STORE"] = hkEmptyStore(t)
	return h
}

func TestHookPrePublishSmallTaskPrompt(t *testing.T) {
	// H5 (F7): with no record for the repository, `git commit -m x` asks when
	// the staged diff meets at least two of (a) more than 300 changed lines,
	// (b) files under more than one top-level directory, (c) a non-test code
	// file changed and no test file changed; otherwise it writes nothing.
	small := hkLines(5)
	type promptCase struct {
		name      string
		committed map[string]string // in the initial commit
		staged    map[string]string // written and staged with git add
		removed   []string          // removed with git rm
		unstaged  map[string]string // written over tracked files, not staged
		ask       bool
	}
	tests := []promptCase{
		// H5 required cases.
		{name: "301+ lines in two directories, code, no tests (a+b+c)", staged: map[string]string{"a/x.go": hkLines(200), "b/y.go": hkLines(200)}, ask: true},
		{name: "301+ lines in one code file, no tests (a+c)", staged: map[string]string{"a/x.go": hkLines(400)}, ask: true},
		{name: "301+ lines of docs in two directories (a+b)", staged: map[string]string{"a/x.md": hkLines(200), "b/y.md": hkLines(200)}, ask: true},
		{name: "small code change in two directories, no tests (b+c)", staged: map[string]string{"a/x.go": small, "b/y.go": small}, ask: true},
		{name: "small code change with its test (none)", staged: map[string]string{"a/x.go": small, "a/x_test.go": small}},
		{name: "301+ lines with a test in one directory (a only)", staged: map[string]string{"a/x.go": hkLines(400), "a/x_test.go": small}},
		{name: "exactly 300 lines in one code file (c only)", staged: map[string]string{"a/x.go": hkLines(300)}},
		{name: "301 lines in one code file (a+c)", staged: map[string]string{"a/x.go": hkLines(301)}, ask: true},
		{name: "nothing staged"},
		{name: "unstaged large edits in two directories", committed: map[string]string{"a/x.go": small, "b/y.go": small}, unstaged: map[string]string{"a/x.go": small + hkLines(200), "b/y.go": small + hkLines(200)}},
		// H5: deleted lines count toward (a).
		{name: "301+ deleted lines of docs in two directories (a+b)", committed: map[string]string{"a/x.md": hkLines(200), "b/y.md": hkLines(200)}, removed: []string{"a/x.md", "b/y.md"}, ask: true},
		// H5: root-level files count for no top-level directory.
		{name: "root-level code file and one directory (c only)", staged: map[string]string{"x.go": small, "a/y.go": small}},
		{name: "small docs change in two directories (b only)", staged: map[string]string{"a/x.md": small, "b/y.md": small}},
	}
	// H5 test-file patterns: each beside a small code change in another
	// directory meets only (b), so nothing is written.
	for _, test := range []string{
		"b/y_test.go", "b/test_y.py", "b/y_test.py", "b/y.test.ts", "b/y.spec.js",
		"b/test/y.go", "b/tests/y.py", "b/testdata/y.go", "b/__tests__/y.js", "b/spec/y.rb",
	} {
		tests = append(tests, promptCase{name: "code change beside test file " + test + " (b only)", staged: map[string]string{"a/x.go": small, test: small}})
	}
	// H14 (rev 4): Swift, JVM and .NET test names and a capitalised Tests/
	// directory count as test files; beside a small Swift change in another
	// top-level directory each meets only (b).
	for _, test := range []string{"Tests/AppTests/AppTests.swift", "src/FooTests.swift", "src/FooTest.kt", "src/FooTests.cs"} {
		tests = append(tests, promptCase{name: "Swift change beside test file " + test + " (b only)", staged: map[string]string{"Sources/App/App.swift": small, test: small}})
	}
	// H14 control: a non-test Swift file in the second directory asks (b+c).
	tests = append(tests, promptCase{name: "Swift change beside non-test src/Foo.swift (b+c)", staged: map[string]string{"Sources/App/App.swift": small, "src/Foo.swift": small}, ask: true})
	// H5 code-file extensions: each beside a small docs change in another
	// directory meets (b) and (c), so the hook asks.
	for _, ext := range []string{".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".rs", ".java", ".kt", ".c", ".h", ".cc", ".cpp", ".rb", ".swift", ".sh"} {
		tests = append(tests, promptCase{name: "code file a/x" + ext + " beside docs (b+c)", staged: map[string]string{"a/x" + ext: small, "b/y.md": small}, ask: true})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := hkCommittedRepo(t, test.committed)
			for rel, content := range test.staged {
				hkWriteFile(t, repo, rel, content)
				hkGit(t, repo, "add", rel)
			}
			for _, rel := range test.removed {
				hkGit(t, repo, "rm", "-q", rel)
			}
			for rel, content := range test.unstaged {
				hkWriteFile(t, repo, rel, content)
			}
			h := hkNoRecordHarness(t)
			got := h.prePublish(t, repo, "git commit -m x")
			if test.ask {
				hkExpectAsk(t, got, "cw task start", hkCommitAskWording)
			} else {
				hkExpectAllow(t, got)
			}
		})
	}

	t.Run("commit -am includes modified tracked files", func(t *testing.T) {
		// H5: large unstaged edits to tracked files in two directories write
		// nothing for `git commit -m x` but ask for `git commit -am x`.
		repo := hkCommittedRepo(t, map[string]string{"a/x.go": small, "b/y.go": small})
		hkWriteFile(t, repo, "a/x.go", small+hkLines(200))
		hkWriteFile(t, repo, "b/y.go", small+hkLines(200))
		h := hkNoRecordHarness(t)
		hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))
		hkExpectAsk(t, h.prePublish(t, repo, "git commit -am x"), "cw task start", hkCommitAskWording)
	})
}

func TestHookPrePublishPromptOptions(t *testing.T) {
	// hkAskRepo is a repository with no record whose staged diff meets all
	// three H5 conditions.
	hkAskRepo := func(t *testing.T) string {
		t.Helper()
		repo := hkCommittedRepo(t, nil)
		hkWriteFile(t, repo, "a/x.go", hkLines(200))
		hkWriteFile(t, repo, "b/y.go", hkLines(200))
		hkGit(t, repo, "add", "-A")
		return repo
	}

	t.Run("small_task_prompt toggles the ask", func(t *testing.T) {
		// H6: SMALL_TASK_PROMPT=false turns the ask into no output; "true"
		// and unset keep it.
		tests := []struct {
			value string
			set   bool
			ask   bool
		}{
			{"", false, true},
			{"true", true, true},
			{"false", true, false},
		}
		for _, test := range tests {
			t.Run(fmt.Sprintf("value %q set %v", test.value, test.set), func(t *testing.T) {
				repo := hkAskRepo(t)
				h := hkNoRecordHarness(t)
				if test.set {
					h.env["CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT"] = test.value
				}
				got := h.prePublish(t, repo, "git commit -m x")
				if test.ask {
					hkExpectAsk(t, got, "cw task start", hkCommitAskWording)
				} else {
					hkExpectAllow(t, got)
				}
			})
		}
	})

	t.Run("small_task_prompt=false does not affect deny", func(t *testing.T) {
		// H6: an open record still denies with SMALL_TASK_PROMPT=false.
		repo := hkRepo(t)
		h := newHKHarness(t)
		record := h.start(t, repo, hkSubstantialPlan)
		h.env["CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT"] = "false"
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), record)
	})

	t.Run("enforce_verify=false does not disable the ask", func(t *testing.T) {
		// H6: ENFORCE_VERIFY=false keeps the H5 ask.
		repo := hkAskRepo(t)
		h := hkNoRecordHarness(t)
		h.env["CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY"] = "false"
		hkExpectAsk(t, h.prePublish(t, repo, "git commit -m x"), "cw task start", hkCommitAskWording)
	})
}

func TestHookPrePushSmallTaskPrompt(t *testing.T) {
	// H7 (F7): with no record, `git push` compares HEAD with @{upstream}
	// (merge-base, three-dot), else with refs/remotes/origin/HEAD, else
	// writes nothing. origin is a local bare repository.
	commitLocally := func(t *testing.T, repo string, files map[string]string) {
		t.Helper()
		for rel, content := range files {
			hkWriteFile(t, repo, rel, content)
		}
		hkGit(t, repo, "add", "-A")
		hkGit(t, repo, "commit", "-q", "-m", "local change")
	}
	// large meets all three H5 signs (a, b, c).
	large := map[string]string{"a/x.go": hkLines(200), "b/y.go": hkLines(200)}
	// Below the thresholds (contract rev 2): fewer than two H5 signs hold.
	// smallWithTest is a few lines of code and its test in one directory (no
	// sign); smallDocs is a few lines of docs in one directory (no sign).
	smallWithTest := map[string]string{"a/x.go": hkLines(5), "a/x_test.go": hkLines(5)}
	smallDocs := map[string]string{"a/x.md": hkLines(5)}
	// pushRepo creates a repository with an initial commit pushed to a bare
	// origin: with -u (upstream), with origin/HEAD set (originHead), or plain.
	pushRepo := func(t *testing.T, upstream, originHead bool) string {
		t.Helper()
		origin := hkCanonicalTempDir(t)
		hkGit(t, origin, "init", "-q", "--bare")
		repo := hkCommittedRepo(t, map[string]string{"README.md": "readme\n"})
		hkGit(t, repo, "remote", "add", "origin", origin)
		if upstream {
			hkGit(t, repo, "push", "-q", "-u", "origin", "main")
		} else {
			hkGit(t, repo, "push", "-q", "origin", "main")
		}
		if originHead {
			hkGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
		}
		return repo
	}
	tests := []struct {
		name       string
		upstream   bool
		originHead bool
		change     map[string]string
		ask        bool
	}{
		{"upstream, large two-directory change", true, false, large, true},
		// H7 rev 2: below the thresholds.
		{"upstream, small code change with its test in one directory", true, false, smallWithTest, false},
		{"upstream, few lines of docs in one directory", true, false, smallDocs, false},
		{"origin/HEAD without upstream, large two-directory change", false, true, large, true},
		// H7 rev 2: below the thresholds.
		{"origin/HEAD without upstream, small code change with its test in one directory", false, true, smallWithTest, false},
		{"origin/HEAD without upstream, few lines of docs in one directory", false, true, smallDocs, false},
		{"neither upstream nor origin/HEAD, large change", false, false, large, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := pushRepo(t, test.upstream, test.originHead)
			commitLocally(t, repo, test.change)
			h := hkNoRecordHarness(t)
			got := h.prePublish(t, repo, "git push")
			if test.ask {
				hkExpectAsk(t, got, hkPushAskWording)
			} else {
				hkExpectAllow(t, got)
			}
		})
	}

	t.Run("upstream changes since the merge base are not counted", func(t *testing.T) {
		// H7: the comparison is merge-base three-dot, so a large change that
		// exists only upstream does not count toward the local small change.
		repo := pushRepo(t, true, false)
		hkGit(t, repo, "checkout", "-q", "-b", "elsewhere")
		hkWriteFile(t, repo, "c/z.go", hkLines(200))
		hkWriteFile(t, repo, "d/w.go", hkLines(200))
		hkGit(t, repo, "add", "-A")
		hkGit(t, repo, "commit", "-q", "-m", "upstream change")
		hkGit(t, repo, "push", "-q", "origin", "elsewhere:main")
		hkGit(t, repo, "checkout", "-q", "main")
		hkWriteFile(t, repo, "a/x.md", hkLines(5))
		hkGit(t, repo, "add", "-A")
		hkGit(t, repo, "commit", "-q", "-m", "local small change")
		h := hkNoRecordHarness(t)
		hkExpectAllow(t, h.prePublish(t, repo, "git push"))
	})
}

func TestHookPrePushUncommittedWithVerifiedRecord(t *testing.T) {
	// H8 (F8e, contract rev 2): with only verified, current records, `git
	// push` asks while a tracked file has uncommitted changes; `git commit -m
	// x` with nothing staged is not affected; after committing the push is
	// allowed. The tracked file is
	// modified before `cw task start`, so the baseline already holds the
	// modification and no source changes after start.
	repo, h := hkDirtyPushState(t)

	hkExpectAsk(t, h.prePublish(t, repo, "git push"), "uncommitted")
	hkExpectAllow(t, h.prePublish(t, repo, "git commit -m x"))

	// Control (contract rev 3): commit everything and nothing else. The
	// source bytes, and so the verified digest, are unchanged, so the record
	// stays verified and the tree is clean.
	hkGit(t, repo, "commit", "-q", "-am", "x")
	hkExpectAllow(t, h.prePublish(t, repo, "git push"))
}

// hkDirtyPushState builds the H8 state: a committed source.txt modified
// without committing before `cw task start`, then assessed and verified, so
// the only record is verified and current while the tree is dirty.
func hkDirtyPushState(t *testing.T) (string, *hkHarness) {
	t.Helper()
	repo := hkRepo(t)
	hkWriteFile(t, repo, "source.txt", "committed source\n")
	hkGit(t, repo, "add", "source.txt")
	hkGit(t, repo, "commit", "-q", "-m", "initial")
	edited := []byte("edited source\n")
	hkWriteFile(t, repo, "source.txt", string(edited))
	h := newHKHarness(t)
	record := h.start(t, repo, hkSubstantialPlan)
	h.assessAndVerify(t, record, edited)
	return repo, h
}

func TestHookPrePushUncommittedEnforceVerifyOff(t *testing.T) {
	// H13 (rev 4): ENFORCE_VERIFY=false silences the H8 dirty-push ask.
	repo, h := hkDirtyPushState(t)
	// Control: the same state asks with the option unset.
	hkExpectAsk(t, h.prePublish(t, repo, "git push"), "uncommitted")
	h.env["CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY"] = "false"
	hkExpectAllow(t, h.prePublish(t, repo, "git push"))
}

func TestHookPrePublishDenyBeatsAsk(t *testing.T) {
	// H12 (rev 4): one Bash input publishing to A (open unverified record)
	// and B (no record, staged change meeting the ask thresholds) denies,
	// naming A's record, in either order.
	gated := hkRepo(t)
	h := newHKHarness(t)
	record := h.start(t, gated, hkSubstantialPlan)
	ungated := hkCommittedRepo(t, nil)
	hkWriteFile(t, ungated, "a/x.go", hkLines(200))
	hkWriteFile(t, ungated, "b/y.go", hkLines(200))
	hkGit(t, ungated, "add", "-A")
	cwd := hkCanonicalTempDir(t)
	// Controls: each publication alone.
	hkExpectAsk(t, h.prePublish(t, cwd, "git -C "+ungated+" commit -m x"), "cw task start", hkCommitAskWording)
	hkExpectDeny(t, h.prePublish(t, cwd, "git -C "+gated+" commit -m x"), record)
	tests := []struct {
		name    string
		command string
	}{
		{"gated first", "git -C " + gated + " commit -m x; git -C " + ungated + " commit -m x"},
		{"ungated first", "git -C " + ungated + " commit -m x; git -C " + gated + " commit -m x"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hkExpectDeny(t, h.prePublish(t, cwd, test.command), record)
		})
	}
}

func TestHookPrePublishFinishedRecord(t *testing.T) {
	// H1 wording and F10 through the hook (rev 4): record A verified on the
	// current bytes, record B started on the same bytes, then a file edited:
	// the deny names B and not A, and still offers `cw task abandon`.
	repo := hkRepo(t)
	source := []byte("verified source\n")
	hkWriteFile(t, repo, "source.txt", string(source))
	h := newHKHarness(t)
	a := h.start(t, repo, hkTitled("Finished A"))
	h.assessAndVerify(t, a, source)
	b := h.start(t, repo, hkTitled("Follow-up B"))
	// F10a precondition: B was created strictly after A's passing
	// verification, so B can finish A.
	verifiedA, err := taskflow.Load(a.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if verifiedA.Verification == nil || !b.CreatedAt.After(verifiedA.Verification.CheckedAt) {
		t.Fatalf("fixture: B created at %v, not after A's verification %+v", b.CreatedAt, verifiedA.Verification)
	}
	// Control: before the edit, B gates and A is current.
	hkExpectDenyOnly(t, h.prePublish(t, repo, "git commit -m x"), []taskflow.Record{b}, a)
	hkWriteFile(t, repo, "source.txt", "edited source\n")
	hkExpectDenyOnly(t, h.prePublish(t, repo, "git commit -m x"), []taskflow.Record{b}, a)
}

// runSessionStart runs `cw hook session-start` with args after the command.
func (h *hkHarness) runSessionStart(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	h.config.In = strings.NewReader(stdin)
	h.out.Reset()
	err := Execute(t.Context(), append([]string{"hook", "session-start"}, args...), h.config)
	return h.out.String(), err
}

// hkWriteInstructions writes an instructions file and returns its path.
func hkWriteInstructions(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(hkCanonicalTempDir(t), "AGENTS.md")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// hkSessionContext requires one SessionStart JSON line and returns its
// additionalContext.
func hkSessionContext(t *testing.T, got string) string {
	t.Helper()
	if !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Fatalf("hook wrote %q, want exactly one newline-terminated line", got)
	}
	var decoded struct {
		HookSpecificOutput map[string]any `json:"hookSpecificOutput"`
	}
	var whole map[string]any
	if err := json.Unmarshal([]byte(got), &whole); err != nil {
		t.Fatalf("session-start line %q is not JSON: %v", got, err)
	}
	if len(whole) != 1 {
		t.Fatalf("session-start object has members %v, want only hookSpecificOutput", whole)
	}
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.HookSpecificOutput) != 2 || decoded.HookSpecificOutput["hookEventName"] != "SessionStart" {
		t.Fatalf("hookSpecificOutput %v, want hookEventName SessionStart and additionalContext", decoded.HookSpecificOutput)
	}
	context, ok := decoded.HookSpecificOutput["additionalContext"].(string)
	if !ok {
		t.Fatalf("additionalContext is not a string: %v", decoded.HookSpecificOutput)
	}
	return context
}

func TestHookSessionStartInstructions(t *testing.T) {
	t.Run("emits file without trailing newlines", func(t *testing.T) {
		// R-SESSION: readable non-blank instructions become additionalContext
		// with trailing newlines removed.
		h := newHKHarness(t)
		file := hkWriteInstructions(t, "# Rules\n\nDo \"things\" `here`.\n\n\n")
		got, err := h.runSessionStart(t, "", "--instructions", file, "--cache", hkCanonicalTempDir(t))
		if err != nil {
			t.Fatalf("session-start: %v", err)
		}
		if context := hkSessionContext(t, got); context != "# Rules\n\nDo \"things\" `here`." {
			t.Fatalf("additionalContext %q", context)
		}
	})

	t.Run("missing or blank file writes nothing", func(t *testing.T) {
		// R-SESSION: a missing or blank instructions file writes nothing and
		// returns nil.
		tests := []struct {
			name string
			file func(t *testing.T) string
		}{
			{"missing", func(t *testing.T) string { return filepath.Join(hkCanonicalTempDir(t), "AGENTS.md") }},
			{"empty", func(t *testing.T) string { return hkWriteInstructions(t, "") }},
			{"newlines only", func(t *testing.T) string { return hkWriteInstructions(t, "\n\n\n") }},
			{"whitespace only", func(t *testing.T) string { return hkWriteInstructions(t, "  \n\t\n") }},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				h := newHKHarness(t)
				got, err := h.runSessionStart(t, "", "--instructions", test.file(t), "--cache", hkCanonicalTempDir(t))
				if err != nil || got != "" {
					t.Fatalf("session-start = %q, %v; want nothing and nil", got, err)
				}
			})
		}
	})

	t.Run("missing instructions flag is a usage error", func(t *testing.T) {
		// R-SESSION: --instructions is required.
		h := newHKHarness(t)
		if _, err := h.runSessionStart(t, "", "--cache", hkCanonicalTempDir(t)); err == nil {
			t.Fatal("session-start without --instructions returned nil")
		}
	})
}

// hkDoctorSummary splits additionalContext into the instructions and the
// one-line doctor summary, checks the summary's required members, and returns
// the raw summary line.
func hkDoctorSummary(t *testing.T, context, instructions string) string {
	t.Helper()
	line, ok := strings.CutPrefix(context, instructions+"\n\n")
	if !ok {
		t.Fatalf("additionalContext %q does not start with the instructions and a blank line", context)
	}
	if line == "" || strings.Contains(line, "\n") {
		t.Fatalf("doctor summary %q is not one line", line)
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(line), &summary); err != nil {
		t.Fatalf("doctor summary %q is not a JSON object: %v", line, err)
	}
	if _, ok := summary["status"]; !ok {
		t.Fatalf("doctor summary has no status: %s", line)
	}
	snapshot, ok := summary["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("doctor summary has no snapshot object: %s", line)
	}
	if fingerprint, ok := snapshot["fingerprint"].(string); !ok || !strings.HasPrefix(fingerprint, "sha256:") {
		t.Fatalf("snapshot.fingerprint %v does not start with sha256:", snapshot["fingerprint"])
	}
	return line
}

// hkDoctorCacheFile is <cache>/doctor/<hex sha256 of the canonical cwd>.json.
func hkDoctorCacheFile(cache, canonicalCwd string) string {
	hash := sha256.Sum256([]byte(canonicalCwd))
	return filepath.Join(cache, "doctor", hex.EncodeToString(hash[:])+".json")
}

// hkSameJSON reports whether two JSON documents decode to equal values.
func hkSameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var left, right any
	if err := json.Unmarshal(a, &left); err != nil {
		t.Fatalf("%q: %v", a, err)
	}
	if err := json.Unmarshal(b, &right); err != nil {
		t.Fatalf("%q: %v", b, err)
	}
	return reflect.DeepEqual(left, right)
}

func TestHookSessionStartDoctor(t *testing.T) {
	const instructions = "# Rules\n\nWork carefully."
	cwdInput := func(t *testing.T, cwd string) string {
		t.Helper()
		data, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "startup", "cwd": cwd})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("summary is appended, cached, reused and refreshed", func(t *testing.T) {
		// R-SESSION: with SESSION_DOCTOR=true the doctor summary is appended
		// and cached privately per canonical cwd for an hour.
		repo := hkRepo(t)
		hkGit(t, repo, "commit", "--allow-empty", "-q", "-m", "x")
		file := hkWriteInstructions(t, instructions+"\n")
		cache := hkCanonicalTempDir(t)
		h := newHKHarness(t)
		h.env["CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"] = "true"
		args := []string{"--instructions", file, "--cache", cache}
		cacheFile := hkDoctorCacheFile(cache, repo)

		got, err := h.runSessionStart(t, cwdInput(t, repo), args...)
		if err != nil {
			t.Fatalf("session-start: %v", err)
		}
		line := hkDoctorSummary(t, hkSessionContext(t, got), instructions)
		cached, err := os.ReadFile(cacheFile)
		if err != nil {
			t.Fatalf("doctor cache file: %v", err)
		}
		if !hkSameJSON(t, cached, []byte(line)) {
			t.Fatalf("cache file %s holds %q, want the appended summary %q", cacheFile, cached, line)
		}
		if info, err := os.Stat(cacheFile); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("cache file mode %v, %v; want 0600", info.Mode().Perm(), err)
		}
		if info, err := os.Stat(filepath.Dir(cacheFile)); err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("cache directory mode %v, %v; want 0700", info.Mode().Perm(), err)
		}

		t.Run("fresh cache is reused verbatim", func(t *testing.T) {
			// R-SESSION: a second call within an hour reads the cache file.
			if err := os.WriteFile(cacheFile, []byte(`{"status":"cached"}`), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := h.runSessionStart(t, cwdInput(t, repo), args...)
			if err != nil {
				t.Fatalf("session-start: %v", err)
			}
			if context, want := hkSessionContext(t, got), instructions+"\n\n"+`{"status":"cached"}`; context != want {
				t.Fatalf("additionalContext %q, want %q", context, want)
			}
		})

		t.Run("stale cache is replaced", func(t *testing.T) {
			// R-SESSION: a cache file older than an hour is recomputed.
			if err := os.WriteFile(cacheFile, []byte(`{"status":"cached"}`), 0600); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(cacheFile, old, old); err != nil {
				t.Fatal(err)
			}
			got, err := h.runSessionStart(t, cwdInput(t, repo), args...)
			if err != nil {
				t.Fatalf("session-start: %v", err)
			}
			line := hkDoctorSummary(t, hkSessionContext(t, got), instructions)
			if line == `{"status":"cached"}` {
				t.Fatal("stale cache was reused")
			}
			replaced, err := os.ReadFile(cacheFile)
			if err != nil {
				t.Fatal(err)
			}
			if !hkSameJSON(t, replaced, []byte(line)) {
				t.Fatalf("cache file holds %q after refresh, want %q", replaced, line)
			}
		})
	})

	t.Run("working directory when stdin has no cwd", func(t *testing.T) {
		// R-SESSION: missing or malformed stdin falls back to WorkingDir.
		for _, stdin := range []string{"", "not json {"} {
			t.Run(fmt.Sprintf("stdin %q", stdin), func(t *testing.T) {
				repo := hkRepo(t)
				file := hkWriteInstructions(t, instructions)
				cache := hkCanonicalTempDir(t)
				h := newHKHarness(t)
				h.env["CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"] = "true"
				h.config.WorkingDir = func() (string, error) { return repo, nil }
				got, err := h.runSessionStart(t, stdin, "--instructions", file, "--cache", cache)
				if err != nil {
					t.Fatalf("session-start: %v", err)
				}
				hkDoctorSummary(t, hkSessionContext(t, got), instructions)
				if _, err := os.Stat(hkDoctorCacheFile(cache, repo)); err != nil {
					t.Fatalf("summary not cached for the working directory: %v", err)
				}
			})
		}
	})

	t.Run("empty cache flag caches nothing", func(t *testing.T) {
		// R-SESSION: --cache "" still appends the summary but writes no cache.
		repo := hkRepo(t)
		file := hkWriteInstructions(t, instructions)
		h := newHKHarness(t)
		h.env["CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"] = "true"
		_, beforeErr := os.Stat("doctor")
		got, err := h.runSessionStart(t, cwdInput(t, repo), "--instructions", file, "--cache", "")
		if err != nil {
			t.Fatalf("session-start: %v", err)
		}
		hkDoctorSummary(t, hkSessionContext(t, got), instructions)
		if _, err := os.Stat("doctor"); os.IsNotExist(beforeErr) && !os.IsNotExist(err) {
			t.Fatal("session-start with --cache \"\" created a doctor cache in the working directory")
		}
	})

	t.Run("plain directory still has a fingerprint", func(t *testing.T) {
		// R-SESSION: a directory outside Git still yields a summary whose
		// snapshot has a sha256 fingerprint.
		dir := hkCanonicalTempDir(t)
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0600); err != nil {
			t.Fatal(err)
		}
		file := hkWriteInstructions(t, instructions)
		h := newHKHarness(t)
		h.env["CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"] = "true"
		got, err := h.runSessionStart(t, cwdInput(t, dir), "--instructions", file, "--cache", hkCanonicalTempDir(t))
		if err != nil {
			t.Fatalf("session-start: %v", err)
		}
		hkDoctorSummary(t, hkSessionContext(t, got), instructions)
	})

	t.Run("doctor off appends nothing", func(t *testing.T) {
		// R-SESSION: only SESSION_DOCTOR=true appends a summary.
		repo := hkRepo(t)
		file := hkWriteInstructions(t, instructions)
		h := newHKHarness(t)
		h.env["CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"] = "false"
		got, err := h.runSessionStart(t, cwdInput(t, repo), "--instructions", file, "--cache", hkCanonicalTempDir(t))
		if err != nil {
			t.Fatalf("session-start: %v", err)
		}
		if context := hkSessionContext(t, got); context != instructions {
			t.Fatalf("additionalContext %q, want the instructions alone", context)
		}
	})
}

const hkVersionInstructions = "# Rules\n\nWork carefully."

// hkManifest is a plugin manifest that holds version.
func hkManifest(version string) string {
	return `{"name":"agent-workflows","version":"` + version + `"}`
}

// hkPluginRoot creates a plugin root whose global/AGENTS.md holds
// hkVersionInstructions, writes each manifest's content to
// <root>/<directory>/plugin.json, and returns the root and the instructions
// path.
func hkPluginRoot(t *testing.T, manifests map[string]string) (string, string) {
	t.Helper()
	root := hkCanonicalTempDir(t)
	for directory, content := range manifests {
		if err := os.MkdirAll(filepath.Join(root, directory), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, directory, "plugin.json"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	instructions := filepath.Join(root, "global", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(instructions), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instructions, []byte(hkVersionInstructions+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root, instructions
}

// hkVersionNotes runs session-start as cw cwVersion, with env, for a plugin
// root holding manifests, and returns what follows the instructions and a
// blank line: "" when the context is the instructions alone. In env, <root>
// stands for the plugin root.
func hkVersionNotes(t *testing.T, cwVersion string, manifests, env map[string]string) string {
	t.Helper()
	root, instructions := hkPluginRoot(t, manifests)
	h := newHKHarness(t)
	h.config.Version = cwVersion
	for name, value := range env {
		h.env[name] = strings.ReplaceAll(value, "<root>", root)
	}
	got, err := h.runSessionStart(t, "", "--instructions", instructions, "--cache", hkCanonicalTempDir(t))
	if err != nil {
		t.Fatalf("session-start: %v", err)
	}
	context := hkSessionContext(t, got)
	if context == hkVersionInstructions {
		return ""
	}
	notes, ok := strings.CutPrefix(context, hkVersionInstructions+"\n\n")
	if !ok {
		t.Fatalf("additionalContext %q does not start with the instructions and a blank line", context)
	}
	return notes
}

func TestHookSessionStartNamesThePluginAndCWVersionsWhenTheyDiffer(t *testing.T) {
	// Oracle: when .claude-plugin/plugin.json above the instructions and cw
	// differ on major.minor.patch or pre-release, the context is the
	// instructions, a blank line and one line naming both versions and the
	// one to update, chosen by semantic version precedence.
	tests := []struct {
		name, plugin, cw, want string
	}{
		{"plugin newer", "3.0.0-rc.2", "v3.0.0-rc.1", "plugin 3.0.0-rc.2, cw 3.0.0-rc.1: upgrade cw"},
		{"cw newer", "3.0.0-rc.1", "v3.0.0-rc.2", "plugin 3.0.0-rc.1, cw 3.0.0-rc.2: update the plugin"},
		{"release above its pre-release", "3.0.0", "v3.0.0-rc.5", "plugin 3.0.0, cw 3.0.0-rc.5: upgrade cw"},
		{"numeric pre-release by value", "3.0.0-rc.10", "v3.0.0-rc.9", "plugin 3.0.0-rc.10, cw 3.0.0-rc.9: upgrade cw"},
		{"minor", "3.1.0", "v3.0.9", "plugin 3.1.0, cw 3.0.9: upgrade cw"},
		{"cw build metadata kept in the line", "3.0.0-rc.2", "v3.0.0-rc.1+dirty", "plugin 3.0.0-rc.2, cw 3.0.0-rc.1+dirty: upgrade cw"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hkVersionNotes(t, test.cw, map[string]string{".claude-plugin": hkManifest(test.plugin)}, nil); got != test.want {
				t.Fatalf("version notes %q, want %q", got, test.want)
			}
		})
	}

	t.Run("before the doctor summary", func(t *testing.T) {
		// Oracle: with session_doctor on, the line sits between the
		// instructions and the doctor summary, each after a blank line.
		repo := hkRepo(t)
		_, instructions := hkPluginRoot(t, map[string]string{".claude-plugin": hkManifest("3.0.0-rc.2")})
		h := newHKHarness(t)
		h.config.Version = "v3.0.0-rc.1"
		h.env["CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"] = "true"
		stdin, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "startup", "cwd": repo})
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.runSessionStart(t, string(stdin), "--instructions", instructions, "--cache", hkCanonicalTempDir(t))
		if err != nil {
			t.Fatalf("session-start: %v", err)
		}
		hkDoctorSummary(t, hkSessionContext(t, got), hkVersionInstructions+"\n\nplugin 3.0.0-rc.2, cw 3.0.0-rc.1: upgrade cw")
	})
}

func TestHookSessionStartSaysNothingWhenVersionsMatchOrCWIsADevBuild(t *testing.T) {
	// Oracle: the context is the instructions alone when the versions match
	// on major.minor.patch and pre-release, when cw is a development build
	// (dev or a Go pseudo-version), and when the manifest or its version
	// cannot be read. The control shows that the same fixture with a
	// released cw that differs gets the line, so the silence comes from the
	// match or the skip.
	const pluginNewer = "3.0.0-rc.2"
	if got := hkVersionNotes(t, "v3.0.0-rc.1", map[string]string{".claude-plugin": hkManifest(pluginNewer)}, nil); got == "" {
		t.Fatal("control: plugin 3.0.0-rc.2 with cw v3.0.0-rc.1 gave no version line")
	}
	tests := []struct {
		name, cw  string
		manifests map[string]string
	}{
		{"same version", "v3.0.0-rc.1", map[string]string{".claude-plugin": hkManifest("3.0.0-rc.1")}},
		{"same version without v", "3.0.0-rc.1", map[string]string{".claude-plugin": hkManifest("3.0.0-rc.1")}},
		{"build metadata ignored", "v3.0.0-rc.1+dirty", map[string]string{".claude-plugin": hkManifest("3.0.0-rc.1")}},
		{"same release", "v3.0.0", map[string]string{".claude-plugin": hkManifest("3.0.0")}},
		{"dev", "dev", map[string]string{".claude-plugin": hkManifest(pluginNewer)}},
		{"devel", "(devel)", map[string]string{".claude-plugin": hkManifest(pluginNewer)}},
		{"pseudo-version after a pre-release tag", "v3.0.0-rc.1.0.20261112143000-abcdef123456", map[string]string{".claude-plugin": hkManifest(pluginNewer)}},
		{"pseudo-version after a release tag", "v3.0.1-0.20261112143000-abcdef123456", map[string]string{".claude-plugin": hkManifest(pluginNewer)}},
		{"pseudo-version with no tag", "v3.0.0-20261112143000-abcdef123456", map[string]string{".claude-plugin": hkManifest(pluginNewer)}},
		{"dirty pseudo-version", "v3.0.0-rc.1.0.20261112143000-abcdef123456+dirty", map[string]string{".claude-plugin": hkManifest(pluginNewer)}},
		{"no manifest", "v3.0.0-rc.1", nil},
		{"manifest without a version", "v3.0.0-rc.1", map[string]string{".claude-plugin": `{"name":"agent-workflows"}`}},
		{"manifest version not semver", "v3.0.0-rc.1", map[string]string{".claude-plugin": hkManifest("next")}},
		{"manifest not JSON", "v3.0.0-rc.1", map[string]string{".claude-plugin": `{"version":"3.0.0-rc.2"`}},
		{"only the Codex manifest under Claude Code", "v3.0.0-rc.1", map[string]string{".codex-plugin": hkManifest(pluginNewer)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hkVersionNotes(t, test.cw, test.manifests, nil); got != "" {
				t.Fatalf("version notes %q, want none", got)
			}
		})
	}
}

func TestHookSessionStartComparesTheManifestOfTheHost(t *testing.T) {
	// Oracle: under Codex, which sets PLUGIN_ROOT, cw compares
	// .codex-plugin/plugin.json; under Claude Code, .claude-plugin/plugin.json.
	// Each case gives the two manifests different versions, so the line
	// appears exactly when the host's manifest differs from cw.
	codex := map[string]string{"PLUGIN_ROOT": "<root>"}
	tests := []struct {
		name, claude, codex string
		env                 map[string]string
		want                string
	}{
		{"Codex reads its own manifest", "3.0.0-rc.1", "3.0.0-rc.2", codex, "plugin 3.0.0-rc.2, cw 3.0.0-rc.1: upgrade cw"},
		{"Claude Code reads its own manifest", "3.0.0-rc.2", "3.0.0-rc.1", nil, "plugin 3.0.0-rc.2, cw 3.0.0-rc.1: upgrade cw"},
		{"Codex ignores the Claude Code manifest", "3.0.0-rc.2", "3.0.0-rc.1", codex, ""},
		{"Claude Code ignores the Codex manifest", "3.0.0-rc.1", "3.0.0-rc.2", nil, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifests := map[string]string{".claude-plugin": hkManifest(test.claude), ".codex-plugin": hkManifest(test.codex)}
			if got := hkVersionNotes(t, "v3.0.0-rc.1", manifests, test.env); got != test.want {
				t.Fatalf("version notes %q, want %q", got, test.want)
			}
		})
	}
}

func TestHookSessionStartNamesAClaudeCodeOlderThanTheMinimum(t *testing.T) {
	// Oracle: under Claude Code, an AI_AGENT of the form
	// claude-code_<major>-<minor>-<patch>_<role> below 2.1.292 adds one line
	// naming the client version and the minimum; at or above it, under
	// Codex, and for any other value there is no line. With plugin drift
	// too, the plugin line comes first.
	matching := map[string]string{".claude-plugin": hkManifest("3.0.0-rc.1")}
	const oldClient = "Claude Code 2.1.291, agent-workflows needs 2.1.292: upgrade Claude Code"
	tests := []struct {
		name, agent string
		codex       bool
		manifests   map[string]string
		want        string
	}{
		{"one below the minimum", "claude-code_2-1-291_harness", false, matching, oldClient},
		{"older minor", "claude-code_2-0-999_harness", false, matching, "Claude Code 2.0.999, agent-workflows needs 2.1.292: upgrade Claude Code"},
		{"Bash tool form", "claude-code_1-9-400_agent", false, matching, "Claude Code 1.9.400, agent-workflows needs 2.1.292: upgrade Claude Code"},
		{"with plugin drift", "claude-code_2-1-291_harness", false, map[string]string{".claude-plugin": hkManifest("3.0.0-rc.2")}, "plugin 3.0.0-rc.2, cw 3.0.0-rc.1: upgrade cw\n" + oldClient},
		{"at the minimum", "claude-code_2-1-292_harness", false, matching, ""},
		{"newer", "claude-code_2-1-311_harness", false, matching, ""},
		{"newer major", "claude-code_3-0-0_harness", false, matching, ""},
		{"unset", "", false, matching, ""},
		{"another agent", "cursor", false, matching, ""},
		{"another format", "claude-code/2.1.200", false, matching, ""},
		{"two numbers", "claude-code_2-1_harness", false, matching, ""},
		{"under Codex", "claude-code_2-1-291_harness", true, matching, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := map[string]string{"AI_AGENT": test.agent}
			manifests := test.manifests
			if test.codex {
				env["PLUGIN_ROOT"] = "<root>"
				manifests = map[string]string{".codex-plugin": manifests[".claude-plugin"]}
			}
			if got := hkVersionNotes(t, "v3.0.0-rc.1", manifests, env); got != test.want {
				t.Fatalf("version notes %q, want %q", got, test.want)
			}
		})
	}
}

func TestMinimumClaudeCodeVersionIsTheREADMEs(t *testing.T) {
	// Oracle: README.md's Quick start states minClaudeCodeVersion as the
	// minimum Claude Code version, so the session-start line and the README
	// cannot drift apart.
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "**Claude Code version.** " + minClaudeCodeVersion + " or newer"; !strings.Contains(string(readme), want) {
		t.Fatalf("README.md does not contain %q", want)
	}
}

// hkGateLine is the session-start line that suggests cw gate install,
// spelled out here as the oracle rather than taken from the code under test.
const hkGateLine = "git hooks absent, so a commit or push from a script, an alias or a terminal skips the verify gate: suggest cw gate install to the user"

func TestHookSessionStartSuggestsGateInstallOnlyForAnOpenTaskWithoutGitHooks(t *testing.T) {
	// Oracle: the session-start context is the instructions, a blank line and
	// a line that suggests cw gate install exactly when the session's
	// repository has an open substantial task and a git hook is absent from
	// the repository's own hooks directory, or is cw's script without an
	// execute bit, with no foreign hook beside it; in every other case below
	// it is the instructions alone. The line is hkGateLine when every hook is
	// absent; when the others are installed, it names the absent hooks and
	// says what skips the gate without them, only a merge commit for
	// pre-merge-commit (F4 review finding 3). The first case is the control
	// for the others. Each case has a cache of its own, so no earlier
	// suggestion silences it.
	const instructions = "# Rules\n\nWork carefully."
	// cw gate reads git's configuration from this process's environment.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gate := func(t *testing.T, h *hkHarness, args ...string) {
		t.Helper()
		h.out.Reset()
		if err := Execute(t.Context(), append([]string{"gate"}, args...), h.config); err != nil {
			t.Fatalf("cw gate %v: %v\n%s", args, err, h.out.String())
		}
	}
	const pushLine = "git hook pre-push absent, so a push from a script, an alias or a terminal skips the verify gate: suggest cw gate install to the user"
	tests := []struct {
		name  string
		setup func(t *testing.T, h *hkHarness, repo string)
		want  string
	}{
		{"open substantial task without git hooks", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
		}, hkGateLine},
		{"open substantial task with one git hook absent", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
			gate(t, h, "install", "--repo", repo)
			if err := os.Remove(filepath.Join(repo, ".git", "hooks", "pre-push")); err != nil {
				t.Fatal(err)
			}
		}, pushLine},
		{"open substantial task with cw's hook without an execute bit", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
			gate(t, h, "install", "--repo", repo)
			// Git skips a hook it cannot execute.
			if err := os.Chmod(filepath.Join(repo, ".git", "hooks", "pre-push"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, pushLine},
		// An install by a cw that wrote no pre-merge-commit.
		{"open substantial task with only the pre-commit and pre-push hooks", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
			gate(t, h, "install", "--repo", repo)
			if err := os.Remove(filepath.Join(repo, ".git", "hooks", "pre-merge-commit")); err != nil {
				t.Fatal(err)
			}
		}, "git hook pre-merge-commit absent, so a merge commit made without conflicts skips the verify gate: suggest cw gate install to the user"},
		{"open substantial task with only the pre-push hook", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
			gate(t, h, "install", "--repo", repo)
			for _, name := range []string{"pre-commit", "pre-merge-commit"} {
				if err := os.Remove(filepath.Join(repo, ".git", "hooks", name)); err != nil {
					t.Fatal(err)
				}
			}
		}, "git hooks pre-commit and pre-merge-commit absent, so a commit from a script, an alias or a terminal, or a merge commit made without conflicts skips the verify gate: suggest cw gate install to the user"},
		{"verified task that is still open", func(t *testing.T, h *hkHarness, repo string) {
			source := []byte("verified source\n")
			hkWriteFile(t, repo, "source.txt", string(source))
			h.assessAndVerify(t, h.start(t, repo, hkSubstantialPlan), source)
		}, hkGateLine},
		{"no task", func(*testing.T, *hkHarness, string) {}, ""},
		{"small task", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSmallPlan)
		}, ""},
		{"abandoned task", func(t *testing.T, h *hkHarness, repo string) {
			h.abandon(t, h.start(t, repo, hkSubstantialPlan), "replaced")
		}, ""},
		{"open task in another repository", func(t *testing.T, h *hkHarness, _ string) {
			h.start(t, hkRepo(t), hkSubstantialPlan)
		}, ""},
		{"git hooks installed", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
			gate(t, h, "install", "--repo", repo)
		}, ""},
		{"a hook cw gate install did not write", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
			hkWriteFile(t, repo, ".git/hooks/pre-commit", "#!/bin/sh\nexit 0\n")
		}, ""},
		{"hooks directory other repositories use", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
			global := hkCanonicalTempDir(t)
			hkWriteFile(t, global, "gitconfig", "[core]\n\thooksPath = "+hkCanonicalTempDir(t)+"\n")
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(global, "gitconfig"))
		}, ""},
		{"gate turned off", func(t *testing.T, h *hkHarness, repo string) {
			h.start(t, repo, hkSubstantialPlan)
			h.env[enforceVerifyOption] = "false"
		}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := hkRepo(t)
			h := newHKHarness(t)
			test.setup(t, h, repo)
			stdin, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "startup", "cwd": repo})
			if err != nil {
				t.Fatal(err)
			}
			got, err := h.runSessionStart(t, string(stdin), "--instructions", hkWriteInstructions(t, instructions), "--cache", hkCanonicalTempDir(t))
			if err != nil {
				t.Fatalf("session-start: %v", err)
			}
			want := instructions
			if test.want != "" {
				want += "\n\n" + test.want
			}
			if context := hkSessionContext(t, got); context != want {
				t.Fatalf("additionalContext %q, want %q", context, want)
			}
		})
	}
}

// sessionContextIn runs session-start for repo with the instructions file
// and cache, and returns its additionalContext.
func (h *hkHarness) sessionContextIn(t *testing.T, repo, instructions, cache string) string {
	t.Helper()
	stdin, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "resume", "cwd": repo})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.runSessionStart(t, string(stdin), "--instructions", instructions, "--cache", cache)
	if err != nil {
		t.Fatalf("session-start: %v", err)
	}
	return hkSessionContext(t, got)
}

func TestHookSessionStartSuggestsGateInstallOncePerTask(t *testing.T) {
	// Oracle: with one cache, the session-start context for a repository
	// with an open substantial task and no git hooks carries hkGateLine the
	// first time and is the instructions alone after that; a second open
	// task brings the line back once. Without a cache, or with a cache where
	// the suggestion cannot be recorded, every context carries the line.
	const instructions = "# Rules\n\nWork carefully."
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	file := hkWriteInstructions(t, instructions)
	withLine := instructions + "\n\n" + hkGateLine
	expect := func(t *testing.T, h *hkHarness, repo, cache string, want ...string) {
		t.Helper()
		for i, context := range want {
			if got := h.sessionContextIn(t, repo, file, cache); got != context {
				t.Fatalf("session start %d: additionalContext %q, want %q", i+1, got, context)
			}
		}
	}

	t.Run("once per task", func(t *testing.T) {
		repo := hkRepo(t)
		h := newHKHarness(t)
		cache := hkCanonicalTempDir(t)
		h.start(t, repo, hkSubstantialPlan)
		expect(t, h, repo, cache, withLine, instructions, instructions)
		h.start(t, repo, hkTitled("Second"))
		expect(t, h, repo, cache, withLine, instructions)
	})

	t.Run("without a cache", func(t *testing.T) {
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.start(t, repo, hkSubstantialPlan)
		expect(t, h, repo, "", withLine, withLine)
	})

	t.Run("cache that cannot be written", func(t *testing.T) {
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.start(t, repo, hkSubstantialPlan)
		cache := filepath.Join(hkCanonicalTempDir(t), "file")
		hkWriteFile(t, filepath.Dir(cache), "file", "a regular file\n")
		expect(t, h, repo, cache, withLine, withLine)
	})
}

func TestHookSessionStartReturnsWhileItsWriterKeepsStdinOpen(t *testing.T) {
	// Oracle: with stdin left open by its writer, session-start returns
	// well within the 10 second wait below. With enforce_verify false and
	// session_doctor off it reads no stdin and its context is the
	// instructions alone. With the gate on it stops reading stdin after its
	// bound and takes the working directory as the session's, so the
	// repository's open task gets hkGateLine.
	const instructions = "# Rules\n\nWork carefully."
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	file := hkWriteInstructions(t, instructions)
	run := func(t *testing.T, h *hkHarness) string {
		t.Helper()
		reader, writer := io.Pipe()
		t.Cleanup(func() { writer.Close() })
		h.config.In = reader
		h.out.Reset()
		args := []string{"hook", "session-start", "--instructions", file, "--cache", hkCanonicalTempDir(t)}
		done := make(chan error, 1)
		go func() { done <- Execute(t.Context(), args, h.config) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("session-start: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("session-start still waited for stdin after 10 seconds")
		}
		return hkSessionContext(t, h.out.String())
	}

	for _, test := range []struct {
		name, enforce, want string
	}{
		{"gate off and no doctor summary", "false", instructions},
		{"gate on", "", instructions + "\n\n" + hkGateLine},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := hkRepo(t)
			h := newHKHarness(t)
			h.start(t, repo, hkSubstantialPlan) // the working directory is now repo
			h.env[enforceVerifyOption] = test.enforce
			if got := run(t, h); got != test.want {
				t.Fatalf("additionalContext %q, want %q", got, test.want)
			}
		})
	}
}
