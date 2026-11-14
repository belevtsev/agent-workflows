package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An edit of cw.toml, of a git hook or of a record of cw needs the user; an
// edit of another file, also one whose name starts like those, does not.
func TestEditReason(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	common := filepath.Join(root, ".git")
	for _, dir := range []string{filepath.Join(common, "hooks"), filepath.Join(common, "cw", "runs"), filepath.Join(root, "sub")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	declaration := filepath.Join(root, "cw.toml")
	cases := []struct{ path, dir, want string }{
		{"cw.toml", root, "cw.toml"},
		{"../cw.toml", filepath.Join(root, "sub"), "cw.toml"},
		{filepath.Join(common, "hooks", "pre-commit"), root, "git hook"},
		{".git/hooks/pre-push", root, "git hook"},
		{".git/cw/runs/x.json", root, "records of cw"},
		{".git/cw/new/dir/x", root, "records of cw"},
		{".git/hooks", root, ""},
		{".git/hooks-old/x", root, ""},
		{".git/config", root, ""},
		{"sub/cw.toml", root, ""},
		{"README.md", root, ""},
		{"", root, ""},
	}
	// The repository reached through a link, as under the temporary directory
	// of macOS: a directory the edit would create cannot be resolved itself.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err == nil {
		cases = append(cases, []struct{ path, dir, want string }{
			{".git/cw/new/dir/x", link, "records of cw"},
			{".git/hooks/new/dir/x", link, "git hook"},
			{filepath.Join(link, ".git", "cw", "new", "x"), root, "records of cw"},
			{"new/dir/cw.toml", link, ""},
			{"cw.toml", link, "cw.toml"},
		}...)
	} else {
		t.Logf("no symbolic link could be made, so those cases are left out: %v", err)
	}
	for _, c := range cases {
		got := editReason([]string{c.path}, c.dir, declaration, common)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("editReason(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
