package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// installerTemplate is the committed fixture source every installer fixture
// copies, and installerTemplateHead is its commit.
var installerTemplate, installerTemplateHead string

// TestMain sets the process-wide settings every test shares, so no test needs
// to change the environment, and builds the installer fixture source once.
func TestMain(m *testing.M) {
	for name, value := range map[string]string{"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1", "GIT_OPTIONAL_LOCKS": "0"} {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := os.Unsetenv("AGENT_WORKFLOWS_FAULT"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// macOS flushes the whole drive on every sync, which made syncing most of
	// this package's runtime. TestDurableWritesSync and the built binary's
	// install smoke test keep real syncs covered.
	syncFile = func(*os.File) error { return nil }
	root, err := os.MkdirTemp("", "workflow-fixture-template-")
	if err == nil {
		installerTemplate = filepath.Join(root, "source")
		installerTemplateHead, err = buildInstallerTemplate(installerTemplate)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "installer fixture template:", err)
		os.Exit(1)
	}
	code := m.Run()
	if err := os.RemoveAll(root); err != nil && code == 0 {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// TestDurableWritesSync runs atomicWrite with real syncs. It stays serial, and
// parallel tests start only after every serial test, so swapping syncFile here
// cannot race with them.
func TestDurableWritesSync(t *testing.T) {
	directory := Normalize(t.TempDir())
	target := filepath.Join(directory, "state.json")
	restore := syncFile
	t.Cleanup(func() { syncFile = restore })
	var synced []string
	syncFile = func(f *os.File) error {
		synced = append(synced, f.Name())
		if f.Name() != directory && exists(target) {
			t.Errorf("%s was renamed into place before its contents were synced", target)
		}
		return f.Sync()
	}
	if err := atomicWrite(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(synced) != 2 || filepath.Dir(synced[0]) != directory || !strings.HasPrefix(filepath.Base(synced[0]), ".agent-workflows-") || synced[1] != directory {
		t.Fatalf("synced %q; want the temporary file, then %s", synced, directory)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("written file = %v, %v; want mode 0600", info, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "{}\n" {
		t.Fatalf("written contents = %q, %v", data, err)
	}
}

// buildInstallerTemplate writes and commits the fixture source, returning its
// commit. The dates are pinned so every run produces the same commit.
func buildInstallerTemplate(destination string) (string, error) {
	maintained, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return "", err
	}
	if err := installerBuildSource(maintained, destination); err != nil {
		return "", err
	}
	git := func(args ...string) (string, error) {
		command := exec.Command("git", append([]string{"-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", destination}, args...)...)
		command.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		output, err := command.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %v: %w: %s", args, err, output)
		}
		return strings.TrimSpace(string(output)), nil
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Installer Fixture"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "gc.auto", "0"},
		{"config", "maintenance.auto", "false"},
		{"add", "."},
		{"commit", "-m", "Maintained fixture suite"},
	} {
		if _, err := git(args...); err != nil {
			return "", err
		}
	}
	return git("rev-parse", "HEAD")
}
