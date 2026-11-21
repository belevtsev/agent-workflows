package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func launchPrepare(t *testing.T, r *kitsutest.Repo, host string, extra ...string) kitsutest.Result {
	t.Helper()
	args := []string{"task", "prepare", "fix-login", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", host, "--workflow", "small", "--assignment", "-", "--dispatch", "--view", "launch"}
	return r.Run("Preserve this original assignment exactly.\n", filepath.Join(kitsutest.Binary(t), compactBinaryName()), append(args, extra...)...)
}

func TestNativeLaunchRoundtripPreservesBothHostContracts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r, _ := skillRepo(t)
			r.Write("kitsu-roles.toml", "version=1\n["+host+".reviewer]\nmodel='chosen-profile'\neffort='high'\nskills=['local']\n")
			skillPath := ".agents/skills/local/SKILL.md"
			if host == "claude" {
				skillPath = ".claude/skills/local/SKILL.md"
			}
			writeSkillFixture(t, r.Path(skillPath), "local")
			leanResult := launchPrepare(t, r, host)
			lean := stageJSON(t, leanResult)
			fullResult := r.Kitsu("task", "dispatch", stageID(t, lean))
			full := stageJSON(t, fullResult)
			for _, key := range []string{"version", "label", "task", "dispatch_id", "path", "sha256", "worktree", "native", "native_prompt_sha256"} {
				if !reflect.DeepEqual(lean[key], full[key]) {
					t.Fatalf("%s changed across views", key)
				}
			}
			if lean["view"] != "launch" || lean["state"] != nil || full["view"] != nil || full["state"] == nil || lean["full_dispatch"] != "kitsu task dispatch "+stageID(t, lean) || len(leanResult.Stdout) >= len(fullResult.Stdout) {
				t.Fatal("launch projection or full reference lost", leanResult.Output(), fullResult.Output())
			}
			native := lean["native"].(map[string]any)
			if native["host"] != host || native["model"] != "chosen-profile" || native["effort"] != "high" || native["fresh_context"] != true || native["fork_turns"] != "none" || lean["native_prompt_sha256"] != workerBriefHash([]byte(native["prompt"].(string))) {
				t.Fatal("mandatory native settings changed", native)
			}
			if host == "claude" && native["role"] != "kitsu-team:reviewer" || host == "codex" && native["role"] != nil {
				t.Fatal("native role changed", native)
			}
			packet := r.Read(".git/kitsu-context/briefs/" + lean["sha256"].(string) + ".txt")
			brief := savedBriefMetadata(t, packet)
			if workerBriefHash([]byte(packet)) != lean["sha256"] || !strings.HasSuffix(packet, "Original assignment:\nPreserve this original assignment exactly.\n") || len(brief.Skills) != 1 || brief.Skills[0].Path != r.Path(skillPath) || brief.Skills[0].SHA256 != workerBriefHash([]byte(r.Read(skillPath))) {
				t.Fatal("packet assignment or selected original skill changed")
			}
			warnings := lean["warnings"].([]any)
			if !slices.Contains(warnings, any(smallTaskBriefWarning)) {
				t.Fatal("required packet warning was hidden", warnings)
			}
		})
	}
}

func TestNativeDispatchReferenceIsExactAndCreatesNoState(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	prepared := r.Run("Preserve this original assignment exactly.\n", filepath.Join(kitsutest.Binary(t), compactBinaryName()), "task", "prepare", "fix-login", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--dispatch")
	d := stageJSON(t, prepared)
	id := stageID(t, d)
	first := r.Kitsu("task", "dispatch", id)
	stageJSON(t, first)
	if first.Stdout != prepared.Stdout {
		t.Fatal("default full v2 contract changed")
	}
	// Removing these proves reads do not recreate either writer lock.
	for _, path := range []string{".git/kitsu-stages.lock", ".git/kitsu-context/cache.lock"} {
		if err := os.Remove(r.Path(path)); err != nil {
			t.Fatal(err)
		}
	}
	r.Write("source.txt", "Permitted source edit after dispatch.\n")
	before := launchFiles(t, r)
	for range 2 {
		result := r.Kitsu("task", "dispatch", id)
		if result.Code != 0 || result.Stdout != first.Stdout || result.Stderr != "" {
			t.Fatal("archival read changed original state or prompt", result.Output())
		}
	}
	if after := launchFiles(t, r); !reflect.DeepEqual(before, after) {
		t.Fatal("full reference reads created or changed files")
	}
}

func TestNativeDispatchRecordUsesSavedPromptAfterTemplateChanges(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("a", 64)
	record := stageRecord{Prompt: "Original immutable native prompt\n{\"dispatch_id\":\"\"}\n"}
	d := nativeDispatchRecord(record, id)
	want := "Original immutable native prompt\n{\"dispatch_id\":\"" + id + "\"}\n"
	if d.Native.Prompt != want || d.PromptSHA256 != workerBriefHash([]byte(want)) {
		t.Fatal("saved prompt was replaced by the current template", d.Native.Prompt)
	}
}

func launchFiles(t *testing.T, r *kitsutest.Repo) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(r.Dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil {
			files[path] = workerBriefHash(data)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestNativeDispatchReferenceRejectsTamperingAndPolicyDrift(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"record", "packet", "role", "skills", "checks", "other worktree", "legacy", "budget", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			r, _ := skillRepo(t)
			r.Write("kitsu-roles.toml", "version=1\n[codex.reviewer]\nskills=['local']\n")
			writeSkillFixture(t, r.Path(".agents/skills/local/SKILL.md"), "local")
			d := stageJSON(t, launchPrepare(t, r, "codex"))
			id := stageID(t, d)
			args := []string{"task", "dispatch", id}
			switch kind {
			case "record":
				name := ".git/kitsu-stages/" + id + ".json"
				r.Write(name, strings.Replace(r.Read(name), `"worktree":`, `"forged":true,"worktree":`, 1))
			case "packet":
				r.Write(".git/kitsu-context/briefs/"+d["sha256"].(string)+".txt", "forged packet")
			case "role":
				r.Write("kitsu-roles.toml", "version=1\n[codex.reviewer]\nmodel='changed'\nskills=['local']\n")
			case "skills":
				r.Write(".agents/skills/local/SKILL.md", r.Read(".agents/skills/local/SKILL.md")+"\nChanged instructions.\n")
			case "checks":
				r.Write("kitsu.toml", "version=1\n")
			case "other worktree":
				name := ".git/kitsu-stages/" + id + ".json"
				var record stageRecord
				if err := json.Unmarshal([]byte(r.Read(name)), &record); err != nil {
					t.Fatal(err)
				}
				record.Worktree += "-other"
				data, _ := json.Marshal(record)
				id = contextpack.Key(record)
				r.Write(".git/kitsu-stages/"+id+".json", string(data))
				args[2] = id
			case "legacy":
				name := ".git/kitsu-stages/" + id + ".json"
				var record stageRecord
				if err := json.Unmarshal([]byte(r.Read(name)), &record); err != nil {
					t.Fatal(err)
				}
				record.Prompt = ""
				data, _ := json.Marshal(record)
				id = contextpack.Key(record)
				r.Write(".git/kitsu-stages/"+id+".json", string(data))
				args[2] = id
			case "budget":
				args = append(args, "--max-bytes", "512")
			case "symlink":
				name := r.Path(".git/kitsu-stages/" + id + ".json")
				if err := os.Rename(name, name+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(name+".old", name); err != nil {
					t.Skip("symlinks unavailable", err)
				}
			}
			result := r.Kitsu(args...)
			if result.Code == 0 || result.Stdout != "" || !r.Exists(".git/kitsu-stages/active.json") {
				t.Fatal("invalid reference accepted or changed active interval", result.Output())
			}
		})
	}
}

func TestNativeLaunchBudgetFailsBeforeSelectionOrBaselineWrites(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	first := launchPrepare(t, r, "codex")
	id := stageID(t, stageJSON(t, first))
	stageJSON(t, r.Kitsu("task", "finish", id))
	before := launchFiles(t, r)
	result := launchPrepare(t, r, "codex", "--max-bytes", strconv.Itoa(len(first.Stdout)-1))
	if result.Code != 2 || result.Stdout != "" || r.Exists(".git/kitsu-stages/active.json") || !reflect.DeepEqual(before, launchFiles(t, r)) {
		t.Fatal("insufficient launch budget changed selections, baselines or dispatches", result.Output())
	}
}

func TestNativeLaunchFlagsHelpAndCompletionAreOffline(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	for _, args := range [][]string{
		{"task", "prepare", "fix-login", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--view", "launch"},
		{"task", "prepare", "fix-login", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--dispatch", "--view", "invalid"},
		{"task", "dispatch"}, {"task", "dispatch", "bad"}, {"task", "dispatch", strings.Repeat("a", 64), "extra"},
	} {
		if result := r.Kitsu(args...); result.Code != 2 || result.Stdout != "" {
			t.Fatal("invalid launch flags accepted", result.Output())
		}
	}
	for _, tc := range []struct {
		words []string
		want  string
	}{
		{[]string{"task", "dis"}, "dispatch"},
		{[]string{"task", "prepare", "--v"}, "--view"},
		{[]string{"task", "prepare", "--view", "l"}, "launch"},
		{[]string{"task", "prepare", "--view", "f"}, "full"},
		{[]string{"task", "dispatch", "--m"}, "--max-bytes"},
	} {
		if values := uxCompletionTestValues(uxQueryRepoCompletion(t, r, tc.words)); !slices.Contains(values, tc.want) {
			t.Fatal("launch completion missing", tc.want, values)
		}
	}
	for _, command := range []string{"prepare", "dispatch"} {
		for _, prefix := range [][]string{{"task", command}, {"help", "task", command}, {"task", "help", command}} {
			if result := r.Kitsu(append(prefix, "--help")...); result.Code != 0 || !strings.Contains(result.Stdout, "usage: kitsu task "+command) {
				t.Fatal(result.Output())
			}
		}
	}
	if r.Exists(".git/kitsu-stages") || r.Exists(".git/kitsu-context") || r.Exists("openspec/changes/.skills") {
		t.Fatal("invalid flags, help or completion created state")
	}
}
