package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
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
	for _, dir := range []string{".git/hooks", ".git/kitsu/runs", "sub", ".githooks"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "kitsu.toml"), []byte("version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := gitx.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return repo, root
}

type editCase struct{ path, dir, want string }

func TestGuardRoleSettingCommands(t *testing.T) {
	t.Parallel()
	_, root := guardRepo(t, "")
	for _, command := range []string{
		"kitsu roles set developer --host codex --model custom",
		"env KITSU=1 command kitsu roles reset developer --host codex",
		`bash -lc 'kitsu roles set reviewer --host claude --reasoning high'`,
		`& 'C:\tools\kitsu.exe' roles reset architect --host codex`,
	} {
		call := toolCall{Tool: "Bash", Dir: root}
		call.Input.Command = command
		if got, _ := guardReason(context.Background(), call); !strings.Contains(got, "role models or reasoning") {
			t.Errorf("guard accepted %q: %q", command, got)
		}
	}
	for _, command := range []string{"kitsu roles --host codex", "kitsu roles text developer", "echo kitsu roles set developer"} {
		call := toolCall{Tool: "Bash", Dir: root}
		call.Input.Command = command
		if got, _ := guardReason(context.Background(), call); got != "" {
			t.Errorf("guard rejected read-only command %q: %q", command, got)
		}
	}
}

func TestGuardAllowsDirectConfiguredRoleSettings(t *testing.T) {
	_, root := guardRepo(t, "")
	uxTestWrite(t, filepath.Join(root, "kitsu-roles.toml"), workflowRoleFixture)
	for _, tc := range []struct {
		command string
		allowed bool
	}{
		{"kitsu role init --json", true},
		{"kitsu role set reviewer --host claude --model opus --reasoning high --json", true},
		{"kitsu role set developer --reasoning ''", true},
		{"kitsu.exe role set reviewer --host codex", true},
		{"kitsu role set reviewer --host claude --global", false},
		{"kitsu role set reviewer --skills custom --root outside", false},
		{"kitsu role set reviewer --host claude && kitsu check trust", false},
		{"kitsu role set reviewer --model ${MODEL}", false},
		{"kitsu config set host codex --global", false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			call := toolCall{Tool: "Bash", Dir: root}
			call.Input.Command = tc.command
			if reason, _ := guardReason(t.Context(), call); (reason == "") != tc.allowed {
				t.Fatalf("allowed=%v, got %q", tc.allowed, reason)
			}
		})
	}
}

func checkEdits(t *testing.T, repo *gitx.Repo, cases []editCase) {
	t.Helper()
	for _, c := range cases {
		got := editReason(context.Background(), repo, c.dir, []string{c.path})
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("editReason(%q in %q) = %q, want %q", c.path, c.dir, got, c.want)
		}
	}
}

// An edit of kitsu.toml, of a git hook, of the git configuration or of a record
// of kitsu needs the user; an edit of another file, also one whose name starts
// like those, does not.
func TestEditReason(t *testing.T) {
	t.Parallel()
	repo, root := guardRepo(t, "")
	common := filepath.Join(root, ".git")
	cases := []editCase{
		{"kitsu.toml", root, "kitsu.toml"},
		{"kitsu-advisers.toml", root, "adviser commands"}, // creation, before the file exists
		{"kitsu-roles.toml", root, "role models"},
		{".claude/settings.json", root, "protected host"},
		{"sub/kitsu-advisers.toml", root, ""},
		{"kitsu-advisers.toml.backup", root, ""},
		{"../kitsu.toml", filepath.Join(root, "sub"), "kitsu.toml"},
		{"sub/../kitsu.toml", root, "kitsu.toml"},
		{filepath.Join(common, "hooks", "pre-commit"), root, "git hook"},
		{".git/hooks/pre-push", root, "git hook"},
		{".git/hooks/new/dir/x", root, "git hook"},
		{".git/hooks", root, "git hook"},
		{".git/kitsu/runs/x.json", root, "records of kitsu"},
		{".git/kitsu/new/dir/x", root, "records of kitsu"},
		{".git/config", root, "git configuration"},
		{".git/hooks-old/x", root, ""},
		{".git/config.lock.old", root, ""},
		{".githooks/pre-commit", root, ""}, // core.hooksPath is not set here
		{"sub/kitsu.toml", root, ""},
		{"README.md", root, ""},
		{"", root, ""},
	}
	// The repository reached through a link, as under the temporary directory
	// of macOS, and the declaration reached through a link to the file.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err == nil {
		alias := filepath.Join(root, "alias.toml")
		if err := os.Symlink("kitsu.toml", alias); err != nil {
			t.Fatal(err)
		}
		// A protected host path can also name a file with a specific diagnostic.
		hostConfig := filepath.Join(root, ".claude")
		if err := os.Mkdir(hostConfig, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../kitsu.toml", filepath.Join(hostConfig, "alias.toml")); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, []editCase{
			{".git/kitsu/new/dir/x", link, "records of kitsu"},
			{".git/hooks/new/dir/x", link, "git hook"},
			{filepath.Join(link, ".git", "kitsu", "new", "x"), root, "records of kitsu"},
			{"new/dir/kitsu.toml", link, ""},
			{"kitsu.toml", link, "kitsu.toml"},
			{"alias.toml", root, "kitsu.toml"},
			{".claude/alias.toml", root, "kitsu.toml"},
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
	if _, err := os.Stat(filepath.Join(root, "Kitsu.TOML")); err != nil {
		if runtime.GOOS == "windows" {
			t.Fatalf("Windows does not find kitsu.toml as Kitsu.TOML: %v", err)
		}
		t.Skip("this file system respects the case of a name")
	}
	checkEdits(t, repo, []editCase{
		{"Kitsu.TOML", root, "kitsu.toml"},
		{"Kitsu.Toml", root, "kitsu.toml"},
		{".GIT/HOOKS/pre-commit", root, "git hook"},
		{".git/Kitsu/runs/x.json", root, "records of kitsu"},
	})
}
