package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSetupChecksArgumentsAndBundleBeforeLaunchingHost(t *testing.T) {
	dir := onboardingFixture(t)
	program, hostCapture := onboardingFakeHost(t)
	dataHome := filepath.Join(dir, "user data")
	t.Setenv("PATH", filepath.Dir(program))
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("LOCALAPPDATA", dataHome)
	archive := filepath.Join(dir, "local bundle")
	if err := os.Mkdir(archive, 0o700); err != nil {
		t.Fatal(err)
	}
	sums := strings.Repeat("0", 64) + "  kitsu-plugin.zip\n"
	for name, data := range map[string]string{"SHA256SUMS": sums, "kitsu-plugin.zip": "wrong plugin bytes"} {
		if err := os.WriteFile(filepath.Join(archive, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		args []string
		code int
		want string
	}{
		{args: []string{"--host", "codex"}, code: ExitUsage, want: "--archive-dir is required"},
		{args: []string{"--host", "other", "--archive-dir", archive}, code: ExitUsage, want: "host must be codex or claude"},
		{args: []string{"--host", "codex", "--host", "claude", "--archive-dir", archive}, code: ExitUsage, want: "select exactly one --host"},
		{args: []string{"--host", "codex", "--archive-dir", archive, "--archive-dir", archive}, code: ExitUsage, want: "supply one nonempty --archive-dir"},
		{args: []string{"--host", "codex", "--archive-dir", archive}, code: ExitFailure, want: "checksum mismatch"},
	}
	for _, test := range tests {
		var out, diagnostics bytes.Buffer
		args := append([]string{"agent", "setup"}, test.args...)
		err := Execute(t.Context(), args, Config{Out: &out, Err: &diagnostics})
		if ExitCode(err) != test.code || !strings.Contains(err.Error(), test.want) || out.Len() != 0 {
			t.Fatalf("setup %v = %v, %q, want exit %d with %q", args, err, &out, test.code, test.want)
		}
	}
	for _, path := range []string{hostCapture, dataHome} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("setup touched %q before verifying inputs: %v", path, err)
		}
	}
}
