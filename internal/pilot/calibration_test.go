package pilot

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var calibrationIndex = flag.String("pilot-calibration-index", "", "private index of completed calibration rollouts; never calls a model")
var calibrationOutput = flag.String("pilot-calibration-output", "", "private retained calibration evidence directory")
var calibrationApprove = flag.Bool("pilot-calibration-approve", false, "operator has reviewed successful isolation and four-role execution")

type calibrationSource struct {
	Name   string   `json:"name"`
	Root   string   `json:"root"`
	Stdout string   `json:"stdout"`
	Traces []string `json:"traces"`
	Note   string   `json:"note"`
}

func TestPilotCaptureCalibration(t *testing.T) {
	if *calibrationIndex == "" || *calibrationOutput == "" {
		t.Skip("explicit completed calibration index and output required; no model invocation")
	}
	data, err := os.ReadFile(*calibrationIndex)
	if err != nil {
		t.Fatal(err)
	}
	var sources []calibrationSource
	if err := json.Unmarshal(data, &sources); err != nil || len(sources) == 0 {
		t.Fatalf("invalid calibration index: %v", err)
	}
	groups := map[string]usageReport{}
	var allTotal int64
	workflowVerified := false
	for _, source := range sources {
		if !slug.MatchString(source.Name) {
			t.Fatal("unsafe calibration name")
		}
		root := filepath.Join(*calibrationOutput, source.Name)
		home := filepath.Join(root, "home")
		for _, path := range source.Traces {
			if err := copyRegular(path, filepath.Join(home, ".codex", "sessions", filepath.Base(path)), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := copyRegular(source.Stdout, filepath.Join(root, "stdout.jsonl"), 0o600); err != nil {
			t.Fatal(err)
		}
		if stdoutThreadID(source.Stdout) != source.Root {
			t.Fatalf("calibration root identity differs: %s", source.Name)
		}
		usage, threads, err := collectUsage(home, ".codex/sessions", root, source.Root)
		if err != nil || !usage.Complete {
			t.Fatalf("incomplete calibration usage %s: %v %v", source.Name, err, usage.Unknown)
		}
		stages := map[string]bool{}
		for _, thread := range threads {
			if !slices.Equal(thread.Models, []string{"gpt-6.1-sol"}) || !slices.Equal(thread.Efforts, []string{"max"}) {
				t.Fatalf("calibration model/effort mismatch in %s", thread.ID)
			}
			stages[thread.Stage] = true
		}
		if source.Name == "workflow" {
			workflowVerified = stages["tests"] && stages["code"] && stages["review"] && stages["commit"] && len(threads) == 5
		}
		if err := writeJSON(filepath.Join(root, "usage.json"), map[string]any{"usage": usage, "note": source.Note}, true); err != nil {
			t.Fatal(err)
		}
		groups[source.Name] = usage
		allTotal += usage.Total
	}
	if !workflowVerified {
		t.Fatal("completed four-role workflow calibration evidence required")
	}
	approved := *calibrationApprove && workflowVerified
	result := map[string]any{
		"version": 1, "approved": approved, "model": "gpt-6.1-sol", "effort": "max",
		"subagents_verified": workflowVerified, "usage_verified": true, "isolation_verified": approved,
		"cli_version": "0.169.0",
		"evidence": map[string]any{"source_index": *calibrationIndex, "groups": sources,
			"isolation":       "Operator verified unshare-all filesystem/PID/network containment; API-only CONNECT bridge; fixed isolated calibration denied GitHub and host/private file reads. Public runtime preflight and 24 integrated base/reference grades are separate no-model checks.",
			"workflow_commit": "a813a47a86b7b73383040924644aabd2059fe027",
			"scope":           "Calibration only: one toy four-role workflow plus access, subagent and runtime probes. Excluded from all 72 measured identities."},
		"usage": map[string]any{"groups": groups, "total": allTotal, "monetary_cost": "unknown"},
	}
	if err := writeJSON(filepath.Join(*calibrationOutput, "calibration.json"), result, true); err != nil {
		t.Fatal(err)
	}
	t.Logf("retained %d calibration groups, %d actual tokens (excluded from measured runs)", len(groups), allTotal)
}
