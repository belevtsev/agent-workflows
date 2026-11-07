package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/taskflow"
)

// Tests for `cw hook pre-publish` (R-PRE, R-CMD) and `cw hook session-start`
// (R-SESSION), driven through Execute.

// hkSubstantialPlan is a substantial plan whose verify can pass without checks
// or adviser checkpoints.
const hkSubstantialPlan = `{"title":"T","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":[],"allow_non_execution":true}],"checks":[]}`

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
	config, out, _, _ := testConfig(t)
	harness := &hkHarness{out: out, home: hkCanonicalTempDir(t), env: map[string]string{}}
	config.HomeDir = func() (string, error) { return harness.home, nil }
	config.Getenv = func(name string) string { return harness.env[name] }
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

// hkExpectDeny requires exactly one line holding the deny decision for record.
func hkExpectDeny(t *testing.T, got string, record taskflow.Record) {
	t.Helper()
	if !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Fatalf("hook wrote %q, want exactly one newline-terminated line", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("deny line %q is not JSON: %v", got, err)
	}
	reason := fmt.Sprintf("agent-workflows: task %s (%s) for this repository has no passing cw task verify on the current source; run `cw task verify --task %s --summary` and retry, or set enforce_verify=false in the plugin config", record.ID, record.Plan.Title, record.Directory)
	want := map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("deny decision\n got: %v\nwant: %v", decoded, want)
	}
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
		Requirements: []taskflow.Requirement{{ID: "source", Description: "Inspect actual source", CheckIDs: []string{}, AllowNonExecution: true}},
		Checks:       []taskflow.Check{},
	}
	record, err := taskflow.Start(t.Context(), repo, store, plan, nil)
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

func TestHookPrePublishNewestRecordDecides(t *testing.T) {
	t.Run("deny names the newest open record", func(t *testing.T) {
		// R-PRE: with two open records the deny reason names the newest.
		repo := hkRepo(t)
		h := newHKHarness(t)
		older := h.start(t, repo, hkSubstantialPlan)
		newer := h.start(t, repo, strings.Replace(hkSubstantialPlan, `"title":"T"`, `"title":"Newer"`, 1))
		if !newer.CreatedAt.After(older.CreatedAt) {
			t.Fatalf("second record created %v, not after the first %v", newer.CreatedAt, older.CreatedAt)
		}
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), newer)
	})

	t.Run("newer verified record hides an older open one", func(t *testing.T) {
		// R-PRE, R-FIND: older records never decide, so a passing verify on
		// the newest record allows although an older record is open.
		repo := hkRepo(t)
		source := []byte("verified source\n")
		if err := os.WriteFile(filepath.Join(repo, "source.txt"), source, 0600); err != nil {
			t.Fatal(err)
		}
		h := newHKHarness(t)
		h.start(t, repo, hkSubstantialPlan)
		newer := h.start(t, repo, strings.Replace(hkSubstantialPlan, `"title":"T"`, `"title":"Newer"`, 1))
		hkExpectDeny(t, h.prePublish(t, repo, "git commit -m x"), newer)
		h.assessAndVerify(t, newer, source)
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
	h := newHKHarness(t)
	record := h.start(t, repo, hkSubstantialPlan)
	tests := []struct {
		command string
		deny    bool
	}{
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
	// R-CMD rev 3, R-PRE rev 3: directory changes the hook cannot follow make
	// the target unknown, `pushd DIR` moves like `cd DIR`, and parentheses and
	// backticks scope a directory change. gated (A) has an open record;
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
		// R-PRE rev 3 required: from A, a directory change the hook cannot
		// follow makes the target unknown, so the commit is not gated.
		{"cd -P makes the target unknown", gated, "cd -P " + ungated + " && git commit -m x", false},
		{"cd -- makes the target unknown", gated, "cd -- " + ungated + " && git commit -m x", false},
		{"cd - makes the target unknown", gated, "cd - && git commit -m x", false},
		{"cd without an argument makes the target unknown", gated, "cd && git commit -m x", false},
		{"cd ~ makes the target unknown", gated, "cd ~ && git commit -m x", false},
		{"cd $HOME makes the target unknown", gated, "cd $HOME && git commit -m x", false},
		{"quoted cd path with a space makes the target unknown", gated, `cd "` + ungated + `/sp ace" && git commit -m x`, false},
		{"popd makes the target unknown", gated, "popd && git commit -m x", false},
		{"pushd -n makes the target unknown", gated, "pushd -n " + ungated + " && git commit -m x", false},
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
		// R-PRE rev 4 required: an option still makes the directory unknown
		// when a redirection follows.
		{"cd -P with a redirection makes the target unknown", ungated, "cd -P " + gated + " 2>/dev/null && git push", false},
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
