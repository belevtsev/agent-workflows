package insights

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func noteFixture(t *testing.T, name, text string) string {
	t.Helper()
	dir := t.TempDir()
	destination := filepath.Join(dir, "openspec", "changes", "example", name)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOptionalWorkflowNotesPreserveClaimsAndOriginalReferences(t *testing.T) {
	t.Parallel()
	text := "# Proposal\n\n## Consequential claims\n| Claim | Kind | Source | Consequence if wrong | Revisit when | Status |\n| --- | --- | --- | --- | --- | --- |\n| Original λ \\| claim | inferred | spec.md:4 | Do NOT drop missing evidence | Regression fails | open |\n"
	dir := noteFixture(t, "proposal.md", text)
	first, err := ReadNotes(dir, "example", 20)
	if err != nil || len(first.Items) != 1 || len(first.Sources) != 1 {
		t.Fatal(first, err)
	}
	item := first.Items[0]
	if item.Fields["claim"] != "Original λ | claim" || item.Fields["kind"] != "inferred" || item.Fields["consequence if wrong"] != "Do NOT drop missing evidence" || item.Line != 6 || len(item.Missing) != 0 {
		t.Fatal("claim or uncertainty lost", item)
	}
	hash := sha256.Sum256([]byte(text))
	if first.Sources[0].SHA256 != hex.EncodeToString(hash[:]) || !strings.Contains(first.Authority, "not machine-certified") {
		t.Fatal(first)
	}
	again, err := ReadNotes(dir, "example", 20)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("read changed notes", again, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "openspec", "changes", "example", "proposal.md"))
	if err != nil || string(data) != text {
		t.Fatal("observation modified originals", err)
	}
	decision := "## Decision notes\n| Choice | Alternatives | Reason | Source | Revisit when |\n| --- | --- | --- | --- | --- |\n| reuse parser | add another parser | preserve compatibility | design-source.md:2 | evidence disproves compatibility |\n"
	if err := os.WriteFile(filepath.Join(dir, "openspec", "changes", "example", "design.md"), []byte(decision), 0o644); err != nil {
		t.Fatal(err)
	}
	withDecision, err := ReadNotes(dir, "example", 20)
	if err != nil || len(withDecision.Items) != 2 || withDecision.Items[1].Fields["alternatives"] != "add another parser" || withDecision.Items[1].Fields["revisit when"] != "evidence disproves compatibility" {
		t.Fatal("consequential decision or reopening evidence lost", withDecision, err)
	}
}

func TestOptionalWorkflowNotesBoundOmissionsAndUnknownFormats(t *testing.T) {
	for _, tc := range []struct {
		name, text      string
		items, problems int
	}{
		{name: "arbitrary prose", text: "## Consequential claims\nAn unresolved assumption in prose.\n", problems: 1},
		{name: "fenced example", text: "```markdown\n## Consequential claims\n| Claim |\n| --- |\n| sample |\n```\n"},
		{name: "partial table", text: "## Consequential claims\n| Claim | Source | Source | Source |\n| --- | --- | --- | --- |\n| uncertain | first | second | third |\n", items: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := noteFixture(t, "proposal.md", tc.text)
			notes, err := ReadNotes(dir, "example", 20)
			if err != nil || len(notes.Items) != tc.items || len(notes.Problems) != tc.problems {
				t.Fatal(notes, err)
			}
			if tc.items > 0 && (!slices.Contains(notes.Items[0].Missing, "source") || notes.Items[0].Fields["source"] != "") {
				t.Fatal("duplicate/missing source became established", notes)
			}
		})
	}
	text := "## Consequential claims\n| Claim | Kind | Source | Consequence if wrong | Revisit when | Status |\n| --- | --- | --- | --- | --- | --- |\n"
	for i := range 25 {
		text += fmt.Sprintf("| assumption-%d | unknown | spec.md:1 | consequence | counterexample | open |\n", i)
	}
	text += "| " + strings.Repeat("x", 512) + " NOT safe | unknown | spec.md:1 | consequence | counterexample | open |\n"
	dir := noteFixture(t, "proposal.md", text)
	bounded, err := ReadNotes(dir, "example", 20)
	if err != nil || len(bounded.Items) != 20 || bounded.Omitted != 6 || len(bounded.Sources) != 1 {
		t.Fatal(bounded, err)
	}
	all, err := ReadNotes(dir, "example", 100)
	if err != nil || !slices.Contains(all.Items[25].OmittedFields, "claim") || all.Items[25].Fields["claim"] != "" {
		t.Fatal("long field silently cut away a negation", all, err)
	}
}

func TestOptionalWorkflowNotesUnsafeSources(t *testing.T) {
	t.Parallel()
	dir := noteFixture(t, "proposal.md", "\xff\x00")
	if _, err := ReadNotes(dir, "example", 20); err == nil {
		t.Fatal("invalid original text accepted")
	}
	if _, err := ReadNotes(dir, "../example", 20); err == nil {
		t.Fatal("unsafe task accepted")
	}
	if _, err := ReadNotes(dir, "example", 0); err == nil {
		t.Fatal("unbounded item request accepted")
	}
}
