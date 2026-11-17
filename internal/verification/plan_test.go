package verification

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

const plannerDeclaration = `version = 1
[[check]]
id = "legacy"
run = ["never-execute"]
paths = ["src/**"]
[[check]]
id = "behavior"
run = ["never-execute"]
paths = ["evidence/**"]
when = ["src/**"]
expect = "reject invalid input"
verify = true
[[check]]
id = "other"
run = ["never-execute"]
when = ["other/**"]
`

func plannerGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".git", "no-global-config"), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func plannerWrite(t *testing.T, dir, path, text string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func plannerRepo(t *testing.T, declaration string, commit bool) (string, *gitx.Repo) {
	t.Helper()
	dir := t.TempDir()
	plannerGit(t, dir, "init", "-q")
	plannerWrite(t, dir, "kitsu.toml", declaration)
	plannerWrite(t, dir, "src/old", "original")
	plannerWrite(t, dir, ".gitignore", "ignored/\n")
	if commit {
		plannerGit(t, dir, "add", "-A")
		plannerGit(t, dir, "commit", "-q", "-m", "base")
	}
	repo, err := gitx.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, repo
}

func plannerBuild(t *testing.T, repo *gitx.Repo, from string) Plan {
	t.Helper()
	tree, err := repo.Snapshot(t.Context(), filepath.Join(repo.GitDir, "planner-tmp"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Build(t.Context(), repo, tree, from)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestPlanUsesCompleteSnapshotAndRetainsSuite(t *testing.T) {
	t.Parallel()
	dir, repo := plannerRepo(t, plannerDeclaration, true)
	// A move must match both the deleted source and added target.
	if err := os.Rename(filepath.Join(dir, "src/old"), filepath.Join(dir, "moved")); err != nil {
		t.Fatal(err)
	}
	plannerGit(t, dir, "add", "-A")
	plannerWrite(t, dir, "moved", "unstaged edit after staging")
	for i := range 25 {
		plannerWrite(t, dir, fmt.Sprintf("src/new-%02d", i), "untracked")
	}
	plannerWrite(t, dir, "ignored/file", "excluded")
	plannerWrite(t, dir, "docs/verification/source", "source")
	plannerWrite(t, dir, "openspec/changes/.verification/task/run.json", "generated")
	plannerWrite(t, dir, "openspec/changes/task/verification/run.json", "generated")
	indexBefore := plannerGit(t, dir, "write-tree")
	plan := plannerBuild(t, repo, "")
	if !slices.Equal(plan.AllIDs(), []string{"legacy", "behavior", "other"}) || !slices.Equal(plan.PriorityIDs(), []string{"behavior"}) || !slices.Equal(plan.RequiredIDs(), []string{"behavior"}) {
		t.Fatalf("wrong selection: %+v", plan)
	}
	if len(plan.Checks[1].MatchedPaths) != 26 || !slices.Contains(plan.Checks[1].MatchedPaths, "src/old") || !slices.Contains(plan.Checks[1].MatchedPaths, "src/new-24") {
		t.Fatalf("truncated/missing changed paths: %+v", plan.Checks[1])
	}
	if !slices.Equal(plan.Uncovered, []string{"docs/verification/source", "moved"}) {
		t.Fatalf("wrong uncovered paths: %v", plan.Uncovered)
	}
	if indexAfter := plannerGit(t, dir, "write-tree"); indexAfter != indexBefore {
		t.Fatal("planner changed the real index")
	}
	content, found, err := repo.Declaration(t.Context(), plan.SourceTree, "moved")
	if err != nil || !found || string(content) != "unstaged edit after staging" {
		t.Fatalf("source snapshot omitted unstaged content: %q, %v", content, err)
	}
}

func TestPlanMetadataAndDeclarationChanges(t *testing.T) {
	t.Parallel()
	dir, repo := plannerRepo(t, plannerDeclaration, true)
	clean := plannerBuild(t, repo, "HEAD")
	if len(clean.PriorityIDs()) != 0 || len(clean.RequiredIDs()) != 0 || len(clean.AllIDs()) != 3 {
		t.Fatalf("clean plan: %+v", clean)
	}
	// Evidence paths do not trigger verification and matching when does not
	// change the check's evidence paths.
	plannerWrite(t, dir, "evidence/file", "changed")
	evidence := plannerBuild(t, repo, "HEAD")
	if len(evidence.PriorityIDs()) != 0 || !slices.Equal(evidence.Uncovered, []string{"evidence/file"}) {
		t.Fatalf("paths used as triggers: %+v", evidence)
	}
	plannerWrite(t, dir, "kitsu.toml", "# layout only\n"+plannerDeclaration)
	layout := plannerBuild(t, repo, "HEAD")
	if layout.PolicyKey != clean.PolicyKey || len(layout.PriorityIDs()) != 3 {
		t.Fatalf("layout policy key/priority: %+v", layout)
	}
	plannerWrite(t, dir, "kitsu.toml", strings.ReplaceAll(plannerDeclaration, "reject invalid input", "accept valid input"))
	changed := plannerBuild(t, repo, "HEAD")
	if changed.PolicyKey == clean.PolicyKey || !slices.Equal(changed.RequiredIDs(), []string{"behavior"}) {
		t.Fatalf("metadata policy change ignored: %+v", changed)
	}
}

func TestPlanAlwaysVerifyUnbornAndPinnedBase(t *testing.T) {
	t.Parallel()
	declaration := plannerDeclaration + "\n[[check]]\nid = \"always\"\nrun = [\"never-execute\"]\nexpect = \"all behavior holds\"\nverify = true\n"
	_, repo := plannerRepo(t, declaration, false)
	unborn := plannerBuild(t, repo, "HEAD")
	if len(unborn.BaseTree) < 40 || len(unborn.Uncovered) != 0 || len(unborn.PriorityIDs()) != 4 || !slices.Equal(unborn.RequiredIDs(), []string{"behavior", "always"}) {
		t.Fatalf("unborn plan: %+v", unborn)
	}
	pinned := plannerBuild(t, repo, unborn.BaseTree)
	if pinned.BaseTree != unborn.BaseTree {
		t.Fatal("pinned empty base changed")
	}
	clean, err := Build(t.Context(), repo, unborn.SourceTree, unborn.SourceTree)
	if err != nil || !slices.Equal(clean.RequiredIDs(), []string{"always"}) {
		t.Fatalf("always verify clean snapshot: %+v, %v", clean, err)
	}
	if _, err := Build(t.Context(), repo, unborn.SourceTree, "missing-revision"); err == nil {
		t.Fatal("accepted missing explicit base")
	}
}

func TestHistoryExclusionsNeverChangeEvidenceDependencies(t *testing.T) {
	t.Parallel()
	dir, repo := plannerRepo(t, plannerDeclaration, true)
	before := plannerBuild(t, repo, "HEAD")
	plannerWrite(t, dir, "openspec/changes/.verification/task/current.json", "small")
	plannerWrite(t, dir, "openspec/changes/task/verification/current.json", "full")
	after := plannerBuild(t, repo, "HEAD")
	if len(after.Uncovered) != 0 || len(after.PriorityIDs()) != 0 {
		t.Fatalf("history triggered plan: %+v", after)
	}
	checks := []decl.Check{{ID: "history", Paths: []string{"openspec/changes/**"}}}
	oldKeys, err := evidence.Keys(t.Context(), repo, before.SourceTree, checks)
	if err != nil {
		t.Fatal(err)
	}
	newKeys, err := evidence.Keys(t.Context(), repo, after.SourceTree, checks)
	if err != nil {
		t.Fatal(err)
	}
	if oldKeys["history"] == newKeys["history"] {
		t.Fatal("history disappeared from evidence dependencies")
	}
	for _, path := range []string{".verification/task/source", "openspec/changes/task/verification.go", "openspec/changes/task/src/verification/source"} {
		plannerWrite(t, dir, path, "source")
	}
	lookalikes := plannerBuild(t, repo, "HEAD")
	if len(lookalikes.Uncovered) != 3 {
		t.Fatalf("history lookalikes were hidden: %v", lookalikes.Uncovered)
	}
}
