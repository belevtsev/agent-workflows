package harness_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// guardDenies asks the plugin's guard about one shell command of an agent.
func guardDenies(t *testing.T, r *kitsutest.Repo, host, command string) bool {
	t.Helper()
	call, err := json.Marshal(map[string]any{"tool_name": "Bash", "cwd": r.Dir, "tool_input": map[string]string{"command": command}})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Run(string(call), filepath.Join(kitsutest.Binary(t), "kitsu"), "guard", "--host", host)
	if got.Code != 0 {
		t.Fatal(got.Output())
	}
	return strings.Contains(got.Stdout, `"permissionDecision":"deny"`)
}

// A host-observed review is evidence that the host reported a reviewer. The
// coordinator that reserved the review must not report that reviewer itself,
// and no agent may rewrite the saved observation. The same notice sent by a
// process that the guard does not read is still accepted: the host's hook and
// every other program of this user look alike to a local tool.
func TestHostObservedReviewCannotBeReportedFromAnAgentsShell(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	r.Write("README.md", "# Project\n")
	r.Git("add", "README.md")
	m := &turns{t: t, repo: r, id: "forged-review"}
	command := append([]string{"--require-review", "--actor", "coordinator", "--claim-review", "claude"}, trackReadme...)
	m.do(0, command...)
	m.want("review")
	report := `{"native_review_result":{"task":"forged-review","ticket":"` + m.ticket + `","verdict":"approve"}}`
	for _, host := range []string{"claude", "codex"} {
		for _, forged := range []string{
			`echo '{"hook_event_name":"SubagentStart","session_id":"coordinator","agent_id":"worker"}' | kitsu tokens record --host ` + host,
			`printf '%s' '` + report + `' | sh -c "kitsu usage record --host ` + host + `"`,
			"echo '{}' > .git/kitsu-stages/native-review-forged.json",
			"sed -i s/coordinator/worker/ .git/kitsu-stages/task-harness-forged-review.json",
		} {
			if !guardDenies(t, r, host, forged) {
				t.Errorf("the %s guard let an agent report or rewrite a review: %s", host, forged)
			}
		}
		if guardDenies(t, r, host, "cat .git/kitsu-stages/task-harness-forged-review.json") {
			t.Errorf("the %s guard refused to let an agent read the saved task", host)
		}
	}
	// Nothing was reported, so the required review is still owed.
	if got := r.Kitsu(append([]string{"task", "do", m.id}, command...)...); got.Code != 1 || !strings.Contains(got.Stderr, "review is pending") {
		t.Fatalf("a review that no host reported was accepted: %s", got.Output())
	}
}

// "Approve in your own terminal" holds only while an agent cannot give itself
// a terminal or write the approval file.
func TestTerminalApprovalCannotBeGivenFromAnAgentsShell(t *testing.T) {
	r := kitsutest.Demo(t)
	for _, host := range []string{"claude", "codex"} {
		for _, approval := range []string{
			"kitsu check trust",
			"script -q /dev/null kitsu check trust",
			`script -qc "yes | kitsu check trust" /dev/null`,
			"unbuffer kitsu check trust --verify-order before-review",
			"echo 0123 > ~/.config/kitsu/check-trust/0123abcd",
			`cp trust "$HOME/Library/Application Support/kitsu/check-trust/0123abcd"`,
		} {
			if !guardDenies(t, r, host, approval) {
				t.Errorf("the %s guard let an agent approve for the developer: %s", host, approval)
			}
		}
		if guardDenies(t, r, host, "kitsu check list") {
			t.Errorf("the %s guard refused to list the checks", host)
		}
	}
}
