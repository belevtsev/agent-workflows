package cli

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// TestRelativeXDGStateHomeIsRefused: the XDG Base Directory specification
// makes a relative XDG_STATE_HOME invalid, and cw refuses it, as
// internal/userconfig refuses a relative XDG_CONFIG_HOME. Joined to the
// working directory, it would move the task store, and the gate that reads
// it, with every cd.
func TestRelativeXDGStateHomeIsRefused(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	// The working directory is outside the repository, as after a cd, so a
	// store joined to it is not refused as one inside the repository.
	cwd := filepath.Join(base, "elsewhere")
	for _, dir := range []string{repo, cwd} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(base, "home")
	configure := func(t *testing.T) (Config, *bytes.Buffer) {
		t.Helper()
		config, out := testConfig(t)
		config.HomeDir = func() (string, error) { return home, nil }
		config.WorkingDir = func() (string, error) { return cwd, nil }
		config.Getenv = func(key string) string {
			if key == "XDG_STATE_HOME" {
				return "relative-state"
			}
			return ""
		}
		return config, out
	}
	start := func(t *testing.T, flags ...string) ([]byte, error) {
		t.Helper()
		config, out := configure(t)
		config.In = strings.NewReader(`{"title":"Relative state","kind":"small","requirements":[{"id":"source","description":"Read source","allow_non_execution":true}],"checks":[]}`)
		args := append([]string{"task", "start", "--repo", repo, "--plan", "-", "--policy", shippedPolicyPath}, flags...)
		err := Execute(t.Context(), args, config)
		return out.Bytes(), err
	}
	refused := func(t *testing.T, command string, out []byte, err error) {
		t.Helper()
		if err == nil || ExitCode(err) != ExitFailure || !strings.Contains(err.Error(), "XDG_STATE_HOME") {
			t.Fatalf("%s = %v (exit %d), want exit %d naming XDG_STATE_HOME", command, err, ExitCode(err), ExitFailure)
		}
		if len(out) != 0 {
			t.Fatalf("%s printed %q, want nothing", command, out)
		}
	}

	t.Run("task start is refused", func(t *testing.T) {
		// Oracle: exit status 1 (invalid input) with an error that names the
		// variable, no record on stdout, and no store under the working
		// directory, the repository or the default state directory.
		out, err := start(t)
		refused(t, "task start", out, err)
		for _, path := range []string{filepath.Join(cwd, "relative-state"), filepath.Join(repo, "relative-state"), filepath.Join(home, ".local", "state")} {
			if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("%s exists after a refused start (%v)", path, err)
			}
		}
	})

	t.Run("the default cost history is refused", func(t *testing.T) {
		// Oracle: the cost history lives in the same state directory, so
		// cw metrics history refuses the variable the same way.
		config, out := configure(t)
		err := Execute(t.Context(), []string{"metrics", "history"}, config)
		refused(t, "metrics history", out.Bytes(), err)
	})

	t.Run("--state-dir takes precedence and the variable is not read", func(t *testing.T) {
		// Oracle: --state-dir comes before XDG_STATE_HOME (docs/operations.md,
		// Paths), so the relative variable is never read and the record lands
		// in the explicit store.
		explicit := filepath.Join(base, "explicit-state")
		out, err := start(t, "--state-dir", explicit)
		if err != nil {
			t.Fatalf("task start with --state-dir = %v", err)
		}
		var record taskflow.Record
		if err := json.Unmarshal(out, &record); err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(filepath.Join(explicit, "workflow-tasks"))
		if err != nil {
			t.Fatal(err)
		}
		if got := filepath.Dir(record.Directory); got != want {
			t.Fatalf("store = %s, want %s", got, want)
		}
	})
}
