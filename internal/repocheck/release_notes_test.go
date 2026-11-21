package repocheck

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The text of a release is a section of the changelog
// (.github/scripts/release-notes.sh): the section of the tag when the
// changelog has one, the unreleased section until then, and never nothing.
// The release job runs on Linux only, so the script is tested there and on
// macOS, not on Windows.
func TestReleaseNotes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the release job does not run on Windows")
	}
	t.Parallel()
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "scripts", "release-notes.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const changelog = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n" +
		"- A thing ([doc](docs/a.md#part)), see [site](https://example.test/x) and [above](#added).\n\n" +
		"## [1.2.0] - 2026-01-02\n\n- Old ([b](README.md)).\n\n## [1.1.0] - 2026-01-01\n\n" +
		"[Unreleased]: https://example.test/compare\n"
	tests := []struct {
		name, changelog, tag string
		code                 int
		want                 []string // parts of standard output, or of standard error when code is not 0
		not                  []string
	}{
		{name: "the section of the tag", changelog: changelog, tag: "v1.2.0",
			want: []string{"- Old ([b](https://github.com/o/r/blob/v1.2.0/README.md))."}, not: []string{"A thing", "## ", "example.test/compare"}},
		{name: "no section of the tag yet", changelog: changelog, tag: "v9.0.0-beta.1",
			want: []string{"### Added", "[doc](https://github.com/o/r/blob/v9.0.0-beta.1/docs/a.md#part)", "[site](https://example.test/x)", "[above](#added)"},
			not:  []string{"Old", "## ["}},
		{name: "a section of the tag that holds nothing", changelog: changelog, tag: "v1.1.0", code: 1,
			want: []string{"CHANGELOG.md has no text under ## [1.1.0]."}},
		{name: "nothing unreleased", changelog: "# Changelog\n\n## [Unreleased]\n\n## [1.0.0]\n\n- First.\n", tag: "v2.0.0", code: 1,
			want: []string{"CHANGELOG.md has no text under ## [Unreleased]."}},
		{name: "no tag", changelog: changelog, tag: "", code: 2, want: []string{"Usage: release-notes.sh TAG"}},
		{name: "an image", changelog: "## [Unreleased]\n\n![shot](docs/shot.png) and [doc](docs/a.md)\n", tag: "v1.0.0",
			want: []string{"![shot](https://github.com/o/r/raw/v1.0.0/docs/shot.png)", "[doc](https://github.com/o/r/blob/v1.0.0/docs/a.md)"}},
		{name: "a heading inside a fenced block", changelog: "## [Unreleased]\n\n- Before.\n\n```text\n## [1.0.0]\n```\n\n- After.\n\n## [1.0.0]\n\n- Old.\n", tag: "v2.0.0",
			want: []string{"- Before.", "- After."}, not: []string{"Old"}},
		{name: "a reference link", changelog: "## [Unreleased]\n\n- See [the guide][guide].\n\n[guide]: docs/a.md\n", tag: "v1.0.0", code: 1,
			want: []string{"a link that the release page could not resolve", "[the guide][guide]"}},
		{name: "a target in angle brackets", changelog: "## [Unreleased]\n\n- See [a](<docs/a b.md>).\n", tag: "v1.0.0", code: 1,
			want: []string{"a link that the release page could not resolve"}},
		{name: "a target without a scheme", changelog: "## [Unreleased]\n\n- See [w](www.example.test).\n", tag: "v1.0.0", code: 1,
			want: []string{"a link that the release page could not resolve"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(test.changelog), 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			cmd := exec.Command("sh", script, test.tag)
			cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &stdout, &stderr
			cmd.Env = append(os.Environ(), "GITHUB_REPOSITORY=o/r")
			code := 0
			if err := cmd.Run(); err != nil {
				code = cmd.ProcessState.ExitCode()
			}
			if code != test.code {
				t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", code, test.code, stdout.String(), stderr.String())
			}
			got := stdout.String()
			if code != 0 {
				if got != "" {
					t.Errorf("a refusal printed on standard output:\n%s", got)
				}
				got = stderr.String()
			}
			for _, part := range test.want {
				if !strings.Contains(got, part) {
					t.Errorf("the output lacks %q:\n%s", part, got)
				}
			}
			for _, part := range test.not {
				if strings.Contains(got, part) {
					t.Errorf("the output holds %q:\n%s", part, got)
				}
			}
		})
	}
}

// installedTag is the line of the release-install document's instruction that
// names the tag to download.
var installedTag = regexp.MustCompile(`(?m)^tag=(v\S+)$`)

// The future download recipes and plugin metadata must describe the same
// release. In particular, a renamed binary must not reuse an old tag.
func TestPreparedReleaseVersions(t *testing.T) {
	t.Parallel()
	installation, err := os.ReadFile(filepath.Join(repoRoot, "docs", "release-install.md"))
	if err != nil {
		t.Fatal(err)
	}
	tags := installedTag.FindAllSubmatch(installation, -1)
	if len(tags) != 1 {
		t.Fatalf("release-install.md has %d shell install tags, want one", len(tags))
	}
	tag := string(tags[0][1])
	if !bytes.Contains(installation, []byte("$tag = \""+tag+"\"")) {
		t.Errorf("the Windows recipe does not install %s", tag)
	}
	const team = "plugins/kitsu-team/"
	for file, name := range map[string]string{".claude-plugin/plugin.json": "kitsu", ".claude-plugin/marketplace.json": "kitsu", ".codex-plugin/plugin.json": "kitsu",
		team + ".claude-plugin/plugin.json": "kitsu-team", team + ".codex-plugin/plugin.json": "kitsu-team"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Name, Version string
			Metadata      struct{ Version string }
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		version := manifest.Version
		if version == "" {
			version = manifest.Metadata.Version
		}
		if manifest.Name != name || "v"+version != tag {
			t.Errorf("%s names %s %s, want %s %s", file, manifest.Name, version, name, tag)
		}
	}
	if tag == "v3.0.0-beta.1" {
		t.Error("v3.0.0-beta.1 contains cw archives and cannot be used for Kitsu downloads")
	}
}

// The changelog gives notes for the tag that the release-install document
// installs, and the notes of the first release of this code list the
// breaking changes.
func TestReleaseNotesOfThisRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the release job does not run on Windows")
	}
	t.Parallel()
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "scripts", "release-notes.sh"))
	if err != nil {
		t.Fatal(err)
	}
	installation, err := os.ReadFile(filepath.Join(repoRoot, "docs", "release-install.md"))
	if err != nil {
		t.Fatal(err)
	}
	found := installedTag.FindSubmatch(installation)
	if found == nil {
		t.Fatal("docs/release-install.md has no line `tag=v...` in its install instruction")
	}
	notes := func(tag string) string {
		cmd := exec.Command("sh", script, tag)
		cmd.Dir = repoRoot
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("release-notes.sh %s: %v", tag, err)
		}
		return string(out)
	}
	for line := range strings.SplitSeq(notes(string(found[1])), "\n") {
		if strings.Contains(line, "](docs/") || strings.Contains(line, "](README.md") {
			t.Errorf("a relative link is left in the notes: %s", line)
		}
	}
	const first = "v3.0.0-beta.1"
	if out := notes(first); !strings.Contains(out, "### Breaking") {
		t.Errorf("the notes of %s do not list the breaking changes:\n%.400s", first, out)
	}
}
