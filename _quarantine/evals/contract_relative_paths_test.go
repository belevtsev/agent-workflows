package evals

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// contractRelativePath matches a backquoted path that starts with "../": in
// a prompt, a file the brief names relative to the worker's contract directory.
var contractRelativePath = regexp.MustCompile("`(\\.\\./[^`\\s]+)`")

// missingContractRelativePaths returns each path the prompt names relative to
// the contract directory that is no file there.
func missingContractRelativePaths(prompt, contractDir string) (named int, missing []string) {
	for _, m := range contractRelativePath.FindAllStringSubmatch(prompt, -1) {
		named++
		if !fileExists(filepath.Join(contractDir, filepath.FromSlash(m[1]))) {
			missing = append(missing, m[1])
		}
	}
	return named, missing
}

// oracle: a prompt that sends a worker to a file relative to its contract's
// directory names a file that exists there. The test-engineer prompts name the
// test-strategy references this way, since the contract no longer links them;
// a renamed reference fails here instead of in a paid run.
func TestPromptPathsRelativeToTheContractExist(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	contractDir := filepath.Join(root, "skills", "task-orchestration", "references", "contracts")
	cases, err := findCases(filepath.Join(root, "evals"))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, dir := range cases {
		data, err := os.ReadFile(filepath.Join(dir, "prompt.md"))
		if err != nil {
			continue // a case without a prompt.md names no file
		}
		rel, _ := filepath.Rel(root, dir)
		named, missing := missingContractRelativePaths(string(data), contractDir)
		total += named
		for _, p := range missing {
			t.Errorf("%s/prompt.md names %s relative to the contract directory, where no such file exists",
				filepath.ToSlash(rel), p)
		}
	}
	if total == 0 {
		t.Fatal("no prompt names a path relative to the contract directory: the test checks nothing")
	}
}

// oracle: a path that exists under the contract directory is not reported; one
// that does not is reported by the text the prompt used.
func TestMissingContractRelativePathsAreReported(t *testing.T) {
	dir := t.TempDir()
	contractDir := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(contractDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "kept.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt := "Apply `../kept.md` and `../gone.md`; `testdata/x.md` is not relative to the contract."
	named, missing := missingContractRelativePaths(prompt, contractDir)
	if named != 2 {
		t.Errorf("named = %d, want 2", named)
	}
	if len(missing) != 1 || missing[0] != "../gone.md" {
		t.Errorf("missing = %v, want [../gone.md]", missing)
	}
}
