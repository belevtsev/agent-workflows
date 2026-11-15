package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gitx"
)

// guardRepo is a new repository with a declaration, opened as the guard
// opens it. hooksPath, when it is not empty, is set as core.hooksPath.
func guardRepo(t *testing.T, hooksPath string) (*gitx.Repo, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	commands := [][]string{{"init", "-q", "."}}
	if hooksPath != "" {
		commands = append(commands, []string{"config", "core.hooksPath", hooksPath})
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for _, dir := range []string{".git/hooks", ".git/gatecub/runs", "sub", ".githooks"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "gatecub.toml"), []byte("version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := gitx.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return repo, root
}

type editCase struct{ path, dir, want string }

func checkEdits(t *testing.T, repo *gitx.Repo, cases []editCase) {
	t.Helper()
	for _, c := range cases {
		got := editReason(context.Background(), repo, c.dir, []string{c.path})
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("editReason(%q in %q) = %q, want %q", c.path, c.dir, got, c.want)
		}
	}
}

// An edit of gatecub.toml, of a git hook, of the git configuration or of a record
// of gatecub needs the user; an edit of another file, also one whose name starts
// like those, does not.
func TestEditReason(t *testing.T) {
	t.Parallel()
	repo, root := guardRepo(t, "")
	common := filepath.Join(root, ".git")
	cases := []editCase{
		{"gatecub.toml", root, "gatecub.toml"},
		{"../gatecub.toml", filepath.Join(root, "sub"), "gatecub.toml"},
		{"sub/../gatecub.toml", root, "gatecub.toml"},
		{filepath.Join(common, "hooks", "pre-commit"), root, "git hook"},
		{".git/hooks/pre-push", root, "git hook"},
		{".git/hooks/new/dir/x", root, "git hook"},
		{".git/hooks", root, "git hook"},
		{".git/gatecub/runs/x.json", root, "records of gatecub"},
		{".git/gatecub/new/dir/x", root, "records of gatecub"},
		{".git/config", root, "git configuration"},
		{".git/hooks-old/x", root, ""},
		{".git/config.lock.old", root, ""},
		{".githooks/pre-commit", root, ""}, // core.hooksPath is not set here
		{"sub/gatecub.toml", root, ""},
		{"README.md", root, ""},
		{"", root, ""},
	}
	// The repository reached through a link, as under the temporary directory
	// of macOS, and the declaration reached through a link to the file.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err == nil {
		alias := filepath.Join(root, "alias.toml")
		if err := os.Symlink("gatecub.toml", alias); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, []editCase{
			{".git/gatecub/new/dir/x", link, "records of gatecub"},
			{".git/hooks/new/dir/x", link, "git hook"},
			{filepath.Join(link, ".git", "gatecub", "new", "x"), root, "records of gatecub"},
			{"new/dir/gatecub.toml", link, ""},
			{"gatecub.toml", link, "gatecub.toml"},
			{"alias.toml", root, "gatecub.toml"},
		}...)
	} else {
		t.Logf("no symbolic link could be made, so those cases are left out: %v", err)
	}
	checkEdits(t, repo, cases)
}

// Where core.hooksPath names a directory, its files are the hooks git runs,
// so an edit of one needs the user as well.
func TestEditReasonFollowsHooksPath(t *testing.T) {
	t.Parallel()
	repo, root := guardRepo(t, ".githooks")
	checkEdits(t, repo, []editCase{
		{".githooks/pre-commit", root, "git hook"},
		{".githooks/new/x", root, "git hook"},
		{".git/hooks/pre-commit", root, "git hook"},
		{"sub/pre-commit", root, ""},
	})
}

// A file system that ignores the case of a name holds one file under every
// spelling, and the guard sees that file. Windows and the default file
// system of macOS do.
func TestEditReasonSeesAnotherSpellingOfAName(t *testing.T) {
	t.Parallel()
	repo, root := guardRepo(t, "")
	if _, err := os.Stat(filepath.Join(root, "Gatecub.TOML")); err != nil {
		if runtime.GOOS == "windows" {
			t.Fatalf("Windows does not find gatecub.toml as Gatecub.TOML: %v", err)
		}
		t.Skip("this file system respects the case of a name")
	}
	checkEdits(t, repo, []editCase{
		{"Gatecub.TOML", root, "gatecub.toml"},
		{"Cw.Toml", root, "gatecub.toml"},
		{".GIT/HOOKS/pre-commit", root, "git hook"},
		{".git/Gatecub/runs/x.json", root, "records of gatecub"},
	})
}
