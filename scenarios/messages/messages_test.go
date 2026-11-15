// Package messages holds tests of what gatecub says in cases that no scenario of
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

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

func wants(t *testing.T, what string, got gatecubtest.Result, code int, holds ...string) {
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

func lacks(t *testing.T, what string, got gatecubtest.Result, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(got.Output(), text) {
			t.Errorf("%s: the output holds %q:\n%s", what, text, got.Output())
		}
	}
}

// gatecub init does not say that it installed a hook another tool wrote.
func TestInitNamesAHookItLeftAlone(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the hook of the other tool is a POSIX shell script")
	}
	repo := gatecubtest.NewRepo(t)
	hook := ".git/hooks/pre-commit"
	writeProgram(t, repo.Path(hook), "#!/bin/sh\necho other tool\ngatecub hook pre-commit \"$@\"\n")
	got := repo.Gatecub("init")
	wants(t, "gatecub init", got, 0, "installed the hooks pre-merge-commit, pre-push", "they call gatecub already: pre-commit")
	lacks(t, "gatecub init", got, "installed the hooks pre-commit")
}

// gatecub uninstall does not say that it removed what was never there.
func TestUninstallWhereNothingIs(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.NewRepo(t)
	got := repo.Gatecub("uninstall")
	wants(t, "gatecub uninstall", got, 0, "nothing to remove")
	lacks(t, "gatecub uninstall", got, "removed the hooks", "is left as it is")

	wants(t, "gatecub init", repo.Gatecub("init"), 0)
	wants(t, "gatecub uninstall after gatecub init", repo.Gatecub("uninstall"), 0, "removed the hooks gatecub wrote", "gatecub.toml is left as it is")
}

// An unknown option is named as it was typed.
func TestUnknownOptionIsNamedAsTyped(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	wants(t, "gatecub run --nosuchflag", repo.Gatecub("run", "--nosuchflag"), 2, "unknown option: --nosuchflag")
	wants(t, "gatecub run -x", repo.Gatecub("run", "-x"), 2, "unknown option: -x")
}

// Where core.hooksPath is set gatecub writes no hook, so no text says that
// `gatecub init` installs them.
func TestHooksPathAdvice(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	repo.Git("config", "core.hooksPath", ".githooks")
	wants(t, "gatecub init", repo.Gatecub("init"), 4, "core.hooksPath is set, so gatecub wrote no hook")
	context := repo.Gatecub("context")
	wants(t, "gatecub context", context, 0, "core.hooksPath", "prints the line to add")
	lacks(t, "gatecub context", context, "installs them")
	status := repo.Gatecub("status")
	wants(t, "gatecub status", status, status.Code, "core.hooksPath", "prints the line to add")
	lacks(t, "gatecub status", status, "are not installed in this clone")
}

// An index that holds no declaration is not said to lack runs.
func TestStatusOfAnIndexWithoutADeclaration(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.NewRepo(t)
	wants(t, "gatecub init", repo.Gatecub("init"), 0)
	wants(t, "gatecub add", repo.Gatecub("add", "ok", "--", "git", "--version"), 0)
	got := repo.Gatecub("status")
	wants(t, "gatecub status", got, 5, "a commit of the index alone would be denied: the tree has no declaration", "git add -A")
	lacks(t, "gatecub status", got, "its content has no passing run of every check")
	if !strings.Contains(repo.Gatecub("status", "--json").Stdout, `"index_problem"`) {
		t.Error("gatecub status --json does not hold index_problem")
	}
}

// A check whose program is ended from outside is not said to have failed
// to start.
func TestACheckEndedByASignal(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sends a POSIX signal")
	}
	repo := gatecubtest.NewRepo(t)
	wants(t, "gatecub init", repo.Gatecub("init"), 0)
	// The check writes its process id and waits to be ended.
	wants(t, "gatecub add", repo.Gatecub("add", "waits", "--", "sh", "-c", "echo $$ > pid; exec sleep 60"), 0)
	repo.Write(".gitignore", "pid\n")
	done := make(chan gatecubtest.Result, 1)
	go func() { done <- repo.Gatecub("run") }()
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
	wants(t, "gatecub run", got, 5, "did not exit on its own")
	lacks(t, "gatecub run", got, "could not be started")

	wants(t, "gatecub add", repo.Gatecub("add", "gone", "--", "no-such-program-of-this-test"), 0)
	wants(t, "gatecub run gone", repo.Gatecub("run", "gone"), 5, "could not be started")
}
