package doctor

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

func writeFile(t *testing.T, root, path, data string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitCommand(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(projectctx.MinimalEnvironment(os.Getenv), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func gitFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical, err := projectctx.CanonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	root = canonical
	gitCommand(t, root, "init", "-b", "main")
	gitCommand(t, root, "config", "user.email", "fixture@example.invalid")
	gitCommand(t, root, "config", "user.name", "Fixture")
	writeFile(t, root, "go.mod", "module example.invalid/fixture\n\ngo 1.27.1\n")
	writeFile(t, root, "main.go", "package main\nfunc main() {}\n")
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-qm", "initial")
	return root
}

func TestDoctorMetadataNeverRunsOptionalToolsAndSeparatesCredentials(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeFile(t, root, "web/package.json", "{}")
	writeFile(t, home, ".codex/config.toml", "[mcp_servers.gopls]\nenabled = false\ncommand = 'gopls'\n[mcp_servers.gopls.env]\nTYPESAFE_API_KEY = 'secret-launcher'\n[shell_environment_policy.set]\nOPENAI_API_KEY = 'secret-shell'\n")
	var lookups []string
	report, err := Doctor(context.Background(), DoctorOptions{Repo: root, Home: home, Getenv: func(key string) string {
		if key == "SUBQ_API_KEY" {
			return "secret-environment"
		}
		return ""
	}, LookPath: func(name string) (string, error) {
		lookups = append(lookups, name)
		return "/fake/" + name, nil
	}, Run: func(context.Context, projectctx.Command) (projectctx.CommandResult, error) {
		t.Fatal("metadata inspection executed an optional tool")
		return projectctx.CommandResult{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lookups, []string{"git", "cw", "jbcontext"}) || report.Checks["gopls_cli"].Status != "not_applicable" || report.Checks["graphify_cli"].Status != "not_applicable" {
		t.Fatalf("non-Go profile assumed global Go tools: %+v %+v", lookups, report.Checks)
	}
	if !report.Credentials.Environment["SUBQ_API_KEY"] || report.Credentials.Environment["OPENAI_API_KEY"] || !report.Credentials.Launcher["TYPESAFE_API_KEY"] || !report.Credentials.CodexShell["OPENAI_API_KEY"] {
		t.Fatal("credential source booleans were merged or lost")
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "secret-") {
		t.Fatalf("report leaked credential values: %v", err)
	}
	if report.Checks["mcp_gopls"].Status != "unchecked" || !strings.Contains(report.Checks["mcp_gopls"].Detail, "disabled") {
		t.Fatal("disabled MCP config was reported available")
	}
}

func TestDoctorLiveEvidenceAndNoRepositoryWrites(t *testing.T) {
	root, home := gitFixture(t), t.TempDir()
	before, err := projectctx.Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, ".venvs/agent-graphify/bin")
	writeFile(t, bin, "graphify", "fixture")
	if err := os.Chmod(filepath.Join(bin, "graphify"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []projectctx.Command
	run := func(_ context.Context, command projectctx.Command) (projectctx.CommandResult, error) {
		calls = append(calls, command)
		for _, env := range command.Env {
			if strings.Contains(env, "secret-") || strings.Contains(env, "API_KEY=") {
				t.Fatal("provider credential forwarded into probe")
			}
		}
		if command.Dir != root {
			t.Fatal("probe targeted installation source or an unrelated directory")
		}
		output := ""
		switch strings.Join(command.Args, " ") {
		case "status --json-output":
			output = fmtStatus(before.Head)
		case "watch status":
			output = "Watch daemon running (pid 123)\n  fixture-id  " + root + "\n  other-id  /other/private/repo\n"
		case "version":
			output = "golang.org/x/tools/gopls v0.21.0"
		case "mcp":
			output = "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n"
		case "--version":
			output = "graphify 0.9.103\n"
		default:
			t.Fatalf("unexpected probe: %+v", command)
		}
		return projectctx.CommandResult{Stdout: []byte(output)}, nil
	}
	report, err := Doctor(context.Background(), DoctorOptions{Repo: root, Home: home, Live: true, Run: run, LookPath: func(name string) (string, error) { return "/fake/" + name, nil }, Getenv: func(key string) string {
		if strings.HasSuffix(key, "API_KEY") {
			return "secret-provider"
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"jbcontext_index", "jbcontext_watch", "gopls_cli", "gopls_mcp", "graphify_version"} {
		if report.Checks[name].Status != "verified" {
			t.Fatalf("%s: %+v", name, report.Checks[name])
		}
	}
	// R3a
	if detail := report.Checks["graphify_version"].Detail; detail != "Graphify reports 0.9.103; graph construction and cache freshness were not queried" {
		t.Fatalf("graphify_version detail = %q", detail)
	}
	if report.Checks["jbcontext_retrieval"].Status != "unchecked" || report.Checks["gopls_dispatch"].Status != "unchecked" || len(calls) != 5 {
		t.Fatal("live metadata overclaimed retrieval or dispatch")
	}
	after, err := projectctx.Capture(context.Background(), root)
	if err != nil || before != after {
		t.Fatal("doctor wrote repository source or Git state")
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "/other/private") || strings.Contains(string(encoded), "secret-") {
		t.Fatal("raw probe metadata escaped into doctor output")
	}
}

func TestDoctorGraphifyVersionPin(t *testing.T) {
	for _, test := range []struct {
		name, output, status, detail string
	}{
		// R3a
		{"supported", "graphify 0.9.103\n", "verified", "Graphify reports 0.9.103; graph construction and cache freshness were not queried"},
		// R3b
		{"old pin", "graphify 0.9.91\n", "stale", "Graphify does not report the supported 0.9.103 version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, home := gitFixture(t), t.TempDir()
			head, err := projectctx.Capture(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(home, ".venvs/agent-graphify/bin")
			writeFile(t, bin, "graphify", "fixture")
			if err := os.Chmod(filepath.Join(bin, "graphify"), 0o755); err != nil {
				t.Fatal(err)
			}
			probed := false
			run := func(_ context.Context, command projectctx.Command) (projectctx.CommandResult, error) {
				output := ""
				switch strings.Join(command.Args, " ") {
				case "status --json-output":
					output = fmtStatus(head.Head)
				case "watch status":
					output = "Watch daemon running (pid 123)\n  fixture-id  " + root + "\n"
				case "version":
					output = "golang.org/x/tools/gopls v0.21.0"
				case "mcp":
					output = "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n"
				case "--version":
					probed = true
					output = test.output
				default:
					t.Fatalf("unexpected probe: %+v", command)
				}
				return projectctx.CommandResult{Stdout: []byte(output)}, nil
			}
			report, err := Doctor(context.Background(), DoctorOptions{Repo: root, Home: home, Live: true, Run: run, LookPath: func(name string) (string, error) { return "/fake/" + name, nil }, Getenv: func(string) string { return "" }})
			if err != nil {
				t.Fatal(err)
			}
			if !probed {
				t.Fatal("live doctor did not run graphify --version")
			}
			check := report.Checks["graphify_version"]
			if check.Status != test.status || check.Detail != test.detail {
				t.Fatalf("graphify_version = %+v, want status %q detail %q", check, test.status, test.detail)
			}
		})
	}
}

// TestDoctorGraphifyPresenceIsAnExecutableLookup: graphify_cli asks LookPath
// for the dedicated runtime cw graphify prefers, then for graphify on PATH.
// Oracle, per row: the names LookPath is asked for, in order, and the
// graphify_cli status. Absolute names go to exec.LookPath, which asks the
// kernel (access(2), X_OK) whether this user may execute the file, so an
// execute bit only the group holds (0o610 on the user's own file) is not
// enough. access(2) lets root execute a file any execute bit allows, so that
// row is "detected" for root.
func TestDoctorGraphifyPresenceIsAnExecutableLookup(t *testing.T) {
	ownerMayNotExecute := "missing"
	if os.Geteuid() == 0 {
		ownerMayNotExecute = "detected"
	}
	for _, test := range []struct {
		name    string
		mode    os.FileMode // of the dedicated runtime; 0 leaves it absent
		onPath  bool
		runtime bool // whether LookPath is asked only for the dedicated runtime
		status  string
	}{
		{"dedicated runtime", 0o755, false, true, "detected"},
		{"graphify on PATH only", 0, true, false, "detected"},
		{"neither", 0, false, false, "missing"},
		{"dedicated runtime the owner may not execute", 0o610, false, ownerMayNotExecute == "detected", ownerMayNotExecute},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			writeFile(t, root, "go.mod", "module example.invalid/fixture\n")
			dedicated := filepath.Join(home, ".venvs", "agent-graphify", "bin", "graphify")
			if test.mode != 0 {
				writeFile(t, filepath.Dir(dedicated), "graphify", "fixture")
				if err := os.Chmod(dedicated, test.mode); err != nil {
					t.Fatal(err)
				}
			}
			var asked []string
			lookPath := func(name string) (string, error) {
				if name == "jbcontext" || name == "gopls" || name == "git" || name == "cw" {
					return "", exec.ErrNotFound
				}
				asked = append(asked, name)
				if filepath.IsAbs(name) {
					return exec.LookPath(name)
				}
				if test.onPath {
					return "/fake/bin/graphify", nil
				}
				return "", exec.ErrNotFound
			}
			report, err := Doctor(t.Context(), DoctorOptions{Repo: root, Home: home, LookPath: lookPath, Getenv: func(string) string { return "" },
				Run: func(context.Context, projectctx.Command) (projectctx.CommandResult, error) {
					t.Error("metadata inspection ran a command")
					return projectctx.CommandResult{}, errors.New("not allowed")
				}})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{dedicated, "graphify"}
			if test.runtime {
				want = want[:1]
			}
			if !reflect.DeepEqual(asked, want) {
				t.Errorf("LookPath asked for %q, want %q", asked, want)
			}
			if got := report.Checks["graphify_cli"].Status; got != test.status {
				t.Errorf("graphify_cli status = %q, want %q", got, test.status)
			}
		})
	}
}

func fmtStatus(head string) string {
	return "{\"type\":\"status_result\",\"indices\":[{\"snapshots\":[{\"revision\":\"" + head + "\"}]}]}"
}

func TestDoctorLiveFailureOversizeAndCancellationAreQualified(t *testing.T) {
	root := gitFixture(t)
	for _, test := range []struct {
		name string
		run  Runner
	}{
		{"failure", func(context.Context, projectctx.Command) (projectctx.CommandResult, error) {
			return projectctx.CommandResult{}, errors.New("secret-provider-error")
		}},
		{"oversized", func(context.Context, projectctx.Command) (projectctx.CommandResult, error) {
			return projectctx.CommandResult{Stdout: []byte(strings.Repeat("secret-provider-output", projectctx.MaxProbeOutput))}, nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := Doctor(context.Background(), DoctorOptions{Repo: root, Home: t.TempDir(), Live: true, Run: test.run, LookPath: func(name string) (string, error) { return name, nil }})
			if err != nil {
				t.Fatal(err)
			}
			if report.Checks["jbcontext_index"].Status != "degraded" || report.Checks["gopls_cli"].Status != "degraded" {
				t.Fatal("failed probe appeared healthy")
			}
			encoded, _ := json.Marshal(report)
			if strings.Contains(string(encoded), "secret-provider") {
				t.Fatal("raw failure or oversized output leaked")
			}
		})
	}
	// The first probe cancels the caller's context. Everything Doctor does
	// before that (project inspection, Git) is excluded from the checks below,
	// which rest on counts and context state rather than elapsed time.
	ctx, cancel := context.WithCancel(context.Background())
	var calls, callsAfterCancel atomic.Int32
	var probeSawCancel atomic.Bool
	var canceledAt atomic.Int64
	run := func(probeCtx context.Context, _ projectctx.Command) (projectctx.CommandResult, error) {
		calls.Add(1)
		if ctx.Err() != nil {
			callsAfterCancel.Add(1)
			return projectctx.CommandResult{}, probeCtx.Err()
		}
		canceledAt.Store(time.Now().UnixNano())
		cancel()
		// Cancellation reaches derived contexts synchronously, so a probe
		// context derived from the caller's is already done here.
		probeSawCancel.Store(probeCtx.Err() != nil)
		return projectctx.CommandResult{}, probeCtx.Err()
	}
	// The largest accepted Timeout keeps a slow fixture from expiring the
	// doctor's own deadline before the first probe runs.
	_, err := Doctor(ctx, DoctorOptions{Repo: root, Home: t.TempDir(), Live: true, Timeout: time.Minute, Run: run, LookPath: func(name string) (string, error) { return name, nil }})
	returned := time.Now()
	if calls.Load() == 0 {
		t.Fatalf("live doctor ran no probe, so cancellation was never exercised: %v", err)
	}
	if !probeSawCancel.Load() {
		t.Fatal("running probe context was not derived from the caller's context")
	}
	if n := callsAfterCancel.Load(); n != 0 {
		t.Fatalf("doctor started %d probes after cancellation", n)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not reported: %v", err)
	}
	// Backstop for non-probe work after cancellation, such as waiting out the
	// one-minute Timeout. Only in-memory work remains after the canceling
	// probe, so this bound is far above what a loaded runner needs.
	if elapsed := returned.Sub(time.Unix(0, canceledAt.Load())); elapsed > 10*time.Second {
		t.Fatalf("doctor returned %v after cancellation", elapsed)
	}
}

func TestIndexAndWatchDoNotOverclaimFreshness(t *testing.T) {
	report := DoctorReport{Checks: map[string]Check{}}
	checkIndex(projectctx.CommandResult{Stdout: []byte(fmtStatus("old"))}, nil, projectctx.Snapshot{Head: "new"}, &report)
	if report.Checks["jbcontext_index"].Status != "stale" {
		t.Fatal("different indexed HEAD appeared current")
	}
	checkIndex(projectctx.CommandResult{Stdout: []byte(fmtStatus("current"))}, nil, projectctx.Snapshot{Head: "current", Dirty: true}, &report)
	if report.Checks["jbcontext_overlay"].Status != "degraded" {
		t.Fatal("dirty source appeared covered by indexed HEAD")
	}
	root := t.TempDir()
	checkWatch(projectctx.CommandResult{Stdout: []byte("Watch daemon running (pid 123)\n  id  " + root + "-suffix\n")}, nil, root, &report)
	if report.Checks["jbcontext_watch"].Status != "detected" {
		t.Fatal("daemon liveness or prefix path appeared to prove target enrollment")
	}
}
