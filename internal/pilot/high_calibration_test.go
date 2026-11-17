package pilot

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var (
	highCalibrationHome       = flag.String("pilot-high-calibration-home", "", "fresh home containing one completed high parent/child profile probe")
	highCalibrationStdout     = flag.String("pilot-high-calibration-stdout", "", "completed profile probe CLI JSONL; never invokes a model")
	highCalibrationRoot       = flag.String("pilot-high-calibration-root", "", "profile probe root thread identity")
	highCalibrationFoundation = flag.String("pilot-high-calibration-foundation", "", "approved original max runtime and four-role workflow calibration")
	highCalibrationOutput     = flag.String("pilot-high-calibration-output", "", "new private high calibration evidence directory")
	highCalibrationApprove    = flag.Bool("pilot-high-calibration-approve", false, "operator verified unchanged isolation runtime and approved reuse of original workflow foundation")
)

// This entry only captures existing evidence. The high probe verifies the
// changed profile; the original max workflow remains the runtime foundation.
func TestPilotCaptureHighCalibration(t *testing.T) {
	if *highCalibrationOutput == "" {
		t.Skip("explicit completed high profile probe required; no model invocation")
	}
	if err := captureHighCalibration(*highCalibrationHome, *highCalibrationStdout, *highCalibrationRoot, *highCalibrationFoundation, *highCalibrationOutput, *highCalibrationApprove); err != nil {
		t.Fatal(err)
	}
}

func captureHighCalibration(home, stdout, rootID, foundationPath, output string, approve bool) error {
	if home == "" || stdout == "" || rootID == "" || foundationPath == "" || output == "" {
		return errors.New("high calibration requires fresh home, completed stdout, root identity, original foundation, and separate output")
	}
	if err := calibrationApproved(foundationPath, "gpt-6.1-sol", "max"); err != nil {
		return fmt.Errorf("original runtime foundation: %w", err)
	}
	var foundation struct {
		Version           int    `json:"version"`
		Approved          bool   `json:"approved"`
		Model             string `json:"model"`
		Effort            string `json:"effort"`
		SubagentsVerified bool   `json:"subagents_verified"`
		UsageVerified     bool   `json:"usage_verified"`
		IsolationVerified bool   `json:"isolation_verified"`
		CLI               string `json:"cli_version"`
		Evidence          struct {
			Groups         []calibrationSource `json:"groups"`
			Isolation      string              `json:"isolation"`
			WorkflowCommit string              `json:"workflow_commit"`
			Scope          string              `json:"scope"`
			SourceIndex    string              `json:"source_index"`
		} `json:"evidence"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := readJSON(foundationPath, &foundation); err != nil {
		return err
	}
	groups := map[string]bool{}
	for _, group := range foundation.Evidence.Groups {
		groups[group.Name] = true
	}
	if foundation.CLI != "0.169.0" || !groups["workflow"] || !groups["isolation-fixed"] || foundation.Evidence.WorkflowCommit == "" || foundation.Evidence.Isolation == "" {
		return errors.New("original foundation lacks matching CLI, fixed isolation, or completed four-role workflow evidence")
	}
	foundationAsset := asset{Path: foundationPath}
	if err := sealAsset(&foundationAsset, "."); err != nil {
		return err
	}
	if stdoutThreadID(stdout) != rootID {
		return errors.New("high probe stdout root identity differs")
	}
	if err := copyRegular(stdout, filepath.Join(output, "stdout.jsonl"), 0o600); err != nil {
		return err
	}
	usage, threads, err := collectUsage(home, ".codex/sessions", output, rootID)
	if writeErr := writeJSON(filepath.Join(output, "usage.json"), map[string]any{"usage": usage, "excluded_from_measured_runs": true, "monetary_cost": "unknown"}, true); writeErr != nil {
		return errors.Join(err, writeErr)
	}
	if err != nil || !usage.Complete || len(threads) != 2 {
		return fmt.Errorf("high profile needs exactly two sessions with complete usage: %v; %v", err, usage.Unknown)
	}
	if err := validateHighProbe(threads, rootID); err != nil {
		return err
	}
	result := map[string]any{
		"version": 1, "approved": approve, "model": "gpt-6.1-sol", "effort": "high", "cli_version": "0.169.0",
		"subagents_verified": true, "usage_verified": true, "isolation_verified": approve,
		"evidence": map[string]any{"foundation": foundationAsset, "foundation_workflow_commit": foundation.Evidence.WorkflowCommit,
			"root": rootID, "traces": usage.RawTraces, "operator_approved_runtime_reuse": approve,
			"scope": "High profile probe only: one completed Sol/high parent and one native fresh Sol/high child. This is not a high four-role workflow calibration. Original max isolation and four-role workflow calibration supplies the runtime foundation; operator verified unchanged runtime containment. Excluded from all measured runs."},
		"usage": map[string]any{"profile_probe": usage, "total": usage.Total, "foundation_usage_recharged": false, "monetary_cost": "unknown"},
	}
	return writeJSON(filepath.Join(output, "calibration.json"), result, true)
}

func validateHighProbe(threads []traceInfo, rootID string) error {
	if len(threads) != 2 {
		return errors.New("high profile requires exactly one parent and one child")
	}
	spawns, roots, children := 0, 0, 0
	for _, thread := range threads {
		if !slices.Equal(thread.Models, []string{"gpt-6.1-sol"}) || !slices.Equal(thread.Efforts, []string{"high"}) {
			return fmt.Errorf("high probe actual model/effort mismatch: %s", thread.ID)
		}
		if thread.ID == rootID && thread.ParentID == "" {
			roots++
		} else if thread.ID != rootID && thread.ParentID == rootID {
			children++
		} else {
			return errors.New("high probe child causality differs from declared root")
		}
		if err := highProbeCompleted(thread.File.Path); err != nil {
			return err
		}
		for _, call := range thread.Calls {
			if call.Name != "spawn_agent" {
				continue
			}
			var args struct {
				ForkTurns string `json:"fork_turns"`
				Model     string `json:"model"`
				Effort    string `json:"reasoning_effort"`
			}
			if thread.ID != rootID || json.Unmarshal(call.Arguments, &args) != nil || args.ForkTurns != "none" || args.Model != "gpt-6.1-sol" || args.Effort != "high" {
				return errors.New("high probe did not request one native fresh Sol/high child")
			}
			spawns++
		}
	}
	if roots != 1 || children != 1 || spawns != 1 {
		return errors.New("high probe requires one causal parent/child pair and one explicit fresh spawn")
	}
	return nil
}

func highProbeCompleted(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	completed := 0
	for {
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
			} `json:"payload"`
		}
		if err := d.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if event.Type == "event_msg" && event.Payload.Type == "task_complete" {
			completed++
		}
	}
	if completed != 1 {
		return fmt.Errorf("high probe session must contain one completed turn: %s", path)
	}
	return nil
}

func TestPilotHighCalibrationCaptureIsOfflineAndRequiresOperatorApproval(t *testing.T) {
	for _, approve := range []bool{false, true} {
		t.Run(fmt.Sprintf("approved-%t", approve), func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			rollouts := filepath.Join(home, ".codex", "sessions")
			if err := os.MkdirAll(rollouts, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"parent", "child"} {
				parent := ""
				if id == "child" {
					parent = "parent"
				}
				trace := strings.ReplaceAll(string(fakeRollout(id, parent, "code", "normal", 100, 40, 10, 2)), "max", "high")
				if id == "parent" {
					trace += `{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","arguments":"{\"fork_turns\":\"none\",\"model\":\"gpt-6.1-sol\",\"reasoning_effort\":\"high\"}"}}` + "\n"
				}
				trace += `{"type":"event_msg","payload":{"type":"task_complete"}}` + "\n"
				if err := os.WriteFile(filepath.Join(rollouts, id+".jsonl"), []byte(trace), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			stdout := filepath.Join(root, "stdout.jsonl")
			if err := os.WriteFile(stdout, []byte("{\"type\":\"thread.started\",\"thread_id\":\"parent\"}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			foundation := filepath.Join(root, "foundation.json")
			if err := writeJSON(foundation, map[string]any{"version": 1, "approved": true, "model": "gpt-6.1-sol", "effort": "max", "cli_version": "0.169.0",
				"subagents_verified": true, "usage_verified": true, "isolation_verified": true,
				"evidence": map[string]any{"groups": []calibrationSource{{Name: "workflow"}, {Name: "isolation-fixed"}}, "workflow_commit": "offline-commit", "isolation": "offline-foundation"}}, true); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "capture")
			if err := captureHighCalibration(home, stdout, "parent", foundation, output, approve); err != nil {
				t.Fatal(err)
			}
			if err := calibrationApproved(filepath.Join(output, "calibration.json"), "gpt-6.1-sol", "high"); (err == nil) != approve {
				t.Fatal("captured calibration did not respect explicit operator approval", err)
			}
			var retained struct {
				Usage    usageReport `json:"usage"`
				Excluded bool        `json:"excluded_from_measured_runs"`
				Cost     string      `json:"monetary_cost"`
			}
			if err := readJSON(filepath.Join(output, "usage.json"), &retained); err != nil || !retained.Excluded || retained.Usage.Total != 220 || len(retained.Usage.Threads) != 2 {
				t.Fatal("high probe usage was lost or charged with foundation usage", retained, err)
			}
			threads := retained.Usage.Threads
			// Serialized usage omits tool calls; inspect retained traces again.
			for i := range threads {
				var err error
				threads[i], err = inspectTrace(threads[i].File.Path)
				if err != nil {
					t.Fatal(err)
				}
			}
			threads[0].Efforts = []string{"max"}
			if err := validateHighProbe(threads, "parent"); err == nil {
				t.Fatal("max session was accepted as high profile evidence")
			}
		})
	}
}
