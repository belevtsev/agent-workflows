package verification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

func TestRawCleanFilterChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "raw-filter-child" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "stable normalized bytes")
	os.Exit(0)
}

func TestPlanSchedulesOriginalBytesHiddenByCleanFilter(t *testing.T) {
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir, repo := plannerRepo(t, plannerDeclaration, true)
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	filter := "'" + strings.ReplaceAll(filepath.ToSlash(program), "'", "'\\''") + "' -test.run=^TestRawCleanFilterChild$ -- raw-filter-child"
	plannerGit(t, dir, "config", "filter.raw.clean", filter)
	plannerWrite(t, dir, ".gitattributes", "src/old filter=raw\n")
	plannerGit(t, dir, "add", "-A")
	plannerGit(t, dir, "commit", "-qm", "pin normalized baseline")
	plannerWrite(t, dir, "src/old", "changed original application bytes")
	plan := plannerBuild(t, repo, "HEAD")
	changes, err := repo.Diff(t.Context(), plan.BaseTree, plan.SourceTree)
	if err != nil || len(changes) != 0 {
		t.Fatalf("filter did not hide Git diff: %+v, %v", changes, err)
	}
	if !slices.Equal(plan.RequiredIDs(), []string{"behavior"}) || !slices.Equal(plan.Checks[1].MatchedPaths, []string{"src/old"}) || !strings.Contains(plan.Checks[1].Reason, "original source-byte differences") {
		t.Fatalf("original bytes did not trigger verification: %+v", plan)
	}
	// Both sources participate exactly once when a visible change accompanies
	// a normalized source edit; declarations and evidence dependencies survive.
	plannerWrite(t, dir, "src/visible", "visible change")
	mixed := plannerBuild(t, repo, "HEAD")
	if !slices.Equal(mixed.Checks[1].MatchedPaths, []string{"src/old", "src/visible"}) || len(mixed.AllIDs()) != 3 {
		t.Fatalf("mixed normalized/visible paths lost or duplicated: %+v", mixed)
	}
}

func TestPlanSchedulesCRLFNormalizedOriginalBytes(t *testing.T) {
	t.Parallel()
	dir, repo := plannerRepo(t, plannerDeclaration, true)
	plannerWrite(t, dir, ".gitattributes", "src/old text eol=lf\n")
	plannerWrite(t, dir, "src/old", "original\r\n")
	plannerGit(t, dir, "add", "-A")
	plannerGit(t, dir, "commit", "-qm", "normalized CRLF baseline")
	plan := plannerBuild(t, repo, "HEAD")
	changes, err := repo.Diff(t.Context(), plan.BaseTree, plan.SourceTree)
	if err != nil || len(changes) != 0 || !slices.Equal(plan.RequiredIDs(), []string{"behavior"}) || !strings.Contains(plan.Checks[1].Reason, "source-byte differences") {
		t.Fatalf("CRLF original bytes invisible: %+v changes=%+v err=%v", plan, changes, err)
	}
}

func TestRawDifferencesSHA256Objects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plannerGit(t, dir, "init", "-q", "--object-format=sha256")
	plannerWrite(t, dir, "kitsu.toml", plannerDeclaration)
	plannerWrite(t, dir, "src/source", "stable original bytes")
	plannerGit(t, dir, "add", "-A")
	plannerGit(t, dir, "commit", "-qm", "sha256 baseline")
	repo, err := gitx.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	plan := plannerBuild(t, repo, "HEAD")
	if len(plan.SourceTree) != 64 || len(plan.PriorityIDs()) != 0 {
		t.Fatalf("SHA256 blob hashing differs: %+v", plan)
	}
}

type rawMutatingContext struct {
	context.Context
	once   sync.Once
	mutate func()
}

func (c *rawMutatingContext) Err() error { c.once.Do(c.mutate); return c.Context.Err() }

func TestRawSourceReadFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"changed while hashing", func(t *testing.T, dir string) { plannerWrite(t, dir, "source", "different larger content") }},
		{"replaced while hashing", func(t *testing.T, dir string) {
			plannerWrite(t, dir, "replacement", "same")
			if err := os.Rename(filepath.Join(dir, "source"), filepath.Join(dir, "old")); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(dir, "replacement"), filepath.Join(dir, "source")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			plannerWrite(t, dir, "source", "same")
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			ctx := &rawMutatingContext{Context: t.Context(), mutate: func() { tc.mutate(t, dir) }}
			if _, err := rawBlobDiffers(ctx, root, "source", strings.Repeat("0", 40), make([]byte, 1024)); err == nil || !strings.Contains(err.Error(), "changed while hashing") {
				t.Fatalf("source drift accepted: %v", err)
			}
		})
	}
	t.Run("missing source", func(t *testing.T) {
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = root.Close() }()
		if _, err := rawBlobDiffers(t.Context(), root, "missing", strings.Repeat("0", 40), make([]byte, 1024)); err == nil {
			t.Fatal("missing source treated as empty")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		dir := t.TempDir()
		plannerWrite(t, dir, "source", "source")
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = root.Close() }()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := rawBlobDiffers(ctx, root, "source", strings.Repeat("0", 40), make([]byte, 1024)); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel ignored: %v", err)
		}
	})
}
