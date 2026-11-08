package skillcheck

// Tests for contract roster-tests, revision 1, section B (no Mermaid in
// first-party Markdown).

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// mermaidFence matches B3's fence opening: leading spaces or tabs, three or
// more backticks or tildes, optional spaces or tabs, then the info string's
// first word, which ends at whitespace or "{".
var mermaidFence = regexp.MustCompile("^[ \t]*(?:`{3,}|~{3,})[ \t]*([^\\s{]*)")

// mermaidFenceLines returns the 1-based numbers of every line that opens a
// mermaid fenced code block (B3), at any indentation and inside other fences.
func mermaidFenceLines(data []byte) []int {
	var lines []int
	for i, line := range strings.Split(string(data), "\n") {
		if match := mermaidFence.FindStringSubmatch(line); match != nil && strings.EqualFold(match[1], "mermaid") {
			lines = append(lines, i+1)
		}
	}
	return lines
}

// firstPartyMarkdown applies B2 to a repository file list: names ending in
// .md (case-insensitive), except files under third_party/.
func firstPartyMarkdown(files []string) []string {
	var markdown []string
	for _, file := range files {
		if strings.HasSuffix(strings.ToLower(file), ".md") && !strings.HasPrefix(file, "third_party/") {
			markdown = append(markdown, file)
		}
	}
	return markdown
}

// mermaidViolations reports "file:line" for each mermaid fence line in the
// first-party Markdown among files, read relative to root.
func mermaidViolations(t *testing.T, root string, files []string) []string {
	t.Helper()
	var violations []string
	for _, file := range firstPartyMarkdown(files) {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		if info.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		for _, line := range mermaidFenceLines(data) {
			violations = append(violations, file+":"+strconv.Itoa(line))
		}
	}
	return violations
}

// B1, B2, B5: the repository has no mermaid fence in first-party Markdown.
// A violation is reported as the repository-relative file and 1-based line.
func TestB1_NoMermaidInFirstPartyMarkdown(t *testing.T) {
	files := repoFiles(t)
	markdown := firstPartyMarkdown(files)
	if len(markdown) == 0 {
		t.Fatal("no first-party Markdown files found")
	}
	// B2: CHANGELOG.md, docs, evals and testdata are in scope.
	if !slices.Contains(markdown, "CHANGELOG.md") {
		t.Error("CHANGELOG.md is not among the scanned first-party Markdown files")
	}
	for _, violation := range mermaidViolations(t, repoRoot, files) {
		t.Errorf("B1: %s: mermaid fenced code block", violation)
	}
}

// B3 and B4 on synthetic documents: the detector must flag every B3 form and
// no B4 form, so the repository-wide test cannot pass vacuously (B5).
func TestB3_MermaidFenceLinesDetected(t *testing.T) {
	for _, test := range []struct {
		name string
		doc  string
		want []int
	}{
		{"backticks", "# T\n\n```mermaid\ngraph TD\n```\n", []int{3}},
		{"tildes", "~~~mermaid\ngraph TD\n~~~\n", []int{1}},
		{"four backticks", "````mermaid\nx\n````\n", []int{1}},
		{"long tilde run", "~~~~~~ mermaid\nx\n~~~~~~\n", []int{1}},
		{"space before info", "```   mermaid\nx\n```\n", []int{1}},
		{"tab before info", "```\tmermaid\nx\n```\n", []int{1}},
		{"upper case", "```MERMAID\nx\n```\n", []int{1}},
		{"mixed case", "~~~MerMaid\nx\n~~~\n", []int{1}},
		{"indented by spaces", "- item\n\n      ```mermaid\n      x\n      ```\n", []int{3}},
		{"indented by tab", "\t```mermaid\n\tx\n\t```\n", []int{1}},
		{"indented by mixed", " \t  ~~~mermaid\nx\n~~~\n", []int{1}},
		{"attributes after word", "```mermaid {theme: dark}\nx\n```\n", []int{1}},
		{"brace right after word", "```mermaid{.diagram}\nx\n```\n", []int{1}},
		{"trailing words", "```mermaid title here\nx\n```\n", []int{1}},
		{"inside a text template", "````text\nUse this:\n```mermaid\ngraph TD\n```\n````\n", []int{3}},
		{"inside a tilde block", "~~~markdown\n```mermaid\nx\n```\n~~~\n", []int{2}},
		{"crlf line endings", "intro\r\n```mermaid\r\nx\r\n```\r\n", []int{2}},
		{"several blocks", "```mermaid\na\n```\n\ntext\n\n~~~ mermaid\nb\n~~~\n", []int{1, 7}},
		{"no trailing newline", "text\n```mermaid", []int{2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := mermaidFenceLines([]byte(test.doc)); !slices.Equal(got, test.want) {
				t.Errorf("mermaidFenceLines(%q) = %v, want %v", test.doc, got, test.want)
			}
		})
	}
}

func TestB4_NonMermaidLinesNotDetected(t *testing.T) {
	for _, test := range []struct {
		name string
		doc  string
	}{
		{"prose", "We no longer use Mermaid diagrams.\nmermaid is a tool.\n"},
		{"prose at line start", "mermaid\n"},
		{"inline code span", "Avoid `mermaid` blocks.\n"},
		{"inline code span at line start", "`mermaid` is not allowed.\n"},
		{"double backtick span", "``mermaid``\n"},
		{"text fence", "```text\nmermaid\n```\n"},
		{"mermaidjs fence", "```mermaidjs\nx\n```\n"},
		{"mermaid-cli fence", "```mermaid-cli\nx\n```\n"},
		{"tilde mermaidjs", "~~~mermaidjs\nx\n~~~\n"},
		{"no info string", "```\nmermaid\n```\n"},
		{"no info string with spaces", "```   \nx\n```\n"},
		{"no info string tildes", "~~~\nx\n~~~\n"},
		{"two backticks only", "``mermaid\n"},
		{"two tildes only", "~~mermaid\n"},
		{"word after other info", "```text mermaid\nx\n```\n"},
		{"empty document", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := mermaidFenceLines([]byte(test.doc)); len(got) != 0 {
				t.Errorf("mermaidFenceLines(%q) = %v, want none", test.doc, got)
			}
		})
	}
}

// B1 and B2 on a synthetic root: the scan reports file and line for every
// first-party Markdown file in scope, case-insensitive on .md, and skips only
// third_party/ and non-Markdown files.
func TestB2_SyntheticScanScopeAndReport(t *testing.T) {
	root := t.TempDir()
	diagram := []byte("# Doc\n\nIntro.\n\n```mermaid\ngraph TD\n```\n")
	files := map[string][]byte{
		"CHANGELOG.md":                       diagram,
		"docs/design/overview.md":            diagram,
		"evals/case/README.MD":               diagram,
		"internal/x/testdata/fixture.Md":     diagram,
		"skills/s/references/template.md":    []byte("````text\n~~~ Mermaid\nx\n~~~\n````\n"),
		"third_party/vendor/README.md":       diagram,
		"docs/diagram.mmd":                   diagram,
		"docs/notes.markdown":                diagram,
		"docs/clean.md":                      []byte("Mermaid is mentioned in prose only.\n```text\nx\n```\n"),
		"nested/third_party/included/doc.md": diagram,
	}
	var list []string
	for name, data := range files {
		writeFixture(t, filepath.Join(root, filepath.FromSlash(name)), data)
		list = append(list, name)
	}
	slices.Sort(list)
	got := mermaidViolations(t, root, list)
	slices.Sort(got)
	want := []string{
		"CHANGELOG.md:5",
		"docs/design/overview.md:5",
		"evals/case/README.MD:5",
		"internal/x/testdata/fixture.Md:5",
		"nested/third_party/included/doc.md:5",
		"skills/s/references/template.md:2",
	}
	if !slices.Equal(got, want) {
		t.Errorf("violations = %q, want %q", got, want)
	}
}
