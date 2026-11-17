package cli

import (
	"errors"
	"os"
	"os/exec"
	"runtime"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
)

type workerToolScope struct {
	Host    string `json:"host"`
	Machine string `json:"machine"`
	Session string `json:"session"`
	Runtime string `json:"runtime_path_cwd"`
}

type workerToolLookup struct {
	State  string          `json:"state"`
	Scope  workerToolScope `json:"scope"`
	Policy string          `json:"policy"`
}

func newWorkerToolScope(host, worktree, machine, session string) workerToolScope {
	scope := workerToolScope{Host: host, Machine: machine, Session: session}
	directory, directoryErr := os.Getwd()
	executable, executableErr := os.Executable()
	if directoryErr != nil || executableErr != nil {
		return scope // Runtime scope is unknown; do not publish a reusable lookup.
	}
	// macOS can return a stale path after the current directory is removed.
	actual, actualErr := os.Stat(".")
	named, namedErr := os.Stat(directory)
	if actualErr != nil || namedErr != nil || !os.SameFile(actual, named) {
		return scope
	}
	mount, _ := os.Readlink("/proc/self/ns/mnt")
	scope.Runtime = contextpack.Key([]string{
		runtime.GOOS, runtime.GOARCH, executable, mount, worktree, directory,
		os.Getenv("PATH"), os.Getenv("PATHEXT"), os.Getenv("GODEBUG"),
	})
	return scope
}

func observeWorkerTool(scope workerToolScope) workerToolLookup {
	// LookPath inspects local files and PATH; it never runs a program or installs it.
	state := "unknown"
	if scope.Runtime != "" {
		_, err := exec.LookPath("rg")
		state = workerLookupState(err)
		if errors.Is(err, exec.ErrNotFound) {
			state = "missing"
		}
	}
	return workerToolLookup{
		State: state, Scope: scope,
		Policy: "Local LookPath only; no proof of execution or authority. Reuse only with matching host, machine, session and runtime/PATH/cwd; " +
			"unknown scope or mismatch means unknown. If missing, use search_fallback without retrying rg. " +
			"If unknown or unverifiable, use search_fallback directly: no hash computation or discovery needed. One permitted lookup remains optional. " +
			"On execution error fall back/revalidate; never probe or install.",
	}
}
