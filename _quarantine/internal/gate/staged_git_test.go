package gate_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// These tests run the real git in a real repository, under a fake store
// that holds one record whose verification passed on the working tree, so
// only the staged-content rule can deny or ask. The working tree always
// holds what the record verified.

// stagedRepo returns a repository whose first commit holds a.txt, b.txt and
// tool.sh, after configure has run in it, with a.txt changed on disk.
func stagedRepo(t *testing.T, configure ...string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := canonicalDir(t)
	gitIn(t, repo, "init", "-q")
	for _, script := range configure {
		shIn(t, repo, script)
	}
	for _, name := range []string{"a.txt", "b.txt", "tool.sh"} {
		writeIn(t, repo, name, name+" first\n", 0o644)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "initial")
	writeIn(t, repo, "a.txt", "a.txt verified\n", 0o644)
	return repo
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func shIn(t *testing.T, dir, script string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "sh", "-c", script)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sh -c %q: %v\n%s", script, err, output)
	}
}

func writeIn(t *testing.T, dir, name, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// staleIn stages other bytes for name, then puts its bytes back on disk.
func staleIn(t *testing.T, repo, name string) {
	t.Helper()
	path := filepath.Join(repo, name)
	verified, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeIn(t, repo, name, "never verified\n", info.Mode().Perm())
	gitIn(t, repo, "add", "--", name)
	writeIn(t, repo, name, string(verified), info.Mode().Perm())
}

// verifiedStore holds one verified record for repo.
func verifiedStore(repo string) *fakeStore {
	return &fakeStore{open: map[string][]taskflow.OpenRecord{repo: {record("task-1", "Staged", "")}}}
}

func decideStaged(t *testing.T, repo, command string) gate.Decision {
	t.Helper()
	return gate.Decide(context.Background(), gate.Input{Command: command, CWD: repo, Enforce: true, Prompt: true},
		gate.Deps{Git: gate.ExecGit, Now: func() time.Time { return fixedNow }, Store: verifiedStore(repo)})
}

func decideStagedHook(t *testing.T, repo string) gate.Decision {
	t.Helper()
	return gate.DecideGitHook(context.Background(), gate.GitHookInput{Dir: repo, Enforce: true, Prompt: true},
		gate.Deps{Git: gate.ExecGit, Now: func() time.Time { return fixedNow }, Store: verifiedStore(repo)})
}

// wantStaged requires permission from both gates for a plain git commit,
// and for a deny or an ask a reason that names path.
func wantStaged(t *testing.T, repo string, permission gate.Permission, path string) {
	t.Helper()
	for name, got := range map[string]gate.Decision{"pre-publish": decideStaged(t, repo, "git commit -q -m x"), "git hook": decideStagedHook(t, repo)} {
		if got.Permission != permission || permission != gate.Allow && !strings.Contains(got.Reason, `"`+path+`"`) {
			t.Errorf("%s: %+v, want %q naming %q", name, got, permission, path)
		}
	}
}

// TestStagedRuleHonoursCoreFileModeFalse: with core.fileMode false, git
// ignores the execute bit on disk, as on file systems where every file looks
// executable. Oracle: a content change staged from such a file is allowed,
// while a mode change the commit itself makes is still compared with disk.
func TestStagedRuleHonoursCoreFileModeFalse(t *testing.T) {
	repo := stagedRepo(t, "git config core.fileMode false")
	writeIn(t, repo, "b.txt", "b.txt verified\n", 0o755)
	gitIn(t, repo, "add", "b.txt")
	wantStaged(t, repo, gate.Allow, "")

	gitIn(t, repo, "update-index", "--chmod=+x", "tool.sh")
	wantStaged(t, repo, gate.Deny, "tool.sh")
}

// TestStagedRuleReadsAttachedCommitOptionValues: -u and -S take only an
// attached value, so -uno, -unormal, -uall and -Sabc are not -o or -a.
// Oracle: each commit over stale staged bytes is denied like a plain one.
func TestStagedRuleReadsAttachedCommitOptionValues(t *testing.T) {
	repo := stagedRepo(t)
	staleIn(t, repo, "a.txt")
	for _, command := range []string{"git commit -q -uno -m x", "git commit -q -unormal -m x", "git commit -q -uall -m x", "git commit -q -Sabc -m x", "git commit -q -S -m x"} {
		if got := decideStaged(t, repo, command); got.Permission != gate.Deny || !strings.Contains(got.Reason, `"a.txt"`) {
			t.Errorf("%s: %+v, want a deny naming a.txt", command, got)
		}
	}
}

// TestStagedRuleExplainsConversions: the gate runs no filter, but allows a
// staged blob that git's end-of-line conversion or a Git LFS pointer explains,
// asks about another filter, and denies anything else. Oracle: git itself
// staged each blob from the bytes on disk, except the stale one.
func TestStagedRuleExplainsConversions(t *testing.T) {
	const lfsClean = `f=$(mktemp); cat > "$f"; printf 'version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %s\n' "$(sha256sum < "$f" | cut -d' ' -f1)" "$(wc -c < "$f" | tr -d ' ')"; rm -f "$f"`
	t.Run("an LFS pointer to the bytes on disk", func(t *testing.T) {
		repo := stagedRepo(t, "git config filter.lfs.clean '"+strings.ReplaceAll(lfsClean, "'", `'\''`)+"'", "git config filter.lfs.smudge cat", `printf '*.bin filter=lfs\n' > .gitattributes`)
		writeIn(t, repo, "d.bin", "payload\n", 0o644)
		gitIn(t, repo, "add", "d.bin")
		wantStaged(t, repo, gate.Allow, "")
		writeIn(t, repo, "d.bin", "other payload\n", 0o644)
		wantStaged(t, repo, gate.Deny, "d.bin")
	})
	t.Run("text=auto with CRLF on disk", func(t *testing.T) {
		repo := stagedRepo(t, `printf '* text=auto\n' > .gitattributes`)
		writeIn(t, repo, "b.txt", "b.txt verified\r\n", 0o644)
		gitIn(t, repo, "add", "b.txt")
		wantStaged(t, repo, gate.Allow, "")
	})
	t.Run("core.autocrlf=input with CRLF on disk", func(t *testing.T) {
		repo := stagedRepo(t, "git config core.autocrlf input")
		writeIn(t, repo, "b.txt", "b.txt verified\r\n", 0o644)
		gitIn(t, repo, "add", "b.txt")
		wantStaged(t, repo, gate.Allow, "")
	})
	t.Run("a stale file under text=auto", func(t *testing.T) {
		repo := stagedRepo(t, `printf '* text=auto\n' > .gitattributes`)
		staleIn(t, repo, "a.txt")
		wantStaged(t, repo, gate.Deny, "a.txt")
	})
	t.Run("another filter", func(t *testing.T) {
		repo := stagedRepo(t, "git config filter.upper.clean 'tr a-z A-Z'", "git config filter.upper.smudge cat", `printf '*.up filter=upper\n' > .gitattributes`)
		writeIn(t, repo, "d.up", "payload\n", 0o644)
		gitIn(t, repo, "add", "d.up")
		wantStaged(t, repo, gate.Ask, "d.up")
	})
}

// TestStagedRuleJudgesWhatCommitAllLeavesStaged: git commit -a does not
// refresh assume-unchanged or skip-worktree entries. Oracle: their stale
// staged bytes are denied.
func TestStagedRuleJudgesWhatCommitAllLeavesStaged(t *testing.T) {
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		repo := stagedRepo(t)
		staleIn(t, repo, "a.txt")
		gitIn(t, repo, "update-index", flag, "a.txt")
		if got := decideStaged(t, repo, "git commit -q -am x"); got.Permission != gate.Deny || !strings.Contains(got.Reason, `"a.txt"`) {
			t.Errorf("%s: %+v, want a deny naming a.txt", flag, got)
		}
	}
}

// TestStagedRuleLeavesAnotherIndexToTheGitHook: a commit through
// GIT_INDEX_FILE records that index, which the plugin hook does not read.
// Oracle: a stale default index does not deny it.
func TestStagedRuleLeavesAnotherIndexToTheGitHook(t *testing.T) {
	repo := stagedRepo(t)
	staleIn(t, repo, "a.txt")
	for _, command := range []string{"GIT_INDEX_FILE=" + filepath.Join(repo, ".git", "alt") + " git commit -q -m x", "git --git-dir=.git commit -q -m x"} {
		if got := decideStaged(t, repo, command); got.Permission != gate.Allow {
			t.Errorf("%s: %+v, want allow", command, got)
		}
	}
}
