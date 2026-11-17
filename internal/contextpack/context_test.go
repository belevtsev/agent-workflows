package contextpack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--normalize-filter" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		fmt.Print("normalized\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fixture(t *testing.T) (*Engine, *kitsutest.Repo) {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Git("config", "core.excludesFile", r.Path(".git/empty-ignore"))
	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	e, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	return e, r
}

func collect(t *testing.T, e *Engine, operations ...Operation) Artifact {
	t.Helper()
	a, _, err := e.Collect(t.Context(), Request{1, operations}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCollectDeterministicRangesSearchAndPartialFailures(t *testing.T) {
	t.Parallel()
	e, r := fixture(t)
	r.Write("src/a.go", "first\nneedle [x]\nthird\nneedle z\n")
	r.Write("src/b.go", "needle z\nother\n")
	r.Write("bad.bin", "text\x00needle")
	a := collect(t, e,
		Operation{ID: "range", Kind: "read", Path: "src/a.go", Start: 2, End: 3},
		Operation{ID: "literal", Kind: "search", Pattern: "needle [x]"},
		Operation{ID: "regex", Kind: "search", Path: "src", Pattern: `^needle [z]`, Regex: true},
		Operation{ID: "missing", Kind: "read", Path: "missing"},
		Operation{ID: "git", Kind: "git"})
	if a.Items[0].Status != "ok" || len(a.Items[0].Lines) != 2 || a.Items[0].Lines[0].Number != 2 || a.Items[0].Lines[1].Number != 3 {
		t.Fatal(a.Items[0])
	}
	if len(a.Items[1].Lines) != 1 || a.Items[1].Lines[0].Text != "needle [x]\n" || a.Items[1].Status != "partial" || a.Items[1].Skipped != 1 {
		t.Fatal(a.Items[1])
	}
	if len(a.Items[2].Lines) != 2 || a.Items[2].Lines[0].Path != "src/a.go" || a.Items[2].Lines[1].Path != "src/b.go" {
		t.Fatal(a.Items[2])
	}
	if a.Items[3].Status != "error" || a.Items[4].Status != "ok" {
		t.Fatal(a.Items)
	}
	second, hit, err := e.Collect(t.Context(), Request{1, []Operation{
		{ID: "range", Kind: "read", Path: "src/a.go", Start: 2, End: 3},
		{ID: "literal", Kind: "search", Pattern: "needle [x]"},
		{ID: "regex", Kind: "search", Path: "src", Pattern: `^needle [z]`, Regex: true},
		{ID: "missing", Kind: "read", Path: "missing"},
		{ID: "git", Kind: "git"},
	}}, nil)
	if err != nil || !hit || a.ID != second.ID || !reflect.DeepEqual(a.Items, second.Items) {
		t.Fatalf("nondeterministic collection: %v %v", hit, err)
	}
	if r.Exists("kitsu.toml") || r.Exists(".git/kitsu") {
		t.Fatal("context installed or initialized gate")
	}
}

func TestContextRequestRejectsExecutableAndMalformedInput(t *testing.T) {
	for _, input := range []string{
		`{"version":2,"operations":[{"id":"a","kind":"git"}]}`,
		`{"version":1,"operations":[]}`,
		`{"version":1,"operations":[{"id":"a","kind":"exec","command":"touch bad"}]}`,
		`{"version":1,"operations":[{"id":"a","kind":"read","path":"../secret"}]}`,
		`{"version":1,"operations":[{"id":"a","kind":"read","path":"/secret"}]}`,
		`{"version":1,"operations":[{"id":"a","kind":"read","path":"a/../secret"}]}`,
		`{"version":1,"operations":[{"id":"a","kind":"read","path":"C:/secret"}]}`,
		`{"version":1,"operations":[{"id":"a","kind":"read","path":"a","start":4,"end":2}]}`,
		`{"version":1,"operations":[{"id":"a","kind":"search","pattern":"[","regex":true}]}`,
		`{"version":1,"operations":[{"id":"a","kind":"git"},{"id":"a","kind":"git"}]}`,
		`{"version":1,"operations":[{"id":"a","kind":"git"}]} {}`,
		strings.Repeat(" ", MaxFileBytes+1),
	} {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Errorf("accepted %s", input[:min(120, len(input))])
		}
	}
	operations := make([]Operation, 33)
	if _, _, err := (&Engine{}).Collect(t.Context(), Request{1, operations}, nil); err == nil {
		t.Fatal("accepted more than 32 operations")
	}
}

func TestCollectConfinesSourcesAndRespectsIgnoreRules(t *testing.T) {
	t.Parallel()
	e, r := fixture(t)
	r.Write("tracked.txt", "secret tracked\n")
	r.Git("add", "tracked.txt")
	r.Write(".gitignore", "ignored/\ntracked.txt\n")
	r.Write("ignored/secret.txt", "secret untracked\n")
	r.Write("safe.txt", "safe\n")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, r.Path("escape.txt")); err != nil {
		t.Log("symlinks unavailable:", err)
	}
	a := collect(t, e, Operation{ID: "tracked", Kind: "read", Path: "tracked.txt"}, Operation{ID: "ignored", Kind: "read", Path: "ignored/secret.txt"}, Operation{ID: "escape", Kind: "read", Path: "escape.txt"}, Operation{ID: "all", Kind: "search", Pattern: "secret"})
	for _, item := range a.Items[:3] {
		if item.Status != "error" || len(item.Lines) > 0 {
			t.Fatal(item)
		}
	}
	if len(a.Items[3].Lines) > 0 {
		t.Fatal("ignored or escaping contents exposed", a.Items[3])
	}
	if len(r.Git("diff", "--cached", "--name-only")) == 0 {
		t.Fatal("real index was changed")
	}
}

func TestBinaryInvalidUTF8AndOversizedSourcesAreExplicit(t *testing.T) {
	t.Parallel()
	e, r := fixture(t)
	r.Write("binary", "a\x00b")
	r.Write("invalid", string([]byte{0xff, 0x80}))
	r.Write("large", strings.Repeat("x", MaxFileBytes+1))
	r.Write("many", strings.Repeat("x\n", MaxFileBytes/2))
	a := collect(t, e, Operation{ID: "binary", Kind: "read", Path: "binary"}, Operation{ID: "invalid", Kind: "read", Path: "invalid"}, Operation{ID: "large", Kind: "read", Path: "large"}, Operation{ID: "many", Kind: "read", Path: "many"})
	for _, item := range a.Items {
		if item.Status != "error" || item.Error == "" {
			t.Fatal(item)
		}
	}
}

func TestCacheInvalidatesContentHEADAndSourcePolicy(t *testing.T) {
	for _, mutation := range []string{"content", "head", "ignore"} {
		t.Run(mutation, func(t *testing.T) {
			e, r := fixture(t)
			r.Write("source", "original\n")
			r.Git("add", "source")
			r.Git("commit", "-qm", "initial")
			op := Operation{ID: "source", Kind: "read", Path: "source"}
			a := collect(t, e, op)
			switch mutation {
			case "content":
				r.Write("source", "changed\n")
			case "head":
				r.Git("commit", "--allow-empty", "-qm", "new HEAD")
			case "ignore":
				r.Write(".git/info/exclude", "source\n")
			}
			if _, err := e.Load(t.Context(), a.ID); err == nil {
				t.Fatal("stale artifact accepted")
			}
			fresh, hit, err := e.Collect(t.Context(), Request{1, []Operation{op}}, nil)
			if err != nil || hit || fresh.ID == a.ID {
				t.Fatalf("cache failed invalidation: %t %v", hit, err)
			}
		})
	}
}

func TestCacheExpirationCorruptionAndMissingArtifacts(t *testing.T) {
	t.Parallel()
	e, _ := fixture(t)
	a := collect(t, e, Operation{ID: "git", Kind: "git"})
	a.Created = time.Now().Add(-Lifetime - time.Second)
	data, _ := json.Marshal(a)
	if err := os.WriteFile(filepath.Join(e.dir, a.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Load(t.Context(), a.ID); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatal(err)
	}
	for _, id := range []string{"../secret", strings.Repeat("a", 64)} {
		if _, err := e.Load(t.Context(), id); err == nil {
			t.Fatal("accepted bad/missing artifact")
		}
	}
	if err := os.WriteFile(filepath.Join(e.dir, a.ID+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Load(t.Context(), a.ID); err == nil {
		t.Fatal("accepted corrupt artifact")
	}
}

func TestCacheEvictsOldestAndKeepsPrivateModes(t *testing.T) {
	t.Parallel()
	e, _ := fixture(t)
	state, err := e.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for range 3 {
		a := Artifact{Kind: "test", State: state, Items: []Item{{ID: "large", Status: "ok", Lines: []Line{{Text: strings.Repeat("a", 24<<20)}}}}}
		if err := e.Save(t.Context(), &a); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	if _, err := e.Load(t.Context(), ids[0]); err == nil {
		t.Fatal("oldest artifact not evicted")
	}
	if _, err := e.Load(t.Context(), ids[2]); err != nil {
		t.Fatal(err)
	}
	var size int64
	files, err := os.ReadDir(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			t.Fatal(err)
		}
		size += info.Size()
		if f.Name() != "cache.lock" && info.Mode().Perm()&0o077 != 0 && os.PathSeparator != '\\' {
			t.Fatalf("public artifact mode: %v", info.Mode())
		}
	}
	if size > CacheBytes+4096 {
		t.Fatalf("cache exceeded bound: %d", size)
	}
}

func TestCacheRemovesOrphanArtifactTemporariesUnderWriterLock(t *testing.T) {
	t.Parallel()
	e, _ := fixture(t)
	orphan := filepath.Join(e.dir, ".artifact-killed-writer")
	f, err := os.Create(orphan)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(CacheBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	a := collect(t, e, Operation{ID: "git", Kind: "git"})
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan cache temporary survived", err)
	}
	if _, err := e.Load(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
}

func TestLinkedWorktreesDoNotShareContextArtifacts(t *testing.T) {
	t.Parallel()
	e, r := fixture(t)
	r.Write("source", "same\n")
	r.Git("add", "source")
	r.Git("commit", "-qm", "initial")
	a := collect(t, e, Operation{ID: "source", Kind: "read", Path: "source"})
	linked := filepath.Join(t.TempDir(), "linked")
	r.Git("worktree", "add", "--detach", linked, "HEAD")
	repo, err := gitx.Open(t.Context(), linked)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	if other.dir == e.dir {
		t.Fatal("worktrees share context store")
	}
	if _, err := other.Load(t.Context(), a.ID); err == nil {
		t.Fatal("read another worktree's artifact")
	}
}

func TestCollectionCancellationProducesNoArtifact(t *testing.T) {
	t.Parallel()
	e, _ := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := e.Collect(ctx, Request{1, []Operation{{ID: "git", Kind: "git"}}}, nil); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestRawContentInvalidatesDespiteGitLineEndingNormalization(t *testing.T) {
	t.Parallel()
	e, r := fixture(t)
	r.Write(".gitattributes", "source text eol=lf\n")
	r.Write("source", "first\r\nsecond\r\n")
	a := collect(t, e, Operation{ID: "source", Kind: "read", Path: "source"})
	r.Write("source", "first\nsecond\n")
	state, err := e.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.Tree != a.State.Tree || state.Raw == a.State.Raw {
		t.Fatal("fixture did not preserve normalized tree and change raw bytes")
	}
	if _, err := e.Load(t.Context(), a.ID); err == nil {
		t.Fatal("raw byte change accepted as cached source")
	}
	fresh, hit, err := e.Collect(t.Context(), Request{1, []Operation{{ID: "source", Kind: "read", Path: "source"}}}, nil)
	if err != nil || hit || fresh.Items[0].Lines[0].Text != "first\n" {
		t.Fatal("normalized tree returned stale raw bytes", hit, err)
	}
}

func TestSaveRejectsChangedSourceAfterPreparation(t *testing.T) {
	t.Parallel()
	e, r := fixture(t)
	r.Write("source", "before\n")
	state, err := e.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r.Write("source", "after\n")
	a := Artifact{Kind: "brief", State: state, Items: []Item{}}
	if err := e.Save(t.Context(), &a); err == nil {
		t.Fatal("stale baseline artifact published")
	}
	if _, err := os.Stat(filepath.Join(e.dir, a.ID+".json")); !os.IsNotExist(err) {
		t.Fatal("stale artifact exists", err)
	}
}

func TestRawDigestFramesBinaryContentWithoutBoundaryAmbiguity(t *testing.T) {
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	e, r := fixture(t)
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	filter := "'" + strings.ReplaceAll(filepath.ToSlash(program), "'", "'\\''") + "' --normalize-filter"
	r.Git("config", "filter.stable.clean", filter)
	r.Write(".gitattributes", "a filter=stable\nb filter=stable\n")
	r.Write("a", "A")
	r.Write("b", "B\x00b\x00C")
	a := collect(t, e, Operation{ID: "source", Kind: "read", Path: "a"})
	if a.Items[0].Status != "ok" || a.Items[0].Lines[0].Text != "A" {
		t.Fatal(a.Items)
	}
	// Both states have identical name/NUL/content/NUL concatenations and normalized Git trees.
	r.Write("a", "A\x00b\x00B")
	r.Write("b", "C")
	state, err := e.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.Tree != a.State.Tree {
		t.Fatal("clean filter did not preserve tree")
	}
	if state.Raw == a.State.Raw {
		t.Fatal("raw digest has an ambiguous file boundary")
	}
	if _, err := e.Load(t.Context(), a.ID); err == nil {
		t.Fatal("binary mutation returned prior text")
	}
	fresh := collect(t, e, Operation{ID: "source", Kind: "read", Path: "a"})
	if fresh.Items[0].Status != "error" {
		t.Fatal("binary source was returned", fresh.Items)
	}
}

func TestBudgetPaginationAndUTF8PreserveEveryLine(t *testing.T) {
	a := Artifact{Version: 1, ID: strings.Repeat("a", 64), Kind: "collection", Metadata: json.RawMessage(`{"required":"limits and uncertainty"}`), Items: []Item{{ID: "source", Status: "ok", Lines: Lines("src", 1, strings.Repeat("Ж🐕", 2000))}, {ID: "error", Status: "error", Error: "unreadable"}}}
	data, err := Render(a, "", "", 2048, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 2048 || !utf8.Valid(data) {
		t.Fatal("invalid budget/UTF8")
	}
	var first Page
	if err := json.Unmarshal(data, &first); err != nil {
		t.Fatal(err)
	}
	if first.Items[1].Error != "unreadable" || first.Items[0].Omitted == 0 || string(first.Metadata) != string(a.Metadata) {
		t.Fatal(first)
	}
	var actual []Line
	item, cursor := "source", ""
	for calls := 0; calls < 100; calls++ {
		data, err := Render(a, item, cursor, 2048, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 2048 {
			t.Fatal("page over budget")
		}
		var page Page
		if err := json.Unmarshal(data, &page); err != nil {
			t.Fatal(err)
		}
		for _, line := range page.Items[0].Lines {
			if line.Path == "" {
				line.Path = page.Items[0].Path
			}
			actual = append(actual, line)
		}
		cursor = page.Items[0].Next
		if cursor == "" {
			break
		}
	}
	if cursor != "" || !reflect.DeepEqual(actual, a.Items[0].Lines) {
		t.Fatal("pagination lost or repeated evidence")
	}
	if _, err := Render(a, "source", nextCursor(strings.Repeat("b", 64), "source", 1), 2048, false); err == nil {
		t.Fatal("accepted foreign cursor")
	}
	a.Metadata, _ = json.Marshal(map[string]string{"required": strings.Repeat("limits", 200)})
	if _, err := Render(a, "", "", 512, false); err == nil {
		t.Fatal("silently dropped required outcomes to fit budget")
	}
}

func TestCheckPreviewsRetainTailAndExpandFromOriginalLog(t *testing.T) {
	lines := []Line{}
	for i := 1; i <= 100; i++ {
		lines = append(lines, Line{Number: i, Text: "noise\n"})
	}
	lines[99].Text = "IMPORTANT failure\n"
	a := Artifact{Version: 1, ID: strings.Repeat("f", 64), Kind: "checks", Items: []Item{{ID: "failed", Status: "failed", Lines: lines}, {ID: "passed", Status: "passed", Lines: lines}}}
	data, err := Render(a, "", "", 4096, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "IMPORTANT failure") {
		t.Fatal("tail diagnostic missing")
	}
	var page Page
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if !page.Items[0].Preview || len(page.Items[1].Lines) != 0 || page.Items[0].Omitted != 96 {
		t.Fatal(page.Items)
	}
	full, err := Render(a, "failed", page.Items[0].Next, 1<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(full, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items[0].Lines) != 100 || page.Items[0].Lines[0].Number != 1 {
		t.Fatal("expansion did not recover original log")
	}
	if got := Lines("log", 1, strings.Repeat(string([]byte{0x80}), 2048)); len(got) == 0 || !utf8.ValidString(got[0].Text) {
		t.Fatal("invalid tool bytes did not terminate safely")
	}
}
