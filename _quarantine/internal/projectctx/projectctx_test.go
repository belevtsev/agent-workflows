package projectctx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
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
	command.Env = append(MinimalEnvironment(os.Getenv), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
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
	canonical, err := CanonicalDirectory(root)
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
	root, _ = CanonicalDirectory(root)
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
	canonical, err := CanonicalDirectory(worktree)
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
