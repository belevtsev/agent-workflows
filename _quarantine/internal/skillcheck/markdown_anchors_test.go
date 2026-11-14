package skillcheck

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
)

var (
	// atxHeading is a heading line: up to three spaces, one to six #, then
	// the end of the line or a space and the heading text.
	atxHeading = regexp.MustCompile(`^ {0,3}#{1,6}(?:[ \t]+(.*))?$`)
	// closingSequence is an ATX heading's optional run of closing #.
	closingSequence = regexp.MustCompile(`(?:^|[ \t]+)#+[ \t]*$`)
	// renderedLink is a link or image, inline or by reference, which renders
	// as its bracketed text.
	renderedLink = regexp.MustCompile(`!?\[([^\[\]]*)\](?:\([^()]*\)|\[[^\[\]]*\])?`)
	htmlTag      = regexp.MustCompile(`</?[A-Za-z][^<>]*>`)
	// emphasisUnderscores are underscore runs at the edge of a word; inside a
	// word, as in snake_case, an underscore is text.
	emphasisUnderscores = regexp.MustCompile(`(^|[^\pL\pN_])_+|_+([^\pL\pN_]|$)`)
)

// headingText returns the text GitHub renders for a heading's Markdown: code
// spans keep their content, links and images keep their bracketed text, and
// HTML tags and emphasis markers are dropped.
func headingText(markdown string) string {
	var text strings.Builder
	plain := func(segment string) {
		segment = renderedLink.ReplaceAllString(segment, "$1")
		segment = htmlTag.ReplaceAllString(segment, "")
		segment = strings.ReplaceAll(segment, "*", "")
		text.WriteString(emphasisUnderscores.ReplaceAllString(segment, "$1$2"))
	}
	start := 0
	for _, span := range codeSpan.FindAllStringIndex(markdown, -1) {
		plain(markdown[start:span[0]])
		code := strings.Trim(markdown[span[0]:span[1]], "`")
		if len(code) > 1 && strings.HasPrefix(code, " ") && strings.HasSuffix(code, " ") {
			code = code[1 : len(code)-1]
		}
		text.WriteString(code)
		start = span[1]
	}
	plain(markdown[start:])
	return strings.TrimSpace(text.String())
}

// githubSlug returns the anchor GitHub gives a heading's rendered text: lower
// case, every character other than a letter, a mark, a number, '_', '-' or a
// space dropped, and each space turned into '-'.
func githubSlug(text string) string {
	var slug strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ':
			slug.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r):
			slug.WriteRune(r)
		}
	}
	return slug.String()
}

// markdownAnchors returns the anchors of a Markdown file's headings outside
// code fences and front matter. A repeated slug gains -1, -2 and so on, in
// document order, skipping any taken by an earlier heading, as on GitHub.
// Only headings define anchors: the checked documents hold no HTML anchors.
func markdownAnchors(data []byte) map[string]bool {
	anchors := map[string]bool{}
	repeats := map[string]int{}
	fence := ""
	frontMatter := bytes.HasPrefix(data, []byte("---\n"))
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(nil, 1<<20)
	for first := true; scanner.Scan(); first = false {
		line := scanner.Text()
		// Front matter runs from a first line of --- to the next ---.
		if frontMatter {
			frontMatter = first || line != "---"
			continue
		}
		trimmed := strings.TrimSpace(line)
		if marker := trimmed[:min(3, len(trimmed))]; marker == "```" || marker == "~~~" {
			switch {
			case fence == "":
				fence = marker
			case fence == marker:
				fence = ""
			}
			continue
		}
		match := atxHeading.FindStringSubmatch(line)
		if fence != "" || match == nil {
			continue
		}
		slug := githubSlug(headingText(closingSequence.ReplaceAllString(match[1], "")))
		anchor := slug
		for anchors[anchor] {
			repeats[slug]++
			anchor = fmt.Sprintf("%s-%d", slug, repeats[slug])
		}
		anchors[anchor] = true
	}
	return anchors
}

// anchorProblems reports each link in documents, slash-separated paths under
// root, whose fragment names no heading of the Markdown file it targets: the
// document itself for "#heading", another file for "file.md#heading". A link
// to a file that is missing or outside root is left to the link check.
func anchorProblems(t *testing.T, root string, documents []string) []string {
	t.Helper()
	cache := map[string]map[string]bool{}
	anchorsOf := func(document string) (map[string]bool, bool) {
		if anchors, ok := cache[document]; ok {
			return anchors, anchors != nil
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(document)))
		if err != nil {
			cache[document] = nil
			return nil, false
		}
		cache[document] = markdownAnchors(data)
		return cache[document], true
	}
	var problems []string
	for _, document := range documents {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(document)))
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range markdownTargets(data) {
			parsed, err := url.Parse(strings.Trim(target, "<>"))
			if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Fragment == "" {
				continue
			}
			targetDocument := document
			if parsed.Path != "" {
				targetDocument = path.Join(path.Dir(document), parsed.Path)
			}
			if !strings.HasSuffix(targetDocument, ".md") || !filepath.IsLocal(filepath.FromSlash(targetDocument)) {
				continue
			}
			if anchors, ok := anchorsOf(targetDocument); ok && !anchors[parsed.Fragment] {
				problems = append(problems, fmt.Sprintf("%s: link %q names no heading of %s", document, target, targetDocument))
			}
		}
	}
	return problems
}

func TestMarkdownAnchorsResolve(t *testing.T) {
	for _, problem := range anchorProblems(t, repoRoot, linkCheckedDocuments(t)) {
		t.Error(problem)
	}
}

// Oracle: the anchors github.com gives these headings (github-slugger, as
// GitHub's Markdown renderer uses it).
func TestGitHubHeadingSlugs(t *testing.T) {
	for heading, want := range map[string]string{
		"Exit statuses":                      "exit-statuses",
		"`cw task verify`":                   "cw-task-verify",
		"What's new? (2026)":                 "whats-new-2026",
		"Install & update — Codex":           "install--update--codex",
		"[3.0.0-rc.2] - 2026-11-12":          "300-rc2---2026-11-12",
		"`XDG_STATE_HOME` and snake_case":    "xdg_state_home-and-snake_case",
		"**Bold** and _emphasis_ and __x__":  "bold-and-emphasis-and-x",
		"[Link text](docs/hooks.md#install)": "link-text",
		"Ünïcode Straße":                     "ünïcode-straße",
		"Two  spaces":                        "two--spaces",
		"<code>cw</code> tags":               "cw-tags",
	} {
		if got := githubSlug(headingText(heading)); got != want {
			t.Errorf("slug of %q = %q; want %q", heading, got, want)
		}
	}
}

// Oracle: GitHub numbers a repeated slug -1, -2 in document order and skips
// a number an earlier heading already took; headings in code fences, front
// matter or with closing # are handled as GitHub renders them.
func TestMarkdownAnchorsOfAFile(t *testing.T) {
	data := []byte("---\nname: x\n# not a heading\n---\n" +
		"# Setup\n\n## Setup ##\n\n### Setup-1\n\n#### Setup\n\n" +
		"```sh\n# not a heading either\n```\n\n    # indented code\n\n#hashtag\n")
	got := slices.Sorted(maps.Keys(markdownAnchors(data)))
	want := []string{"setup", "setup-1", "setup-1-1", "setup-2"}
	if !slices.Equal(got, want) {
		t.Fatalf("anchors = %q; want %q", got, want)
	}
}

// Oracle: a fixture whose links name a missing heading, in the same file and
// in another file, fails the check with one problem per bad link; its good
// links, links to other file types and external links pass.
func TestAnchorProblemsReportsMissingHeadings(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "docs", "guide.md"), []byte("# Guide\n\n## Exit statuses\n\n## Exit statuses\n"))
	writeFixture(t, filepath.Join(root, "README.md"), []byte("# Read me\n\n"+
		"[good](#read-me) [good](docs/guide.md#exit-statuses) [good](docs/guide.md#exit-statuses-1)\n"+
		"[code](cmd/main.go#L1) [web](https://example.com/x.md#nowhere) [missing file](docs/none.md#x)\n"+
		"[bad](#no-such-heading) [bad](docs/guide.md#exit-status)\n"+
		"[ref]: docs/guide.md#exit-statuses-2\n"))
	got := anchorProblems(t, root, []string{"README.md", "docs/guide.md"})
	want := []string{
		`README.md: link "#no-such-heading" names no heading of README.md`,
		`README.md: link "docs/guide.md#exit-status" names no heading of docs/guide.md`,
		`README.md: link "docs/guide.md#exit-statuses-2" names no heading of docs/guide.md`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("problems = %q; want %q", got, want)
	}
}
