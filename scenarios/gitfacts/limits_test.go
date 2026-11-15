package gitfacts_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Observation: "git runs `pre-commit` without holding the index lock and
// reads the index again after the hook: what a `pre-commit` hook stages is
// in the commit."
func TestWhatPreCommitStagesIsInTheCommit(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	log := newHookLog(t)
	installHook(t, repo, "pre-commit",
		"if test -e \"$(git rev-parse --git-path index.lock)\"; then echo locked >> "+log.sh()+"; else echo free >> "+log.sh()+"; fi\n"+
			"printf 'staged by the hook\\n' > second.txt\n"+
			"git add second.txt || exit 9\n")

	repo.Write("f.txt", "b\n")
	repo.Git("add", "f.txt")
	repo.Git("commit", "-q", "-m", "c")

	if got := log.take(); strings.Join(got, ",") != "free" {
		t.Errorf("inside pre-commit the index lock was %v, want free", got)
	}
	if got := repo.Git("show", "HEAD:second.txt"); got != "staged by the hook" {
		t.Errorf("HEAD:second.txt is %q, want the file the hook staged", got)
	}
	if got := repo.Git("show", "HEAD:f.txt"); got != "b" {
		t.Errorf("HEAD:f.txt is %q, want %q", got, "b")
	}
}

// Observation: "`git am` creates a commit without running `pre-commit`; it
// runs `pre-applypatch` instead."
func TestGitAmRunsPreApplypatchNotPreCommit(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	other := repo.In(filepath.Join(t.TempDir(), "other"))
	repo.Git("clone", "-q", repo.Dir, other.Dir)
	commitFile(other, "g.txt", "g\n", "from the patch")
	patches := t.TempDir()
	patch := strings.TrimSpace(other.Git("format-patch", "-1", "-o", patches))
	if !filepath.IsAbs(patch) {
		patch = filepath.Join(other.Dir, patch)
	}

	log := newHookLog(t)
	installHook(t, repo, "pre-commit", "echo pre-commit >> "+log.sh()+"\necho 'denied (test)' >&2\nexit 1\n")
	installHook(t, repo, "pre-applypatch", "echo pre-applypatch >> "+log.sh()+"\n")
	before := repo.Git("rev-parse", "HEAD")

	if result := repo.TryGit("am", "-q", patch); result.Code != 0 {
		t.Fatalf("git am with a pre-commit hook that always fails: exit status %d, want 0\n%s", result.Code, result.Output())
	}
	if got := repo.Git("log", "-1", "--format=%s"); got != "from the patch" {
		t.Errorf("HEAD is %q after git am, want the commit of the patch", got)
	}
	if got := repo.Git("rev-parse", "HEAD~1"); got != before {
		t.Errorf("the parent of the applied commit is %s, want %s", got, before)
	}
	if got := log.take(); strings.Join(got, ",") != "pre-applypatch" {
		t.Errorf("git am ran the hooks %v, want only pre-applypatch", got)
	}

	repo.Git("reset", "-q", "--hard", before)
	installHook(t, repo, "pre-applypatch", "echo pre-applypatch >> "+log.sh()+"\necho 'denied (test)' >&2\nexit 1\n")
	if result := repo.TryGit("am", "-q", patch); result.Code == 0 {
		t.Errorf("git am exited 0 with a pre-applypatch hook that fails\n%s", result.Output())
	}
	if got := repo.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD moved to %s although pre-applypatch failed, want %s", got, before)
	}
	if got := log.take(); strings.Join(got, ",") != "pre-applypatch" {
		t.Errorf("the stopped git am ran the hooks %v, want only pre-applypatch", got)
	}
	repo.Git("am", "--abort")
}

// Observation: "`git commit-tree` with `git update-ref` creates a commit and
// moves a branch without running any hook."
//
// As written the sentence is too wide: `git update-ref` runs the
// `reference-transaction` hook (git 2.28 and later), and a failing one stops
// the update ("ref updates aborted by hook"). What holds is that none of the
// hooks of a commit runs, `pre-commit` among them. The test holds that, and
// holds that `reference-transaction` does run, so that the sentence is
// corrected rather than relied on.
func TestCommitTreeAndUpdateRefRunNoCommitHook(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	log := newHookLog(t)
	for _, name := range []string{
		"pre-commit", "pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-commit",
		"post-rewrite", "post-checkout", "post-merge",
	} {
		installHook(t, repo, name, "echo "+name+" >> "+log.sh()+"\necho 'denied (test)' >&2\nexit 1\n")
	}
	installHook(t, repo, "reference-transaction", "echo \"reference-transaction $1\" >> "+log.sh()+"\ncat > /dev/null\n")
	before := repo.Git("rev-parse", "HEAD")
	repo.Write("f.txt", "b\n")
	repo.Git("add", "f.txt")
	log.take()

	tree := repo.Git("write-tree")
	commit := repo.Git("commit-tree", tree, "-p", "HEAD", "-m", "plumbing")
	if got := log.take(); len(got) != 0 {
		t.Errorf("git commit-tree ran the hooks %v, want none", got)
	}
	repo.Git("update-ref", "refs/heads/main", commit, before)

	if got := repo.Git("rev-parse", "main"); got != commit {
		t.Errorf("main is %s, want the new commit %s", got, commit)
	}
	if got := repo.Git("log", "-1", "--format=%s", "main"); got != "plumbing" {
		t.Errorf("main points at %q, want the commit plumbing", got)
	}
	var commitHooks, transaction []string
	for _, line := range log.take() {
		if strings.HasPrefix(line, "reference-transaction ") {
			transaction = append(transaction, line)
		} else {
			commitHooks = append(commitHooks, line)
		}
	}
	if len(commitHooks) != 0 {
		t.Errorf("git update-ref ran the commit hooks %v, want none", commitHooks)
	}
	if len(transaction) == 0 {
		t.Errorf("git update-ref did not run reference-transaction; the sentence would then hold as written")
	}
}

// Observation: "`git commit` runs `pre-commit` also when there is nothing to
// commit."
func TestPreCommitRunsWhenThereIsNothingToCommit(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	marker := filepath.Join(t.TempDir(), "ran")
	installHook(t, repo, "pre-commit", ": > "+shQuote(marker)+"\n")
	before := repo.Git("rev-parse", "HEAD")

	if result := repo.TryGit("commit", "-m", "x"); result.Code == 0 {
		t.Errorf("git commit -m x in a clean repository exited 0\n%s", result.Output())
	}
	if got := repo.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD moved from %s to %s: something was committed", before, got)
	}
	if !exists(marker) {
		t.Errorf("pre-commit did not run for a commit with nothing to commit")
	}
}

// fileURL is the file:// URL of a local path, also for a Windows path with
// a drive letter.
func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + slashed
}

// contacted says whether a GIT_TRACE output shows git starting a fetch or
// the upload-pack of a remote.
func contacted(trace string) bool {
	for _, line := range strings.Split(trace, "\n") {
		if strings.Contains(line, "upload-pack") ||
			strings.Contains(line, "run_command:") && strings.Contains(line, " fetch ") {
			return true
		}
	}
	return false
}

// gitAtLeast says whether git is at least major.minor.
func gitAtLeast(t *testing.T, repo *gatecubtest.Repo, major, minor int) bool {
	t.Helper()
	version := repo.Git("version")
	match := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(version)
	if match == nil {
		t.Fatalf("cannot read the version of git from %q", version)
	}
	gotMajor, _ := strconv.Atoi(match[1])
	gotMinor, _ := strconv.Atoi(match[2])
	return gotMajor > major || gotMajor == major && gotMinor >= minor
}

// Observation: "In a partial clone made with `--filter=blob:none`, a git
// command that needs a missing blob fetches it from the remote; with
// `GIT_NO_LAZY_FETCH=1` in its environment it fails instead and contacts
// nothing."
func TestAPartialCloneFetchesAMissingBlobUnlessLazyFetchIsOff(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	commitFile(repo, "g.txt", "g\n", "g")
	commitFile(repo, "h.txt", "h\n", "h")
	base := t.TempDir()
	remote := filepath.Join(base, "origin.git")
	repo.Git("clone", "-q", "--bare", repo.Dir, remote)
	repo.Git("-C", remote, "config", "uploadpack.allowFilter", "true")
	repo.Git("-C", remote, "config", "uploadpack.allowAnySHA1InWant", "true")
	clone := repo.In(filepath.Join(base, "clone"))
	repo.Git("clone", "-q", "--no-checkout", "--filter=blob:none", fileURL(remote), clone.Dir)

	// isMissing says whether git lists the blob of a path as missing.
	isMissing := func(path string) bool {
		t.Helper()
		blob := clone.Git("rev-parse", "HEAD:"+path)
		missing := runEnv(t, clone, []string{"GIT_NO_LAZY_FETCH=1"}, "rev-list", "--objects", "--missing=print", "HEAD")
		return strings.Contains(missing.Stdout, "?"+blob)
	}
	for _, path := range []string{"f.txt", "g.txt", "h.txt"} {
		if !isMissing(path) {
			t.Fatalf("the blob of %s is in the partial clone before any command needed it", path)
		}
	}
	// traced runs git in the clone with GIT_TRACE written to a file, and
	// returns how it ended and the trace.
	traced := func(extra []string, args ...string) (gatecubtest.Result, string) {
		t.Helper()
		trace := filepath.Join(t.TempDir(), "trace")
		result := runEnv(t, clone, append([]string{"GIT_TRACE=" + trace}, extra...), args...)
		data, err := os.ReadFile(trace)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return result, string(data)
	}

	if gitAtLeast(t, repo, 2, 45) {
		result, trace := traced([]string{"GIT_NO_LAZY_FETCH=1"}, "cat-file", "-p", "HEAD:f.txt")
		if result.Code == 0 {
			t.Errorf("with GIT_NO_LAZY_FETCH=1, git cat-file of a missing blob exited 0\n%s", result.Output())
		}
		if contacted(trace) {
			t.Errorf("with GIT_NO_LAZY_FETCH=1, git contacted the remote:\n%s", trace)
		}
		if !isMissing("f.txt") {
			t.Errorf("with GIT_NO_LAZY_FETCH=1, the blob of f.txt was fetched")
		}
	} else {
		t.Log("git is older than 2.45, which does not know GIT_NO_LAZY_FETCH: that half is not checked")
	}

	result, trace := traced(nil, "cat-file", "-p", "HEAD:f.txt")
	if result.Code != 0 || strings.TrimSpace(result.Stdout) != "a" {
		t.Errorf("git cat-file of a missing blob: exit status %d, printed %q, want 0 and the content a\n%s",
			result.Code, result.Stdout, result.Stderr)
	}
	if !contacted(trace) {
		t.Errorf("git cat-file of a missing blob did not run upload-pack on the remote; trace:\n%s", trace)
	}
	if isMissing("f.txt") {
		t.Errorf("the blob of f.txt is still missing after git cat-file fetched it")
	}

	// The blob came from the remote: without it, the next missing blob
	// cannot be had.
	if err := os.Rename(remote, remote+".gone"); err != nil {
		t.Fatal(err)
	}
	if result := clone.TryGit("cat-file", "-p", "HEAD:g.txt"); result.Code == 0 {
		t.Errorf("git cat-file of a missing blob exited 0 with the remote gone\n%s", result.Output())
	}
	if !isMissing("h.txt") {
		t.Errorf("the blob of h.txt, which no command needed, is no longer missing")
	}
}
