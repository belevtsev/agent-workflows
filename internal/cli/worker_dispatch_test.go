package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func savedDispatch(t *testing.T, r *kitsutest.Repo, assignment string, extra ...string) workerDispatch {
	t.Helper()
	args := []string{"task", "brief", "fix-login", "--role", "developer", "--stage", "code", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--save"}
	args = append(args, extra...)
	result := r.Run(assignment, filepath.Join(kitsutest.Binary(t), compactBinaryName()), args...)
	if result.Code != 0 || result.Stderr != "" {
		t.Fatal(result.Output())
	}
	if len(result.Stdout) > 2048 || strings.Contains(result.Stdout, "instructions") || strings.Contains(result.Stdout, assignment) {
		t.Fatalf("worker body escaped the dispatch: %s", result.Output())
	}
	var d workerDispatch
	if err := json.Unmarshal([]byte(result.Stdout), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func savedBriefMetadata(t *testing.T, data string) workerBrief {
	t.Helper()
	_, rest, ok := strings.Cut(data, "Worker brief metadata:\n")
	if !ok {
		t.Fatal("missing saved brief metadata")
	}
	var b workerBrief
	if err := json.NewDecoder(strings.NewReader(rest)).Decode(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSavedWorkerBriefDeliversOriginalCompleteAssignmentOnce(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n[codex.developer]\nmodel='chosen'\neffort='max'\nskills=['local']\n")
	writeSkillFixture(t, r.Path(".agents/skills/local/SKILL.md"), "local")
	assignment := "  Fix the login logic.\r\nKeep this exact source: `a\\b` and λ.\n\n"
	d := savedDispatch(t, r, assignment)
	data, err := os.ReadFile(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	body, err := roles.Text("developer")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "kitsu-task: fix-login code 1\n") || !strings.HasSuffix(text, "Original assignment:\n"+assignment) || strings.Count(text, body) != 1 {
		t.Fatal("label, original role or raw assignment changed", text)
	}
	b := savedBriefMetadata(t, text)
	if d.Version != 1 || d.Model != (roles.Model{Model: "chosen", Effort: "max"}) || d.SHA256 != workerBriefHash(data) || d.Baseline != b.Baseline || d.Selection != b.Selection || d.Path != r.Path(".git/kitsu-context/briefs/"+d.SHA256+".txt") {
		t.Fatal("incomplete dispatch", d, b)
	}
	if b.Instructions != "" || b.Writes == "" || b.LoadStatus != "unverified" || len(b.Skills) != 1 || b.Skills[0].Path != r.Path(".agents/skills/local/SKILL.md") || b.Skills[0].SHA256 == "" || len(b.Constraints) < 5 || len(b.TestPaths) != 1 {
		t.Fatal("mandatory worker context lost", b)
	}
	if result := r.Kitsu("role", "check", "developer", "--from", d.Baseline); result.Code != 0 {
		t.Fatal("receipt or private brief appeared after baseline", result.Output())
	}
	if again := savedDispatch(t, r, assignment); again != d {
		t.Fatal("identical assignment did not retain immutable identity", again, d)
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{d.Path: 0o600, filepath.Dir(d.Path): 0o700} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != want {
				t.Fatal("private permissions", path, info, err)
			}
		}
	}
}

func TestSavedWorkerBriefUsesNativeClaudeRoleAndFullTaskReferences(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	r.Write("openspec/changes/fix-login/proposal.md", "# Fix login\n")
	d := savedDispatch(t, r, "Review the specification independently.", "--role", "reviewer", "--stage", "spec-review", "--host", "claude", "--workflow", "full")
	data, err := os.ReadFile(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	b := savedBriefMetadata(t, string(data))
	roleBody, err := roles.Text("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if b.NativeRole != "kitsu-team:reviewer" || b.Instructions != "" || strings.Contains(string(data), roleBody) || len(b.References) != 5 || b.State.Workflow != "full" || !strings.Contains(strings.Join(b.Constraints, " "), "Review sources independently") {
		t.Fatal("native role, independence or task references lost", b)
	}
	for _, ref := range b.References {
		if strings.Contains(ref, "reviews") || strings.Contains(ref, "advice") {
			t.Fatal("reviewer context includes author or adviser records", ref)
		}
	}
}

func TestSavedWorkerBriefInputErrorsDoNotWriteSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input string
		extra       []string
	}{
		{name: "missing assignment", extra: []string{"--save"}},
		{name: "missing save", extra: []string{"--assignment", "-"}},
		{name: "empty", extra: []string{"--save", "--assignment", "-"}},
		{name: "oversized", input: strings.Repeat("x", maxWorkerAssignment+1), extra: []string{"--save", "--assignment", "-"}},
		{name: "invalid UTF8", input: "bad\xff", extra: []string{"--save", "--assignment", "-"}},
		{name: "NUL", input: "bad\x00", extra: []string{"--save", "--assignment", "-"}},
		{name: "small dispatch budget", input: "valid assignment", extra: []string{"--save", "--assignment", "-", "--max-bytes", "512"}},
		{name: "missing file", extra: []string{"--save", "--assignment", "does-not-exist"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := skillRepo(t)
			r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
			args := []string{"task", "brief", "fix-login", "--role", "developer", "--stage", "code", "--round", "1", "--host", "codex", "--workflow", "small"}
			result := r.Run(tc.input, filepath.Join(kitsutest.Binary(t), compactBinaryName()), append(args, tc.extra...)...)
			if result.Code == 0 || result.Stdout != "" || r.Exists("openspec/changes/.skills") {
				t.Fatal("invalid request wrote receipt or output", result.Output())
			}
		})
	}
}

func TestSavedWorkerBriefAcceptsAssignmentFileWithoutRewritingIt(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
	assignment := "No final newline.  "
	r.Write("assignment.txt", assignment)
	d := savedDispatch(t, r, assignment, "--assignment", r.Path("assignment.txt"))
	data, err := os.ReadFile(d.Path)
	if err != nil || !bytes.HasSuffix(data, []byte(assignment)) || r.Read("assignment.txt") != assignment {
		t.Fatal("assignment file changed", err)
	}
}

func TestSavedWorkerBriefRejectsTamperingAndLinkedStorage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"contents", "file permissions", "file symlink", "directory symlink", "cache symlink"} {
		t.Run(mode, func(t *testing.T) {
			r, _ := skillRepo(t)
			r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
			assignment := "Keep the assignment safe."
			d := savedDispatch(t, r, assignment)
			outside := filepath.Join(r.Home, "outside")
			if err := os.Mkdir(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			target := d.Path
			if mode == "file permissions" {
				if runtime.GOOS == "windows" {
					t.Skip("Windows does not expose Unix permission bits")
				}
				if err := os.Chmod(target, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if mode == "contents" {
				if err := os.WriteFile(target, []byte("tampered"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if mode == "directory symlink" {
					target = filepath.Dir(d.Path)
				} else if mode == "cache symlink" {
					target = filepath.Dir(filepath.Dir(d.Path))
				}
				if err := os.RemoveAll(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			args := []string{"task", "brief", "fix-login", "--role", "developer", "--stage", "code", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--save"}
			result := r.Run(assignment, filepath.Join(kitsutest.Binary(t), compactBinaryName()), args...)
			if result.Code == 0 || result.Stdout != "" {
				t.Fatal("tampered worker brief accepted", result.Output())
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("brief write escaped private store", entries, err)
			}
		})
	}
}

type failingAssignmentReader struct{}

func (failingAssignmentReader) Read([]byte) (int, error) {
	return 0, errors.New("intentional input failure")
}

func TestWorkerAssignmentReaderReportsErrors(t *testing.T) {
	t.Parallel()
	if _, err := readWorkerAssignment(failingAssignmentReader{}, "-"); err == nil || !strings.Contains(err.Error(), "intentional input failure") {
		t.Fatal(err)
	}
	if _, err := readWorkerAssignment(nil, "-"); err == nil {
		t.Fatal("missing reader accepted")
	}
	if _, err := readWorkerAssignment(io.LimitReader(strings.NewReader("\n \t"), 100), "-"); err == nil {
		t.Fatal("blank assignment accepted")
	}
}

func TestPreparedWorkerBriefSeparatesFreshDispatchFromWorkerCompletion(t *testing.T) {
	r, _ := skillRepo(t)
	t.Chdir(r.Dir)
	for _, kv := range r.Env() {
		name, value, ok := strings.Cut(kv, "=")
		if ok && name != "" {
			t.Setenv(name, value)
		}
	}
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n[codex.developer]\nskills=['local']\n")
	writeSkillFixture(t, r.Path(".agents/skills/local/SKILL.md"), "local")
	cfg := Config{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard}
	b := workerBrief{Task: "fix-login", Workflow: "small", Role: "developer", Stage: "code", Round: 1, Host: "codex"}
	p, err := prepareWorkerBrief(t.Context(), cfg, b, "Implement the original assignment.\n")
	if err != nil {
		t.Fatal(err)
	}
	// The stage ledger's JSON round trip must preserve the reference and its policy.
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var retained preparedWorkerBrief
	if err := json.Unmarshal(encoded, &retained); err != nil {
		t.Fatal(err)
	}
	if err := verifySavedWorkerBrief(t.Context(), cfg, retained); err != nil {
		t.Fatal("fresh dispatch rejected", err)
	}
	s, _, err := compactEngine(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	r.Write("source.go", "package fixed\n")
	if err := verifyWorkerBriefPolicy(s, retained); err != nil {
		t.Fatal("completion rejected legitimate worker source edits", err)
	}
	if err := verifySavedWorkerBrief(t.Context(), cfg, retained); err == nil || !strings.Contains(err.Error(), "repository content") {
		t.Fatal("fresh dispatch reused after source edits", err)
	}
	forged := retained
	forged.Dispatch.Path = r.Path("outside.txt")
	if err := verifyWorkerBriefPolicy(s, forged); err == nil {
		t.Fatal("arbitrary saved path accepted")
	}
	forged = retained
	forged.Brief.Constraints = []string{"Forged authority"}
	if err := verifyWorkerBriefPolicy(s, forged); err == nil {
		t.Fatal("altered stored policy metadata accepted")
	}
	r.Write(".agents/skills/local/SKILL.md", "---\nname: local\ndescription: changed\n---\nChanged original skill instructions.\n")
	if err := verifyWorkerBriefPolicy(s, retained); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatal("external source policy drift accepted", err)
	}
}

type interruptedBriefReader struct{}

func (interruptedBriefReader) Read(p []byte) (int, error) {
	return copy(p, "partial canonical assignment"), errors.New("interrupted staging")
}

func TestSavedWorkerBriefInterruptedStagingCanRetry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	canonical := []byte("kitsu-task: fix-login code 1\nComplete original assignment.\n")
	name := workerBriefHash(canonical) + ".txt"
	if _, err := stageWorkerBrief(root, interruptedBriefReader{}); err == nil || !strings.Contains(err.Error(), "interrupted staging") {
		t.Fatal("partial staging did not report failure", err)
	}
	if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted staging exposed a canonical file", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed staging left temporary files", entries, err)
	}
	// A killed process can leave its unpublished temporary behind. That file
	// must not claim the immutable name or prevent a retry of the same brief.
	if err := os.WriteFile(filepath.Join(dir, ".brief-interrupted"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveWorkerBrief(root, name, canonical); err != nil {
		t.Fatal("retry of the original assignment failed", err)
	}
	retained, err := readSavedWorkerBrief(root, name)
	if err != nil || !bytes.Equal(retained, canonical) {
		t.Fatal("retry did not publish complete canonical bytes", err)
	}
	if err := saveWorkerBrief(root, name, canonical); err != nil {
		t.Fatal("idempotent publication failed", err)
	}
	if err := saveWorkerBrief(root, name, []byte("different assignment")); err == nil {
		t.Fatal("immutable canonical file was overwritten")
	}
}
