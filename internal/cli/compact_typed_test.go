package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestTypedContextReadsAndSearchesNativeOperands(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	path := `docs/source "quoted" file.txt`
	if os.PathSeparator == '\\' {
		path = "docs/source with spaces.txt"
	}
	text := "alpha\nfmt.Println(\"a.b\")\nfmtXPrintln(\"axb\")\nliteral \"quote\" \\ slash [x] $HOME; $(exit 9)\n--help -flag\n-\nomega\n"
	r.Write(path, text)
	r.Write("-", "literal dash file\n")
	for _, tt := range []struct {
		name string
		args []string
		id   string
		want []int
	}{
		{"whole file", []string{"read", path}, "source", []int{1, 2, 3, 4, 5, 6, 7}},
		{"range after operand", []string{"read", path, "--start", "2", "--end", "3", "--json"}, "source", []int{2, 3}},
		{"open range", []string{"read", "--start=6", path}, "source", []int{6, 7}},
		{"end only", []string{"read", path, "--end=2"}, "source", []int{1, 2}},
		{"literal punctuation", []string{"search", "fmt.Println", path}, "matches", []int{2}},
		{"default repository scope", []string{"search", "fmt.Println"}, "matches", []int{2}},
		{"explicit repository scope", []string{"search", "fmt.Println", "."}, "matches", []int{2}},
		{"literal invalid regexp", []string{"search", "[", path}, "matches", []int{4}},
		{"regexp", []string{"search", "fmt.Println", path, "--regex"}, "matches", []int{2, 3}},
		{"escaped regexp", []string{"search", `fmt\.Println`, path, "--regex"}, "matches", []int{2}},
		{"shell syntax is literal", []string{"search", `"quote" \ slash [x] $HOME; $(exit 9)`, path}, "matches", []int{4}},
		{"separator", []string{"search", "--", "-flag", path}, "matches", []int{5}},
		{"help operand", []string{"search", "--", "--help", path}, "matches", []int{5}},
		{"literal dash pattern", []string{"search", "-", path}, "matches", []int{5, 6}},
		{"literal dash path", []string{"read", "-"}, "source", []int{1}},
		{"no matches", []string{"search", "absent", path}, "matches", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page := compactResult(t, r.Kitsu(append([]string{"context"}, tt.args...)...), 0)
			if page.Version != 1 || page.Kind != "collection" || len(page.Items) != 1 {
				t.Fatal(page)
			}
			item := page.Items[0]
			if item.ID != tt.id || item.Status != "ok" || item.Omitted != 0 || item.Next != "" {
				t.Fatal(item)
			}
			var got []int
			for _, line := range item.Lines {
				got = append(got, line.Number)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("line numbers %v, want %v: %+v", got, tt.want, item)
			}
		})
	}
}

func TestTypedContextRejectsInvalidFlagsBeforeOpeningCache(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"missing path", []string{"read"}},
		{"extra path", []string{"read", "a", "b"}},
		{"missing pattern", []string{"search"}},
		{"extra search operand", []string{"search", "a", "b", "c"}},
		{"negative start", []string{"read", "a", "--start=-1"}},
		{"negative end", []string{"read", "a", "--end=-1"}},
		{"reversed range", []string{"read", "a", "--start=3", "--end=2"}},
		{"noninteger line", []string{"read", "a", "--start=first"}},
		{"absolute path", []string{"read", r.Path("a")}},
		{"traversal", []string{"read", "../a"}},
		{"unclean path", []string{"search", "a", "docs/../a"}},
		{"backslash path", []string{"read", `docs\a`}},
		{"empty path", []string{"read", ""}},
		{"empty pattern", []string{"search", ""}},
		{"oversized pattern", []string{"search", strings.Repeat("x", 4097)}},
		{"invalid regexp", []string{"search", "[", "--regex"}},
		{"read regexp flag", []string{"read", "a", "--regex"}},
		{"search range flag", []string{"search", "a", "--start=2"}},
		{"schema op flag", []string{"read", "a", "--op=read"}},
		{"schema type flag", []string{"search", "a", "--type=search"}},
		{"schema query flag", []string{"search", "a", "--query=a"}},
		{"request flag", []string{"read", "a", "--request=-"}},
		{"invalid task", []string{"read", "a", "--task=../task"}},
		{"small budget", []string{"read", "a", "--max-bytes=511"}},
		{"large budget", []string{"search", "a", "--max-bytes=1048577"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Kitsu(append([]string{"context"}, tt.args...)...)
			if result.Code != ExitUsage || result.Stdout != "" || !strings.Contains(result.Stderr, "usage: kitsu context "+tt.args[0]) {
				t.Fatalf("invalid flags %q: %s (exit %d)", tt.args, result.Output(), result.Code)
			}
			if r.Exists(".git/kitsu") {
				t.Fatal("invalid native request opened the private store")
			}
		})
	}
}

func TestTypedContextSharesCollectionCacheAndTaskMetadata(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	r.Write("source.txt", "one\ntwo\nthree\n")
	r.Write("openspec/changes/fix-login/proposal.md", "# Fix login\n")
	typed := compactResult(t, r.Kitsu("context", "read", "source.txt", "--start=2", "--end=2", "--task=fix-login"), 0)
	request := contextpack.Request{Version: 1, Operations: []contextpack.Operation{{ID: "source", Kind: "read", Path: "source.txt", Start: 2, End: 2}}}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	collected := compactResult(t, r.Run(string(data), filepath.Join(kitsutest.Binary(t), compactBinaryName()), "context", "collect", "--request=-", "--task=fix-login"), 0)
	if typed.ID != collected.ID || !collected.CacheHit || !reflect.DeepEqual(typed.Items, collected.Items) || string(typed.Metadata) != string(collected.Metadata) {
		t.Fatalf("native flags changed collection semantics: typed=%+v collected=%+v", typed, collected)
	}
	var metadata uxTaskView
	if err := json.Unmarshal(typed.Metadata, &metadata); err != nil || metadata.Task != "fix-login" || metadata.Workflow != "full" {
		t.Fatalf("missing task metadata: %s (%v)", typed.Metadata, err)
	}
}

func TestTypedContextPreservesBudgetsCursorsAndFreshness(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	var source strings.Builder
	for i := range 80 {
		fmt.Fprintf(&source, "needle %03d %s\n", i, strings.Repeat("x", 80))
	}
	r.Write("source.txt", source.String())
	for _, command := range [][]string{{"read", "source.txt"}, {"search", "needle", "source.txt"}} {
		t.Run(command[0], func(t *testing.T) {
			result := r.Kitsu(append(append([]string{"context"}, command...), "--max-bytes=2048")...)
			page := compactResult(t, result, 0)
			if len(result.Stdout) > 2048 || len(page.Items) != 1 || page.Items[0].Omitted == 0 || page.Items[0].Next == "" {
				t.Fatal("missing bounded continuation", page)
			}
			item := page.Items[0]
			all := append([]contextpack.Line(nil), item.Lines...)
			for item.Next != "" {
				next := compactResult(t, r.Kitsu("context", "show", page.ID, "--item", item.ID, "--cursor", item.Next, "--max-bytes=2048"), 0)
				item = next.Items[0]
				if len(item.Lines) == 0 {
					t.Fatal("cursor made no progress")
				}
				all = append(all, item.Lines...)
				if len(all) > 80 {
					t.Fatal("cursor repeated evidence")
				}
			}
			if len(all) != 80 {
				t.Fatalf("recovered %d lines, want 80", len(all))
			}
			for i, line := range all {
				if line.Number != i+1 {
					t.Fatalf("reordered or repeated line %d: %+v", i, line)
				}
			}
			cached := compactResult(t, r.Kitsu(append([]string{"context"}, command...)...), 0)
			if cached.ID != page.ID || !cached.CacheHit {
				t.Fatal("native operation did not reuse retained artifact", cached)
			}
			r.Write("source.txt", "changed\n")
			stale := r.Kitsu("context", "show", page.ID)
			if stale.Code != ExitFailure || !strings.Contains(stale.Stderr, "stale") {
				t.Fatalf("stale native artifact accepted: %s", stale.Output())
			}
			r.Write("source.txt", source.String())
		})
	}
}

func TestTypedContextKeepsSourceContainmentAndOmissions(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	r.Write(".gitignore", "private/\n")
	r.Write("public/source.txt", "needle public\n")
	r.Write("public/nested/source.txt", "needle nested\n")
	r.Write("publicity/source.txt", "needle elsewhere\n")
	r.Write("private/secret.txt", "needle SECRET\n")
	r.Write("public/binary", "needle\x00SECRET\n")
	page := compactResult(t, r.Kitsu("context", "search", "needle", "public"), 0)
	item := page.Items[0]
	if item.Status != "partial" || item.Skipped != 1 || item.Error == "" || len(item.Lines) != 2 || strings.Contains(stringMustJSON(page), "SECRET") || strings.Contains(stringMustJSON(item.Lines), "elsewhere") {
		t.Fatal("source boundary or omissions lost", page)
	}
	for _, path := range []string{"private/secret.txt", ".git/config", "missing.txt"} {
		page := compactResult(t, r.Kitsu("context", "read", path), 0)
		if page.Items[0].Status != "error" || page.Items[0].Error == "" || len(page.Items[0].Lines) != 0 {
			t.Fatalf("unavailable source %q escaped containment: %+v", path, page)
		}
	}
	t.Run("symlink", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "secret.txt")
		if err := os.WriteFile(outside, []byte("needle SECRET\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, r.Path("public/link.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		page := compactResult(t, r.Kitsu("context", "read", "public/link.txt"), 0)
		if page.Items[0].Status != "error" || len(page.Items[0].Lines) != 0 || strings.Contains(stringMustJSON(page), "SECRET") {
			t.Fatal("symlink escaped source containment", page)
		}
	})
}
