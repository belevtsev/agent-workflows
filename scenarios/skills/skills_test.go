package skills_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestSkillCLIRecordsChecksAndPromotesSelections(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	personal := t.TempDir()
	for name, value := range map[string]string{"HOME": personal, "USERPROFILE": personal, "CODEX_HOME": filepath.Join(personal, ".codex"), "XDG_CONFIG_HOME": filepath.Join(personal, "config"), "APPDATA": filepath.Join(personal, "config")} {
		repo.Setenv(name, value)
	}
	const source = "---\nname: task-testing\ndescription: Test a selected task.\n---\nRead the project tests.\n"
	skillPath := ".agents/skills/task-testing/SKILL.md"
	repo.Write(skillPath, source)
	if got := repo.Kitsu("roles", "set", "developer", "--host", "codex", "--skills", "task-testing"); got.Code != 0 {
		t.Fatal(got.Output())
	}
	result := repo.Kitsu("skills", "resolve", "developer", "--host", "codex", "--task", "sample-task", "--workflow", "small", "--json")
	var resolved struct {
		SelectionID string `json:"selection_id"`
		LoadStatus  string `json:"load_status"`
		Skills      []struct{ ID, Path, SHA256 string }
	}
	if err := json.Unmarshal([]byte(result.Stdout), &resolved); result.Code != 0 || err != nil || len(resolved.Skills) != 1 || resolved.SelectionID == "" {
		t.Fatalf("resolve did not produce a receipt: %s; %v", result.Output(), err)
	}
	if resolved.LoadStatus != "unverified" || !filepath.IsAbs(resolved.Skills[0].Path) {
		t.Fatalf("loading or source path misreported: %+v", resolved)
	}
	small := "openspec/changes/.skills/sample-task/" + resolved.SelectionID + ".json"
	receipt := repo.Read(small)
	if strings.Contains(receipt, personal) || strings.Contains(receipt, resolved.Skills[0].Path) {
		t.Fatalf("portable receipt stores a machine path: %s", receipt)
	}
	if got := repo.Kitsu("change", "check"); got.Code != 0 || !strings.Contains(got.Stdout, "sample-task") {
		t.Fatalf("small-only history was not discovered: %s", got.Output())
	}
	if err := os.Remove(repo.Path(skillPath)); err != nil {
		t.Fatal(err)
	}
	if got := repo.Kitsu("change", "check", "sample-task"); got.Code != 0 {
		t.Fatalf("offline validation required installed skills: %s", got.Output())
	}
	if got := repo.Kitsu("skills", "resolve", "developer", "--host", "codex", "--task", "sample-task", "--workflow", "small"); got.Code != 1 {
		t.Fatalf("worker preflight accepted a missing skill: %s", got.Output())
	}
	if repo.Read(small) != receipt {
		t.Fatal("failed preflight rewrote history")
	}
	repo.Write(skillPath, source)
	repo.Write("openspec/changes/sample-task/proposal.md", "# Proposal: sample-task\n")
	if got := repo.Kitsu("skills", "resolve", "developer", "--host", "codex", "--task", "sample-task", "--workflow", "full"); got.Code != 0 {
		t.Fatal(got.Output())
	}
	promoted := "openspec/changes/sample-task/skills/" + resolved.SelectionID + ".json"
	if repo.Read(promoted) != receipt {
		t.Fatal("promotion changed the original selection")
	}
	if _, err := os.Stat(repo.Path(small)); !os.IsNotExist(err) {
		t.Fatalf("promotion retained competing history: %v", err)
	}
	if got := repo.Kitsu("change", "check", "sample-task"); got.Code != 0 {
		t.Fatalf("promoted selection failed validation: %s", got.Output())
	}
	repo.Write(promoted, receipt+" ")
	if got := repo.Kitsu("change", "check", "sample-task"); got.Code != 5 || !strings.Contains(got.Stdout, "skill records") {
		t.Fatalf("modified receipt passed: %s", got.Output())
	}
}

func TestSkillChecksRejectLinkedFullTaskBeforeReadingTarget(t *testing.T) {
	t.Parallel()
	repo := kitsutest.NewRepo(t)
	outside := t.TempDir()
	for _, dir := range []string{repo.Path("openspec/changes"), filepath.Join(outside, "skills"), filepath.Join(outside, "proposal.md")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, repo.Path("openspec/changes/linked-task")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	for _, mode := range []string{"global", "named"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"change", "check"}
			if mode == "named" {
				args = append(args, "linked-task")
			}
			got := repo.Kitsu(args...)
			output := strings.ReplaceAll(got.Output(), "\\", "/")
			if got.Code != 5 || !strings.Contains(output, "openspec/changes/linked-task") {
				t.Fatalf("linked full task was not refused with its path: exit %d\n%s", got.Code, output)
			}
			// The target's proposal.md is a directory: an attempted read would
			// fail there before the checker could report the linked task path.
			if strings.Contains(output, "proposal.md") {
				t.Fatalf("checker read through the task link before refusing it:\n%s", output)
			}
		})
	}
}

func TestSkillChecksRejectUnsafeTemporaryEntries(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			repo := kitsutest.NewRepo(t)
			const recordDir = "openspec/changes/.skills/temporary-task"
			if err := os.MkdirAll(repo.Path(recordDir), 0o755); err != nil {
				t.Fatal(err)
			}
			name := repo.Path(recordDir + "/.tmp-0123456789abcdef")
			if kind == "directory" {
				if err := os.Mkdir(name, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(t.TempDir(), name); err != nil {
				t.Skipf("symbolic links unavailable: %v", err)
			}
			for _, mode := range []string{"global", "named"} {
				t.Run(mode, func(t *testing.T) {
					args := []string{"change", "check"}
					if mode == "named" {
						args = append(args, "temporary-task")
					}
					got := repo.Kitsu(args...)
					if got.Code != 5 || !strings.Contains(got.Output(), ".tmp-0123456789abcdef") {
						t.Fatalf("unsafe temporary %s was not refused: exit %d\n%s", kind, got.Code, got.Output())
					}
				})
			}
		})
	}
}
