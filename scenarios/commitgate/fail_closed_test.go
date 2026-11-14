package commitgate_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Requirement "Fail closed", scenario "`cw` is missing".
func TestCWIsMissing(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("builds a PATH of symbolic links")
	}
	g := newGate(t)
	g.Write("src/app.txt", "ok")
	before := g.head()

	// A PATH that holds git and sh and nothing else, so no cw.
	bin := t.TempDir()
	for _, name := range []string{"git", "sh"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	env := append(g.Env(), "PATH="+bin)
	command := exec.Command(filepath.Join(bin, "git"), "commit", "-a", "-m", "change")
	command.Dir, command.Env = g.Dir, env
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	var exit *exec.ExitError
	if err == nil {
		t.Errorf("git commit exited 0 without cw on PATH")
	} else if !errors.As(err, &exit) {
		t.Fatal(err)
	}
	output := stdout.String() + stderr.String()
	if after := g.head(); after != before {
		t.Errorf("git recorded a commit: HEAD moved from %s to %s", before, after)
	}
	if !strings.Contains(output, "not found") {
		t.Errorf("the output does not say that cw was not found:\n%s", output)
	}
	if !strings.Contains(output, "install") {
		t.Errorf("the output does not say where the install instruction is:\n%s", output)
	}
	if goCommand := regexp.MustCompile(`\bgo (install|build|run|get)\b`); goCommand.MatchString(output) {
		t.Errorf("the output names a Go command:\n%s", output)
	}
}

// Requirement "Fail closed", scenario "The declaration is invalid".
func TestInvalidDeclarationDenies(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("cw.toml", `version = 1

[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]

[[check]]
tiemout = "5m"
id = "lint"
run = ["sh", "lint.sh"]
`)
	if line := strings.Split(g.Read("cw.toml"), "\n")[8]; line != `tiemout = "5m"` {
		t.Fatalf("line 9 of cw.toml is %q", line)
	}
	g.Git("add", "cw.toml")
	result := g.commitDenied("pre-commit", "commit", "-m", "change")
	if !strings.Contains(result.Stderr, "cw.toml:9") {
		t.Errorf("the reason does not hold cw.toml:9:\n%s", result.Stderr)
	}
}

// Requirement "Fail closed", scenario "The declaration is gone".
func TestRemovedDeclarationDenies(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Git("rm", "-q", "cw.toml")
	result := g.commitDenied("pre-commit", "commit", "-m", "remove")
	assertLineWith(t, result.Stderr, "tree has no declaration")
}

// Requirement "Fail closed", scenario "The store cannot be read".
func TestUnreadableStoreDenies(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not make a directory unreadable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory of mode 000")
	}
	g := newGate(t)
	g.checked()
	store := g.StoreDir()
	info, err := os.Stat(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(store, info.Mode().Perm()); err != nil {
			t.Error(err)
		}
	})

	g.Write("src/app.txt", "ok")
	result := g.commitDenied("pre-commit", "commit", "-a", "-m", "change")
	if !strings.Contains(result.Stderr, store) {
		t.Errorf("the reason does not name %s:\n%s", store, result.Stderr)
	}
}
