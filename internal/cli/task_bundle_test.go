package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/taskflow"
)

// cw task bundle and the bundle member of cw task assign (contract
// WP8-bundle revision 1, section CLI).

func taskBundleSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func taskBundleDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// taskBundleFiles returns every file in the task directory by relative path.
func taskBundleFiles(t *testing.T, directory string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		files[relative] = string(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

type taskBundleHarness struct {
	config Config
	out    *bytes.Buffer
	repo   string
	record taskflow.Record
}

func newTaskBundleHarness(t *testing.T) *taskBundleHarness {
	t.Helper()
	repo, home := taskBundleDir(t), t.TempDir()
	for name, data := range map[string]string{"source.txt": "verified source\n", "other.txt": "other v0\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config, out, _, _ := testConfig(t)
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	config.Getenv = func(string) string { return "" }
	config.In = strings.NewReader(`{"title":"Review project source","kind":"small","requirements":[{"id":"source","description":"Inspect source","allow_non_execution":true}],"checks":[]}`)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, config); err != nil {
		t.Fatal(err)
	}
	harness := &taskBundleHarness{config: config, out: out, repo: repo}
	if err := json.Unmarshal(out.Bytes(), &harness.record); err != nil {
		t.Fatal(err)
	}
	return harness
}

func (h *taskBundleHarness) write(t *testing.T, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.repo, name), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

type taskBundleOutput struct {
	Bundle       string `json:"bundle"`
	SHA256       string `json:"sha256"`
	ReviewID     string `json:"review_id"`
	Kind         string `json:"kind"`
	Rows         int    `json:"rows"`
	ChangedFiles int    `json:"changed_files"`
}

// bundle runs cw task bundle and, on success, checks that stdout is exactly
// one line holding exactly the documented JSON members.
func (h *taskBundleHarness) bundle(t *testing.T, args ...string) (taskBundleOutput, error) {
	t.Helper()
	h.out.Reset()
	err := Execute(t.Context(), append([]string{"task", "bundle"}, args...), h.config)
	var output taskBundleOutput
	if err != nil {
		return output, err
	}
	text := h.out.String()
	line := strings.TrimSuffix(text, "\n")
	if line == "" || strings.Contains(line, "\n") {
		t.Fatalf("stdout is not exactly one line: %q", text)
	}
	var members map[string]any
	if err := json.Unmarshal([]byte(line), &members); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, text)
	}
	if keys := slices.Sorted(maps.Keys(members)); !reflect.DeepEqual(keys, []string{"bundle", "changed_files", "kind", "review_id", "rows", "sha256"}) {
		t.Fatalf("stdout members %v", keys)
	}
	if err := json.Unmarshal([]byte(line), &output); err != nil {
		t.Fatal(err)
	}
	return output, nil
}

func (h *taskBundleHarness) run(t *testing.T, command string, input []byte) error {
	t.Helper()
	h.config.In = bytes.NewReader(input)
	h.out.Reset()
	err := Execute(t.Context(), []string{"task", command, "--task", h.record.Directory, "--input", "-"}, h.config)
	if err == nil {
		if decodeErr := json.Unmarshal(h.out.Bytes(), &h.record); decodeErr != nil {
			t.Fatalf("%s returned invalid JSON: %v", command, decodeErr)
		}
	}
	return err
}

// CLI: cw task bundle writes EncodeBundle(BuildBundle) to a new 0600 file,
// prints the one-line summary and leaves the task record unchanged.
func TestTaskBundleWritesBundleAndSummary(t *testing.T) {
	h := newTaskBundleHarness(t)
	h.write(t, "source.txt", "changed source\n")
	before := taskBundleFiles(t, h.record.Directory)
	path := filepath.Join(taskBundleDir(t), "bundle.json")
	output, err := h.bundle(t, "--task", h.record.Directory, "--review-id", "review1", "--output", path)
	if err != nil {
		t.Fatal(err)
	}
	if after := taskBundleFiles(t, h.record.Directory); !reflect.DeepEqual(after, before) {
		t.Fatal("cw task bundle changed the task directory")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := taskflow.BuildBundle(t.Context(), h.record.Directory, "review1")
	if err != nil {
		t.Fatal(err)
	}
	want, err := taskflow.EncodeBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) || !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("written bundle differs from EncodeBundle(BuildBundle):\n%s\nwant\n%s", data, want)
	}
	if !filepath.IsAbs(output.Bundle) || !samePath(output.Bundle, path) {
		t.Fatalf("bundle path %q, want absolute %q", output.Bundle, path)
	}
	if output.SHA256 != taskBundleSum(data) || output.ReviewID != "review1" || output.Kind != "full" || output.Rows != 1 || output.ChangedFiles != 1 {
		t.Fatalf("summary %+v (file sha256 %s)", output, taskBundleSum(data))
	}
	if output.Rows != len(bundle.Reach) || output.ChangedFiles != len(bundle.ChangedFiles) {
		t.Fatalf("summary counts %+v, bundle rows %d changed %d", output, len(bundle.Reach), len(bundle.ChangedFiles))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("bundle mode %v, want 0600", info.Mode().Perm())
		}
	}
}

// CLI: an existing output path is refused and left untouched.
func TestTaskBundleRefusesExistingOutput(t *testing.T) {
	h := newTaskBundleHarness(t)
	h.write(t, "source.txt", "changed source\n")
	path := filepath.Join(taskBundleDir(t), "existing.json")
	if err := os.WriteFile(path, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := taskBundleFiles(t, h.record.Directory)
	_, err := h.bundle(t, "--task", h.record.Directory, "--review-id", "review1", "--output", path)
	if err == nil || ExitCode(err) == 0 {
		t.Fatalf("existing output accepted: %v", err)
	}
	if data, readErr := os.ReadFile(path); readErr != nil || string(data) != "keep\n" {
		t.Fatalf("existing output changed: %q %v", data, readErr)
	}
	if after := taskBundleFiles(t, h.record.Directory); !reflect.DeepEqual(after, before) {
		t.Fatal("refused bundle changed the task directory")
	}
}

// CLI: --output and --review-id are required (usage error).
func TestTaskBundleRequiresOutputAndReviewID(t *testing.T) {
	h := newTaskBundleHarness(t)
	path := filepath.Join(taskBundleDir(t), "bundle.json")
	for name, args := range map[string][]string{
		"missing output":    {"--task", h.record.Directory, "--review-id", "review1"},
		"missing review id": {"--task", h.record.Directory, "--output", path},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.bundle(t, args...); err == nil || ExitCode(err) != 2 {
				t.Fatalf("usage error expected, got %v (exit %d)", err, ExitCode(err))
			}
			if _, err := os.Lstat(path); err == nil {
				t.Fatal("bundle written despite usage error")
			}
		})
	}
}

// CLI: cw task assign accepts the brief's bundle member; a later bundle is a
// follow-up of the completed review and counts only files changed since it.
func TestTaskAssignAcceptsBundleMemberAndFollowUpBundle(t *testing.T) {
	h := newTaskBundleHarness(t)
	h.write(t, "source.txt", "changed source\n")
	path := filepath.Join(taskBundleDir(t), "review1.json")
	if _, err := h.bundle(t, "--task", h.record.Directory, "--review-id", "review1", "--output", path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle taskflow.Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	for index := range bundle.Reach {
		row := &bundle.Reach[index]
		row.Callers, row.InvocationModes, row.Platforms, row.Consumers = "n/a: data file", "n/a: not executed", "All platforms", "n/a: no consumers"
	}
	filled, err := taskflow.EncodeBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	filledPath := filepath.Join(taskBundleDir(t), "filled.json")
	if err := os.WriteFile(filledPath, filled, 0o600); err != nil {
		t.Fatal(err)
	}
	brief := taskflow.WorkerBrief{
		ID: "review1", AgentID: "/root/reviewer", Role: "review",
		Goal: "Review the changed source", OwnedPaths: []string{"."},
		Constraints: []string{"Read source without changing it"}, CheckIDs: []string{},
		StopCondition: "Return findings with source evidence",
	}
	encoded, err := json.Marshal(brief)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]any
	if err := json.Unmarshal(encoded, &members); err != nil {
		t.Fatal(err)
	}
	delete(members, "bundle")
	unbundled, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.run(t, "assign", unbundled); err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Fatalf("review without bundle: %v", err)
	}
	members["bundle"] = map[string]any{"path": filledPath, "sha256": taskBundleSum(filled)}
	withBundle, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.run(t, "assign", withBundle); err != nil {
		t.Fatal(err)
	}
	if len(h.record.Assignments) != 1 {
		t.Fatalf("assignments %d", len(h.record.Assignments))
	}
	assignment := h.record.Assignments[0]
	if assignment.Brief.Bundle == nil || *assignment.Brief.Bundle != (taskflow.BundleRef{Path: filledPath, SHA256: taskBundleSum(filled)}) || assignment.BundleFile == "" {
		t.Fatalf("assignment bundle %+v file %q", assignment.Brief.Bundle, assignment.BundleFile)
	}

	source := []byte("changed source\n")
	ref := taskflow.SourceRef{Path: "source.txt", StartLine: 1, EndLine: 1, SHA256: taskBundleSum(source)}
	result, err := json.Marshal(taskflow.WorkerResult{
		TaskID: h.record.ID, AssignmentID: "review1", AssignmentSHA256: assignment.SHA256,
		AgentID: "/root/reviewer", Snapshot: assignment.Snapshot, Status: "completed",
		Summary: "Reviewed the changed source", Evidence: []taskflow.SourceRef{ref},
		ChangedFiles: []taskflow.WorkerChangedFile{}, CheckIDs: []string{},
		Findings: []taskflow.WorkerFinding{}, Uncertainties: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.run(t, "handoff", result); err != nil {
		t.Fatal(err)
	}
	acceptance, err := json.Marshal(taskflow.WorkerAcceptance{
		TaskID: h.record.ID, AssignmentID: "review1", ResultSHA256: h.record.WorkerResults[0].SHA256,
		CoordinatorID: "/root", Status: "accepted", Reason: "Checked the review against the selected source",
		Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, Findings: []taskflow.FindingDisposition{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.run(t, "accept", acceptance); err != nil {
		t.Fatal(err)
	}

	h.write(t, "other.txt", "other v1\n")
	output, err := h.bundle(t, "--task", h.record.Directory, "--review-id", "review2", "--output", filepath.Join(taskBundleDir(t), "review2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if output.ReviewID != "review2" || output.Kind != "follow_up" || output.Rows != 1 || output.ChangedFiles != 2 {
		t.Fatalf("follow-up summary %+v", output)
	}
}

// O1 (revision 2): an output path inside the project root, spelled through
// the canonical root or through a symlinked parent directory, is refused with
// an error mentioning "outside"; no file is created and the task directory is
// unchanged.
func TestTaskBundleRefusesOutputInsideProject(t *testing.T) {
	h := newTaskBundleHarness(t)
	h.write(t, "source.txt", "changed source\n")
	alias := filepath.Join(taskBundleDir(t), "project-alias")
	if err := os.Symlink(h.repo, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for name, path := range map[string]string{
		"canonical root":   filepath.Join(h.repo, "bundle.json"),
		"symlinked parent": filepath.Join(alias, "bundle.json"),
	} {
		t.Run(name, func(t *testing.T) {
			before := taskBundleFiles(t, h.record.Directory)
			_, err := h.bundle(t, "--task", h.record.Directory, "--review-id", "review1", "--output", path)
			if err == nil || !strings.Contains(err.Error(), "outside") {
				t.Fatalf("output inside the project: %v", err)
			}
			for _, written := range []string{path, filepath.Join(h.repo, "bundle.json")} {
				if _, statErr := os.Lstat(written); statErr == nil {
					t.Fatalf("refused bundle created %s", written)
				}
			}
			if after := taskBundleFiles(t, h.record.Directory); !reflect.DeepEqual(after, before) {
				t.Fatal("refused bundle changed the task directory")
			}
		})
	}
}
