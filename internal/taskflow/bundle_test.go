package taskflow

import (
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// Reviewer bundle and baseline manifest tests (contract WP8-bundle revision 1).
// Requirement IDs are named in each test's comment.

func bundleTestSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// bundleTestDir returns a fresh temporary directory without symlinked path
// components, so absolute bundle paths never pass through a symlink.
func bundleTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func bundleTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	base := []string{"-C", root, "-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "-c", "commit.gpgsign=false"}
	command := exec.Command("git", append(base, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, output)
	}
}

func bundleTestWrite(t *testing.T, root, path, data string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func bundleTestRemove(t *testing.T, root, path string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatal(err)
	}
}

// bundleTestRepo writes files into a fresh project; with git it also commits
// them as the initial revision.
func bundleTestRepo(t *testing.T, files map[string]string, git bool) string {
	t.Helper()
	root := bundleTestDir(t)
	for path, data := range files {
		bundleTestWrite(t, root, path, data)
	}
	if git {
		bundleTestGit(t, root, "init", "-q", "-b", "main")
		bundleTestGit(t, root, "add", ".")
		bundleTestGit(t, root, "commit", "-qm", "baseline")
	}
	return root
}

func bundleTestStart(t *testing.T, repo string, plan Plan) Record {
	t.Helper()
	record, err := StartWithPacks(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), plan, fixtureBinding(), SelectionExplicit, nil)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// bundleTestRef cites a whole current file.
func bundleTestRef(t *testing.T, record Record, path string) SourceRef {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(record.Baseline.Root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(data), "\n")
	if !strings.HasSuffix(string(data), "\n") {
		lines++
	}
	return SourceRef{Path: path, StartLine: 1, EndLine: max(lines, 1), SHA256: bundleTestSum(data)}
}

func mustBuildBundle(t *testing.T, record Record, reviewID string) Bundle {
	t.Helper()
	bundle, err := BuildBundle(t.Context(), record.Directory, reviewID)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func bundleTestFill(bundle *Bundle, text string) {
	for index := range bundle.Reach {
		row := &bundle.Reach[index]
		row.Callers, row.InvocationModes, row.Platforms, row.Consumers = text, text, text, text
	}
}

func bundleTestWriteBytes(t *testing.T, data []byte) *BundleRef {
	t.Helper()
	path := filepath.Join(bundleTestDir(t), "bundle.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return &BundleRef{Path: path, SHA256: bundleTestSum(data)}
}

func bundleTestWriteBundle(t *testing.T, bundle Bundle) (*BundleRef, []byte) {
	t.Helper()
	data, err := EncodeBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	return bundleTestWriteBytes(t, data), data
}

// fixtureReviewBundle builds the bundle for a review about to be assigned,
// fills every facet with a fixture rationale and writes it to a private file.
// mustAssignWorker uses it for review briefs that carry no bundle.
func fixtureReviewBundle(t *testing.T, record Record, reviewID string) *BundleRef {
	t.Helper()
	bundle := mustBuildBundle(t, record, reviewID)
	bundleTestFill(&bundle, "n/a: test fixture")
	ref, _ := bundleTestWriteBundle(t, bundle)
	return ref
}

// bundleTestSubmitReview assigns a review (with a generated fixture bundle),
// runs the focused check and submits a result with the given status.
func bundleTestSubmitReview(t *testing.T, record Record, id, status string, findings []WorkerFinding) Record {
	t.Helper()
	record = mustAssignWorker(t, record, workerBrief(id, "/root/reviewer", "review", []string{"."}, "focused"))
	if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	record, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	result := workerResult(t, record, id, status, bundleTestRef(t, record, "source.txt"))
	if findings != nil {
		result.Findings = findings
	}
	return mustSubmitWorker(t, record, result)
}

func bundleTestAccept(t *testing.T, record Record, id, status string, dispositions []FindingDisposition) Record {
	t.Helper()
	var acceptance WorkerAcceptance
	if status == "unused" {
		acceptance = workerAcceptance(t, record, id, status)
		acceptance.CheckIDs = []string{}
	} else {
		acceptance = workerAcceptance(t, record, id, status, bundleTestRef(t, record, "source.txt"))
	}
	if dispositions != nil {
		acceptance.Findings = dispositions
	}
	return mustAcceptWorker(t, record, acceptance)
}

func bundleTestChangedPaths(files []ChangedFile) []string {
	paths := []string{}
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func bundleTestReachPaths(rows []ReachRow) []string {
	paths := []string{}
	for _, row := range rows {
		paths = append(paths, row.Path)
	}
	return paths
}

// bundleTestRefused asserts that AssignWorker refuses the brief, that the
// error mentions every wanted fragment, and that no task file changed.
func bundleTestRefused(t *testing.T, record Record, brief WorkerBrief, want ...string) {
	t.Helper()
	before := productionTaskBytes(t, record.Directory)
	_, err := AssignWorker(t.Context(), record.Directory, brief)
	if err == nil {
		t.Fatalf("assignment %q accepted", brief.ID)
	}
	for _, fragment := range want {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("refusal %q does not mention %q", err, fragment)
		}
	}
	if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
		t.Fatal("refused assignment changed task files")
	}
}

// M1: Start writes a baseline manifest artifact into the task directory and
// binds its hash and entry count in the record.
func TestStartRecordsBaselineManifest(t *testing.T) {
	repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "a.txt": "a\n", "nested/b.txt": "b\n"}, false)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	manifest := record.BaselineManifest
	if manifest == nil {
		t.Fatal("Start wrote no baseline manifest")
	}
	if manifest.File == "" || filepath.Base(manifest.File) != manifest.File {
		t.Fatalf("manifest file %q is not a file name inside the task directory", manifest.File)
	}
	data, err := os.ReadFile(filepath.Join(record.Directory, manifest.File))
	if err != nil {
		t.Fatal(err)
	}
	if bundleTestSum(data) != manifest.SHA256 {
		t.Fatalf("manifest sha256 %s does not match file bytes %s", manifest.SHA256, bundleTestSum(data))
	}
	if manifest.Files != 3 {
		t.Fatalf("manifest lists %d files, want 3", manifest.Files)
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BaselineManifest == nil || *loaded.BaselineManifest != *manifest {
		t.Fatalf("loaded manifest %+v, started %+v", loaded.BaselineManifest, manifest)
	}
}

// B1, B4: changed_files compares the baseline and current manifests; files
// dirty at start but unchanged since and ignored files are not listed; a
// mode-only change is listed; deletions carry empty mode and hash.
func TestBundleChangedFilesCompareBaselineAndCurrentManifests(t *testing.T) {
	repo := bundleTestRepo(t, map[string]string{
		"source.txt": "first line\nsecond line\n",
		".gitignore": "*.log\n",
		"keep.txt":   "keep\n",
		"edit.txt":   "edit v0\n",
		"gone.txt":   "gone\n",
		"tool.sh":    "#!/bin/sh\necho tool\n",
		"dirty.txt":  "dirty v0\n",
	}, true)
	bundleTestWrite(t, repo, "dirty.txt", "dirty v1\n")
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "edit.txt", "edit v1\n")
	bundleTestRemove(t, repo, "gone.txt")
	if err := os.Chmod(filepath.Join(repo, "tool.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	bundleTestWrite(t, repo, "new.txt", "new\n")
	bundleTestWrite(t, repo, "out.log", "ignored output\n")

	bundle := mustBuildBundle(t, record, "review")
	want := []ChangedFile{
		{Path: "edit.txt", Mode: "100644", SHA256: bundleTestSum([]byte("edit v1\n"))},
		{Path: "gone.txt", Mode: "", SHA256: "", Deleted: true},
		{Path: "new.txt", Mode: "100644", SHA256: bundleTestSum([]byte("new\n"))},
		{Path: "tool.sh", Mode: "100755", SHA256: bundleTestSum([]byte("#!/bin/sh\necho tool\n"))},
	}
	if !reflect.DeepEqual(bundle.ChangedFiles, want) {
		t.Fatalf("changed_files:\n got %+v\nwant %+v", bundle.ChangedFiles, want)
	}
	paths := []string{"edit.txt", "gone.txt", "new.txt", "tool.sh"}
	if bundle.Kind != "full" || bundle.PreviousReview != "" || !reflect.DeepEqual(bundle.ChangedSinceReview, paths) {
		t.Fatalf("first review: kind %q previous %q since %v", bundle.Kind, bundle.PreviousReview, bundle.ChangedSinceReview)
	}
	if !reflect.DeepEqual(bundleTestReachPaths(bundle.Reach), paths) {
		t.Fatalf("reach rows %v, want %v", bundleTestReachPaths(bundle.Reach), paths)
	}
	for _, row := range bundle.Reach {
		if row.Deleted != (row.Path == "gone.txt") {
			t.Errorf("row %s deleted = %v", row.Path, row.Deleted)
		}
		if row.Callers != "" || row.InvocationModes != "" || row.Platforms != "" || row.Consumers != "" {
			t.Errorf("row %s has generated facets: %+v", row.Path, row)
		}
	}
}

// B2: manifest_sha256 is lowercase hex, stable for identical source and
// record, and changes with changed_files.
func TestBundleManifestDigestIsStableAndBindsChangedFiles(t *testing.T) {
	repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "a.txt": "a0\n"}, false)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "a.txt", "a1\n")
	first := mustBuildBundle(t, record, "review")
	second := mustBuildBundle(t, record, "review")
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(first.ManifestSHA256) {
		t.Fatalf("manifest_sha256 %q is not 64 lowercase hex", first.ManifestSHA256)
	}
	if first.ManifestSHA256 != second.ManifestSHA256 {
		t.Fatalf("identical builds differ: %s vs %s", first.ManifestSHA256, second.ManifestSHA256)
	}
	bundleTestWrite(t, repo, "a.txt", "a2\n")
	third := mustBuildBundle(t, record, "review")
	if reflect.DeepEqual(third.ChangedFiles, first.ChangedFiles) {
		t.Fatal("fixture precondition: changed_files did not change")
	}
	if third.ManifestSHA256 == first.ManifestSHA256 {
		t.Fatal("different changed_files kept the same manifest_sha256")
	}
}

// B3, B4: a follow-up review lists files whose changed_files entry differs
// from the previous reviewed bundle, including a file reverted to baseline
// and a deleted file, and omits files untouched since that review.
func TestBundleFollowUpListsChangesSincePreviousReview(t *testing.T) {
	repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "a.txt": "a0\n", "b.txt": "b0\n", "c.txt": "c0\n", "d.txt": "d0\n"}, false)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "a.txt", "a1\n")
	bundleTestWrite(t, repo, "b.txt", "b1\n")
	first := mustBuildBundle(t, record, "review1")
	if first.Kind != "full" || first.PreviousReview != "" || !reflect.DeepEqual(first.ChangedSinceReview, []string{"a.txt", "b.txt"}) {
		t.Fatalf("first review: kind %q previous %q since %v", first.Kind, first.PreviousReview, first.ChangedSinceReview)
	}
	record = bundleTestSubmitReview(t, record, "review1", "completed", nil)
	record = bundleTestAccept(t, record, "review1", "accepted", nil)

	bundleTestWrite(t, repo, "b.txt", "b0\n")
	bundleTestWrite(t, repo, "c.txt", "c1\n")
	bundleTestRemove(t, repo, "d.txt")
	second := mustBuildBundle(t, record, "review2")
	if second.Kind != "follow_up" || second.PreviousReview != "review1" {
		t.Fatalf("second review: kind %q previous %q", second.Kind, second.PreviousReview)
	}
	if got := bundleTestChangedPaths(second.ChangedFiles); !reflect.DeepEqual(got, []string{"a.txt", "c.txt", "d.txt"}) {
		t.Fatalf("changed_files paths %v", got)
	}
	since := []string{"b.txt", "c.txt", "d.txt"}
	if !reflect.DeepEqual(second.ChangedSinceReview, since) {
		t.Fatalf("changed_since_review %v, want %v", second.ChangedSinceReview, since)
	}
	want := []ReachRow{
		{Path: "b.txt", Hints: ReachHints{Shebang: "", References: []string{}}},
		{Path: "c.txt", Hints: ReachHints{Shebang: "", References: []string{}}},
		{Path: "d.txt", Deleted: true, Hints: ReachHints{Shebang: "", References: []string{}}},
	}
	if !reflect.DeepEqual(second.Reach, want) {
		t.Fatalf("reach:\n got %+v\nwant %+v", second.Reach, want)
	}
}

// B3: which closed reviews count as the previous reviewed source.
func TestBundlePreviousReviewRequiresCompletedUnrejectedResult(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		result      string
		acceptances []string
		followUp    bool
	}{
		{"completed_unaccepted", "completed", nil, true},
		{"completed_accepted", "completed", []string{"accepted"}, true},
		{"completed_unused", "completed", []string{"unused"}, true},
		{"completed_rejected", "completed", []string{"rejected"}, false},
		{"accepted_then_rejected", "completed", []string{"accepted", "rejected"}, false},
		{"cancelled", "cancelled", nil, false},
		{"blocked", "blocked", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "a.txt": "a0\n", "c.txt": "c0\n"}, false)
			record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
			bundleTestWrite(t, repo, "a.txt", "a1\n")
			record = bundleTestSubmitReview(t, record, "review1", test.result, nil)
			for _, status := range test.acceptances {
				record = bundleTestAccept(t, record, "review1", status, nil)
			}
			bundleTestWrite(t, repo, "c.txt", "c1\n")
			bundle := mustBuildBundle(t, record, "review2")
			kind, previous, since := "full", "", []string{"a.txt", "c.txt"}
			if test.followUp {
				kind, previous, since = "follow_up", "review1", []string{"c.txt"}
			}
			if bundle.Kind != kind || bundle.PreviousReview != previous || !reflect.DeepEqual(bundle.ChangedSinceReview, since) {
				t.Fatalf("kind %q previous %q since %v; want %q %q %v", bundle.Kind, bundle.PreviousReview, bundle.ChangedSinceReview, kind, previous, since)
			}
		})
	}
}

// B3: the most recent counted review wins; a later cancelled review does not
// replace it.
func TestBundlePreviousReviewIsMostRecentCountedReview(t *testing.T) {
	t.Parallel()
	repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "a.txt": "a0\n", "c.txt": "c0\n"}, false)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "a.txt", "a1\n")
	record = bundleTestSubmitReview(t, record, "review1", "completed", nil)
	record = bundleTestAccept(t, record, "review1", "accepted", nil)
	bundleTestWrite(t, repo, "a.txt", "a2\n")
	record = bundleTestSubmitReview(t, record, "review2", "completed", nil)
	record = bundleTestAccept(t, record, "review2", "accepted", nil)
	bundleTestWrite(t, repo, "c.txt", "c1\n")
	record = bundleTestSubmitReview(t, record, "review3", "cancelled", nil)
	bundle := mustBuildBundle(t, record, "review4")
	if bundle.Kind != "follow_up" || bundle.PreviousReview != "review2" || !reflect.DeepEqual(bundle.ChangedSinceReview, []string{"c.txt"}) {
		t.Fatalf("kind %q previous %q since %v; want follow_up review2 [c.txt]", bundle.Kind, bundle.PreviousReview, bundle.ChangedSinceReview)
	}
}

// B4, B5, B6, B9: reach hints (shebang and ranked, capped references from
// documentation and script files only) and the bundle's identity fields.
func TestBundleReachHintsAndIdentity(t *testing.T) {
	var docs strings.Builder
	for line := 1; line <= 15; line++ {
		fmt.Fprintf(&docs, "Line %d mentions install.sh\n", line)
	}
	long := strings.Repeat("x", 300) + " tool.sh"
	repo := bundleTestRepo(t, map[string]string{
		"source.txt":     "first line\nsecond line\n",
		".gitignore":     "ignored/\n",
		"install.sh":     "#!/bin/sh\necho old\n",
		"tool.sh":        "#!/usr/bin/env bash\necho old\n",
		"old.sh":         "#!/bin/bash\necho removed\n",
		"docs/a.md":      docs.String(),
		"zz/README.md":   "Title\n\ncurl -fsSL https://x/install.sh | sh\n",
		"main.go":        "package main\n\n// curl https://x/install.sh | sh\n// bash tool.sh\nfunc main() {}\n",
		"notes.json":     "{\"run\": \"./tool.sh\"}\n",
		"Makefile":       "all:\n\t./tool.sh --fast\n",
		"ci/build.yml":   "  steps:\n    - run: bash scripts/tool.sh   \n    - name: tool.sh docs\n",
		"docs/guide.md":  "Use tool.sh carefully\nwget https://example/x\n" + long + "\nsudo curl -o tool.sh https://h\nTOOL.SH uppercase\n",
		"scripts/helper": "cat tool.sh | sudo bash\n",
	}, true)
	bundleTestWrite(t, repo, "aa/untracked.txt", "See install.sh for setup\n")
	bundleTestWrite(t, repo, "ignored/notes.md", "./install.sh now\n")
	plan := helperPlan(t, "small", "pass")
	plan.Requirements = append(plan.Requirements, Requirement{ID: "docs", Description: "Documentation stays accurate", AllowNonExecution: true})
	record := bundleTestStart(t, repo, plan)
	bundleTestWrite(t, repo, "install.sh", "#!/bin/sh \t\n./install.sh --help\n")
	bundleTestWrite(t, repo, "tool.sh", "#!/usr/bin/env bash\n./tool.sh recursion\n")
	bundleTestRemove(t, repo, "old.sh")
	bundleTestWrite(t, repo, "plain.txt", "nothing to see\n")

	bundle := mustBuildBundle(t, record, "review")
	installRefs := []string{"zz/README.md:3: curl -fsSL https://x/install.sh | sh", "aa/untracked.txt:1: See install.sh for setup"}
	for line := 1; line <= 8; line++ {
		installRefs = append(installRefs, fmt.Sprintf("docs/a.md:%d: Line %d mentions install.sh", line, line))
	}
	toolRefs := []string{
		"Makefile:2: ./tool.sh --fast",
		"ci/build.yml:2: - run: bash scripts/tool.sh",
		"docs/guide.md:4: sudo curl -o tool.sh https://h",
		"scripts/helper:1: cat tool.sh | sudo bash",
		"ci/build.yml:3: - name: tool.sh docs",
		"docs/guide.md:1: Use tool.sh carefully",
		"docs/guide.md:3: " + strings.Repeat("x", 240),
	}
	want := []ReachRow{
		{Path: "install.sh", Hints: ReachHints{Shebang: "#!/bin/sh", References: installRefs}},
		{Path: "old.sh", Deleted: true, Hints: ReachHints{Shebang: "", References: []string{}}},
		{Path: "plain.txt", Hints: ReachHints{Shebang: "", References: []string{}}},
		{Path: "tool.sh", Hints: ReachHints{Shebang: "#!/usr/bin/env bash", References: toolRefs}},
	}
	for index := range max(len(want), len(bundle.Reach)) {
		if index >= len(want) || index >= len(bundle.Reach) || !reflect.DeepEqual(bundle.Reach[index], want[index]) {
			var got, expected any
			if index < len(bundle.Reach) {
				got = bundle.Reach[index]
			}
			if index < len(want) {
				expected = want[index]
			}
			t.Errorf("reach[%d]:\n got %+v\nwant %+v", index, got, expected)
		}
	}
	if bundle.Version != 2 || bundle.TaskID != record.ID || bundle.ReviewID != "review" {
		t.Fatalf("identity: version %d task %q review %q", bundle.Version, bundle.TaskID, bundle.ReviewID)
	}
	if record.Baseline.Head == "" || bundle.BaselineHead != record.Baseline.Head {
		t.Fatalf("baseline_head %q, baseline snapshot head %q", bundle.BaselineHead, record.Baseline.Head)
	}
	if current := currentWorkerSnapshot(t, record); bundle.Snapshot != current {
		t.Fatalf("snapshot %+v, current %+v", bundle.Snapshot, current)
	}
	requirements := []BundleRequirement{{ID: "behavior", Description: "Verified source behavior"}, {ID: "docs", Description: "Documentation stays accurate"}}
	if !reflect.DeepEqual(bundle.Requirements, requirements) {
		t.Fatalf("requirements %+v, want %+v", bundle.Requirements, requirements)
	}
	if len(bundle.Instructions) == 0 {
		t.Fatal("bundle has no instructions")
	}
}

// B7: prior findings follow record order and take the disposition of the
// latest acceptance that lists them, else pending.
func TestBundlePriorFindingsUseLatestDisposition(t *testing.T) {
	t.Parallel()
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	first := WorkerFinding{ID: "f1", Summary: "First concern about the source.", Severity: "major", Evidence: []SourceRef{ref}}
	second := WorkerFinding{ID: "f2", Summary: "Second concern about the source.", Severity: "major", Evidence: []SourceRef{ref}}
	record = bundleTestSubmitReview(t, record, "review1", "completed", []WorkerFinding{first, second})
	record = bundleTestAccept(t, record, "review1", "accepted", []FindingDisposition{
		{FindingID: "f1", Status: "accepted", Reason: "Acknowledged f1 at first.", Evidence: []SourceRef{}},
		{FindingID: "f2", Status: "accepted", Reason: "Acknowledged f2 at first.", Evidence: []SourceRef{}},
	})
	record = bundleTestAccept(t, record, "review1", "accepted", []FindingDisposition{
		{FindingID: "f1", Status: "rejected", Reason: "Source contradicts f1.", Evidence: []SourceRef{ref}},
		{FindingID: "f2", Status: "accepted", Reason: "Acknowledged f2 again.", Evidence: []SourceRef{}},
	})
	pending := WorkerFinding{ID: "g1", Summary: "Concern awaiting adjudication.", Severity: "major", Evidence: []SourceRef{ref}}
	record = bundleTestSubmitReview(t, record, "review2", "completed", []WorkerFinding{pending})

	bundle := mustBuildBundle(t, record, "review3")
	want := []BundleFinding{
		{AssignmentID: "review1", FindingID: "f1", Summary: first.Summary, Severity: "major", Evidence: []SourceRef{ref}, Disposition: "rejected", Reason: "Source contradicts f1."},
		{AssignmentID: "review1", FindingID: "f2", Summary: second.Summary, Severity: "major", Evidence: []SourceRef{ref}, Disposition: "accepted", Reason: "Acknowledged f2 again."},
		{AssignmentID: "review2", FindingID: "g1", Summary: pending.Summary, Severity: "major", Evidence: []SourceRef{ref}, Disposition: "pending", Reason: ""},
	}
	if !reflect.DeepEqual(bundle.PriorFindings, want) {
		t.Fatalf("prior_findings:\n got %+v\nwant %+v", bundle.PriorFindings, want)
	}
}

// B8: one check entry per plan check in plan order, describing its latest
// run, its exit code, log path and whether it ran at the current source.
func TestBundleChecksDescribeLatestRuns(t *testing.T) {
	pass := helperPlan(t, "small", "pass").Checks[0].Argv
	fail := append(slices.Clone(pass[:3]), "fail")
	plan := Plan{
		Title: "Source-bound example", Kind: "small",
		Requirements: []Requirement{{ID: "behavior", Description: "Verified source behavior", CheckIDs: []string{"zeta", "focused", "flaky"}}},
		Checks:       []Check{{ID: "zeta", Argv: pass, TimeoutSeconds: 10}, {ID: "focused", Argv: pass, TimeoutSeconds: 10}, {ID: "flaky", Argv: fail, TimeoutSeconds: 10}},
	}
	record, _ := fixtureTask(t, plan)
	flaky, err := RunCheck(t.Context(), record.Directory, "flaky")
	if err == nil || flaky.Status != "failed" {
		t.Fatalf("failing fixture check: %+v %v", flaky, err)
	}
	focused, err := RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatal(err)
	}
	zero, nine := 0, 9
	notRun := BundleCheck{CheckID: "zeta", RunID: "", Status: "not_run", ExitCode: nil, Current: false, Log: ""}
	entry := func(run CheckRun, code *int, current bool) BundleCheck {
		return BundleCheck{CheckID: run.CheckID, RunID: run.ID, Status: run.Status, ExitCode: code, Current: current, Log: filepath.Join(record.Directory, run.Log)}
	}
	assertChecks := func(label string, want []BundleCheck) {
		t.Helper()
		got := mustBuildBundle(t, record, "review").Checks
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s checks:\n got %s\nwant %s", label, bundleTestChecks(got), bundleTestChecks(want))
		}
	}
	assertChecks("at run source", []BundleCheck{notRun, entry(focused, &zero, true), entry(flaky, &nine, true)})
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "source.txt"), []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertChecks("after edit", []BundleCheck{notRun, entry(focused, &zero, false), entry(flaky, &nine, false)})
	rerun, err := RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatal(err)
	}
	if rerun.ID == focused.ID {
		t.Fatal("fixture precondition: rerun reused the run ID")
	}
	assertChecks("after rerun", []BundleCheck{notRun, entry(rerun, &zero, true), entry(flaky, &nine, false)})
}

func bundleTestChecks(checks []BundleCheck) string {
	parts := []string{}
	for _, check := range checks {
		code := "nil"
		if check.ExitCode != nil {
			code = fmt.Sprint(*check.ExitCode)
		}
		parts = append(parts, fmt.Sprintf("{%s run=%q status=%s exit=%s current=%v log=%q}", check.CheckID, check.RunID, check.Status, code, check.Current, check.Log))
	}
	return strings.Join(parts, " ")
}

// B10: BuildBundle refuses invalid and already used review IDs and writes
// nothing.
func TestBuildBundleRefusesInvalidOrUsedReviewIDs(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	record = mustAssignWorker(t, record, workerBrief("notes", "/root/evidence", "evidence", []string{"source.txt"}))
	record = mustAssignWorker(t, record, workerBrief("review", "/root/reviewer", "review", []string{"."}, "focused"))
	for _, id := range []string{"", "1review", "_review", "-review", "re view", "re/view", "review.1", "révue", strings.Repeat("a", 65)} {
		before := productionTaskBytes(t, record.Directory)
		if bundle, err := BuildBundle(t.Context(), record.Directory, id); err == nil {
			t.Errorf("review ID %q accepted: %+v", id, bundle.ReviewID)
		}
		if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
			t.Fatalf("refused review ID %q changed task files", id)
		}
	}
	for _, id := range []string{"review", "notes"} {
		before := productionTaskBytes(t, record.Directory)
		if _, err := BuildBundle(t.Context(), record.Directory, id); err == nil || !strings.Contains(err.Error(), "already") {
			t.Errorf("used ID %q: %v", id, err)
		}
		if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
			t.Fatalf("refused used ID %q changed task files", id)
		}
	}
	for _, id := range []string{"R", "Review_2-b", "a" + strings.Repeat("Z9_-", 15) + "abc"} {
		if bundle, err := BuildBundle(t.Context(), record.Directory, id); err != nil || bundle.ReviewID != id {
			t.Errorf("valid review ID %q (%d bytes) refused: %v", id, len(id), err)
		}
	}
}

// bundleTestAssignFixture returns a small task whose next review bundle has one reach
// row (tool.sh) and one prior finding, filled with valid facets.
func bundleTestAssignFixture(t *testing.T) (Record, Bundle) {
	t.Helper()
	repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "tool.sh": "#!/bin/sh\necho one\n"}, false)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	ref := bundleTestRef(t, record, "source.txt")
	finding := WorkerFinding{ID: "f1", Summary: "Quoting concern in the script.", Severity: "major", Evidence: []SourceRef{ref}}
	record = bundleTestSubmitReview(t, record, "first", "completed", []WorkerFinding{finding})
	record = bundleTestAccept(t, record, "first", "accepted", []FindingDisposition{{FindingID: "f1", Status: "accepted", Reason: "Acknowledged for follow-up.", Evidence: []SourceRef{}}})
	bundleTestWrite(t, repo, "tool.sh", "#!/bin/sh\necho two\n")
	bundle := mustBuildBundle(t, record, "review")
	if !reflect.DeepEqual(bundleTestReachPaths(bundle.Reach), []string{"tool.sh"}) || len(bundle.PriorFindings) != 1 || bundle.Reach[0].Hints.Shebang != "#!/bin/sh" {
		t.Fatalf("fixture precondition: reach %+v prior findings %+v", bundle.Reach, bundle.PriorFindings)
	}
	bundleTestFill(&bundle, "Called by the release workflow")
	return record, bundle
}

func bundleTestReviewBrief(id string, bundle *BundleRef) WorkerBrief {
	brief := workerBrief(id, "/root/second-reviewer", "review", []string{"."}, "focused")
	brief.Bundle = bundle
	return brief
}

// A1: a review brief without a bundle is refused, for small and substantial
// tasks alike.
func TestReviewAssignmentRequiresBundle(t *testing.T) {
	small, _ := bundleTestAssignFixture(t)
	bundleTestRefused(t, small, bundleTestReviewBrief("review", nil), "bundle")
	substantial, _ := preparedChangedTask(t)
	bundleTestRefused(t, substantial, bundleTestReviewBrief("review", nil), "bundle")
}

// A2: a non-review brief carrying a bundle is refused.
func TestNonReviewAssignmentRefusesBundle(t *testing.T) {
	record, _ := bundleTestAssignFixture(t)
	bundle := mustBuildBundle(t, record, "impl")
	bundleTestFill(&bundle, "Called by the release workflow")
	ref, _ := bundleTestWriteBundle(t, bundle)
	brief := workerBrief("impl", "/root/implementation", "implementation", []string{"tool.sh"}, "focused")
	brief.Bundle = ref
	bundleTestRefused(t, record, brief)
}

// A3: the bundle file must be an absolute, existing regular file (not a
// symlink) whose bytes match sha256 and decode as a strict bundle.
func TestReviewAssignmentRefusesInvalidBundleFile(t *testing.T) {
	t.Parallel()
	record, bundle := bundleTestAssignFixture(t)
	valid, data := bundleTestWriteBundle(t, bundle)
	dir := bundleTestDir(t)
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(valid.Path, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		t.Fatal(err)
	}
	members["unexpected"] = true
	unknown, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	for name, ref := range map[string]*BundleRef{
		"relative path":   {Path: "bundle.json", SHA256: valid.SHA256},
		"missing file":    {Path: filepath.Join(dir, "missing.json"), SHA256: valid.SHA256},
		"directory":       {Path: dir, SHA256: valid.SHA256},
		"symlink":         {Path: link, SHA256: valid.SHA256},
		"wrong sha256":    {Path: valid.Path, SHA256: strings.Repeat("0", 64)},
		"invalid JSON":    bundleTestWriteBytes(t, []byte("not a bundle\n")),
		"unknown member":  bundleTestWriteBytes(t, unknown),
		"empty file":      bundleTestWriteBytes(t, []byte{}),
		"truncated JSON":  bundleTestWriteBytes(t, data[:len(data)/2]),
		"sha256 of other": {Path: valid.Path, SHA256: bundleTestSum([]byte("other"))},
	} {
		t.Run(name, func(t *testing.T) {
			bundleTestRefused(t, record, bundleTestReviewBrief("review", ref))
		})
	}
}

// A4: the bundle's review_id must equal the brief's id.
func TestReviewAssignmentRefusesBundleForAnotherReview(t *testing.T) {
	record, bundle := bundleTestAssignFixture(t)
	ref, _ := bundleTestWriteBundle(t, bundle)
	bundleTestRefused(t, record, bundleTestReviewBrief("other", ref))
}

// A5: anything but the four facets must equal what BuildBundle generates at
// assign time.
func TestReviewAssignmentRefusesEditedOrStaleBundle(t *testing.T) {
	t.Parallel()
	edits := map[string]func(*Bundle){
		"finding reason": func(b *Bundle) { b.PriorFindings[0].Reason = "A different reason." },
		"finding status": func(b *Bundle) { b.PriorFindings[0].Disposition = "rejected" },
		"shebang hint":   func(b *Bundle) { b.Reach[0].Hints.Shebang = "#!/bin/bash" },
		"reference hint": func(b *Bundle) {
			b.Reach[0].Hints.References = append(b.Reach[0].Hints.References, "docs/x.md:1: tool.sh")
		},
		"added row": func(b *Bundle) {
			b.Reach = append(b.Reach, ReachRow{Path: "extra.txt", Hints: ReachHints{References: []string{}}, Callers: "x", InvocationModes: "x", Platforms: "x", Consumers: "x"})
		},
		"removed row":      func(b *Bundle) { b.Reach = b.Reach[:0] },
		"row deleted flag": func(b *Bundle) { b.Reach[0].Deleted = true },
		"changed file":     func(b *Bundle) { b.ChangedFiles[0].SHA256 = strings.Repeat("a", 64) },
		"dropped file":     func(b *Bundle) { b.ChangedFiles = b.ChangedFiles[:0] },
		"manifest digest":  func(b *Bundle) { b.ManifestSHA256 = strings.Repeat("b", 64) },
		"instructions":     func(b *Bundle) { b.Instructions = append(b.Instructions, "Skip the tests.") },
		"kind":             func(b *Bundle) { b.Kind, b.PreviousReview = "full", "" },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			record, bundle := bundleTestAssignFixture(t)
			edit(&bundle)
			ref, _ := bundleTestWriteBundle(t, bundle)
			bundleTestRefused(t, record, bundleTestReviewBrief("review", ref))
		})
	}
	t.Run("later source edit", func(t *testing.T) {
		record, bundle := bundleTestAssignFixture(t)
		ref, _ := bundleTestWriteBundle(t, bundle)
		bundleTestWrite(t, record.Baseline.Root, "tool.sh", "#!/bin/sh\necho three\n")
		bundleTestRefused(t, record, bundleTestReviewBrief("review", ref))
	})
	t.Run("later check run", func(t *testing.T) {
		record, bundle := bundleTestAssignFixture(t)
		ref, _ := bundleTestWriteBundle(t, bundle)
		if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
			t.Fatal(err)
		}
		record, err := Load(record.Directory)
		if err != nil {
			t.Fatal(err)
		}
		bundleTestRefused(t, record, bundleTestReviewBrief("review", ref))
	})
}

// A6: every facet must be nonblank, and n/a needs "n/a:" plus a reason; the
// refusal names the row path and the facet's JSON name.
func TestReviewAssignmentValidatesFacets(t *testing.T) {
	t.Parallel()
	record, bundle := bundleTestAssignFixture(t)
	facets := []struct {
		name string
		set  func(*ReachRow, string)
	}{
		{"callers", func(row *ReachRow, text string) { row.Callers = text }},
		{"invocation_modes", func(row *ReachRow, text string) { row.InvocationModes = text }},
		{"platforms", func(row *ReachRow, text string) { row.Platforms = text }},
		{"consumers", func(row *ReachRow, text string) { row.Consumers = text }},
	}
	for _, facet := range facets {
		for _, bad := range []string{"", "   ", "\t\n", "n/a", "N/A", "n/a:", "n/a:   ", "N/a: \t"} {
			t.Run(fmt.Sprintf("%s=%q", facet.name, bad), func(t *testing.T) {
				edited := bundle
				edited.Reach = slices.Clone(bundle.Reach)
				facet.set(&edited.Reach[0], bad)
				ref, _ := bundleTestWriteBundle(t, edited)
				bundleTestRefused(t, record, bundleTestReviewBrief("review", ref), "tool.sh", facet.name)
			})
		}
	}
	accepted := bundle
	accepted.Reach = slices.Clone(bundle.Reach)
	accepted.Reach[0].Callers = "n/a: deleted file"
	accepted.Reach[0].InvocationModes = "N/A: no callers"
	accepted.Reach[0].Platforms = "Called by install.sh main"
	accepted.Reach[0].Consumers = "n/a: no state or format consumers"
	ref, _ := bundleTestWriteBundle(t, accepted)
	if _, err := AssignWorker(t.Context(), record.Directory, bundleTestReviewBrief("review", ref)); err != nil {
		t.Fatalf("valid facets refused: %v", err)
	}
}

// A7: a valid filled bundle is accepted; the assignment keeps the brief's
// bundle reference and an immutable copy in the task directory.
func TestReviewAssignmentStoresFilledBundle(t *testing.T) {
	record, bundle := bundleTestAssignFixture(t)
	ref, data := bundleTestWriteBundle(t, bundle)
	want := *ref
	updated, err := AssignWorker(t.Context(), record.Directory, bundleTestReviewBrief("review", ref))
	if err != nil {
		t.Fatal(err)
	}
	assigned := findAssignment(updated, "review")
	if assigned == nil || assigned.Brief.Bundle == nil || *assigned.Brief.Bundle != want {
		t.Fatalf("assignment bundle %+v, want %+v", assigned, want)
	}
	if assigned.BundleFile == "" {
		t.Fatal("assignment has no stored bundle file")
	}
	stored, err := os.ReadFile(filepath.Join(updated.Directory, assigned.BundleFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(data) {
		t.Fatal("stored bundle bytes differ from the filled bundle file")
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if again := findAssignment(loaded, "review"); again == nil || again.BundleFile != assigned.BundleFile || again.Brief.Bundle == nil || *again.Brief.Bundle != want {
		t.Fatalf("loaded assignment %+v", again)
	}
}

// A7: a bundle with zero rows (nothing changed) is valid.
func TestReviewAssignmentAcceptsZeroRowBundle(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	bundle := mustBuildBundle(t, record, "review")
	if len(bundle.Reach) != 0 || len(bundle.ChangedFiles) != 0 || len(bundle.ChangedSinceReview) != 0 {
		t.Fatalf("fixture precondition: unchanged source produced rows %+v", bundle.Reach)
	}
	ref, data := bundleTestWriteBundle(t, bundle)
	updated, err := AssignWorker(t.Context(), record.Directory, bundleTestReviewBrief("review", ref))
	if err != nil {
		t.Fatalf("zero-row bundle refused: %v", err)
	}
	assigned := findAssignment(updated, "review")
	if assigned == nil || assigned.BundleFile == "" {
		t.Fatalf("assignment %+v", assigned)
	}
	if stored, err := os.ReadFile(filepath.Join(updated.Directory, assigned.BundleFile)); err != nil || string(stored) != string(data) {
		t.Fatalf("stored zero-row bundle differs: %v", err)
	}
	if _, err := Load(record.Directory); err != nil {
		t.Fatal(err)
	}
}

// Revision 2 (NEW requirements M3, B5a, B6a, B9a, O2).

// bundleTestEncodes asserts that the bundle encodes to valid UTF-8 JSON and
// returns the decoded round trip.
func bundleTestEncodes(t *testing.T, bundle Bundle) Bundle {
	t.Helper()
	data, err := EncodeBundle(bundle)
	if err != nil {
		t.Fatalf("EncodeBundle failed: %v", err)
	}
	if !utf8.Valid(data) {
		t.Fatal("encoded bundle is not valid UTF-8")
	}
	var decoded Bundle
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("encoded bundle does not decode: %v", err)
	}
	return decoded
}

// M3: a non-UTF-8 file name does not make Start fail, and BuildBundle lists
// the changed file with each invalid byte spelled as \xNN. Skipped where the
// filesystem refuses such names (macOS APFS).
func TestBundleListsNonUTF8PathWithEscapedSpelling(t *testing.T) {
	repo := bundleTestDir(t)
	name := "caf\xe9.txt"
	if err := os.WriteFile(filepath.Join(repo, name), []byte("v0\n"), 0o644); err != nil {
		t.Skipf("filesystem refuses a non-UTF-8 file name: %v", err)
	}
	bundleTestWrite(t, repo, "source.txt", "first line\nsecond line\n")
	bundleTestGit(t, repo, "init", "-q", "-b", "main")
	bundleTestGit(t, repo, "add", ".")
	bundleTestGit(t, repo, "commit", "-qm", "baseline")
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	if record.BaselineManifest == nil || record.BaselineManifest.Files != 2 {
		t.Fatalf("baseline manifest %+v, want 2 files", record.BaselineManifest)
	}
	if err := os.WriteFile(filepath.Join(repo, name), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := mustBuildBundle(t, record, "review")
	spelled := `caf\xe9.txt`
	want := []ChangedFile{{Path: spelled, Mode: "100644", SHA256: bundleTestSum([]byte("v1\n"))}}
	if !reflect.DeepEqual(bundle.ChangedFiles, want) {
		t.Fatalf("changed_files %+v, want %+v", bundle.ChangedFiles, want)
	}
	if !reflect.DeepEqual(bundle.ChangedSinceReview, []string{spelled}) || !reflect.DeepEqual(bundleTestReachPaths(bundle.Reach), []string{spelled}) {
		t.Fatalf("changed_since_review %q reach %q", bundle.ChangedSinceReview, bundleTestReachPaths(bundle.Reach))
	}
	bundleTestEncodes(t, bundle)
}

// B5a: a first line with invalid UTF-8 is returned with each maximal invalid
// run replaced by one U+FFFD, and the bundle still encodes.
func TestBundleShebangReplacesInvalidUTF8(t *testing.T) {
	repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "tool.sh": "#!/bin/sh\necho old\n"}, true)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "tool.sh", "#!/bin/sh caf\xe9\xe9 x\xff  \necho new\n")
	bundle := mustBuildBundle(t, record, "review")
	const shebang = "#!/bin/sh caf� x�"
	if len(bundle.Reach) != 1 || bundle.Reach[0].Path != "tool.sh" || bundle.Reach[0].Hints.Shebang != shebang {
		t.Fatalf("reach %+v, want tool.sh with shebang %q", bundle.Reach, shebang)
	}
	if decoded := bundleTestEncodes(t, bundle); decoded.Reach[0].Hints.Shebang != shebang {
		t.Fatalf("encoded shebang %q, want %q", decoded.Reach[0].Hints.Shebang, shebang)
	}
}

// B6a: reference lines with invalid UTF-8 appear with each maximal invalid run
// replaced by U+FFFD; the bundle encodes and, filled, is accepted.
func TestBundleReferencesReplaceInvalidUTF8(t *testing.T) {
	repo := bundleTestRepo(t, map[string]string{
		"source.txt":    "first line\nsecond line\n",
		"tool.sh":       "#!/bin/sh\necho old\n",
		"docs/notes.md": "caf\xe9 runs tool.sh\n./tool.sh \xff\xfe go\n",
	}, true)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "tool.sh", "#!/bin/sh\necho new\n")
	bundle := mustBuildBundle(t, record, "review")
	references := []string{"docs/notes.md:2: ./tool.sh � go", "docs/notes.md:1: caf� runs tool.sh"}
	if len(bundle.Reach) != 1 || bundle.Reach[0].Path != "tool.sh" || !reflect.DeepEqual(bundle.Reach[0].Hints.References, references) {
		t.Fatalf("reach %+v, want tool.sh with references %q", bundle.Reach, references)
	}
	bundleTestEncodes(t, bundle)
	bundleTestFill(&bundle, "n/a: documentation only")
	ref, _ := bundleTestWriteBundle(t, bundle)
	if _, err := AssignWorker(t.Context(), record.Directory, bundleTestReviewBrief("review", ref)); err != nil {
		t.Fatalf("bundle with replaced references refused: %v", err)
	}
}

// B9a: the instructions name `git diff <baseline_head>` only for a task that
// started clean at a commit; otherwise they say changed_files is the change set.
func TestBundleInstructionsDependOnBaselineCleanliness(t *testing.T) {
	contains := func(instructions []string, text string) bool {
		return slices.ContainsFunc(instructions, func(line string) bool { return strings.Contains(line, text) })
	}
	t.Run("clean git", func(t *testing.T) {
		repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "a.txt": "a0\n"}, true)
		record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
		if record.Baseline.Dirty || record.Baseline.Head == "" {
			t.Fatalf("fixture precondition: baseline %+v is not clean at a commit", record.Baseline)
		}
		bundleTestWrite(t, repo, "a.txt", "a1\n")
		bundle := mustBuildBundle(t, record, "review")
		if want := "git diff " + record.Baseline.Head; !contains(bundle.Instructions, want) {
			t.Fatalf("no instruction contains %q: %q", want, bundle.Instructions)
		}
	})
	for _, test := range []struct {
		name  string
		git   bool
		dirty bool
	}{{"dirty git", true, true}, {"not git", false, false}} {
		t.Run(test.name, func(t *testing.T) {
			repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", "a.txt": "a0\n"}, test.git)
			if test.dirty {
				bundleTestWrite(t, repo, "a.txt", "uncommitted at start\n")
			}
			record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
			if test.git && !record.Baseline.Dirty {
				t.Fatalf("fixture precondition: baseline %+v is not dirty", record.Baseline)
			}
			if !test.git && record.Baseline.Head != "" {
				t.Fatalf("fixture precondition: non-Git baseline has head %q", record.Baseline.Head)
			}
			bundleTestWrite(t, repo, "a.txt", "edited after start\n")
			bundle := mustBuildBundle(t, record, "review")
			if contains(bundle.Instructions, "Full diff since the task started") {
				t.Fatalf("instructions offer a full diff: %q", bundle.Instructions)
			}
			if !contains(bundle.Instructions, "changed_files is the change set") {
				t.Fatalf("instructions do not name changed_files as the change set: %q", bundle.Instructions)
			}
		})
	}
}

// O2: a review bundle stored inside the project root is refused with an error
// mentioning "outside"; the same bytes outside the root are accepted. The
// inside copy sits in an ignored directory so it does not change the source.
func TestReviewAssignmentRefusesBundleInsideProject(t *testing.T) {
	repo := bundleTestRepo(t, map[string]string{"source.txt": "first line\nsecond line\n", ".gitignore": "review/\n", "a.txt": "a0\n"}, true)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "a.txt", "a1\n")
	bundle := mustBuildBundle(t, record, "review")
	bundleTestFill(&bundle, "n/a: data file")
	outside, data := bundleTestWriteBundle(t, bundle)
	inside := filepath.Join(record.Baseline.Root, "review", "bundle.json")
	bundleTestWrite(t, record.Baseline.Root, "review/bundle.json", string(data))
	bundleTestRefused(t, record, bundleTestReviewBrief("review", &BundleRef{Path: inside, SHA256: outside.SHA256}), "outside")
	if _, err := AssignWorker(t.Context(), record.Directory, bundleTestReviewBrief("review", outside)); err != nil {
		t.Fatalf("the same bundle outside the project was refused: %v", err)
	}
}

// Revision 3 (REV3 requirements M4 and B6b).

// M4: look-alike names keep distinct spellings, and editing one lists only
// that one in changed_files. The backslash part runs on macOS; the raw-byte
// part is skipped where the filesystem refuses a non-UTF-8 name.
func TestBundleLookAlikeNamesChangeIndependently(t *testing.T) {
	for _, test := range []struct {
		name          string
		first, second string // names on disk
		firstSpelled  string
		secondSpelled string
	}{
		{"backslash escape look-alike", "a\\x41.txt", "aA.txt", `a\\x41.txt`, "aA.txt"},
		{"raw byte look-alike", "caf\xe9.txt", "caf\\xe9.txt", `caf\xe9.txt`, `caf\\xe9.txt`},
	} {
		for edited := range 2 {
			t.Run(fmt.Sprintf("%s/edit %d", test.name, edited+1), func(t *testing.T) {
				repo := bundleTestDir(t)
				if err := os.WriteFile(filepath.Join(repo, test.first), []byte("first v0\n"), 0o644); err != nil {
					t.Skipf("filesystem refuses file name %q: %v", test.first, err)
				}
				bundleTestWrite(t, repo, test.second, "second v0\n")
				bundleTestWrite(t, repo, "source.txt", "first line\nsecond line\n")
				bundleTestGit(t, repo, "init", "-q", "-b", "main")
				bundleTestGit(t, repo, "add", ".")
				bundleTestGit(t, repo, "commit", "-qm", "baseline")
				record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
				if record.BaselineManifest == nil || record.BaselineManifest.Files != 3 {
					t.Fatalf("baseline manifest %+v, want 3 distinct files", record.BaselineManifest)
				}
				name, spelled := test.first, test.firstSpelled
				if edited == 1 {
					name, spelled = test.second, test.secondSpelled
				}
				if err := os.WriteFile(filepath.Join(repo, name), []byte("edited\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				bundle := mustBuildBundle(t, record, "review")
				want := []ChangedFile{{Path: spelled, Mode: "100644", SHA256: bundleTestSum([]byte("edited\n"))}}
				if !reflect.DeepEqual(bundle.ChangedFiles, want) {
					t.Fatalf("changed_files %+v, want %+v", bundle.ChangedFiles, want)
				}
				if !reflect.DeepEqual(bundle.ChangedSinceReview, []string{spelled}) {
					t.Fatalf("changed_since_review %q, want [%q]", bundle.ChangedSinceReview, spelled)
				}
				bundleTestEncodes(t, bundle)
			})
		}
	}
}

// B6b: a doc whose name contains a newline and mentions the changed file
// does not break BuildBundle or AssignWorker; its line is listed with the
// name's literal newline, and other references are still listed. Skipped
// where the filesystem refuses a newline in a file name.
func TestBundleReferencesFromNewlineNamedDoc(t *testing.T) {
	repo := bundleTestDir(t)
	doc := "notes\nx.md"
	if err := os.WriteFile(filepath.Join(repo, doc), []byte("Run ./tool.sh now\n"), 0o644); err != nil {
		t.Skipf("filesystem refuses a newline in a file name: %v", err)
	}
	bundleTestWrite(t, repo, "source.txt", "first line\nsecond line\n")
	bundleTestWrite(t, repo, "tool.sh", "#!/bin/sh\necho old\n")
	bundleTestWrite(t, repo, "docs/a.md", "Use tool.sh here\n")
	bundleTestGit(t, repo, "init", "-q", "-b", "main")
	bundleTestGit(t, repo, "add", ".")
	bundleTestGit(t, repo, "commit", "-qm", "baseline")
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "tool.sh", "#!/bin/sh\necho new\n")

	bundle := mustBuildBundle(t, record, "review")
	references := []string{"notes\nx.md:1: Run ./tool.sh now", "docs/a.md:1: Use tool.sh here"}
	if len(bundle.Reach) != 1 || bundle.Reach[0].Path != "tool.sh" || !reflect.DeepEqual(bundle.Reach[0].Hints.References, references) {
		t.Fatalf("reach %+v, want tool.sh with references %q", bundle.Reach, references)
	}
	bundleTestEncodes(t, bundle)
	bundleTestFill(&bundle, "n/a: documentation only")
	ref, _ := bundleTestWriteBundle(t, bundle)
	if _, err := AssignWorker(t.Context(), record.Directory, bundleTestReviewBrief("review", ref)); err != nil {
		t.Fatalf("bundle with a newline-named reference refused: %v", err)
	}
}

// B5b (REV4): hints for a changed file whose name needs an M3 spelling come
// from the file on disk: the row path is spelled (two backslashes), the
// shebang is read from t\ool.sh, and references search for the on-disk base
// name (one backslash). Runs on macOS and Linux.
func TestBundleHintsForBackslashNameComeFromDisk(t *testing.T) {
	repo := bundleTestRepo(t, map[string]string{
		"source.txt": "first line\nsecond line\n",
		"t\\ool.sh":  "#!/bin/sh\necho old\n",
		"doc.md":     "Run ./t\\ool.sh here\nMentions tool.sh without the backslash\n",
	}, true)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "t\\ool.sh", "#!/bin/sh\necho new\n")

	bundle := mustBuildBundle(t, record, "review")
	spelled := `t\\ool.sh`
	want := []ReachRow{{Path: spelled, Hints: ReachHints{Shebang: "#!/bin/sh", References: []string{"doc.md:1: Run ./t\\ool.sh here"}}}}
	if !reflect.DeepEqual(bundle.Reach, want) {
		t.Fatalf("reach:\n got %+v\nwant %+v", bundle.Reach, want)
	}
	if got := bundleTestChangedPaths(bundle.ChangedFiles); !reflect.DeepEqual(got, []string{spelled}) {
		t.Fatalf("changed_files paths %q, want [%q]", got, spelled)
	}
	bundleTestEncodes(t, bundle)
}
