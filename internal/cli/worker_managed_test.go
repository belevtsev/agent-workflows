package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

const managedFixtureConfig = `version = 2
[paths]
tests = ["tests/**"]
[roles.lookup]
host = "codex"
[roles.spec-writer]
host = "claude"
[roles.architect]
host = "codex"
[roles.quality-engineer]
host = "claude"
[roles.developer]
host = "claude"
[roles.reviewer]
host = "codex"
model = "configured-reviewer"
effort = "high"
[roles.git]
host = "codex"
model = "configured-git"
effort = "high"
`

func prepareConfiguredFixture(t *testing.T, r *kitsutest.Repo, role, stage, workflow string,
	options ...string,
) kitsutest.Result {
	t.Helper()
	args := []string{"task", "prepare", "fix-login", "--role", role, "--stage", stage,
		"--round", "1", "--workflow", workflow, "--assignment", "-", "--dispatch"}
	return r.Run("Follow the recorded assignment. Keep existing edits.\n",
		filepath.Join(kitsutest.Binary(t), compactBinaryName()), append(args, options...)...)
}

func installManagedCodexFixture(t *testing.T, r *kitsutest.Repo, model string, dispatch map[string]any) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a native executable shell script")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
set -eu
if [ "$1" = "app-server" ]; then
  IFS= read -r init
  printf '%s\n' '{"id":1,"result":{}}'
  IFS= read -r initialized
  IFS= read -r thread
  cat "$KITSU_MANAGED_THREAD"; printf '\n'
  IFS= read -r turn
  printf 'start\n' >> "$KITSU_MANAGED_LAUNCHES"
  if [ "${KITSU_MANAGED_OMIT_HOOKS:-}" = "" ]; then
    kitsu tokens record --host codex < "$KITSU_MANAGED_REVIEW_START"
    kitsu tokens record --host codex < "$KITSU_MANAGED_REVIEW_STOP"
  fi
  printf '%s\n' '{"id":3,"result":{"turn":{"id":"fixture-review-turn"}}}'
  printf '%s\n' '{"method":"turn/completed","params":{"threadId":"fixture-review-parent","turn":{"id":"fixture-review-turn","status":"completed"}}}'
  exit 0
fi
[ "$1" = "exec" ]
shift
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message) output="$2"; shift 2 ;;
    --model) [ "$2" = "$KITSU_MANAGED_MODEL" ]; shift 2 ;;
    --sandbox) [ "$2" = "read-only" ]; shift 2 ;;
    *) shift ;;
  esac
done
cat > /dev/null
printf 'start\n' >> "$KITSU_MANAGED_LAUNCHES"
printf '%s\n' '{"type":"thread.started","thread_id":"fixture-native-worker"}' '{"type":"turn.started"}'
cat "$KITSU_MANAGED_RESULT" > "$output"
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":12,"output_tokens":4}}'
`
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	r.Setenv("PATH", bin+string(os.PathListSeparator)+kitsutest.Binary(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r.Setenv("KITSU_MANAGED_MODEL", model)
	r.Setenv("KITSU_MANAGED_LAUNCHES", r.Path(".git/fixture-launches"))
	r.Setenv("KITSU_MANAGED_RESULT", r.Path(".git/fixture-managed-result.json"))
	id := stageID(t, dispatch)
	report := workerReturnRequest{
		Version: 1, DispatchID: id, Task: "fix-login", PacketSHA256: dispatch["sha256"].(string),
		ChangedPaths: []string{}, Checks: []stageCheck{}, Findings: []workerFinding{}, Blockers: []string{},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(".git/fixture-managed-result.json", string(data))
	if dispatch["role"] == "reviewer" {
		installManagedReviewNotices(t, r, id, model, data)
	}
	return id
}

func installManagedReviewNotices(t *testing.T, r *kitsutest.Repo, id, model string, payload []byte) {
	t.Helper()
	var record stageRecord
	if err := json.Unmarshal([]byte(r.Read(".git/kitsu-stages/"+id+".json")), &record); err != nil {
		t.Fatal(err)
	}
	const parent, worker = "fixture-review-parent", "fixture-native-worker"
	thread := fmt.Sprintf(`{"id":2,"result":{"thread":{"id":%q,"sessionId":%q},"cwd":%q,"model":%q,"sandbox":{"type":"readOnly"},"approvalPolicy":"never","reasoningEffort":"high"}}`, parent, parent, r.Dir, model)
	r.Write(".git/fixture-review-thread.json", thread)
	r.Setenv("KITSU_MANAGED_THREAD", r.Path(".git/fixture-review-thread.json"))
	report, err := json.Marshal(managedReviewReport{Candidate: managedReviewCandidate(record), Result: payload})
	if err != nil {
		t.Fatal(err)
	}
	for event, file := range map[string]string{"SubagentStart": "start", "SubagentStop": "stop"} {
		notice := tokens.Notice{Event: event, AgentID: worker, Session: parent, Cwd: r.Dir}
		if event == "SubagentStop" {
			notice.LastAssistant = string(report)
		}
		data, err := json.Marshal(notice)
		if err != nil {
			t.Fatal(err)
		}
		path := ".git/fixture-review-" + file + ".json"
		r.Write(path, string(data))
		r.Setenv("KITSU_MANAGED_REVIEW_"+strings.ToUpper(file), r.Path(path))
	}
}

func TestConfiguredManagedWorkerUsesPinnedRoute(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("kitsu-roles.toml", managedFixtureConfig)
	r.Setenv("KITSU_HOST", "claude")
	wrong := prepareConfiguredFixture(t, r, "reviewer", "review", "small", "--host", "claude")
	if wrong.Code == 0 || !strings.Contains(wrong.Stderr, "assigned to codex") || r.Exists(".git/kitsu-stages/active.json") {
		t.Fatal(wrong.Output())
	}
	d := stageJSON(t, prepareConfiguredFixture(t, r, "reviewer", "review", "small"))
	native := d["native"].(map[string]any)
	if native["host"] != "codex" || native["model"] != "configured-reviewer" || native["effort"] != "high" {
		t.Fatalf("dispatch did not bind the configured profile: %v", native)
	}
	id := installManagedCodexFixture(t, r, "configured-reviewer", d)
	r.Write("source.txt", "Outside edit after reservation.\n")
	if result := r.Kitsu("task", "dispatch", id, "--run"); result.Code == 0 || r.Exists(".git/fixture-launches") {
		t.Fatal("stale dispatch launched a model", result.Output())
	}
	r.Write("source.txt", "original\n")
	first := r.Kitsu("task", "dispatch", id, "--run")
	result := stageJSON(t, first)
	if result["host_session"] != "fixture-review-parent" || result["review_worker"] != "fixture-native-worker" || result["status"] != "returned" {
		t.Fatalf("lost the observed native return: %v", result)
	}
	replay := r.Kitsu("task", "dispatch", id, "--run")
	if replay.Code != 0 || replay.Stdout != first.Stdout || r.Read(".git/fixture-launches") != "start\n" {
		t.Fatal("replaying a saved return launched again", replay.Output())
	}
	bound := stageJSON(t, r.Kitsu("task", "return", id, "--request", result["result_path"].(string)))
	if bound["authority"] != "untrusted-worker-claims" || !r.Exists(".git/kitsu-stages/active.json") {
		t.Fatalf("native handback gained approval or completed its interval: %v", bound)
	}
	stageJSON(t, r.Kitsu("task", "finish", id))
	r.Write("kitsu-roles.toml", strings.Replace(managedFixtureConfig, "configured-reviewer", "different-reviewer", 1))
	next := prepareConfiguredFixture(t, r, "developer", "code", "small")
	if next.Code == 0 || !strings.Contains(next.Stderr, "assignments for fix-login changed") || r.Exists(".git/kitsu-stages/active.json") {
		t.Fatal("managed task silently changed its saved role profile", next.Output())
	}
}

func TestConfiguredManagedReviewRequiresNativeHooks(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"parent only", "other candidate"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			r.Write("kitsu-roles.toml", managedFixtureConfig)
			d := stageJSON(t, prepareConfiguredFixture(t, r, "reviewer", "review", "small"))
			id := installManagedCodexFixture(t, r, "configured-reviewer", d)
			if result := r.Kitsu("task", "finish", id); result.Code == 0 || !strings.Contains(result.Stderr, "native hook observation") {
				t.Fatal("configured review finished before any worker", result.Output())
			}
			if name == "parent only" {
				r.Setenv("KITSU_MANAGED_OMIT_HOOKS", "1")
			} else {
				const path = ".git/fixture-review-stop.json"
				stop := strings.Replace(r.Read(path), `\"candidate\":\"`, `\"candidate\":\"other-`, 1)
				r.Write(path, stop)
			}
			if result := r.Kitsu("task", "dispatch", id, "--run"); result.Code == 0 || !strings.Contains(result.Stderr, "native reviewer") {
				t.Fatal("an unmatched parent or candidate produced a review", result.Output())
			}
			if result := r.Kitsu("task", "finish", id); result.Code == 0 || !r.Exists(".git/kitsu-stages/active.json") {
				t.Fatal("missing review observation released the reserved interval", result.Output())
			}
		})
	}
}

func TestConfiguredManagedFullStageRequiresOpenSpecApprovals(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("kitsu-roles.toml", managedFixtureConfig)
	r.Write("openspec/changes/fix-login/proposal.md", "# Fix login\n")
	result := prepareConfiguredFixture(t, r, "architect", "design", "full")
	if result.Code == 0 || !strings.Contains(result.Stderr, "outstanding work and approvals") ||
		r.Exists(".git/kitsu-stages/active.json") {
		t.Fatal("design stage started without a specification approval", result.Output())
	}
}

func TestConfiguredManagedGitUsesExplicitStagedPaths(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("kitsu-roles.toml", managedFixtureConfig)
	r.Git("add", "kitsu.toml", "kitsu-roles.toml", "source.txt")
	r.Git("commit", "-qm", "Base")
	if result := r.Kitsu("gate", "init"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	r.Write("source.txt", "Verified changed content.\n")
	d := stageJSON(t, prepareConfiguredFixture(t, r, "git", "commit", "small"))
	id := installManagedCodexFixture(t, r, "configured-git", d)
	stageJSON(t, r.Kitsu("task", "dispatch", id, "--run"))
	paths := []string{"openspec/changes/.skills/fix-login/" + d["selection_id"].(string) + ".json", "source.txt"}
	r.Git("add", "--", paths[0], paths[1])
	if result := r.Kitsu("check", "run"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	head := r.Git("rev-parse", "HEAD")
	index := r.Git("write-tree")
	finish := []string{"task", "finish", id, "--expect-checks", "pass", "--message", "Verified scoped commit", "--include", paths[0]}
	failed := r.Kitsu(finish...)
	if failed.Code == 0 || !strings.Contains(failed.Stderr, "explicit --include for \"source.txt\"") ||
		r.Git("rev-parse", "HEAD") != head || r.Git("write-tree") != index {
		t.Fatal("under-scoped commit changed the branch or staging", failed.Output())
	}
	finish = append(finish, "--include", paths[1])
	result := stageJSON(t, r.Kitsu(finish...))
	commit := r.Git("rev-parse", "HEAD")
	if commit == head || result["commit"] != commit || r.Git("diff", "--name-only", head, commit) != strings.Join(paths, "\n") {
		t.Fatalf("commit does not match the authorized change: %v", result)
	}
	replay := stageJSON(t, r.Kitsu(finish...))
	if replay["commit"] != commit || r.Git("rev-parse", "HEAD") != commit || r.Exists(".git/kitsu-stages/active.json") {
		t.Fatalf("commit replay changed delivery: %v", replay)
	}
}
