package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestReviewExactCheckDeclarationBeforeAgentExecution(t *testing.T) {
	r := kitsutest.NewRepo(t)
	const policy = "version = 1\n\n[[check]]\nid = \"status\"\nrun = [\"git\", \"status\", \"--short\"]\n"
	r.Write("kitsu.toml", policy)
	r.Git("add", "kitsu.toml")
	r.Git("commit", "-qm", "Declare the fixture check")
	t.Chdir(r.Dir)
	t.Setenv("HOME", r.Home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(r.Home, ".config"))
	t.Setenv("APPDATA", filepath.Join(r.Home, "AppData", "Roaming"))

	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_, fingerprint, err := workingCheckPolicy(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"codex", "claude"} {
		for _, command := range []string{"kitsu run", "kitsu check run", "kitsu task test test", "kitsu task do test -- true", "kitsu task verify test", "kitsu task commit test", "kitsu check trust"} {
			if got := checkApprovalNotice(t, host, r.Dir, command); !strings.Contains(got, `"permissionDecision":"deny"`) || !strings.Contains(got, "their own terminal") {
				t.Fatalf("%s accepted %q: %s", host, command, got)
			}
		}
		for _, command := range []string{"kitsu check list", "kitsu check plan", "kitsu task next sample", "kitsu task test --help", "echo kitsu check run"} {
			if got := checkApprovalNotice(t, host, r.Dir, command); got != "" {
				t.Fatalf("%s blocked inspection %q: %s", host, command, got)
			}
		}
	}
	if result := r.Kitsu("check", "run"); result.Code != 0 {
		t.Fatalf("direct CLI requires unnecessary approval: %s", result.Output())
	}

	var output bytes.Buffer
	cfg := Config{In: strings.NewReader("yes\n"), Out: &output, Err: io.Discard}
	err = Execute(t.Context(), []string{"check", "trust"}, cfg)
	if ExitCode(err) != ExitUsage || !strings.Contains(output.String(), fingerprint) || !strings.Contains(output.String(), `"git" "status" "--short"`) {
		t.Fatalf("a nonterminal request must preview the declaration and stop: %v\n%s", err, &output)
	}
	before, err := trustFile(repo, "")
	if err != nil || before.exists {
		t.Fatalf("invalid approval saved: %v, %t", err, before.exists)
	}
	r.Write("kitsu.toml", policy+"# Review this changed declaration\n")
	if err := confirmCheckTrust(t.Context(), cfg, repo, fingerprint, false); err == nil || !strings.Contains(err.Error(), "declaration changed") {
		t.Fatalf("a change during the prompt must invalidate approval: %v", err)
	}

	r.Write("kitsu.toml", policy)
	cfg.In = strings.NewReader("yes\n")
	if err := confirmCheckTrust(t.Context(), cfg, repo, fingerprint, false); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(t.TempDir(), "shared")
	r.Git("worktree", "add", "-qb", "linked", shared)
	for _, dir := range []string{r.Dir, shared} {
		if got := checkApprovalNotice(t, "codex", dir, "kitsu check run"); got != "" {
			t.Fatalf("matching approved declaration in %q was blocked: %s", dir, got)
		}
	}
	cloned := filepath.Join(t.TempDir(), "cloned")
	r.Git("clone", "-q", r.Dir, cloned)
	if got := checkApprovalNotice(t, "codex", cloned, "kitsu check run"); !strings.Contains(got, `"permissionDecision":"deny"`) {
		t.Fatalf("approval was reused across repositories: %s", got)
	}
	r.Write("kitsu.toml", policy+"# An exact declaration change needs new approval\n")
	if got := checkApprovalNotice(t, "claude", r.Dir, "kitsu check run"); !strings.Contains(got, `"permissionDecision":"deny"`) {
		t.Fatalf("changed declaration reused approval: %s", got)
	}
}

func checkApprovalNotice(t *testing.T, host, dir, command string) string {
	t.Helper()
	notice, err := json.Marshal(map[string]any{"tool_name": "Bash", "cwd": dir, "tool_input": map[string]string{"command": command}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cfg := Config{In: bytes.NewReader(notice), Out: &out, Err: io.Discard}
	if err = Execute(t.Context(), []string{"guard", "--host", host}, cfg); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestLocalCIApprovalRequiresDeveloperInUnconfiguredRepo(t *testing.T) {
	r := kitsutest.NewRepo(t)
	t.Chdir(r.Dir)
	t.Setenv("HOME", r.Home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(r.Home, ".config"))
	t.Setenv("APPDATA", filepath.Join(r.Home, "AppData", "Roaming"))
	for _, host := range []string{"codex", "claude"} {
		if reply := checkApprovalNotice(t, host, r.Dir, "kitsu check trust --ci product -- git status"); !strings.Contains(reply, "their own terminal") {
			t.Fatalf("%s did not block the approval command: %s", host, reply)
		}
		if reply := checkApprovalNotice(t, host, r.Dir, "kitsu task ci work --from https://github.com/example/project/pull/17 --check product"); reply != "" {
			t.Fatalf("%s blocked intake before Kitsu could resolve the local approval: %s", host, reply)
		}
	}
	var output bytes.Buffer
	cfg := Config{In: strings.NewReader("yes\n"), Out: &output, Err: io.Discard}
	args := []string{"check", "trust", "--ci", "product", "--", "git", "status"}
	if err := Execute(t.Context(), args, cfg); ExitCode(err) != ExitUsage || !strings.Contains(output.String(), `"git" "status"`) {
		t.Fatalf("a nonterminal request must preview the command and stop: %v\n%s", err, &output)
	}
	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	local, err := ciTrustFile(repo, "product")
	if err != nil || local.exists {
		t.Fatalf("local approval created on refused request: %v, %t", err, local.exists)
	}
	write := toolCall{Tool: "Write", Dir: r.Dir}
	write.Input.Path = local.path
	if reason, developerOnly := guardReason(t.Context(), write); !developerOnly || !strings.Contains(reason, "local check approvals") {
		t.Fatalf("guard allowed direct approval file creation: %s", reason)
	}
}

func TestVerificationOrderGrantIsCheckoutLocal(t *testing.T) {
	r := kitsutest.Demo(t)
	t.Chdir(r.Dir)
	t.Setenv("HOME", r.Home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(r.Home, ".config"))
	t.Setenv("APPDATA", filepath.Join(r.Home, "AppData", "Roaming"))
	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := workingCheckPolicy(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{In: strings.NewReader("yes\n"), Out: io.Discard, Err: io.Discard}
	for _, args := range [][]string{{"--verify-order", beforeReview}, {"--verify-order", reviewFirst}, {"--verify-order", beforeReview, "--ci", "unit"}} {
		if err := runUXCheckTrust(t.Context(), cfg, args); ExitCode(err) != ExitUsage {
			t.Fatalf("expected usage error for %q: %v", args, err)
		}
	}
	cfg.In = strings.NewReader("yes\n")
	if err := confirmCheckTrust(t.Context(), cfg, repo, digest, true); err != nil {
		t.Fatal(err)
	}
	s := &session{repo: repo}
	if !verificationOrderApproved(s, digest) {
		t.Fatal("confirmed approval is unavailable")
	}
	linked := filepath.Join(t.TempDir(), "linked-order")
	r.Git("worktree", "add", "-qb", "other-order", linked)
	other, err := gitx.Open(t.Context(), linked)
	if err != nil {
		t.Fatal(err)
	}
	if verificationOrderApproved(&session{repo: other}, digest) {
		t.Fatal("shared Git directory inherited early verification")
	}
	path, err := verificationOrderFile(repo)
	if err != nil || editReason(t.Context(), repo, r.Dir, []string{path.path}) != checkTrustEditReason {
		t.Fatalf("unprotected verification order file: %v", err)
	}
	for _, host := range []string{"codex", "claude"} {
		command := "kitsu check run && kitsu check trust --verify-order review-first"
		if notice := checkApprovalNotice(t, host, r.Dir, command); !strings.Contains(notice, "their own terminal") {
			t.Fatalf("%s did not protect the order change: %s", host, notice)
		}
	}
}
