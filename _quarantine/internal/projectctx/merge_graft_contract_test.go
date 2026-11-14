package projectctx_test

// Independent contract test (assignment tests-3) for projectctx.InspectMerge
// with replace refs and grafts. Expectations come from the interface contract
// revision 2, section 1.4 (MergeFacts.Parents are the commit's parents) and
// R19 (.e: a single-parent commit has one parent and ParentFromBase false),
// as the coordinator's brief states the requirement: a fake second parent
// given by `git replace --graft` or by `.git/info/grafts` is not a parent.
//
// Not parallel: each case sets a hermetic environment with t.Setenv.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

func graftEnv(t *testing.T) {
	t.Helper()
	home := graftResolved(t, t.TempDir())
	gitConfig := filepath.Join(home, "gitconfig-empty")
	if err := os.WriteFile(gitConfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"HOME":                home,
		"XDG_CONFIG_HOME":     filepath.Join(home, "xdg"),
		"GIT_CONFIG_GLOBAL":   gitConfig,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "Contract Author",
		"GIT_AUTHOR_EMAIL":    "author@example.com",
		"GIT_COMMITTER_NAME":  "Contract Author",
		"GIT_COMMITTER_EMAIL": "author@example.com",
		"GIT_TERMINAL_PROMPT": "0",
		"GOFLAGS":             "",
		"GOENV":               filepath.Join(home, "missing-goenv"),
	} {
		t.Setenv(k, v)
	}
	for _, k := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NO_REPLACE_OBJECTS",
		"GIT_REPLACE_REF_BASE", "GIT_GRAFT_FILE", "GIT_CEILING_DIRECTORIES",
		"GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG_COUNT",
	} {
		t.Setenv(k, "") // records the old value for restoration
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
}

func graftResolved(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func graftGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func graftCommit(t *testing.T, dir, rel, content, message string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	graftGit(t, dir, "add", "-A")
	graftGit(t, dir, "commit", "-q", "--no-verify", "-m", message)
	return graftGit(t, dir, "rev-parse", "HEAD")
}

// TestContractGraftInspectMergeReportsTrueParents builds
//
//	A (root, main) -> O (main, refs/remotes/origin/main, adds o.txt)
//	A              -> S (feature, HEAD, single parent, adds s.txt)
//
// then gives S the fake parents (A, O) by a replace ref or by a grafts file.
// InspectMerge must still report S's one true parent A and ParentFromBase
// false. Violation that fails it: InspectMerge honouring the graft, which
// reports Parents [A, O] and ParentFromBase true and so lets a squash commit
// pass as a merge from the base (C7, C11).
func TestContractGraftInspectMergeReportsTrueParents(t *testing.T) {
	for _, mode := range []string{"replace-graft", "info-grafts"} {
		t.Run(mode, func(t *testing.T) {
			graftEnv(t)
			root := graftResolved(t, t.TempDir())
			graftGit(t, root, "init", "-q", "-b", "main")
			first := graftCommit(t, root, "a.txt", "a\n", "A: root")
			graftGit(t, root, "branch", "feature")
			upstream := graftCommit(t, root, "o.txt", "o\n", "O: upstream")
			graftGit(t, root, "update-ref", "refs/remotes/origin/main", upstream)
			graftGit(t, root, "checkout", "-q", "feature")
			squash := graftCommit(t, root, "s.txt", "s\n", "S: squash-style single-parent commit")
			if got := graftGit(t, root, "rev-list", "--parents", "-n", "1", squash); got != squash+" "+first {
				t.Fatalf("fixture: true parents of S = %q, want S A", got)
			}

			switch mode {
			case "replace-graft":
				graftGit(t, root, "replace", "--graft", squash, first, upstream)
			case "info-grafts":
				grafts := filepath.Join(root, ".git", "info", "grafts")
				if err := os.MkdirAll(filepath.Dir(grafts), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(grafts, []byte(squash+" "+first+" "+upstream+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// Precondition: plain Git now reports the fake second parent.
			if got := graftGit(t, root, "rev-list", "--parents", "-n", "1", squash); got != squash+" "+first+" "+upstream {
				if mode == "info-grafts" {
					t.Skipf("installed git ignores .git/info/grafts (rev-list shows %q); the grafts case cannot be exercised", got)
				}
				t.Fatalf("fixture: grafted parents of S = %q, want S A O", got)
			}

			for _, name := range []string{squash, "HEAD"} {
				facts, err := projectctx.InspectMerge(context.Background(), root, projectctx.MergeQuery{
					Commit:       name,
					BaseRef:      "origin/main",
					Head:         squash,
					BaselineHead: first,
				})
				if err != nil {
					t.Fatalf("InspectMerge(Commit %s): %v", name, err)
				}
				if facts.Commit != squash || facts.BaseCommit != upstream {
					t.Errorf("Commit %s: Commit/BaseCommit = %s/%s, want S %s / O %s", name, facts.Commit, facts.BaseCommit, squash, upstream)
				}
				if !reflect.DeepEqual(facts.Parents, []string{first}) {
					t.Errorf("Commit %s: Parents = %v, want the one true parent [A %s]", name, facts.Parents, first)
				}
				if facts.ParentFromBase {
					t.Errorf("Commit %s: ParentFromBase = true, want false (S's only parent A is not the base side)", name)
				}
				if !facts.FromHead || facts.FromBaseline || facts.FromBase {
					t.Errorf("Commit %s: FromHead/FromBaseline/FromBase = %v/%v/%v, want true/false/false",
						name, facts.FromHead, facts.FromBaseline, facts.FromBase)
				}
				if !reflect.DeepEqual(facts.Changed, []string{"s.txt"}) {
					t.Errorf("Commit %s: Changed = %v, want [s.txt] (relative to the first parent A)", name, facts.Changed)
				}
			}
		})
	}
}
