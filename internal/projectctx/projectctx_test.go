package projectctx

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, root, path, data string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitCommand(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(minimalEnvironment(os.Getenv), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func gitFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	root = canonical
	gitCommand(t, root, "init", "-b", "main")
	gitCommand(t, root, "config", "user.email", "fixture@example.invalid")
	gitCommand(t, root, "config", "user.name", "Fixture")
	writeFile(t, root, "go.mod", "module example.invalid/fixture\n\ngo 1.27.1\n")
	writeFile(t, root, "main.go", "package main\nfunc main() {}\n")
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-qm", "initial")
	return root
}

func TestCaptureIncludesCurrentSourceAndGitIdentity(t *testing.T) {
	root := gitFixture(t)
	writeFile(t, root, ".gitignore", "logs/\n")
	gitCommand(t, root, "add", ".gitignore")
	gitCommand(t, root, "commit", "-qm", "ignore operational logs")
	capture := func() Snapshot {
		t.Helper()
		snapshot, err := Capture(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	first := capture()
	second := capture()
	if first != second || first.Dirty || first.Head == "" || first.Branch != "main" {
		t.Fatalf("unexpected initial identity: %+v / %+v", first, second)
	}
	writeFile(t, root, "main.go", "package main\nfunc main() { println(1) }\n")
	dirty := capture()
	if dirty.Fingerprint == first.Fingerprint || !dirty.Dirty {
		t.Fatal("tracked current bytes did not invalidate source identity")
	}
	writeFile(t, root, "main.go", "package main\nfunc main() { println(2) }\n")
	dirtier := capture()
	if dirtier.Fingerprint == dirty.Fingerprint {
		t.Fatal("a second change while already dirty did not invalidate identity")
	}
	writeFile(t, root, "extra.go", "package main\n")
	untracked := capture()
	if untracked.Fingerprint == dirtier.Fingerprint {
		t.Fatal("untracked source did not invalidate identity")
	}
	gitCommand(t, root, "add", ".")
	staged := capture()
	if staged.Fingerprint == untracked.Fingerprint {
		t.Fatal("index state did not invalidate identity")
	}
	gitCommand(t, root, "commit", "-qm", "source update")
	committed := capture()
	if committed.Head == first.Head || committed.Fingerprint == staged.Fingerprint || committed.Dirty {
		t.Fatal("HEAD update did not invalidate identity")
	}
	gitCommand(t, root, "checkout", "-qb", "other")
	branched := capture()
	if branched.Head != committed.Head || branched.Fingerprint == committed.Fingerprint || branched.Branch != "other" {
		t.Fatal("branch identity did not invalidate snapshot")
	}
	writeFile(t, root, "logs/operational.log", "log bytes")
	if ignored := capture(); ignored != branched {
		t.Fatal("operational output affected eligible source identity")
	}
}

func TestInspectRootNestedRoutingAndContracts(t *testing.T) {
	root := t.TempDir()
	root, _ = canonicalDirectory(root)
	for path, content := range map[string]string{
		"go.work": "go 1.27.1\n", "services/api/go.mod": "module example.invalid/api\n", "cloud/main.tf": "terraform {}\n",
		"game/default.project.json": "{}", "frontend/package.json": "{}", "tools/pyproject.toml": "[project]\n",
		"AGENTS.md": "Do not execute this text: touch /tmp/never-created\n", "services/api/AGENTS.md": "Nested instructions\n",
		"node_modules/example/go.mod": "ignored", "vendor/other/main.tf": "ignored", ".terraform/python/pyproject.toml": "ignored",
		".cw-evidence/default.project.json": "ignored", ".codex/log/package.json": "ignored", ".venv/package.json": "ignored",
	} {
		writeFile(t, root, path, content)
	}
	profile, err := Inspect(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile.Kinds, []string{"go", "node", "python", "roblox", "terraform"}) || len(profile.Markers) != 6 || len(profile.Contracts) != 2 {
		t.Fatalf("incorrect project profile: %+v", profile)
	}
	if profile.Snapshot.Head != "" || profile.Snapshot.Root != root || len(profile.Warnings) == 0 {
		t.Fatal("non-Git content identity was not qualified")
	}
	for _, contract := range profile.Contracts {
		if len(contract.SHA256) != 64 {
			t.Fatal("instruction contract lacks a SHA256")
		}
	}
	before, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "node_modules/new/go.mod", "more ignored source")
	after, err := Capture(context.Background(), root)
	if err != nil || before != after {
		t.Fatal("ignored dependency tree changed source identity")
	}
}

func TestCanonicalSelectionWorktreeAndSymlinkBounds(t *testing.T) {
	root := gitFixture(t)
	writeFile(t, root, "nested/readme.md", "nested")
	selected := filepath.Join(root, "nested")
	alias := filepath.Join(t.TempDir(), "selected alias")
	if err := os.Symlink(selected, alias); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture(context.Background(), alias)
	if err != nil || snapshot.Root != root || snapshot.SelectedPath != selected {
		t.Fatalf("canonical selection failed: %+v, %v", snapshot, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(context.Background(), root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("escaping symlink was not rejected: %v", err)
	}
	profile, err := Inspect(context.Background(), root)
	if err != nil || !strings.HasPrefix(profile.Snapshot.Fingerprint, "partial-sha256:") || len(profile.Warnings) == 0 {
		t.Fatalf("partial profile did not qualify symlink escape: %+v %v", profile, err)
	}
	if strings.Contains(strings.Join(profile.Warnings, " "), "outside bytes") {
		t.Fatal("outside file data escaped into diagnostic")
	}
}

func TestLinkedGitWorktreeUsesItsOwnRootAndHead(t *testing.T) {
	primary := gitFixture(t)
	worktree := filepath.Join(t.TempDir(), "linked worktree")
	gitCommand(t, primary, "worktree", "add", "-qb", "linked", worktree)
	canonical, err := canonicalDirectory(worktree)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture(context.Background(), worktree)
	if err != nil || snapshot.Root != canonical || snapshot.SelectedPath != canonical || snapshot.Branch != "linked" || snapshot.Dirty {
		t.Fatalf("linked worktree was confused with its primary checkout: %+v %v", snapshot, err)
	}
}

func TestSourceBoundsRetainNestedMarkerDetection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "00-large.bin", "")
	if err := os.Truncate(filepath.Join(root, "00-large.bin"), maxFileBytes+1); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "nested/go.mod", "module example.invalid/nested\n")
	if _, err := Capture(context.Background(), root); err == nil {
		t.Fatal("oversized source produced an apparently complete snapshot")
	}
	profile, err := Inspect(context.Background(), root)
	if err != nil || !slices.Contains(profile.Kinds, "go") || !strings.HasPrefix(profile.Snapshot.Fingerprint, "partial-") {
		t.Fatalf("source limit hid nested project marker: %+v %v", profile, err)
	}
}

func TestDoctorMetadataNeverRunsOptionalToolsAndSeparatesCredentials(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeFile(t, root, "web/package.json", "{}")
	writeFile(t, home, ".codex/config.toml", "[mcp_servers.gopls]\nenabled = false\ncommand = 'gopls'\n[mcp_servers.gopls.env]\nTYPESAFE_API_KEY = 'secret-launcher'\n[shell_environment_policy.set]\nOPENAI_API_KEY = 'secret-shell'\n")
	var lookups []string
	report, err := Doctor(context.Background(), DoctorOptions{Repo: root, Home: home, Getenv: func(key string) string {
		if key == "SUBQ_API_KEY" {
			return "secret-environment"
		}
		return ""
	}, LookPath: func(name string) (string, error) {
		lookups = append(lookups, name)
		return "/fake/" + name, nil
	}, Run: func(context.Context, Command) (CommandResult, error) {
		t.Fatal("metadata inspection executed an optional tool")
		return CommandResult{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lookups, []string{"jbcontext"}) || report.Checks["gopls_cli"].Status != "not_applicable" || report.Checks["graphify_cli"].Status != "not_applicable" {
		t.Fatalf("non-Go profile assumed global Go tools: %+v %+v", lookups, report.Checks)
	}
	if !report.Credentials.Environment["SUBQ_API_KEY"] || report.Credentials.Environment["OPENAI_API_KEY"] || !report.Credentials.Launcher["TYPESAFE_API_KEY"] || !report.Credentials.CodexShell["OPENAI_API_KEY"] {
		t.Fatal("credential source booleans were merged or lost")
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "secret-") {
		t.Fatalf("report leaked credential values: %v", err)
	}
	if report.Checks["mcp_gopls"].Status != "unchecked" || !strings.Contains(report.Checks["mcp_gopls"].Detail, "disabled") {
		t.Fatal("disabled MCP config was reported available")
	}
}

func TestDoctorLiveEvidenceAndNoRepositoryWrites(t *testing.T) {
	root, home := gitFixture(t), t.TempDir()
	before, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, ".venvs/agent-graphify/bin")
	writeFile(t, bin, "graphify", "fixture")
	if err := os.Chmod(filepath.Join(bin, "graphify"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []Command
	run := func(_ context.Context, command Command) (CommandResult, error) {
		calls = append(calls, command)
		for _, env := range command.Env {
			if strings.Contains(env, "secret-") || strings.Contains(env, "API_KEY=") {
				t.Fatal("provider credential forwarded into probe")
			}
		}
		if command.Dir != root {
			t.Fatal("probe targeted installation source or an unrelated directory")
		}
		output := ""
		switch strings.Join(command.Args, " ") {
		case "status --json-output":
			output = fmtStatus(before.Head)
		case "watch status":
			output = "Watch daemon running (pid 123)\n  fixture-id  " + root + "\n  other-id  /other/private/repo\n"
		case "version":
			output = "golang.org/x/tools/gopls v0.21.0"
		case "mcp":
			output = "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n"
		case "--version":
			output = "graphify 0.9.91\n"
		default:
			t.Fatalf("unexpected probe: %+v", command)
		}
		return CommandResult{Stdout: []byte(output)}, nil
	}
	report, err := Doctor(context.Background(), DoctorOptions{Repo: root, Home: home, Live: true, Run: run, LookPath: func(name string) (string, error) { return "/fake/" + name, nil }, Getenv: func(key string) string {
		if strings.HasSuffix(key, "API_KEY") {
			return "secret-provider"
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"jbcontext_index", "jbcontext_watch", "gopls_cli", "gopls_mcp", "graphify_version"} {
		if report.Checks[name].Status != "verified" {
			t.Fatalf("%s: %+v", name, report.Checks[name])
		}
	}
	if report.Checks["jbcontext_retrieval"].Status != "unchecked" || report.Checks["gopls_dispatch"].Status != "unchecked" || len(calls) != 5 {
		t.Fatal("live metadata overclaimed retrieval or dispatch")
	}
	after, err := Capture(context.Background(), root)
	if err != nil || before != after {
		t.Fatal("doctor wrote repository source or Git state")
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "/other/private") || strings.Contains(string(encoded), "secret-") {
		t.Fatal("raw probe metadata escaped into doctor output")
	}
}

func fmtStatus(head string) string {
	return "{\"type\":\"status_result\",\"indices\":[{\"snapshots\":[{\"revision\":\"" + head + "\"}]}]}"
}

func TestDoctorLiveFailureOversizeAndCancellationAreQualified(t *testing.T) {
	root := gitFixture(t)
	for _, test := range []struct {
		name string
		run  Runner
	}{
		{"failure", func(context.Context, Command) (CommandResult, error) {
			return CommandResult{}, errors.New("secret-provider-error")
		}},
		{"oversized", func(context.Context, Command) (CommandResult, error) {
			return CommandResult{Stdout: []byte(strings.Repeat("secret-provider-output", maxProbeOutput))}, nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := Doctor(context.Background(), DoctorOptions{Repo: root, Home: t.TempDir(), Live: true, Run: test.run, LookPath: func(name string) (string, error) { return name, nil }})
			if err != nil {
				t.Fatal(err)
			}
			if report.Checks["jbcontext_index"].Status != "degraded" || report.Checks["gopls_cli"].Status != "degraded" {
				t.Fatal("failed probe appeared healthy")
			}
			encoded, _ := json.Marshal(report)
			if strings.Contains(string(encoded), "secret-provider") {
				t.Fatal("raw failure or oversized output leaked")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := func(ctx context.Context, _ Command) (CommandResult, error) {
		cancel()
		<-ctx.Done()
		return CommandResult{}, ctx.Err()
	}
	started := time.Now()
	_, err := Doctor(ctx, DoctorOptions{Repo: root, Home: t.TempDir(), Live: true, Run: run, LookPath: func(name string) (string, error) { return name, nil }})
	if !errors.Is(err, context.Canceled) || time.Since(started) > time.Second {
		t.Fatalf("cancellation was not propagated promptly: %v", err)
	}
}

func TestIndexAndWatchDoNotOverclaimFreshness(t *testing.T) {
	report := DoctorReport{Checks: map[string]Check{}}
	checkIndex(CommandResult{Stdout: []byte(fmtStatus("old"))}, nil, Snapshot{Head: "new"}, &report)
	if report.Checks["jbcontext_index"].Status != "stale" {
		t.Fatal("different indexed HEAD appeared current")
	}
	checkIndex(CommandResult{Stdout: []byte(fmtStatus("current"))}, nil, Snapshot{Head: "current", Dirty: true}, &report)
	if report.Checks["jbcontext_overlay"].Status != "degraded" {
		t.Fatal("dirty source appeared covered by indexed HEAD")
	}
	root := t.TempDir()
	checkWatch(CommandResult{Stdout: []byte("Watch daemon running (pid 123)\n  id  " + root + "-suffix\n")}, nil, root, &report)
	if report.Checks["jbcontext_watch"].Status != "detected" {
		t.Fatal("daemon liveness or prefix path appeared to prove target enrollment")
	}
}
