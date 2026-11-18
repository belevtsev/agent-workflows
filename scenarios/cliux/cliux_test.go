package cliux_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/shellwords"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

func uxRepo(t *testing.T) *kitsutest.Repo {
	t.Helper()
	repo := kitsutest.NewRepo(t)
	repo.Setenv("KITSU_HOST", "")
	repo.Setenv("CODEX_HOME", filepath.Join(repo.Home, ".codex"))
	repo.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(repo.Home, ".claude"))
	return repo
}

func mustKitsu(t *testing.T, repo *kitsutest.Repo, args ...string) string {
	t.Helper()
	result := repo.Kitsu(args...)
	if result.Code != 0 {
		t.Fatalf("kitsu %q: exit %d\n%s", args, result.Code, result.Output())
	}
	return result.Stdout
}

func TestCLIUXDefaultsAndSkillInheritance(t *testing.T) {
	t.Parallel()
	repo := uxRepo(t)
	repo.Write(".agents/skills/go-backend/SKILL.md", "---\nname: go-backend\ndescription: Existing Go project guidance.\n---\nUse project checks.\n")
	personalSkill := filepath.Join(repo.Home, ".agents", "skills", "base", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(personalSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personalSkill, []byte("---\nname: base\ndescription: Personal project guidance.\n---\nRead before coding.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustKitsu(t, repo, "config", "set", "host", "codex", "--global")
	mustKitsu(t, repo, "role", "set", "developer", "--model", "account-model", "--reasoning", "max")
	mustKitsu(t, repo, "skill", "attach", "base", "--role", "developer", "--global")
	mustKitsu(t, repo, "skill", "attach", "go-backend", "--role", "developer")
	config, err := roles.Parse([]byte(repo.Read(roles.FileName)))
	if err != nil || !slices.Equal(config.Skills["codex"]["developer"], []string{"codex:user:base", "codex:repo:.agents/skills/go-backend"}) || config.Models["codex"]["developer"] != (roles.Model{Model: "account-model", Effort: "max"}) {
		t.Fatalf("attach replaced inherited skills or model settings: %+v, %v", config, err)
	}
	before := repo.Read(roles.FileName)
	mustKitsu(t, repo, "skill", "attach", "go-backend", "--role", "developer")
	if repo.Read(roles.FileName) != before {
		t.Fatal("repeated attach rewrote settings")
	}
	mustKitsu(t, repo, "skill", "clear", "--role", "developer")
	cleared, err := roles.Parse([]byte(repo.Read(roles.FileName)))
	if err != nil || len(cleared.Skills["codex"]["developer"]) != 0 || cleared.Models["codex"]["developer"].Model != "account-model" {
		t.Fatalf("clear lost model choices or failed to pin an empty list: %+v, %v", cleared, err)
	}
	mustKitsu(t, repo, "skill", "reset", "--role", "developer")
	reset, err := roles.Parse([]byte(repo.Read(roles.FileName)))
	if _, exists := reset.Skills["codex"]["developer"]; err != nil || exists || reset.Models["codex"]["developer"].Model != "account-model" {
		t.Fatalf("skill reset failed to restore inheritance independently: %+v, %v", reset, err)
	}
	if err := os.Remove(personalSkill); err != nil {
		t.Fatal(err)
	}
	mustKitsu(t, repo, "skill", "detach", "codex:user:base", "--role", "developer")
	mustKitsu(t, repo, "skill", "resolve", "developer", "--task", "small-example", "--workflow", "small")
	listing := mustKitsu(t, repo, "task", "list", "--json")
	if !strings.Contains(listing, `"stage": "unknown"`) || !strings.Contains(listing, "small-example") {
		t.Fatalf("small task was omitted or given an invented stage: %s", listing)
	}
	mustKitsu(t, repo, "task", "check", "small-example")
	if old := mustKitsu(t, repo, "change", "check", "small-example"); old != mustKitsu(t, repo, "task", "check", "small-example") {
		t.Fatal("legacy and preferred task checking differ")
	}
	legacy := mustKitsu(t, repo, "roles", "--json")
	if !strings.Contains(legacy, `"models"`) || !strings.Contains(legacy, `"claude"`) || !strings.Contains(legacy, `"codex"`) {
		t.Fatal("saved UI preference changed the legacy role JSON schema")
	}
	repo.Write("openspec/changes/help/proposal.md", "# Proposal: help\n")
	if shown := mustKitsu(t, repo, "task", "show", "help"); !strings.HasPrefix(shown, "help:") {
		t.Fatalf("task named help was intercepted as a help command: %s", shown)
	}
}

func TestCLIUXForwardedCommandUsage(t *testing.T) {
	t.Parallel()
	repo := uxRepo(t)
	for _, tc := range []struct{ group, command, operands string }{
		{"check", "add", "<id> [options] -- <program> [<argument>...]"},
		{"task", "test", "<task> [options] [-- <program> [<argument>...]]"},
	} {
		help := mustKitsu(t, repo, tc.group, tc.command, "--help")
		usage, _, _ := strings.Cut(help, "\n")
		want := "usage: kitsu " + tc.group + " " + tc.command + " " + tc.operands
		if usage != want {
			t.Errorf("got %q, want %q", usage, want)
		}
	}
}

func TestCLIUXCheckAddHelpEnablesVerification(t *testing.T) {
	t.Parallel()
	repo := uxRepo(t)
	repo.Write(decl.FileName, "version = 1\n")
	help := mustKitsu(t, repo, "check", "add", "--help")
	for line := range strings.Lines(help) {
		if !strings.HasPrefix(line, "  kitsu check add ") {
			continue
		}
		commands, ok := shellwords.Split(line)
		if !ok || len(commands) != 1 {
			t.Fatalf("expected a single copyable command in help: %q", line)
		}
		mustKitsu(t, repo, commands[0][1:]...)
	}

	file, err := decl.Parse([]byte(repo.Read(decl.FileName)))
	if err != nil {
		t.Fatal(err)
	}
	check, found := file.Check("readme")
	if !found || !check.Verify || check.Expect != "README.md is tracked" || !slices.Equal(check.When, []string{"README.md"}) {
		t.Fatalf("help examples did not enable verification for README.md: %+v", check)
	}
	if want := []string{"git", "ls-files", "--error-unmatch", "--", "README.md"}; !slices.Equal(check.Run, want) {
		t.Fatalf("saved child command %q, want %q", check.Run, want)
	}

	repo.Write("README.md", "# Project\n")
	repo.Git("add", "README.md")
	var plan verification.Plan
	if err := json.Unmarshal([]byte(mustKitsu(t, repo, "check", "plan", "--json")), &plan); err != nil {
		t.Fatal(err)
	}
	if required := plan.RequiredIDs(); !slices.Equal(required, []string{"readme"}) {
		t.Fatalf("required verification checks for a README change: %v", required)
	}
	mustKitsu(t, repo, "check", "run", "readme")
}

func TestCLIUXHelpAndForwardedArguments(t *testing.T) {
	t.Parallel()
	repo := uxRepo(t)
	repo.Write(decl.FileName, "version = 1\n")
	mustKitsu(t, repo, "check", "add", "inspect", "--", "echo", "--help", "help")
	file, err := decl.Parse([]byte(repo.Read(decl.FileName)))
	if err != nil || len(file.Checks) != 1 || !slices.Equal(file.Checks[0].Run, []string{"echo", "--help", "help"}) {
		t.Fatalf("Kitsu intercepted forwarded program arguments: %+v, %v", file, err)
	}
	before := repo.Read(decl.FileName)
	for _, args := range [][]string{{"check", "add", "--help"}, {"gate", "uninstall", "--help"}, {"role", "edit", "developer", "--help"}, {"adviser", "ask", "--help"}, {"help", "skill", "attach"}} {
		if output := mustKitsu(t, repo, args...); !strings.Contains(output, "usage:") {
			t.Fatalf("missing context help for %q: %s", args, output)
		}
	}
	if before != repo.Read(decl.FileName) || repo.Exists(roles.FileName) || repo.Exists(".git/kitsu-cli.toml") {
		t.Fatal("help mutated configuration")
	}
	if shown := mustKitsu(t, repo, "check", "list"); !strings.Contains(shown, "inspect") {
		t.Fatal("declared check not discoverable through preferred commands")
	}
	if got := repo.Kitsu("role", "ste"); got.Code != 2 || !strings.Contains(got.Stderr, `Did you mean "role set"?`) {
		t.Fatalf("typo did not offer a safe correction: %s", got.Output())
	}
}

func TestCLIUXGateRemovalPreservesHostPreference(t *testing.T) {
	t.Parallel()
	repo := uxRepo(t)
	mustKitsu(t, repo, "config", "set", "host", "codex", "--global")
	mustKitsu(t, repo, "config", "set", "host", "claude")
	if !repo.Exists(".git/kitsu-cli.toml") {
		t.Fatal("private preference was stored inside removable gate evidence")
	}
	mustKitsu(t, repo, "gate", "init")
	mustKitsu(t, repo, "gate", "uninstall")
	if got := mustKitsu(t, repo, "config", "get", "host"); strings.TrimSpace(got) != "claude" {
		t.Fatalf("removing the gate lost the checkout override: %q", got)
	}
	if !repo.Exists(".git/kitsu") {
		t.Fatal("gate removal lost its local records")
	}
	mustKitsu(t, repo, "gate", "uninstall", "--purge")
	if repo.Exists(".git/kitsu") {
		t.Fatal("explicit purge retained check records")
	}
	if !repo.Exists(decl.FileName) {
		t.Fatal("gate removal deleted declared configuration")
	}
	configPath := repo.Path(".git/kitsu-cli.toml")
	notice, err := json.Marshal(map[string]any{"tool_name": "Write", "cwd": repo.Dir, "tool_input": map[string]string{"file_path": configPath}})
	if err != nil {
		t.Fatal(err)
	}
	program := "kitsu"
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	got := repo.Run(string(notice), filepath.Join(kitsutest.Binary(t), program), "guard", "--host", "codex")
	if got.Code != 0 || !strings.Contains(got.Stdout, `"permissionDecision":"deny"`) {
		t.Fatalf("direct preference edits bypassed guard: %s", got.Output())
	}
}

func TestCLIUXAdviserActionsPreserveTheTaskRecord(t *testing.T) {
	t.Parallel()
	repo := uxRepo(t)
	name := "kitsu"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	program := filepath.Join(kitsutest.Binary(t), name)
	repo.Write(advisers.FileName, fmt.Sprintf("version = 1\n[advisers.local]\nmodel = 'version'\ncommand = [%q, '{model}']\n[access]\ncoordinator = ['local']\n", program))
	repo.Write("request.json", `{"version":1,"kind":"advice","question":"What build is this?"}`)
	mustKitsu(t, repo, "adviser", "list")
	mustKitsu(t, repo, "adviser", "show", "local")
	mustKitsu(t, repo, "adviser", "bind", "choose-build", "--workflow", "small")
	answer := mustKitsu(t, repo, "adviser", "ask", "local", "--task", "choose-build", "--role", "coordinator", "--stage", "code", "--request", "request.json", "--json")
	var saved struct {
		ID        string `json:"id"`
		Execution struct {
			Outcome string `json:"outcome"`
		} `json:"execution"`
	}
	if err := json.Unmarshal([]byte(answer), &saved); err != nil || saved.ID == "" || saved.Execution.Outcome != "success" {
		t.Fatalf("preferred ask did not save the exchange: %s; %v", answer, err)
	}
	resultPath := "openspec/changes/.advice/choose-build/exchanges/" + saved.ID + "/result.json"
	before := repo.Read(resultPath)
	for _, override := range [][]string{{"--disposition", "reject"}, {"--disposition=reject"}, {"-disposition=defer"}} {
		args := append([]string{"adviser", "accept", saved.ID, "--task", "choose-build", "--reason", "Keep action semantics"}, override...)
		if got := repo.Kitsu(args...); got.Code != 2 || !strings.Contains(got.Stderr, "already specifies the disposition") || repo.Exists("openspec/changes/.advice/choose-build/dispositions") {
			t.Fatalf("preferred action allowed a contradictory disposition: %s", got.Output())
		}
	}
	mustKitsu(t, repo, "adviser", "accept", saved.ID, "--task", "choose-build", "--reason", "Recorded build identity")
	mustKitsu(t, repo, "adviser", "status", "choose-build")
	mustKitsu(t, repo, "adviser", "check", "choose-build")
	mustKitsu(t, repo, "task", "check", "choose-build")
	if shown := mustKitsu(t, repo, "task", "show", "choose-build"); !strings.Contains(shown, "unknown (small)") {
		t.Fatalf("adviser-only small history was not navigable: %s", shown)
	}
	if unchanged := repo.Read(resultPath); unchanged != before {
		t.Fatal("disposition replaced the saved answer")
	}
}

func TestCLIUXAttachesIndependentlyAndInspectsSourceAliases(t *testing.T) {
	t.Parallel()
	repo := uxRepo(t)
	repo.Write(roles.FileName, "version = 1\n[codex.developer]\nmodel = 'keep-model'\nskills = ['codex:repo:.agents/skills/missing']\n")
	repo.Write(".agents/skills/manual/SKILL.md", "---\nname: manual\ndescription: Explicit invocation only.\ndisable-model-invocation: true\n---\nInstructions.\n")
	physical := t.TempDir()
	if err := os.WriteFile(filepath.Join(physical, "SKILL.md"), []byte("---\nname: linked-guidance\ndescription: Original guidance at a physical target.\n---\nRead project checks.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, repo.Path(".agents/skills/linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, ref := range []string{physical, filepath.Join(physical, "SKILL.md")} {
		if shown := mustKitsu(t, repo, "skill", "show", ref, "--host", "codex", "--json"); !strings.Contains(shown, "linked-guidance") {
			t.Fatalf("physical source was hidden by its catalog alias: %s", shown)
		}
	}
	mustKitsu(t, repo, "skill", "attach", "linked-guidance", "--role", "developer", "--host", "codex")
	parsed, err := roles.Parse([]byte(repo.Read(roles.FileName)))
	if err != nil || !slices.Equal(parsed.Skills["codex"]["developer"], []string{"codex:repo:.agents/skills/missing", "codex:repo:.agents/skills/linked"}) || parsed.Models["codex"]["developer"].Model != "keep-model" {
		t.Fatalf("attach lost the unavailable selection or model: %+v, %v", parsed, err)
	}
	before := repo.Read(roles.FileName)
	if shown := mustKitsu(t, repo, "skill", "show", "manual", "--host", "codex"); !strings.Contains(shown, "Manual-only") {
		t.Fatalf("manual source could not be inspected: %s", shown)
	}
	if got := repo.Kitsu("skill", "attach", "manual", "--role", "developer", "--host", "codex"); got.Code != 1 || repo.Read(roles.FileName) != before {
		t.Fatalf("manual-only source was attached or partially edited: %s", got.Output())
	}
	mustKitsu(t, repo, "skill", "detach", "codex:repo:.agents/skills/missing", "--role", "developer", "--host", "codex")
}

func TestTaskListSharesUnchangedVerificationPolicy(t *testing.T) {
	r := uxRepo(t)
	for _, id := range []string{"one", "two", "three"} {
		r.Write("openspec/changes/"+id+"/proposal.md", "# Proposal\n")
	}
	trace := filepath.Join(t.TempDir(), "git-events.jsonl")
	r.Setenv("GIT_TRACE2_EVENT", trace)
	out := mustKitsu(t, r, "task", "list", "--json")
	if !strings.Contains(out, `"three"`) {
		t.Fatal(out)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		var e struct{ Event, Name string }
		if json.Unmarshal([]byte(line), &e) == nil && e.Event == "cmd_name" && e.Name == "write-tree" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("listed three ordinary tasks with %d snapshot writes, want 1", count)
	}
}
