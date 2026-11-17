package contextpack

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// Logical observations deliberately ignore page layout and chunk size. Their oracle
// reads source bytes independently, rather than using Lines or selectLines.
type equivalenceLine struct {
	Path   string
	Number int
	Text   string
}

type equivalenceOutcome struct {
	ID       string
	Status   string
	Skipped  int
	HasError bool
	Lines    []equivalenceLine
}

func TestContextOfflineReadSearchEquivalence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		seed []byte
	}{
		{"ascii", []byte{0, 3, 6, 9}},
		{"multibyte", []byte{1, 4, 7, 255}},
		{"escaped", []byte{2, 5, 8, 254}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, repo := fixture(t)
			files := equivalenceFiles(tc.seed)
			for name, text := range files {
				repo.Write(name, text)
			}
			ops := equivalenceOperations(2, 7)
			metadata := json.RawMessage(`{"required":["offline only","preserve errors and uncertainty"]}`)
			a, hit, err := e.Collect(t.Context(), Request{Version: 1, Operations: ops}, metadata)
			if err != nil || hit {
				t.Fatalf("initial collect: hit=%v err=%v", hit, err)
			}
			want := equivalenceOracle(t, repo.Dir, files, ops)
			equivalenceAssertOutcomes(t, a.Items, want)
			for _, budget := range []int{512, 2048, 4096, 16384} {
				t.Run(fmt.Sprintf("budget_%d", budget), func(t *testing.T) { equivalenceAssertPages(t, a, budget, want) })
			}
			cached, hit, err := e.Collect(t.Context(), Request{Version: 1, Operations: ops}, metadata)
			if err != nil || !hit || cached.ID != a.ID {
				t.Fatalf("cache collect: hit=%v err=%v", hit, err)
			}
			equivalenceAssertOutcomes(t, cached.Items, want)
		})
	}
}

func TestContextOfflineFreshnessEquivalence(t *testing.T) {
	t.Parallel()
	e, repo := fixture(t)
	files := map[string]string{"source.txt": "old needle\nkeep\n"}
	repo.Write("source.txt", files["source.txt"])
	ops := []Operation{{ID: "read", Kind: "read", Path: "source.txt"}, {ID: "search", Kind: "search", Pattern: "needle"}}
	before := collect(t, e, ops...)
	files["source.txt"] = "changed Ж\nkeep\nlate needle 🐕\n"
	repo.Write("source.txt", files["source.txt"])
	if _, err := e.Load(t.Context(), before.ID); err == nil {
		t.Fatal("stale source remained loadable")
	}
	after, hit, err := e.Collect(t.Context(), Request{Version: 1, Operations: ops}, nil)
	if err != nil || hit || after.ID == before.ID {
		t.Fatalf("source change failed invalidation: hit=%v err=%v", hit, err)
	}
	want := equivalenceOracle(t, repo.Dir, files, ops)
	equivalenceAssertOutcomes(t, after.Items, want)
	equivalenceAssertPages(t, after, 2048, want)
}

func TestContextOfflineGitEquivalence(t *testing.T) {
	t.Parallel()
	e, repo := fixture(t)
	repo.Write("changed.txt", "before\n")
	repo.Write("removed.txt", "removed\n")
	repo.Git("add", ".")
	repo.Git("commit", "-qm", "baseline")
	repo.Write("changed.txt", "after Ж\n")
	repo.Git("rm", "-q", "removed.txt")
	repo.Write("new file.txt", "added 🐕\n")
	repo.Git("add", ".")
	a := collect(t, e, Operation{ID: "git", Kind: "git"})
	if len(a.Items) != 1 || len(a.Items[0].Lines) == 0 {
		t.Fatal("git collection dropped source state")
	}
	var state State
	if err := json.Unmarshal([]byte(a.Items[0].Lines[0].Text), &state); err != nil {
		t.Fatal(err)
	}
	if state != a.State || state.Head != strings.TrimSpace(repo.Git("rev-parse", "HEAD")) || state.Tree != strings.TrimSpace(repo.Git("write-tree")) {
		t.Fatal("git source state differs from direct HEAD and staged tree")
	}
	// The staged fixture includes all worktree changes. Read Git's ordinary
	// diff directly, without calling the engine's snapshot or Diff helpers.
	fields := strings.Split(strings.TrimSuffix(repo.Git("diff", "--cached", "--no-renames", "--name-status", "-z", "HEAD"), "\x00"), "\x00")
	want := equivalenceOutcome{ID: "git", Status: "ok", Lines: []equivalenceLine{{Number: 0, Text: a.Items[0].Lines[0].Text}}}
	kinds := map[string]string{"A": "added", "M": "changed", "D": "removed"}
	if len(fields) != 6 {
		t.Fatalf("fixture must exercise addition, modification and deletion: %q", fields)
	}
	for i := 0; i < len(fields); i += 2 {
		kind, ok := kinds[fields[i]]
		if !ok {
			t.Fatalf("unexpected direct Git status %q", fields[i])
		}
		want.Lines = append(want.Lines, equivalenceLine{fields[i+1], i/2 + 1, kind})
	}
	equivalenceAssertOutcomes(t, a.Items, []equivalenceOutcome{want})
	for _, budget := range []int{512, 2048, 4096} {
		equivalenceAssertPages(t, a, budget, []equivalenceOutcome{want})
	}
}

func equivalenceFiles(seed []byte) map[string]string {
	if len(seed) == 0 {
		seed = []byte{0}
	}
	if len(seed) > 64 {
		seed = seed[:64]
	}
	tokens := []string{"abc", "Ж🐕界", "\"\\\t\r", "e\u0301", "x [literal]", " "}
	var a strings.Builder
	a.WriteString("\n")
	for i, b := range seed {
		fmt.Fprintf(&a, "row %d %s\n", i+2, strings.Repeat(tokens[int(b)%len(tokens)], 1+int(b)%9))
	}
	// A match at the end of a long multibyte line must retain the entire
	// source line, with byte anchors that can cross page boundaries.
	a.WriteString(strings.Repeat(tokens[int(seed[0])%len(tokens)], 760))
	a.WriteString(" late needle [literal]\n")
	a.WriteString("last needle without newline")
	return map[string]string{
		"src/a.txt":     a.String(),
		"src/sub/b.txt": "start\r\n\nneedle [literal]\nЖ🐕 needle\n",
		"other/c.txt":   "outside prefix needle\n",
		"src/bad.bin":   "unreadable\x00needle",
	}
}

func equivalenceOperations(start, end int) []Operation {
	return []Operation{
		{ID: "whole", Kind: "read", Path: "src/a.txt"},
		{ID: "range", Kind: "read", Path: "src/a.txt", Start: start, End: end},
		{ID: "past_end", Kind: "read", Path: "src/sub/b.txt", Start: 999},
		{ID: "literal", Kind: "search", Pattern: "needle [literal]"},
		{ID: "prefix", Kind: "search", Path: "src", Pattern: "needle"},
		{ID: "regex", Kind: "search", Path: "src/sub", Pattern: `^Ж.*needle`, Regex: true},
		{ID: "none", Kind: "search", Path: "other/c.txt", Pattern: "does-not-exist"},
		{ID: "missing", Kind: "read", Path: "absent.txt"},
		{ID: "binary", Kind: "read", Path: "src/bad.bin"},
	}
}

func equivalenceOracle(t *testing.T, dir string, files map[string]string, ops []Operation) []equivalenceOutcome {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]equivalenceOutcome, 0, len(ops))
	for _, op := range ops {
		result := equivalenceOutcome{ID: op.ID, Status: "ok"}
		found := false
		for _, name := range names {
			if op.Kind == "read" && name != op.Path {
				continue
			}
			if op.Kind == "search" && op.Path != "" && op.Path != "." && name != op.Path && !strings.HasPrefix(name, op.Path+"/") {
				continue
			}
			found = true
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
				result.Skipped++
				result.Status, result.HasError = "partial", true
				if op.Kind == "read" || op.Path == name {
					result.Status = "error"
				}
				continue
			}
			reader := bufio.NewReader(strings.NewReader(string(data)))
			for number := 1; ; number++ {
				line, err := reader.ReadString('\n')
				if err != nil && err != io.EOF {
					t.Fatal(err)
				}
				if line != "" {
					selected := number >= max(1, op.Start) && (op.End == 0 || number <= op.End)
					if op.Kind == "search" {
						if op.Regex {
							selected = regexp.MustCompile(op.Pattern).MatchString(line)
						} else {
							selected = strings.Contains(line, op.Pattern)
						}
					}
					if selected {
						result.Lines = append(result.Lines, equivalenceLine{name, number, line})
					}
				}
				if err == io.EOF {
					break
				}
			}
		}
		if !found && op.Path != "" {
			result.Status, result.HasError = "error", true
		}
		out = append(out, result)
	}
	return out
}

func equivalenceAssertOutcomes(t *testing.T, items []Item, want []equivalenceOutcome) {
	t.Helper()
	if len(items) != len(want) {
		t.Fatalf("items=%d, want %d", len(items), len(want))
	}
	for i, item := range items {
		got := equivalenceOutcome{ID: item.ID, Status: item.Status, Skipped: item.Skipped, HasError: item.Error != ""}
		for _, part := range item.Lines {
			if !utf8.ValidString(part.Text) || part.Text == "" {
				t.Fatalf("invalid evidence chunk: %+v", part)
			}
			name := part.Path
			if name == "" {
				name = item.Path
			}
			if part.Offset == 0 {
				got.Lines = append(got.Lines, equivalenceLine{name, part.Number, part.Text})
				continue
			}
			if len(got.Lines) == 0 {
				t.Fatal("continuation before first source chunk")
			}
			last := &got.Lines[len(got.Lines)-1]
			if last.Path != name || last.Number != part.Number || len(last.Text) != part.Offset {
				t.Fatalf("noncontiguous source anchor: %+v after %+v", part, last)
			}
			last.Text += part.Text
		}
		if !reflect.DeepEqual(got, want[i]) {
			t.Fatalf("semantic mismatch for %s: got %+v, want %+v", item.ID, got, want[i])
		}
	}
}

func equivalenceAssertPages(t *testing.T, a Artifact, budget int, want []equivalenceOutcome) {
	t.Helper()
	data, err := Render(a, "", "", budget, false)
	if err != nil {
		if len(data) != 0 || !strings.Contains(err.Error(), "budget") {
			t.Fatalf("unexpected budget refusal: data=%q err=%v", data, err)
		}
		if budget >= 4096 {
			t.Fatalf("generated fixture must be expandable at budget %d: %v", budget, err)
		}
		return
	}
	first := equivalenceDecodePage(t, data, budget, a)
	if len(first.Items) != len(a.Items) {
		t.Fatal("summary dropped operation outcomes")
	}
	recovered := make([]Item, len(first.Items))
	for i, initial := range first.Items {
		original := a.Items[i]
		recovered[i] = original
		recovered[i].Lines = nil
		current, previous := initial, ""
		for calls := 0; ; calls++ {
			if calls > len(original.Lines) {
				t.Fatal("cursor traversal exceeded evidence bound")
			}
			if current.ID != original.ID || current.Status != original.Status || current.Error != original.Error || current.Skipped != original.Skipped {
				t.Fatalf("page lost operation outcome: %+v", current)
			}
			for _, line := range current.Lines {
				if line.Path == "" {
					line.Path = current.Path
				}
				recovered[i].Lines = append(recovered[i].Lines, line)
			}
			if current.Total != len(original.Lines) || current.Omitted != len(original.Lines)-len(recovered[i].Lines) {
				t.Fatal("omission counts do not account for retained evidence")
			}
			if (current.Omitted > 0) != (current.Next != "") {
				t.Fatal("omitted evidence has no continuation or complete page has one")
			}
			if current.Next == "" {
				break
			}
			if current.Next == previous {
				t.Fatal("cursor failed to advance")
			}
			previous = current.Next
			nextBudget := budget
			data, err := Render(a, original.ID, current.Next, nextBudget, true)
			if err != nil {
				if len(data) != 0 || !strings.Contains(err.Error(), "budget") {
					t.Fatalf("unexpected expansion refusal: %v", err)
				}
				// A small budget may fit a summary but no long evidence chunk.
				// Explicit refusal is recoverable by increasing the budget.
				nextBudget = 16384
				data, err = Render(a, original.ID, current.Next, nextBudget, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			page := equivalenceDecodePage(t, data, nextBudget, a)
			if !page.CacheHit || len(page.Items) != 1 || len(page.Items[0].Lines) == 0 {
				t.Fatal("successful expansion made no evidence progress")
			}
			current = page.Items[0]
		}
	}
	equivalenceAssertOutcomes(t, recovered, want)
}

func equivalenceDecodePage(t *testing.T, data []byte, budget int, a Artifact) Page {
	t.Helper()
	if len(data) > budget || !utf8.Valid(data) || !strings.HasSuffix(string(data), "\n") {
		t.Fatal("invalid UTF-8 or exact-byte page budget")
	}
	var p Page
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Version != 1 || p.ID != a.ID || p.Kind != a.Kind || p.State != a.State || !p.Created.Equal(a.Created) || !reflect.DeepEqual(p.Metadata, a.Metadata) {
		t.Fatal("page dropped or changed required metadata")
	}
	return p
}

func TestContextRequiredMetadataBudgetRefusal(t *testing.T) {
	t.Parallel()
	for _, size := range []int{0, 128, 2048, 8192} {
		t.Run(fmt.Sprintf("metadata_%d", size), func(t *testing.T) {
			metadata, err := json.Marshal(map[string]any{"constraints": strings.Repeat("Ж🐕", size), "checks": []string{"failed", "unknown"}})
			if err != nil {
				t.Fatal(err)
			}
			a := Artifact{Version: 1, ID: strings.Repeat("c", 64), Kind: "collection", Metadata: metadata, Items: []Item{
				{ID: "partial", Status: "partial", Error: "unreadable sources", Skipped: 3, Lines: []Line{{Path: "source", Number: 1, Text: "retained evidence\n"}}},
				{ID: "error", Status: "error", Error: "source missing"},
			}}
			// This deliberately omits framing fields, totals and cursors. Even
			// this lower bound must fit before success could preserve outcomes.
			minimum, err := json.Marshal(map[string]any{"metadata": json.RawMessage(metadata), "items": []Item{
				{ID: "partial", Status: "partial", Error: "unreadable sources", Skipped: 3},
				{ID: "error", Status: "error", Error: "source missing"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			for _, budget := range []int{512, 1024, 4096, 65536} {
				data, err := Render(a, "", "", budget, false)
				if len(minimum) > budget && err == nil {
					t.Fatal("budget silently discarded required metadata or outcomes")
				}
				if err != nil {
					if len(data) != 0 || !strings.Contains(err.Error(), "budget") {
						t.Fatalf("invalid budget refusal: %v", err)
					}
					if budget == 65536 {
						t.Fatalf("large budget must hold this fixture's mandatory outcomes: %v", err)
					}
					continue
				}
				p := equivalenceDecodePage(t, data, budget, a)
				if len(p.Items) != 2 || p.Items[0].Status != "partial" || p.Items[0].Skipped != 3 || p.Items[0].Error != a.Items[0].Error || p.Items[1].Status != "error" || p.Items[1].Error != a.Items[1].Error {
					t.Fatal("budget silently dropped operation outcomes")
				}
			}
		})
	}
}

func equivalenceCursor(artifact, item string, offset int) string {
	data, _ := json.Marshal(map[string]any{"a": artifact, "i": item, "o": offset})
	return base64.RawURLEncoding.EncodeToString(data)
}

func TestContextCursorRejectsCrossItemAndMalformed(t *testing.T) {
	t.Parallel()
	a := Artifact{Version: 1, ID: strings.Repeat("d", 64), Kind: "collection", Items: []Item{
		{ID: "left", Status: "ok", Lines: []Line{{Path: "a", Number: 1, Text: "left\n"}}},
		{ID: "right", Status: "error", Error: "unavailable"},
	}}
	for _, tc := range []struct {
		name, item, continuation string
		budget                   int
	}{
		{"malformed_base64", "left", "!", 4096},
		{"malformed_json", "left", base64.RawURLEncoding.EncodeToString([]byte("{")), 4096},
		{"negative_offset", "left", equivalenceCursor(a.ID, "left", -1), 4096},
		{"past_evidence", "left", equivalenceCursor(a.ID, "left", 2), 4096},
		{"foreign_artifact", "left", equivalenceCursor(strings.Repeat("e", 64), "left", 0), 4096},
		{"cross_item", "right", equivalenceCursor(a.ID, "left", 0), 4096},
		{"missing_item", "", equivalenceCursor(a.ID, "left", 0), 4096},
		{"unknown_item", "absent", "", 4096},
		{"too_small", "left", "", 511},
		{"too_large", "left", "", MaxBytes + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if data, err := Render(a, tc.item, tc.continuation, tc.budget, false); err == nil || len(data) != 0 {
				t.Fatalf("accepted invalid continuation or returned partial output: %q %v", data, err)
			}
		})
	}
	data, err := Render(a, "left", equivalenceCursor(a.ID, "left", 1), 4096, false)
	if err != nil {
		t.Fatal(err)
	}
	p := equivalenceDecodePage(t, data, 4096, a)
	if len(p.Items) != 1 || p.Items[0].Omitted != 0 || p.Items[0].Next != "" || len(p.Items[0].Lines) != 0 {
		t.Fatal("end cursor did not terminate")
	}
}

func FuzzContextReadSearchEquivalence(f *testing.F) {
	f.Add([]byte{}, uint8(0), uint8(0))
	f.Add([]byte{1, 4, 7, 255}, uint8(2), uint8(3))
	f.Add([]byte{2, 5, 8, 254}, uint8(19), uint8(255))
	f.Add([]byte("late multibyte Ж🐕"), uint8(7), uint8(8))
	f.Fuzz(func(t *testing.T, seed []byte, startByte, budgetByte uint8) {
		dir := t.TempDir()
		files := equivalenceFiles(seed)
		names := make([]string, 0, len(files))
		for name, text := range files {
			dest := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dest, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		sort.Strings(names)
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = root.Close() }()
		start := int(startByte) % 20
		ops := equivalenceOperations(start, max(1, start)+int(budgetByte)%12)
		a := Artifact{Version: 1, ID: strings.Repeat("f", 64), Kind: "collection", Metadata: json.RawMessage(`{"required":"offline; retain uncertainty"}`)}
		e := &Engine{}
		for _, op := range ops {
			a.Items = append(a.Items, e.collectOne(t.Context(), root, State{}, names, op))
		}
		want := equivalenceOracle(t, dir, files, ops)
		equivalenceAssertOutcomes(t, a.Items, want)
		budgets := []int{512, 1024, 2048, 4096, 8192, 16384}
		equivalenceAssertPages(t, a, budgets[int(budgetByte)%len(budgets)], want)
		// Always exercise pagination equivalence even if the generated small
		// budget correctly refuses to hold the batch's mandatory outcomes.
		equivalenceAssertPages(t, a, 4096, want)
	})
}

func FuzzContextCursorValidation(f *testing.F) {
	f.Add("", uint8(0))
	f.Add("{}", uint8(1))
	f.Add("Ж🐕", uint8(3))
	f.Add("eyJhIjoib3RoZXIifQ", uint8(7))
	f.Fuzz(func(t *testing.T, payload string, mode uint8) {
		if len(payload) > 256 {
			payload = payload[:256]
		}
		a := Artifact{Version: 1, ID: strings.Repeat("a", 64), Kind: "collection", Items: []Item{{ID: "left", Status: "partial", Error: "some sources unavailable", Skipped: 1, Lines: []Line{{Path: "source", Number: 1, Text: "retained Ж🐕\n"}}}, {ID: "right", Status: "error", Error: "absent"}}}
		item, continuation := "left", ""
		switch mode % 8 {
		case 0:
			continuation = "!" + payload
		case 1:
			data, _ := json.Marshal(payload)
			continuation = base64.RawURLEncoding.EncodeToString(data)
		case 2:
			continuation = equivalenceCursor("foreign"+payload, item, 0)
		case 3:
			continuation = equivalenceCursor(a.ID, "other"+payload, 0)
		case 4:
			continuation = equivalenceCursor(a.ID, item, -1-int(mode))
		case 5:
			continuation = equivalenceCursor(a.ID, item, 2+len(payload))
		case 6:
			continuation, item = equivalenceCursor(a.ID, item, 0), ""
		case 7:
			continuation, item = equivalenceCursor(a.ID, item, 0), "right"
		}
		if data, err := Render(a, item, continuation, 4096, false); err == nil || len(data) != 0 {
			t.Fatalf("invalid cursor exposed evidence: item=%q cursor=%q data=%q err=%v", item, continuation, data, err)
		}
	})
}
