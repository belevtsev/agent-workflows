package skillcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// maxChangelogLine is the longest line the unreleased changelog may hold: one
// change per bullet, with the detail in the doc the bullet links to.
const maxChangelogLine = 200

// releasedHeading is the heading of a released version, such as
// "## [2.0.96] - 2026-11-07"; "## [Unreleased]" does not match.
var releasedHeading = regexp.MustCompile(`^## \[v?[0-9]+\.[0-9]+\.[0-9]+[^\]]*\]`)

// oracle: no line of CHANGELOG.md above the first released version heading is
// longer than 200 characters.
func TestChangelogLineLength(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	for n, line := range strings.Split(string(data), "\n") {
		if releasedHeading.MatchString(line) {
			return
		}
		if length := utf8.RuneCountInString(line); length > maxChangelogLine {
			t.Errorf("CHANGELOG.md:%d has %d characters, want at most %d: put the detail in the linked doc", n+1, length, maxChangelogLine)
		}
	}
}
