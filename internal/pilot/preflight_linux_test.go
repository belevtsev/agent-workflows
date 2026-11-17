package pilot

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var preflightCases = flag.String("pilot-preflight-cases", "", "public cases JSON for explicit no-model runtime validation")
var preflightOutput = flag.String("pilot-preflight-output", "", "private retained no-model preflight evidence")

// TestPilotPublicRuntimes executes no model. It checks actual approved commands
// with the readonly dependency mounts that the solver will receive.
func TestPilotPublicRuntimes(t *testing.T) {
	if *preflightCases == "" || *preflightOutput == "" {
		t.Skip("explicit public case catalog and private preflight output required")
	}
	data, err := os.ReadFile(*preflightCases)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Cases []struct {
			ID       string `json:"id"`
			Snapshot string `json:"snapshot"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil || len(catalog.Cases) != 12 {
		t.Fatalf("expected twelve public cases: %v", err)
	}
	for _, c := range catalog.Cases {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			if !slug.MatchString(c.ID) {
				t.Fatal("unsafe case identity")
			}
			root := filepath.Join(*preflightOutput, c.ID)
			work, home := filepath.Join(root, "work"), filepath.Join(root, "home")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := copySnapshot(c.Snapshot, work); err != nil {
				t.Fatal(err)
			}
			env := executionEnv(pilotHost{Env: map[string]string{"PATH": "/usr/bin:/bin"}}, map[string]string{"home": home})
			for i, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "Public preflight baseline"}} {
				call, err := invoke(t.Context(), work, filepath.Join(root, "git-"+string(rune('0'+i))), "", append([]string{"/usr/bin/git"}, args...), env, 60)
				if err != nil || call.ExitCode != 0 {
					t.Fatalf("Git preparation: exit=%d error=%v evidence=%s", call.ExitCode, err, root)
				}
			}
			for i, args := range [][]string{{"gate", "init"}, {"check", "run", "--compact"}} {
				argv := publicPreflightArgs(c.ID, work, home, args)
				call, err := invoke(t.Context(), work, filepath.Join(root, "runtime-"+string(rune('0'+i))), "", argv, env, 300)
				if writeErr := writeJSON(filepath.Join(root, "runtime-"+string(rune('0'+i))+".json"), call, true); writeErr != nil {
					t.Fatal(writeErr)
				}
				if err != nil || call.ExitCode != 0 || call.Stopped || call.Cleanup != "" {
					t.Fatalf("public runtime: exit=%d error=%v evidence=%s", call.ExitCode, err, root)
				}
			}
		})
	}
}

func publicPreflightArgs(id, work, home string, command []string) []string {
	runtime := filepath.Join("/tmp/kitsu-pilot-runtime", id)
	args := []string{"/usr/bin/bwrap", "--unshare-all", "--die-with-parent", "--new-session", "--clearenv",
		"--ro-bind", "/usr", "/usr", "--ro-bind", "/lib", "/lib", "--ro-bind", "/lib64", "/lib64",
		"--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin", "--proc", "/proc", "--dev", "/dev",
		"--tmpfs", "/tmp", "--dir", "/etc", "--dir", "/home", "--dir", "/opt",
		"--ro-bind", "/etc/alternatives", "/etc/alternatives",
		"--ro-bind", "/etc/hosts", "/etc/hosts", "--ro-bind", "/etc/nsswitch.conf", "/etc/nsswitch.conf",
		"--ro-bind", "/tmp/kitsu-codex-pilot/bin", "/opt/tools",
		"--ro-bind", filepath.Join(runtime, "go"), "/opt/go",
		"--ro-bind", filepath.Join(runtime, "node"), "/opt/node",
		"--ro-bind", filepath.Join(runtime, "mod"), "/opt/gomod",
		"--bind", home, "/home/solver", "--bind", work, "/workspace",
		"--ro-bind", filepath.Join(runtime, "node_modules"), "/workspace/node_modules", "--chdir", "/workspace"}
	for _, pair := range [][2]string{
		{"HOME", "/home/solver"}, {"PATH", "/opt/tools:/opt/go/bin:/opt/node/bin:/usr/bin:/bin"},
		{"LANG", "C.UTF-8"}, {"TZ", "UTC"}, {"CI", "1"}, {"GOTOOLCHAIN", "local"},
		{"GOCACHE", "/tmp/go-build"}, {"GOMODCACHE", "/opt/gomod"}, {"GOMAXPROCS", "2"},
		{"GOFLAGS", "-p=2"}, {"GOWORK", "off"}, {"GOPROXY", "off"}, {"GOSUMDB", "off"},
		{"GIT_CONFIG_NOSYSTEM", "1"}, {"GIT_CONFIG_GLOBAL", "/dev/null"},
		{"GIT_AUTHOR_NAME", "Kitsu Pilot"}, {"GIT_COMMITTER_NAME", "Kitsu Pilot"},
		{"GIT_AUTHOR_EMAIL", "pilot@example.invalid"}, {"GIT_COMMITTER_EMAIL", "pilot@example.invalid"},
		{"npm_config_update_notifier", "false"}, {"npm_config_audit", "false"},
	} {
		args = append(args, "--setenv", pair[0], pair[1])
	}
	return append(append(args, "--", "/opt/tools/kitsu"), command...)
}
