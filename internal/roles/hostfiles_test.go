package roles

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "write the host files from the role texts")

// root is the root of the repository, two directories above this package.
func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// The files a host needs are made from the one text of each role. This test
// fails when one of them differs from what its text gives; run it with
// -update to write them again.
func TestHostFilesAreMadeFromTheTexts(t *testing.T) {
	files, err := HostFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		full := filepath.Join(root(t), filepath.FromSlash(path))
		if *update {
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(full)
		switch {
		case err != nil:
			t.Errorf("%s is missing: %v; run `go test ./internal/roles -update`", path, err)
		case strings.ReplaceAll(string(got), "\r\n", "\n") != want:
			t.Errorf("%s differs from what its text in internal/roles/text gives; edit the text and run `go test ./internal/roles -update`", path)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root(t), TeamRoot, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if _, made := files[TeamRoot+"/agents/"+entry.Name()]; !made {
			t.Errorf("%s/agents/%s belongs to no role", TeamRoot, entry.Name())
		}
	}
}

// Each text has a budget, so that a role's instructions stay readable and
// cheap to load.
func TestTextsStayWithinTheirBudgets(t *testing.T) {
	total := TextSize(Coordinator)
	if total > MaxCoordinatorText {
		t.Errorf("the coordinator's instructions have %d characters; the limit is %d", total, MaxCoordinatorText)
	}
	for _, id := range IDs() {
		size := TextSize(id)
		if size == 0 {
			t.Errorf("the role %s has no text", id)
		}
		if size > MaxRoleText {
			t.Errorf("the text of %s has %d characters; the limit is %d", id, size, MaxRoleText)
		}
		total += size
	}
	if total > MaxAllText {
		t.Errorf("the texts have %d characters together; the limit is %d", total, MaxAllText)
	}
}

// The workflow is for any language, so no text names the tool of one.
func TestTextsNameNoLanguageOrBuildTool(t *testing.T) {
	words := []string{"go test", "npm", "pytest", "cargo", "gradle", "maven", "golang", "python", "javascript", "typescript", "rust", "java "}
	for _, id := range append(IDs(), Coordinator) {
		text, err := Text(id)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(text)
		for _, word := range words {
			if strings.Contains(lower, word) {
				t.Errorf("the text of %s names %q", id, strings.TrimSpace(word))
			}
		}
	}
}
