package evidence_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Requirement "Snapshot", scenario "An untracked file is part of the snapshot".
func TestUntrackedFileIsPartOfTheSnapshot(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	before := status(t, repo).Tree

	repo.Write("src/new.txt", "new\n")
	after := status(t, repo).Tree

	if after == "" || after == before {
		t.Fatalf("tree with src/new.txt = %q, tree before = %q; want a different tree", after, before)
	}
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "add src/new.txt")
	if committed := repo.Git("rev-parse", "HEAD^{tree}"); after != committed {
		t.Fatalf("tree with src/new.txt = %q, tree of the commit = %q", after, committed)
	}
}

// Requirement "Snapshot", scenario "An ignored file is not".
func TestIgnoredFileIsNotPartOfTheSnapshot(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	before := status(t, repo).Tree

	repo.Write("build/out.bin", "binary\n")
	after := status(t, repo).Tree

	if before == "" || after != before {
		t.Fatalf("tree with build/out.bin = %q, tree before = %q; want the same tree", after, before)
	}
}

// Requirement "Snapshot", scenario "The index is left alone".
func TestRunLeavesTheIndexAlone(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	repo.Write("src/app.txt", "edited\n")
	repo.Write("src/new.txt", "new\n")
	before := repo.Git("status", "--porcelain")

	result := repo.Gatecub("run")

	if after := repo.Git("status", "--porcelain"); after != before {
		t.Fatalf("git status --porcelain before gatecub run:\n%s\nafter (gatecub run exit status %d):\n%s", before, result.Code, after)
	}
}

// Requirement "Snapshot", scenario "A large repository has a snapshot".
func TestLargeRepositoryHasASnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("creates 30,001 files and a 68 MiB file")
	}
	t.Parallel()
	repo := gatecubtest.Demo(t)
	many := repo.Path("many")
	for i := range 30001 {
		dir := filepath.Join(many, fmt.Sprintf("d%03d", i/1000))
		if i%1000 == 0 {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d.txt", i)), []byte(fmt.Sprintf("%d\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	big := make([]byte, 68<<20)
	for i := range big {
		big[i] = byte(i * 7)
	}
	if err := os.WriteFile(repo.Path("big.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}

	result := repo.Gatecub("status", "--json")

	if strings.Contains(strings.ToLower(result.Output()), "too large") {
		t.Fatalf("gatecub status --json reports that the source is too large (exit status %d):\n%s", result.Code, result.Output())
	}
	if out := decodeStatus(t, result); out.Tree == "" {
		t.Fatalf("gatecub status --json printed no tree:\n%s", result.Output())
	}
}
