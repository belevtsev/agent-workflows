package repocheck

import (
	"bufio"
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// repoRoot is the repository checkout this package lives in.
var repoRoot = filepath.Join("..", "..")

// repoFiles lists the files of the checkout that git tracks or would track.
func repoFiles(t *testing.T) []string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", "-C", repoRoot, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	output, err := command.Output()
	if err != nil {
		// CI always works in a checkout; there, a skip would hide every
		// check of the repository's files, the size limits among them.
		if os.Getenv("CI") != "" {
			t.Fatalf("CI must run in a git checkout, and git cannot list the files: %v", err)
		}
		t.Skipf("not a git checkout: %v", err)
	}
	var files []string
	for name := range strings.SplitSeq(strings.TrimSuffix(string(output), "\x00"), "\x00") {
		if _, err := os.Lstat(filepath.Join(repoRoot, name)); err == nil {
			files = append(files, name)
		}
	}
	return files
}

// linkCheckedDocuments are the Markdown files whose links must resolve:
// every Markdown file of the checkout.
func linkCheckedDocuments(t *testing.T) []string {
	t.Helper()
	var documents []string
	for _, file := range repoFiles(t) {
		if strings.HasSuffix(file, ".md") {
			documents = append(documents, file)
		}
	}
	return documents
}

var (
	// A link's text starts at a bracket that does not follow a word or a
	// closing bracket, so Go generics such as Map[T](x) are not links.
	inlineLink = regexp.MustCompile(`(?:^|[^\w\]\\])!?\[[^\[\]]*\]\(\s*(<[^>]*>|[^)\s]+)`)
	// A definition gives a label its destination: [label]: destination.
	referenceLink = regexp.MustCompile(`^\s{0,3}\[([^\]]+)\]:\s*(<[^>]*>|\S+)`)
	codeSpan      = regexp.MustCompile("`+[^`]*`+")
	// GitHub shows nothing of an HTML comment; one that is not closed runs
	// to the end of the file.
	htmlComment = regexp.MustCompile(`(?s)<!--.*?(?:-->|\z)`)
	// GitHub shows the HTML of a Markdown file, so a link or an image can
	// be written as <a href="..."> or <img src="...">: with the value in
	// double quotes, in single quotes or bare, and with the tag on one line
	// or on several.
	htmlLink      = regexp.MustCompile(`(?i)<a\b[^>]*?\shref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	htmlImage     = regexp.MustCompile(`(?i)<img\b[^>]*?\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	markdownImage = regexp.MustCompile(`!\[[^\[\]]*\]\(\s*(<[^>]*>|[^)\s]+)`)
	// An image can name a definition in place of its destination:
	// ![text][label], ![label][] or ![label]. The second group tells these
	// from ![text](destination).
	referenceImage = regexp.MustCompile(`!\[([^\[\]]*)\](\(|\[[^\[\]]*\])?`)
)

// markdownProse returns what GitHub shows of a Markdown file as text: the
// lines outside code fences, without their code spans and without HTML
// comments.
func markdownProse(data []byte) string {
	var lines []string
	fence := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
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
		if fence != "" {
			continue
		}
		lines = append(lines, codeSpan.ReplaceAllString(line, ""))
	}
	return htmlComment.ReplaceAllString(strings.Join(lines, "\n"), "")
}

// submatches returns, for every match of each pattern in text, its first
// group that is not empty.
func submatches(text string, patterns ...*regexp.Regexp) []string {
	var found []string
	for _, pattern := range patterns {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if at := slices.IndexFunc(match[1:], func(group string) bool { return group != "" }); at >= 0 {
				found = append(found, match[at+1])
			}
		}
	}
	return found
}

// markdownTargets returns the link and image destinations that GitHub
// shows, written in Markdown or in HTML, and the destination of every
// definition.
func markdownTargets(data []byte) []string {
	prose := markdownProse(data)
	targets := submatches(prose, htmlLink, htmlImage)
	for line := range strings.SplitSeq(prose, "\n") {
		targets = append(targets, submatches(line, inlineLink)...)
		if match := referenceLink.FindStringSubmatch(line); match != nil {
			targets = append(targets, match[2])
		}
	}
	return targets
}

// referenceLabel writes the label of a definition as Markdown compares it:
// without regard to case and to the amount of space.
func referenceLabel(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), " "))
}

// markdownImages returns the destinations of the images that GitHub shows:
// <img src="target">, ![text](target), and ![text][label] with the
// destination of its definition.
func markdownImages(data []byte) []string {
	prose := markdownProse(data)
	targets := submatches(prose, htmlImage)
	definitions := map[string]string{}
	for line := range strings.SplitSeq(prose, "\n") {
		targets = append(targets, submatches(line, markdownImage)...)
		if match := referenceLink.FindStringSubmatch(line); match != nil {
			definitions[referenceLabel(match[1])] = match[2]
		}
	}
	for _, match := range referenceImage.FindAllStringSubmatch(prose, -1) {
		label, rest := match[1], match[2]
		if rest == "(" {
			continue
		}
		if len(rest) > 2 {
			label = rest[1 : len(rest)-1]
		}
		if target, defined := definitions[referenceLabel(label)]; defined {
			targets = append(targets, target)
		}
	}
	return targets
}

func TestRelativeMarkdownLinksResolve(t *testing.T) {
	documents := linkCheckedDocuments(t)
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range documents {
		data, err := os.ReadFile(filepath.Join(repoRoot, document))
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range markdownTargets(data) {
			target = strings.Trim(target, "<>")
			parsed, err := url.Parse(target)
			if err != nil {
				t.Errorf("%s: invalid link %q", document, target)
				continue
			}
			if parsed.Scheme != "" || parsed.Host != "" || parsed.Path == "" {
				continue
			}
			resolved := filepath.Join(root, filepath.Dir(document), filepath.FromSlash(parsed.Path))
			if strings.HasPrefix(parsed.Path, "/") || !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
				t.Errorf("%s: link %q leaves the repository", document, target)
			} else if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s: broken link %q", document, target)
			}
		}
	}
}

func TestLinksAndImagesAreReadInEveryForm(t *testing.T) {
	document := []byte(strings.Join([]string{
		"[a](one.md) and ![b](two.svg), <a href='three.md'>c</a> and <img src=four.svg>.",
		`<img alt="d"`,
		`     src="five.svg" width="100%">`,
		"![e][Some Label], ![some label][], ![some  LABEL] and ![no definition].",
		"[some label]: six.svg",
		"`[f](seven.md)` <!-- [g](eight.md)",
		`<img src="nine.svg"> -->`,
		"```",
		`[h](ten.md) <img src="eleven.svg">`,
		"```",
		"<!-- [i](twelve.md)",
	}, "\n"))
	if got, want := markdownTargets(document), []string{"three.md", "four.svg", "five.svg", "one.md", "two.svg", "six.svg"}; !slices.Equal(got, want) {
		t.Errorf("the destinations are %q, want %q", got, want)
	}
	if got, want := markdownImages(document), []string{"four.svg", "five.svg", "two.svg", "six.svg", "six.svg", "six.svg"}; !slices.Equal(got, want) {
		t.Errorf("the images are %q, want %q", got, want)
	}
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
