package pilot

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Inspect completed real-task records with the frozen production CLI in the
// same offline namespace. No model command or API tunnel survives this argv.
func focusedDiagnosticArgs(m manifest, root string, vars map[string]string) ([]string, error) {
	if len(m.Host.Argv) == 0 || m.Host.Argv[0] != "/usr/bin/bwrap" {
		return nil, fmt.Errorf("diagnostic replay requires the isolated bubblewrap host")
	}
	end := slices.Index(m.Host.Argv, "--")
	if end < 0 {
		return nil, fmt.Errorf("host command boundary missing")
	}
	args := []string{}
	for i := 0; i < end; i++ {
		if m.Host.Argv[i] == "--ro-bind" && i+2 < end && m.Host.Argv[i+1] == filepath.Join(root, "api-proxy.sock") {
			i += 2
			continue
		}
		args = append(args, m.Host.Argv[i])
	}
	return append(expandedArgv(args, vars), "--", "/opt/tools/kitsu", "usage", "show", "--diagnostics", "--task", vars["case_id"], "--json"), nil
}

func TestFocusedDiagnosticReplayRemovesModelAndTunnel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := manifest{Host: pilotHost{Argv: []string{"/usr/bin/bwrap", "--unshare-all", "--ro-bind", filepath.Join(root, "api-proxy.sock"), "/run/api-proxy.sock", "--bind", "{worktree}", "/workspace", "--", "codex", "exec", "-"}}}
	args, err := focusedDiagnosticArgs(m, root, map[string]string{"worktree": "/task", "case_id": "case"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/bwrap", "--unshare-all", "--bind", "/task", "/workspace", "--", "/opt/tools/kitsu", "usage", "show", "--diagnostics", "--task", "case", "--json"}
	if !slices.Equal(args, want) {
		t.Fatal("offline diagnostics retained a model or tunnel argument", args)
	}
	for _, argv := range [][]string{{"codex", "exec"}, {"/usr/bin/bwrap", "codex"}} {
		m.Host.Argv = argv
		if _, err := focusedDiagnosticArgs(m, root, nil); err == nil {
			t.Fatal("unrecognized execution boundary accepted", argv)
		}
	}
}

func TestFocusedInsightsOffline(t *testing.T) {
	if *focusedRoot == "" {
		t.Skip("explicit completed private experiment required; no model invocation")
	}
	trial := filepath.Join(*focusedRoot, "trial")
	var m manifest
	var p plan
	if err := readJSON(filepath.Join(trial, "frozen-manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	if err := readJSON(filepath.Join(trial, "plan.json"), &p); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(*focusedRoot, "posthoc", "diagnostics")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal("new offline diagnostic evidence directory required", err)
	}
	for _, selected := range p.Runs {
		runDir := filepath.Join(trial, "runs", selected.ID)
		var result runResult
		if err := readJSON(filepath.Join(runDir, "result.json"), &result); err != nil || result.Status != "completed" || !result.Usage.Complete {
			t.Fatal("completed accounting required", selected.ID, err)
		}
		vars := map[string]string{"worktree": filepath.Join(runDir, "worktree"), "home": filepath.Join(runDir, "home"), "case_id": selected.CaseID, "run_dir": runDir}
		args, err := focusedDiagnosticArgs(m, *focusedRoot, vars)
		if err != nil {
			t.Fatal(err)
		}
		call, err := invoke(t.Context(), vars["worktree"], filepath.Join(out, selected.ID), "", args, executionEnv(m.Host, vars), 60)
		if err != nil || call.ExitCode != 0 || call.Stopped {
			t.Fatal("offline diagnostic failed", selected.ID, err, call.ExitCode)
		}
		var diagnostic map[string]any
		if err := readJSON(call.Stdout.Path, &diagnostic); err != nil {
			t.Fatal(err)
		}
		if err := writeJSON(filepath.Join(out, selected.ID+".json"), map[string]any{"execution": call, "diagnostic": diagnostic, "separate_native_captured_tokens": result.Usage.Total, "scope": "Read-only posthoc projection, outside solver timing and tokens. Native accounting is separate from available Kitsu hook records; do not invent missing counters."}, true); err != nil {
			t.Fatal(err)
		}
		if _, exists := diagnostic["captured_tokens"]; !exists {
			t.Fatal("diagnostic projection missing captured counter", selected.ID)
		}
		t.Logf("%s: frozen CLI diagnostic succeeded offline; native tokens=%d; CLI counters=%v", selected.ID, result.Usage.Total, diagnostic["captured_tokens"])
	}
}
