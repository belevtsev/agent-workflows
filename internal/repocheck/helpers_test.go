package repocheck

import (
	"bufio"
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	inlineLink    = regexp.MustCompile(`(?:^|[^\w\]\\])!?\[[^\[\]]*\]\(\s*(<[^>]*>|[^)\s]+)`)
	referenceLink = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*(<[^>]*>|\S+)`)
	codeSpan      = regexp.MustCompile("`+[^`]*`+")
)

// markdownTargets returns the link destinations outside code fences and
// code spans.
func markdownTargets(data []byte) []string {
	var targets []string
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
		line = codeSpan.ReplaceAllString(line, "")
		for _, match := range inlineLink.FindAllStringSubmatch(line, -1) {
			targets = append(targets, match[1])
		}
		if match := referenceLink.FindStringSubmatch(line); match != nil {
			targets = append(targets, match[1])
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

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
