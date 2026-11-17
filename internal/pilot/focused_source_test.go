package pilot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Capture new production packages as well as tracked edits. A tracked-only diff
// cannot reproduce a local build that imports untracked Go sources.
func freezeFocusedSource(ctx context.Context, repo, root string) (string, error) {
	index, err := canarySourceIndex(ctx, repo)
	if err != nil {
		return "", err
	}
	source := filepath.Join(root, "frozen-source")
	if err := os.Mkdir(source, 0o700); err != nil {
		return "", err
	}
	for rel, digest := range index {
		path := filepath.Join(repo, rel)
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		out := filepath.Join(source, rel)
		if err := copyRegular(path, out, 0o400|info.Mode().Perm()&0o111); err != nil {
			return "", err
		}
		if got, err := hashFile(out); err != nil || got != digest {
			return "", fmt.Errorf("source changed during capture: %s", rel)
		}
	}
	after, err := canarySourceIndex(ctx, repo)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(index, after) {
		return "", fmt.Errorf("source changed during capture; retain evidence and do not launch")
	}
	if err := writeJSON(filepath.Join(root, "source-index.json"), index, true); err != nil {
		return "", err
	}
	return source, nil
}

func TestFocusedSourceFreezeRetainsUntrackedPackagesAndOriginalBytes(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	path := filepath.Join(repo, "new-package", "source.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("package fresh\n// Preserve λ and NOT with complete source.\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source, err := freezeFocusedSource(t.Context(), repo, root)
	if err != nil {
		t.Fatal(err)
	}
	var index map[string]string
	if err := readJSON(filepath.Join(root, "source-index.json"), &index); err != nil || len(index) != 1 {
		t.Fatal(index, err)
	}
	if err := os.WriteFile(path, []byte("subsequent edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	frozen := filepath.Join(source, "new-package", "source.go")
	data, err := os.ReadFile(frozen)
	if err != nil || string(data) != string(original) {
		t.Fatal("retained source changed with working copy", err)
	}
	if _, err := freezeFocusedSource(t.Context(), repo, root); err == nil {
		t.Fatal("existing frozen source was replaced")
	}
}
