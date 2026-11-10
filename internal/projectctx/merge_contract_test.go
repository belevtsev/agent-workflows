package projectctx_test

// Contract tests for projectctx.InspectMerge (interface contract R19,
// "Amendment needs a merge from the base ref"). Every expectation below comes
// from the interface contract's R19 text and the MergeFacts field comments;
// each fixture is a real temporary Git repository built with the git binary on
// PATH and an isolated Git configuration.
//
// TestContractInspectMerge sets the isolated environment once and runs its
// cases as parallel subtests (t.Setenv is not allowed in parallel tests; the
// parent's environment stays in place until every subtest has finished). Cases
// that only read share one fixture repository; cases that write (refs, objects,
// pruning, the working tree) build their own.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// mrgEnv isolates every git process started by the test and by InspectMerge
// from the user's and the system's Git configuration (including the default
// $XDG_CONFIG_HOME/git/ignore and attributes files and ~/.gitconfig), from
// inherited repository-selection variables, and from repositories above the
// temporary directory. git on PATH is a hard prerequisite.
func mrgEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required on PATH: %v", err)
	}
	home := mrgTempDir(t)
	global := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_ATTR_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_AUTHOR_NAME", "Contract Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "contract@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Contract Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "contract@example.com")
	t.Setenv("GIT_AUTHOR_DATE", "@1700000000 +0000")
	t.Setenv("GIT_COMMITTER_DATE", "@1700000000 +0000")
	tmp := filepath.Clean(os.TempDir())
	ceiling := tmp
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil && resolved != tmp {
		ceiling = resolved + string(os.PathListSeparator) + tmp
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", ceiling)
	for _, key := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE",
		"GIT_OPTIONAL_LOCKS", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS",
	} {
		mrgUnsetenv(t, key)
	}
}

func mrgUnsetenv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if !had {
		return
	}
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv(key, old) })
}

// mrgTempDir returns a symlink-resolved temporary directory (macOS /var is a
// symlink to /private/var).
func mrgTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mrgGitEnv(t *testing.T, dir string, env []string, stdin []byte, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("fixture: git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func mrgGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return mrgGitEnv(t, dir, nil, nil, args...)
}

func mrgInspect(t *testing.T, root string, q projectctx.MergeQuery) (projectctx.MergeFacts, error) {
	t.Helper()
	// The deadline only bounds waiting; R19 itself bounds each probe by 60 s.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return projectctx.InspectMerge(ctx, root, q)
}

// mrgRepo is the contract's shared fixture (section 2), reduced to its Git
// history:
//
//	R  root commit: go.mod, internal/{a,b,c,d}/*.go
//	B  baseline HEAD (parent R): changes internal/d/d.go
//	O  tip of origin/main (parent B): changes go.mod and internal/b/b.go
//	F  on feature (parent B): changes internal/a/a.go
//	M  merge on feature, parents [F, O]; relative to F it changed exactly
//	   go.mod and internal/b/b.go; HEAD is M on branch feature
//	T  tip of branch topic (parent B): changes internal/c/c.go; never merged
//
// prefix places the project in a subdirectory of the repository ("" or
// "proj/").
type mrgRepo struct {
	dir    string
	prefix string
	R, B   string
	O, F   string
	M, T   string
}

func (r *mrgRepo) write(t *testing.T, rel, content string) {
	t.Helper()
	path := filepath.Join(r.dir, filepath.FromSlash(r.prefix+rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *mrgRepo) commitAll(t *testing.T, msg string) string {
	t.Helper()
	mrgGit(t, r.dir, "add", "-A")
	mrgGit(t, r.dir, "commit", "-q", "-m", msg)
	return mrgGit(t, r.dir, "rev-parse", "HEAD")
}

// p spells a project file relative to the repository root.
func (r *mrgRepo) p(rel string) string { return r.prefix + rel }

// commitTree creates an unreferenced commit with the given tree and parents in
// order, without touching refs, index or working tree.
func (r *mrgRepo) commitTree(t *testing.T, tree string, parents ...string) string {
	t.Helper()
	args := []string{"commit-tree", tree, "-m", "synthetic"}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	return mrgGit(t, r.dir, args...)
}

func (r *mrgRepo) tree(t *testing.T, commit string) string {
	t.Helper()
	return mrgGit(t, r.dir, "rev-parse", commit+"^{tree}")
}

// mrgBuild builds the shared fixture. The caller (or its parent test) must
// have called mrgEnv.
func mrgBuild(t *testing.T, prefix, objectFormat string) *mrgRepo {
	t.Helper()
	r := &mrgRepo{dir: mrgTempDir(t), prefix: prefix}
	args := []string{"init", "-q", "-b", "feature"}
	if objectFormat != "" {
		args = append(args, "--object-format="+objectFormat)
	}
	mrgGit(t, r.dir, append(args, ".")...)

	r.write(t, "go.mod", "module example.com/m\n\ngo 1.22\n")
	for _, p := range []string{"a", "b", "c", "d"} {
		r.write(t, "internal/"+p+"/"+p+".go", "package "+p+"\n")
	}
	r.R = r.commitAll(t, "root")

	r.write(t, "internal/d/d.go", "package d\n\n// baseline\n")
	r.B = r.commitAll(t, "baseline")

	mrgGit(t, r.dir, "checkout", "-q", "-b", "mainside")
	r.write(t, "go.mod", "module example.com/m\n\ngo 1.23\n")
	r.write(t, "internal/b/b.go", "package b\n\n// from base\n")
	r.O = r.commitAll(t, "base change")
	mrgGit(t, r.dir, "update-ref", "refs/remotes/origin/main", r.O)

	mrgGit(t, r.dir, "checkout", "-q", "-b", "topic", r.B)
	r.write(t, "internal/c/c.go", "package c\n\n// topic\n")
	r.T = r.commitAll(t, "topic change")

	mrgGit(t, r.dir, "checkout", "-q", "feature")
	mrgGit(t, r.dir, "branch", "-q", "-D", "mainside")
	r.write(t, "internal/a/a.go", "package a\n\n// feature\n")
	r.F = r.commitAll(t, "feature change")
	mrgGit(t, r.dir, "merge", "-q", "--no-ff", "-m", "merge origin/main", "origin/main")
	r.M = mrgGit(t, r.dir, "rev-parse", "HEAD")
	return r
}

// mrgShared is the read-only fixture the parallel cases share: the shared
// history plus everything the cases need created up front, so no case writes
// to it.
type mrgShared struct {
	*mrgRepo
	blob     string // blob ID of go.mod at HEAD
	ref256   string // branch name of exactly 256 bytes pointing at M
	ref257   string // branch name of exactly 257 bytes pointing at M
	mergeBO  string // commit-tree merge [B, O], tree of M, off HEAD
	mergeOT  string // commit-tree merge [O, T], tree of T
	mergeFT  string // commit-tree merge [F, T], tree of M
	octopus  string // commit-tree merge [F, T, O], tree of M
	oddPaths string // commit (parent F) adding bad\x80name.go, back\slash.go, café.go
}

func mrgBuildShared(t *testing.T) *mrgShared {
	t.Helper()
	r := mrgBuild(t, "", "")
	s := &mrgShared{mrgRepo: r}
	s.blob = mrgGit(t, r.dir, "rev-parse", "HEAD:go.mod")
	s.ref256, s.ref257 = mrgLongRef(256), mrgLongRef(257)
	if len(s.ref256) != 256 || len(s.ref257) != 257 {
		t.Fatalf("fixture: ref lengths %d, %d", len(s.ref256), len(s.ref257))
	}
	mrgGit(t, r.dir, "branch", s.ref256, r.M)
	mrgGit(t, r.dir, "branch", s.ref257, r.M)
	treeM, treeT := r.tree(t, r.M), r.tree(t, r.T)
	s.mergeBO = r.commitTree(t, treeM, r.B, r.O)
	s.mergeOT = r.commitTree(t, treeT, r.O, r.T)
	s.mergeFT = r.commitTree(t, treeM, r.F, r.T)
	s.octopus = r.commitTree(t, treeM, r.F, r.T, r.O)
	s.oddPaths = mrgFastImport(t, r.dir, "refs/heads/odd", r.F, []string{
		"bad\x80name.go",
		`back\slash.go`,
		"caf\xc3\xa9.go",
	})
	return s
}

func mrgWant(t *testing.T, label string, got, want projectctx.MergeFacts, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: InspectMerge error = %v, want nil", label, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: InspectMerge facts mismatch\n got: %#v\nwant: %#v", label, got, want)
	}
}

// mrgWantError checks an error return and that no partial facts accompany it
// (R19: "returns an error, never partial facts").
func mrgWantError(t *testing.T, label string, got projectctx.MergeFacts, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: InspectMerge error = nil, want an error (facts %#v)", label, got)
		return
	}
	if !reflect.DeepEqual(got, projectctx.MergeFacts{}) {
		t.Errorf("%s: InspectMerge returned partial facts with error %v: %#v", label, err, got)
	}
}

// mrgWantNoCommit checks R19.b's shape: Commit names no commit.
func mrgWantNoCommit(t *testing.T, label string, got projectctx.MergeFacts, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: InspectMerge error = %v, want nil (an unresolvable Commit is a fact)", label, err)
	}
	if got.Commit != "" {
		t.Errorf("%s: Commit = %q, want \"\"", label, got.Commit)
	}
	if got.Parents != nil {
		t.Errorf("%s: Parents = %#v, want nil", label, got.Parents)
	}
	if got.Changed != nil {
		t.Errorf("%s: Changed = %#v, want nil", label, got.Changed)
	}
	if got.FromHead || got.FromBaseline || got.FromBase || got.ParentFromBase {
		t.Errorf("%s: booleans FromHead=%t FromBaseline=%t FromBase=%t ParentFromBase=%t, want all false",
			label, got.FromHead, got.FromBaseline, got.FromBase, got.ParentFromBase)
	}
}

// mergeFactsOfM is R19.a's expected outcome for the shared fixture.
func (r *mrgRepo) mergeFactsOfM() projectctx.MergeFacts {
	return projectctx.MergeFacts{
		Commit:         r.M,
		BaseCommit:     r.O,
		Parents:        []string{r.F, r.O},
		FromHead:       true,
		FromBaseline:   false,
		FromBase:       false,
		ParentFromBase: true,
		Changed:        []string{r.p("go.mod"), r.p("internal/b/b.go")},
	}
}

// mergeFactsOfMWithoutBase is R19.c's expected outcome: as R19.a with no base.
func (r *mrgRepo) mergeFactsOfMWithoutBase() projectctx.MergeFacts {
	want := r.mergeFactsOfM()
	want.BaseCommit, want.FromBase, want.ParentFromBase = "", false, false
	return want
}

// mrgMalformedRevisions are R19.d's malformed revision shapes, each derived
// from a revision that would otherwise resolve (HEAD).
func mrgMalformedRevisions() map[string]string {
	return map[string]string{
		"empty":            "",
		"leading dash":     "-HEAD",
		"option":           "--all",
		"single dash":      "-",
		"trailing space":   "HEAD ",
		"leading space":    " HEAD",
		"inner space":      "HEAD ~0",
		"tab":              "HEAD\t",
		"newline":          "HEAD\n",
		"carriage return":  "HEAD\r",
		"control 0x01":     "HEAD\x01",
		"control DEL 0x7f": "HEAD\x7f",
		"NUL":              "HEAD\x00",
		"longer than 256":  "HEAD" + strings.Repeat("~0", 127), // 258 bytes
	}
}

// mrgLongRef returns a valid branch name of exactly n bytes (n >= 140).
func mrgLongRef(n int) string {
	head := "l/" + strings.Repeat("a", 120) + "/"
	return head + strings.Repeat("b", n-len(head))
}

// mrgMalformedIDs are R19.h's malformed Head/BaselineHead values, derived from
// a well-formed full ID.
func mrgMalformedIDs(full string) map[string]string {
	return map[string]string{
		"symbolic":    "HEAD",
		"abbreviated": full[:12],
		"uppercase":   strings.ToUpper(full),
		"39 chars":    full[:39],
		"41 chars":    full + "0",
		"63 chars":    full + strings.Repeat("0", 63-len(full)),
		"65 chars":    full + strings.Repeat("0", 65-len(full)),
		"non-hex":     "g" + full[1:],
		"padded":      " " + full,
	}
}

// mrgFastImport commits, on top of parent and under ref, the given paths
// (raw bytes) each holding a one-line blob.
func mrgFastImport(t *testing.T, dir, ref, parent string, paths []string) string {
	t.Helper()
	var in bytes.Buffer
	in.WriteString("blob\nmark :1\ndata 2\nx\n")
	fmt.Fprintf(&in, "commit %s\ncommitter Contract Test <contract@example.com> 1700000000 +0000\ndata 9\nsynthetic\nfrom %s\n", ref, parent)
	for _, p := range paths {
		fmt.Fprintf(&in, "M 100644 :1 %s\n", p)
	}
	in.WriteString("\n")
	mrgGitEnv(t, dir, nil, in.Bytes(), "fast-import", "--quiet")
	return mrgGit(t, dir, "rev-parse", ref)
}

// mrgState captures refs, HEAD, index bytes and status without writing the
// index itself (GIT_OPTIONAL_LOCKS=0).
func mrgState(t *testing.T, dir string) string {
	t.Helper()
	noLocks := []string{"GIT_OPTIONAL_LOCKS=0"}
	index, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	refs := mrgGitEnv(t, dir, noLocks, nil, "for-each-ref", "--format=%(refname) %(objectname)")
	head := mrgGitEnv(t, dir, noLocks, nil, "rev-parse", "HEAD")
	sym := mrgGitEnv(t, dir, noLocks, nil, "symbolic-ref", "HEAD")
	status := mrgGitEnv(t, dir, noLocks, nil, "status", "--porcelain=v1", "-z")
	return fmt.Sprintf("index sha256 %x\nHEAD %s -> %s\nrefs:\n%s\nstatus: %q\n",
		sha256.Sum256(index), sym, head, refs, status)
}

// TestContractInspectMerge runs every R19 case except the PATH-wrapper case,
// which needs its own PATH.
func TestContractInspectMerge(t *testing.T) {
	mrgEnv(t)
	s := mrgBuildShared(t)
	r := s.mrgRepo
	run := func(name string, f func(t *testing.T)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f(t)
		})
	}

	// R19.a: a merge of origin/main named by full ID, abbreviated ID or HEAD.
	// Fails when any fact of M is wrong, Changed is unsorted, duplicated,
	// holds a file M did not change relative to F, or is spelled relative to
	// anything but the repository root.
	run("MergeCommitFacts", func(t *testing.T) {
		for _, commit := range []string{r.M, r.M[:12], "HEAD"} {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: commit, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
			})
			mrgWant(t, "Commit="+commit, got, r.mergeFactsOfM(), err)
		}
	})

	// R19.a and R19.h: a SHA-256 repository has 64-character IDs, which Head,
	// BaselineHead and every returned ID must accept and use. Fails when a
	// 64-character Head is rejected or IDs are truncated.
	run("SHA256Repository", func(t *testing.T) {
		r := mrgBuild(t, "", "sha256")
		if len(r.M) != 64 {
			t.Fatalf("fixture: sha256 repository produced ID %q", r.M)
		}
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: "HEAD", BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWant(t, "sha256", got, r.mergeFactsOfM(), err)
	})

	// Path base (section 1.4): Changed is relative to the repository root,
	// not to a project subdirectory. Fails when the "proj/" prefix is stripped
	// or paths are absolute.
	run("PathBaseIsRepositoryRoot", func(t *testing.T) {
		r := mrgBuild(t, "proj/", "")
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: r.M, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWant(t, "merge in proj/", got, r.mergeFactsOfM(), err)
		if err == nil && !reflect.DeepEqual(got.Changed, []string{"proj/go.mod", "proj/internal/b/b.go"}) {
			t.Errorf("Changed = %#v, want [proj/go.mod proj/internal/b/b.go]", got.Changed)
		}
		got, err = mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: r.F, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		if err != nil {
			t.Fatalf("Commit=F: error = %v", err)
		}
		if !reflect.DeepEqual(got.Changed, []string{"proj/internal/a/a.go"}) {
			t.Errorf("Commit=F: Changed = %#v, want [proj/internal/a/a.go]", got.Changed)
		}
	})

	// R19.b: Commit naming no commit is a fact, not an error. Fails when an
	// unresolvable Commit returns an error, a non-empty Commit, non-nil
	// Parents or Changed, or any true boolean.
	run("CommitNamesNoCommit", func(t *testing.T) {
		for _, commit := range []string{
			strings.Repeat("0", 39) + "1", // well-formed, unknown full ID
			"no-such-branch",
			r.tree(t, r.M), // a tree, not a commit
			s.blob,         // a blob, not a commit
		} {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: commit, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
			})
			mrgWantNoCommit(t, "Commit="+commit, got, err)
		}
	})

	// R19.c: BaseRef naming no commit leaves BaseCommit empty and FromBase and
	// ParentFromBase false while every other fact is as in R19.a.
	run("BaseRefNamesNoCommit", func(t *testing.T) {
		for _, base := range []string{"origin/does-not-exist", r.tree(t, r.O)} {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: r.M, BaseRef: base, Head: r.M, BaselineHead: r.B,
			})
			mrgWant(t, "BaseRef="+base, got, r.mergeFactsOfMWithoutBase(), err)
		}
	})

	// R19.d: a malformed Commit is treated as naming no commit (R19.b shape).
	// Fails when such a value resolves (for example after trimming) or errors.
	run("MalformedCommitNamesNoCommit", func(t *testing.T) {
		for name, commit := range mrgMalformedRevisions() {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: commit, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
			})
			mrgWantNoCommit(t, "malformed Commit ("+name+")", got, err)
		}
	})

	// R19.d: a malformed BaseRef is treated as naming no commit (R19.c shape).
	run("MalformedBaseRefNamesNoCommit", func(t *testing.T) {
		revs := mrgMalformedRevisions()
		revs["trailing space"] = "origin/main "
		revs["leading dash"] = "-origin/main"
		revs["longer than 256"] = "origin/main" + strings.Repeat("~0", 123) // 257 bytes
		for name, base := range revs {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: r.M, BaseRef: base, Head: r.M, BaselineHead: r.B,
			})
			mrgWant(t, "malformed BaseRef ("+name+")", got, r.mergeFactsOfMWithoutBase(), err)
		}
	})

	// R19.d boundary: a revision of exactly 256 bytes is passed to Git and
	// resolves; one of 257 bytes names no commit even though Git could resolve
	// it. Fails on an off-by-one in the length limit.
	run("RevisionLengthBoundary", func(t *testing.T) {
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: s.ref256, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWant(t, "Commit of 256 bytes", got, r.mergeFactsOfM(), err)

		got, err = mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: s.ref257, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWantNoCommit(t, "Commit of 257 bytes", got, err)

		got, err = mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: r.M, BaseRef: s.ref257, Head: r.M, BaselineHead: r.B,
		})
		mrgWant(t, "BaseRef of 257 bytes", got, r.mergeFactsOfMWithoutBase(), err)
	})

	// R19.e: a single-parent commit has one parent, ParentFromBase false and
	// the files it changed relative to that parent. The ancestry booleans
	// follow the MergeFacts field definitions.
	run("SingleParentCommit", func(t *testing.T) {
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: r.F, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWant(t, "Commit=F", got, projectctx.MergeFacts{
			Commit:     r.F,
			BaseCommit: r.O,
			Parents:    []string{r.B},
			FromHead:   true,
			Changed:    []string{"internal/a/a.go"},
		}, err)

		// O is the base itself: FromBase true, yet one parent so
		// ParentFromBase false.
		got, err = mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: "origin/main", BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWant(t, "Commit=O", got, projectctx.MergeFacts{
			Commit:     r.O,
			BaseCommit: r.O,
			Parents:    []string{r.B},
			FromHead:   true,
			FromBase:   true,
			Changed:    []string{"go.mod", "internal/b/b.go"},
		}, err)
	})

	// R19.f and the FromBaseline/FromBase/FromHead definitions: B is the
	// baseline itself and an ancestor of HEAD and of the base. With
	// BaselineHead "" the same commit reports FromBaseline false.
	run("BaselineFacts", func(t *testing.T) {
		want := projectctx.MergeFacts{
			Commit:       r.B,
			BaseCommit:   r.O,
			Parents:      []string{r.R},
			FromHead:     true,
			FromBaseline: true,
			FromBase:     true,
			Changed:      []string{"internal/d/d.go"},
		}
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: r.B, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWant(t, "Commit=B, BaselineHead=B", got, want, err)

		want.FromBaseline = false
		got, err = mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: r.B, BaseRef: "origin/main", Head: r.M, BaselineHead: "",
		})
		mrgWant(t, "Commit=B, BaselineHead empty (R19.f)", got, want, err)
	})

	// MergeFacts.FromHead and ParentFromBase over merges built off-branch:
	// ParentFromBase looks only at parents after the first, and any of them
	// counts. Fails when the first parent is consulted, only the second
	// parent is consulted, or reachability from HEAD is not checked.
	run("ParentFromBase", func(t *testing.T) {
		cases := []struct {
			name string
			id   string
			want projectctx.MergeFacts
		}{{
			// A merge of the base on another branch (R3.c): not reachable
			// from HEAD.
			name: "parents [B, O], off HEAD",
			id:   s.mergeBO,
			want: projectctx.MergeFacts{
				Parents: []string{r.B, r.O}, ParentFromBase: true,
				Changed: []string{"go.mod", "internal/a/a.go", "internal/b/b.go"},
			},
		}, {
			// Only the first parent is from the base.
			name: "parents [O, T]",
			id:   s.mergeOT,
			want: projectctx.MergeFacts{
				Parents: []string{r.O, r.T}, ParentFromBase: false,
				Changed: []string{"go.mod", "internal/b/b.go", "internal/c/c.go"},
			},
		}, {
			// A merge of a topic branch not reachable from the base (R3.e).
			name: "parents [F, T]",
			id:   s.mergeFT,
			want: projectctx.MergeFacts{
				Parents: []string{r.F, r.T}, ParentFromBase: false,
				Changed: []string{"go.mod", "internal/b/b.go"},
			},
		}, {
			// Octopus: the third parent is the base.
			name: "parents [F, T, O]",
			id:   s.octopus,
			want: projectctx.MergeFacts{
				Parents: []string{r.F, r.T, r.O}, ParentFromBase: true,
				Changed: []string{"go.mod", "internal/b/b.go"},
			},
		}}
		for _, tc := range cases {
			tc.want.Commit, tc.want.BaseCommit = tc.id, r.O
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: tc.id, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
			})
			mrgWant(t, tc.name, got, tc.want, err)
		}

		// The octopus as Head: F is then reachable from Head.
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: r.F, BaseRef: "origin/main", Head: s.octopus, BaselineHead: r.B,
		})
		if err != nil {
			t.Fatalf("Head=octopus: error = %v", err)
		}
		if !got.FromHead {
			t.Errorf("Head=octopus, Commit=F: FromHead = false, want true (F is a parent of Head)")
		}
	})

	// R19.g: root outside any Git repository is an error with no facts.
	// GIT_CEILING_DIRECTORIES (mrgEnv) keeps discovery inside the temporary
	// directory.
	run("NotARepository", func(t *testing.T) {
		plain := mrgTempDir(t)
		q := projectctx.MergeQuery{Commit: r.M, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B}
		got, err := mrgInspect(t, plain, q)
		mrgWantError(t, "root is a plain directory", got, err)
		got, err = mrgInspect(t, filepath.Join(plain, "missing"), q)
		mrgWantError(t, "root does not exist", got, err)
	})

	// R19.h: a malformed Head (including "") or a malformed non-empty
	// BaselineHead is an error with no facts.
	run("MalformedHeadIDs", func(t *testing.T) {
		heads := mrgMalformedIDs(r.M)
		heads["empty"] = ""
		for name, head := range heads {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: r.M, BaseRef: "origin/main", Head: head, BaselineHead: r.B,
			})
			mrgWantError(t, "Head "+name, got, err)
		}
		for name, base := range mrgMalformedIDs(r.B) {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: r.M, BaseRef: "origin/main", Head: r.M, BaselineHead: base,
			})
			mrgWantError(t, "BaselineHead "+name, got, err)
		}
	})

	// R19 ("Head does not name a commit there"): a well-formed Head that is
	// unknown, or names a tree, is an error.
	run("HeadNamesNoCommit", func(t *testing.T) {
		for name, head := range map[string]string{
			"unknown ID": strings.Repeat("0", 39) + "1",
			"tree ID":    r.tree(t, r.M),
		} {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: r.M, BaseRef: "origin/main", Head: head, BaselineHead: r.B,
			})
			mrgWantError(t, "Head "+name, got, err)
		}
	})

	// R19.i: an already-cancelled context yields an error wrapping
	// context.Canceled and no facts.
	run("CancelledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got, err := projectctx.InspectMerge(ctx, r.dir, projectctx.MergeQuery{
			Commit: r.M, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want errors.Is(err, context.Canceled)", err)
		}
		if !reflect.DeepEqual(got, projectctx.MergeFacts{}) {
			t.Errorf("cancelled call returned partial facts: %#v", got)
		}
	})

	// R19.j: a rename relative to the first parent lists both the old and the
	// new path. Fails when rename detection collapses it to one side.
	run("RenameListsBothPaths", func(t *testing.T) {
		r := &mrgRepo{dir: mrgTempDir(t)}
		mrgGit(t, r.dir, "init", "-q", "-b", "feature", ".")
		r.write(t, "x/a.go", "package x\n\n"+strings.Repeat("// line of stable content\n", 40))
		r.write(t, "q.txt", "q\n")
		root := r.commitAll(t, "root")
		mrgGit(t, r.dir, "checkout", "-q", "-b", "mainside")
		if err := os.MkdirAll(filepath.Join(r.dir, "y"), 0o755); err != nil {
			t.Fatal(err)
		}
		mrgGit(t, r.dir, "mv", "x/a.go", "y/a.go")
		base := r.commitAll(t, "rename on base")
		mrgGit(t, r.dir, "update-ref", "refs/remotes/origin/main", base)
		mrgGit(t, r.dir, "checkout", "-q", "feature")
		mrgGit(t, r.dir, "branch", "-q", "-D", "mainside")
		r.write(t, "q.txt", "q changed\n")
		first := r.commitAll(t, "feature change")
		mrgGit(t, r.dir, "merge", "-q", "--no-ff", "-m", "merge origin/main", "origin/main")
		merge := mrgGit(t, r.dir, "rev-parse", "HEAD")

		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: merge, BaseRef: "origin/main", Head: merge, BaselineHead: root,
		})
		mrgWant(t, "merge renaming x/a.go", got, projectctx.MergeFacts{
			Commit: merge, BaseCommit: base, Parents: []string{first, base},
			FromHead: true, ParentFromBase: true,
			Changed: []string{"x/a.go", "y/a.go"},
		}, err)

		got, err = mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: base, BaseRef: "origin/main", Head: merge, BaselineHead: root,
		})
		if err != nil {
			t.Fatalf("single-parent rename: error = %v", err)
		}
		if !reflect.DeepEqual(got.Changed, []string{"x/a.go", "y/a.go"}) {
			t.Errorf("single-parent rename: Changed = %#v, want [x/a.go y/a.go]", got.Changed)
		}
	})

	// R19.k: InspectMerge leaves refs, index, working tree and status
	// unchanged, on a dirty tree and for every kind of query, including
	// failing ones.
	run("IsReadOnly", func(t *testing.T) {
		r := mrgBuild(t, "", "")
		r.write(t, "internal/c/c.go", "package c\n\n// uncommitted\n")
		r.write(t, "untracked.txt", "untracked\n")
		before := mrgState(t, r.dir)
		for _, q := range []projectctx.MergeQuery{
			{Commit: r.M, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B},
			{Commit: "HEAD", BaseRef: "origin/main", Head: r.M, BaselineHead: ""},
			{Commit: r.F, BaseRef: "topic", Head: r.M, BaselineHead: r.B},
			{Commit: r.R, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B},
			{Commit: "no-such-branch", BaseRef: "origin/does-not-exist", Head: r.M, BaselineHead: r.B},
			{Commit: r.M, BaseRef: "origin/main", Head: strings.Repeat("0", 39) + "1", BaselineHead: r.B},
		} {
			_, _ = mrgInspect(t, r.dir, q)
		}
		if after := mrgState(t, r.dir); after != before {
			t.Errorf("repository state changed\nbefore:\n%s\nafter:\n%s", before, after)
		}
		data, err := os.ReadFile(filepath.Join(r.dir, "internal", "c", "c.go"))
		if err != nil || string(data) != "package c\n\n// uncommitted\n" {
			t.Errorf("working-tree edit changed: %q, %v", data, err)
		}
	})

	// R19.l: a changed path that is not valid UTF-8 or holds a backslash is
	// spelled with each backslash as \\ and each invalid byte as \xNN; valid
	// UTF-8 stays literal. Fails on Git's own quoting (octal, double quotes)
	// or on raw invalid bytes.
	run("UnusualPathSpelling", func(t *testing.T) {
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: s.oddPaths, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		want := []string{`bad\x80name.go`, `back\\slash.go`, "café.go"}
		gotSet := append([]string(nil), got.Changed...)
		sort.Strings(gotSet)
		sort.Strings(want)
		if !reflect.DeepEqual(gotSet, want) {
			t.Errorf("Changed = %#v, want (in any order) %#v", got.Changed, want)
		}
	})

	// R19.m: a root commit has an empty, non-nil Parents, no Changed, and no
	// error. Ancestry follows the field definitions (R precedes B, O and M).
	run("RootCommit", func(t *testing.T) {
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: r.R, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWant(t, "root commit", got, projectctx.MergeFacts{
			Commit:       r.R,
			BaseCommit:   r.O,
			Parents:      []string{},
			FromHead:     true,
			FromBaseline: true,
			FromBase:     true,
			Changed:      nil,
		}, err)
		if err == nil && got.Parents == nil {
			t.Errorf("root commit: Parents is nil, want empty and non-nil")
		}
	})

	// R19.n and the exit-status rule: a well-formed BaselineHead the
	// repository does not hold makes the ancestry probe fail with a status
	// other than 1, which is an error, never FromBaseline false. Own
	// repository: it prunes objects.
	run("MissingBaselineObject", func(t *testing.T) {
		r := mrgBuild(t, "", "")
		pruned := r.commitTree(t, r.tree(t, r.F), r.F)
		mrgGit(t, r.dir, "prune", "--expire=now")
		cmd := exec.Command("git", "cat-file", "-e", pruned)
		cmd.Dir = r.dir
		if cmd.Run() == nil {
			t.Fatalf("fixture: pruned commit %s is still present", pruned)
		}
		for name, base := range map[string]string{
			"pruned commit":   pruned,
			"never existed":   strings.Repeat("0", 39) + "1",
			"never existed 2": strings.Repeat("f", 40),
		} {
			got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
				Commit: r.M, BaseRef: "origin/main", Head: r.M, BaselineHead: base,
			})
			mrgWantError(t, "BaselineHead "+name, got, err)
		}
	})

	// R19.o: a shallow clone is refused with an error naming the cause and
	// the fix, whatever the query asks.
	run("ShallowRepository", func(t *testing.T) {
		parent := mrgTempDir(t)
		mrgGit(t, parent, "clone", "-q", "--depth", "1", "file://"+r.dir, "shallow")
		shallow := filepath.Join(parent, "shallow")
		if got := mrgGit(t, shallow, "rev-parse", "--is-shallow-repository"); got != "true" {
			t.Fatalf("fixture: clone is not shallow (%q)", got)
		}
		head := mrgGit(t, shallow, "rev-parse", "HEAD")
		for _, q := range []projectctx.MergeQuery{
			{Commit: "HEAD", BaseRef: "origin/feature", Head: head, BaselineHead: ""},
			{Commit: head, BaseRef: "origin/feature", Head: head, BaselineHead: head},
			{Commit: "no-such-branch", BaseRef: "origin/does-not-exist", Head: head, BaselineHead: ""},
		} {
			got, err := mrgInspect(t, shallow, q)
			mrgWantError(t, "shallow "+q.Commit, got, err)
			if err == nil {
				continue
			}
			for _, phrase := range []string{"repository is shallow", "git fetch --unshallow"} {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("shallow %s: error %q does not contain %q", q.Commit, err, phrase)
				}
			}
		}
	})

	// R19 ("a probe's output exceeds 8 MiB"): a commit whose changed-file
	// list alone is about 9.8 MB is an error, not truncated facts.
	run("OversizedProbeOutput", func(t *testing.T) {
		if testing.Short() {
			t.Skip("builds a 50000-file tree")
		}
		r := mrgBuild(t, "", "")
		dirName := "big/" + strings.Repeat("p", 180) + "/"
		paths := make([]string, 50000)
		total := 0
		for i := range paths {
			paths[i] = fmt.Sprintf("%s%08d.go", dirName, i)
			total += len(paths[i]) + 1
		}
		if total <= 8<<20+(1<<20) {
			t.Fatalf("fixture: changed-path bytes %d not well above 8 MiB", total)
		}
		big := mrgFastImport(t, r.dir, "refs/heads/big", r.F, paths)
		got, err := mrgInspect(t, r.dir, projectctx.MergeQuery{
			Commit: big, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B,
		})
		mrgWantError(t, "changed list over 8 MiB", got, err)
	})
}

// mrgWrapGit puts a logging git wrapper first on PATH: it appends every
// argument of every invocation to the returned log file, then runs the real
// git.
func mrgWrapGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git is required on PATH: %v", err)
	}
	if real, err = filepath.Abs(real); err != nil {
		t.Fatal(err)
	}
	bin := mrgTempDir(t)
	logFile := filepath.Join(mrgTempDir(t), "git-argv.log")
	script := fmt.Sprintf("#!/bin/sh\nfor a in \"$@\"; do printf '%%s\\n' \"$a\" >> '%s'; done\nprintf '%%s\\n' '--end--' >> '%s'\nexec '%s' \"$@\"\n",
		logFile, logFile, real)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logFile
}

func mrgReadLog(t *testing.T, logFile string) []byte {
	t.Helper()
	data, err := os.ReadFile(logFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mrgResetLog(t *testing.T, logFile string) {
	t.Helper()
	if err := os.Remove(logFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

// R19.d ("without being passed to Git") and R19.h ("no Git command runs").
// Fails when a malformed revision reaches a git argv, or when a malformed
// Head/BaselineHead still starts git. It runs on its own (not in parallel)
// because it changes PATH. The control call fails the test, rather than
// skipping it, when a valid call leaves no trace: git on PATH is a hard
// prerequisite of these tests.
func TestContractInspectMergeMalformedInputsNeverReachGit(t *testing.T) {
	mrgEnv(t)
	r := mrgBuild(t, "", "")
	logFile := mrgWrapGit(t)
	valid := projectctx.MergeQuery{Commit: r.M, BaseRef: "origin/main", Head: r.M, BaselineHead: r.B}
	if _, err := mrgInspect(t, r.dir, valid); err != nil || len(mrgReadLog(t, logFile)) == 0 {
		t.Fatalf("control: a valid InspectMerge call (error %v) did not run the git found on PATH", err)
	}

	const marker = "zzzcontractprobe"
	for name, rev := range map[string]string{
		"leading dash":    "-" + marker,
		"option":          "--" + marker,
		"space":           "HEAD " + marker,
		"tab":             marker + "\tHEAD",
		"newline":         marker + "\n",
		"control 0x01":    marker + "\x01",
		"control DEL":     marker + "\x7f",
		"longer than 256": marker + strings.Repeat("x", 300),
	} {
		mrgResetLog(t, logFile)
		q := valid
		q.Commit = rev
		_, _ = mrgInspect(t, r.dir, q)
		q = valid
		q.BaseRef = rev
		_, _ = mrgInspect(t, r.dir, q)
		if bytes.Contains(mrgReadLog(t, logFile), []byte(marker)) {
			t.Errorf("malformed revision (%s) was passed to git:\n%s", name, mrgReadLog(t, logFile))
		}
	}

	for name, head := range mrgMalformedIDs(r.M) {
		mrgResetLog(t, logFile)
		q := valid
		q.Head = head
		_, _ = mrgInspect(t, r.dir, q)
		if log := mrgReadLog(t, logFile); len(log) != 0 {
			t.Errorf("malformed Head (%s) still ran git:\n%s", name, log)
		}
		mrgResetLog(t, logFile)
		q = valid
		q.BaselineHead = mrgMalformedIDs(r.B)[name]
		_, _ = mrgInspect(t, r.dir, q)
		if log := mrgReadLog(t, logFile); len(log) != 0 {
			t.Errorf("malformed BaselineHead (%s) still ran git:\n%s", name, log)
		}
	}
}
