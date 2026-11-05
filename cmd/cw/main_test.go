package main_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// payloadRefusal is the stable part of the R1.4 refusal.
const payloadRefusal = "has no embedded workflow payload"

// goTool finds the go command of the running toolchain, then PATH.
func goTool(t *testing.T) string {
	t.Helper()
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if root := runtime.GOROOT(); root != "" {
		candidate := filepath.Join(root, "bin", name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	path, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go tool not found: %v", err)
	}
	return path
}

// buildPlainCW builds cmd/cw with a plain go build, so the binary carries no
// release payload.
func buildPlainCW(t *testing.T) string {
	t.Helper()
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "cw")
	build := exec.CommandContext(t.Context(), goTool(t), "build", "-o", binary, "github.com/belevtsev/agent-workflows/cmd/cw")
	build.Dir = module
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build cmd/cw: %v\n%s", err, output)
	}
	return binary
}

// runCW runs the binary with a temporary HOME and a minimal environment: no
// XDG roots, host homes, adviser credentials or reachable proxy.
func runCW(t *testing.T, binary, home string, arguments ...string) (int, string, string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), binary, arguments...)
	command.Dir = t.TempDir()
	command.Env = []string{
		"HOME=" + home,
		"PATH=/usr/bin:/bin",
		"HTTPS_PROXY=http://127.0.0.1:9",
		"HTTP_PROXY=http://127.0.0.1:9",
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("cw %v did not run: %v", arguments, err)
		}
		code = exit.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

// requireEmptyHome accepts an absent or empty HOME only.
func requireEmptyHome(t *testing.T, home string) {
	t.Helper()
	entries, err := os.ReadDir(home)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("cw wrote under HOME: %v", names)
	}
}

// R1.4 rev 3: a cw built from source without the release payload refuses every
// lifecycle action with exit 1 before creating or changing anything under HOME,
// while version and the manager protocol still answer.
func TestPlainBuildRefusesLifecycleWithoutPayload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds cmd/cw")
	}
	binary := buildPlainCW(t)
	for _, arguments := range [][]string{
		{"status"}, {"install", "--dry-run"}, {"setup", "--dry-run"}, {"install"}, {},
		{"update", "--dry-run"}, {"rollback", "--dry-run"}, {"recover"}, {"uninstall"},
	} {
		name := strings.Join(arguments, "_")
		if name == "" {
			name = "bare"
		}
		t.Run(name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := runCW(t, binary, home, arguments...)
			if code != 1 || !strings.Contains(stderr, payloadRefusal) {
				t.Fatalf("cw %v: exit %d, stdout %q, stderr %q", arguments, code, stdout, stderr)
			}
			requireEmptyHome(t, home)
		})
	}
	t.Run("version", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		code, stdout, stderr := runCW(t, binary, home, "version")
		if code != 0 || !strings.HasPrefix(stdout, `{"arch":"`) {
			t.Fatalf("cw version: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		requireEmptyHome(t, home)
	})
	t.Run("manager_protocol", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		code, stdout, stderr := runCW(t, binary, home, "--manager-protocol")
		if code != 0 || stdout != "cw-manager-v10\n" {
			t.Fatalf("cw --manager-protocol: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		requireEmptyHome(t, home)
	})
}
