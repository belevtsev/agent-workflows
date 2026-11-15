// Package messages holds tests of what kitsu says in cases that no scenario of
// the specifications covers. Each was found by a reader of the program, and
// each printed a sentence that was not true.
package messages

import (
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func wants(t *testing.T, what string, got kitsutest.Result, code int, holds ...string) {
	t.Helper()
	if got.Code != code {
		t.Errorf("%s: exit %d, want %d:\n%s", what, got.Code, code, got.Output())
	}
	for _, text := range holds {
		if !strings.Contains(got.Output(), text) {
			t.Errorf("%s: the output does not hold %q:\n%s", what, text, got.Output())
		}
	}
}

func lacks(t *testing.T, what string, got kitsutest.Result, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(got.Output(), text) {
			t.Errorf("%s: the output holds %q:\n%s", what, text, got.Output())
		}
	}
}

// kitsu init does not say that it installed a hook another tool wrote.
func TestInitNamesAHookItLeftAlone(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the hook of the other tool is a POSIX shell script")
	}
	repo := kitsutest.NewRepo(t)
	hook := ".git/hooks/pre-commit"
	writeProgram(t, repo.Path(hook), "#!/bin/sh\necho other tool\nkitsu hook pre-commit \"$@\"\n")
	got := repo.Kitsu("init")
	wants(t, "kitsu init", got, 0, "installed the hooks pre-merge-commit, pre-push", "they call kitsu already: pre-commit")
	lacks(t, "kitsu init", got, "installed the hooks pre-commit")
}

// kitsu uninstall does not say that it removed what was never there.
func TestUninstallWhereNothingIs(t *testing.T) {
	t.Parallel()
	repo := kitsutest.NewRepo(t)
	got := repo.Kitsu("uninstall")
	wants(t, "kitsu uninstall", got, 0, "nothing to remove")
	lacks(t, "kitsu uninstall", got, "removed the hooks", "is left as it is")

	wants(t, "kitsu init", repo.Kitsu("init"), 0)
	wants(t, "kitsu uninstall after kitsu init", repo.Kitsu("uninstall"), 0, "removed the hooks kitsu wrote", "kitsu.toml is left as it is")
}

// An unknown option is named as it was typed.
func TestUnknownOptionIsNamedAsTyped(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	wants(t, "kitsu run --nosuchflag", repo.Kitsu("run", "--nosuchflag"), 2, "unknown option: --nosuchflag")
	wants(t, "kitsu run -x", repo.Kitsu("run", "-x"), 2, "unknown option: -x")
}

// Where core.hooksPath is set kitsu writes no hook, so no text says that
// `kitsu init` installs them.
func TestHooksPathAdvice(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	repo.Git("config", "core.hooksPath", ".githooks")
	wants(t, "kitsu init", repo.Kitsu("init"), 4, "core.hooksPath is set, so kitsu wrote no hook")
	context := repo.Kitsu("context")
	wants(t, "kitsu context", context, 0, "core.hooksPath", "prints the line to add")
	lacks(t, "kitsu context", context, "installs them")
	status := repo.Kitsu("status")
	wants(t, "kitsu status", status, status.Code, "core.hooksPath", "prints the line to add")
	lacks(t, "kitsu status", status, "are not installed in this clone")
}

// An index that holds no declaration is not said to lack runs.
func TestStatusOfAnIndexWithoutADeclaration(t *testing.T) {
	t.Parallel()
	repo := kitsutest.NewRepo(t)
	wants(t, "kitsu init", repo.Kitsu("init"), 0)
	wants(t, "kitsu add", repo.Kitsu("add", "ok", "--", "git", "--version"), 0)
	got := repo.Kitsu("status")
	wants(t, "kitsu status", got, 5, "a commit of the index alone would be denied: the tree has no declaration", "git add -A")
	lacks(t, "kitsu status", got, "its content has no passing run of every check")
	if !strings.Contains(repo.Kitsu("status", "--json").Stdout, `"index_problem"`) {
		t.Error("kitsu status --json does not hold index_problem")
	}
}

// A check whose program is ended from outside is not said to have failed
// to start.
func TestACheckEndedByASignal(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sends a POSIX signal")
	}
	repo := kitsutest.NewRepo(t)
	wants(t, "kitsu init", repo.Kitsu("init"), 0)
	// The check writes its process id and waits to be ended.
	wants(t, "kitsu add", repo.Kitsu("add", "waits", "--", "sh", "-c", "echo $$ > pid; exec sleep 60"), 0)
	repo.Write(".gitignore", "pid\n")
	done := make(chan kitsutest.Result, 1)
	go func() { done <- repo.Kitsu("run") }()
	deadline := time.Now().Add(20 * time.Second)
	for !repo.Exists("pid") || strings.TrimSpace(repo.Read("pid")) == "" {
		if time.Now().After(deadline) {
			t.Fatal("the check did not start within 20 seconds")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var pid int
	for _, c := range strings.TrimSpace(repo.Read("pid")) {
		pid = pid*10 + int(c-'0')
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	got := <-done
	wants(t, "kitsu run", got, 5, "did not exit on its own")
	lacks(t, "kitsu run", got, "could not be started")

	wants(t, "kitsu add", repo.Kitsu("add", "gone", "--", "no-such-program-of-this-test"), 0)
	wants(t, "kitsu run gone", repo.Kitsu("run", "gone"), 5, "could not be started")
}
