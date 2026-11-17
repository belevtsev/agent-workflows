package pilot

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

var (
	pilotAction     = flag.String("pilot-action", "status", "freeze, run, status, or report; never runs by default")
	pilotManifest   = flag.String("pilot-manifest", "", "input manifest for freeze; all later actions use output/frozen-manifest.json")
	pilotOutput     = flag.String("pilot-output", "", "private experiment output directory outside task snapshots")
	pilotRun        = flag.String("pilot-run", "next", "a frozen run id, next, or all; existing claims are never retried")
	pilotController = flag.Int("pilot-controller", -1, "assigned frozen paired controller, zero-based; -1 selects all")
)

// TestCodexPilot is the explicit entry point for paid measured execution. It is
// skipped in ordinary tests. Root must complete calibration and assign runs;
// unit tests below use a fake local process and never call a model or API.
//
//	go test ./internal/pilot -run '^TestCodexPilot$' -count=1 -timeout=0 -args \
//	  -pilot-action=freeze -pilot-manifest=/private/input.json -pilot-output=/private/pilot
//
// Replace freeze with run and choose -pilot-run=<id|next|all> only after the
// frozen calibration explicitly approves model, subagents, usage and isolation.
func TestCodexPilot(t *testing.T) {
	if *pilotOutput == "" {
		t.Skip("explicit private pilot output and action required")
	}
	output, err := filepath.Abs(*pilotOutput)
	if err != nil {
		t.Fatal(err)
	}
	if *pilotAction == "freeze" {
		if *pilotManifest == "" {
			t.Fatal("freeze requires -pilot-manifest")
		}
		if err := freeze(t.Context(), *pilotManifest, output); err != nil {
			t.Fatal(err)
		}
		t.Log("frozen manifest and randomized paired plan:", output)
		return
	}
	m, p, err := loadFrozen(t.Context(), output)
	if err != nil {
		t.Fatal(err)
	}
	switch *pilotAction {
	case "run":
		if *pilotController < -1 || *pilotController >= m.Controllers {
			t.Fatal("controller is outside the frozen assignment")
		}
		found := false
		for _, selected := range p.Runs {
			if *pilotController >= 0 && selected.Controller != *pilotController {
				continue
			}
			if *pilotRun != "all" && *pilotRun != "next" && selected.ID != *pilotRun {
				continue
			}
			if _, err := os.Stat(filepath.Join(output, "runs", selected.ID)); err == nil {
				if *pilotRun == selected.ID {
					t.Fatalf("run %s is already claimed; inspect its retained result instead of silently retrying", selected.ID)
				}
				continue
			}
			found = true
			if err := executeCell(t.Context(), m, p, selected, output); err != nil {
				t.Errorf("%s: %v; failed evidence retained", selected.ID, err)
			}
			if *pilotRun != "all" || t.Context().Err() != nil {
				break
			}
		}
		if !found {
			t.Log("no unclaimed assigned run remains")
		}
		fallthrough
	case "status", "report":
		summary, err := summarize(output, m, p)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeJSON(filepath.Join(output, "summary.json"), summary, false); err != nil {
			t.Fatal(err)
		}
		t.Logf("planned=%d measured=%d results=%d unknown_usage=%d; %s", len(p.Runs), summary.Measured, summary.Results, summary.UnknownUsage, filepath.Join(output, "summary.json"))
	default:
		t.Fatalf("unknown pilot action %q", *pilotAction)
	}
}

func freeze(ctx context.Context, source, output string) error {
	path, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	var m manifest
	if err := readJSON(path, &m); err != nil {
		return err
	}
	if err := sealManifest(ctx, &m, filepath.Dir(path)); err != nil {
		return err
	}
	for _, c := range m.Cases {
		if inside(c.Snapshot, output) || inside(output, c.Snapshot) {
			return errors.New("experiment output and public snapshots must not contain one another")
		}
	}
	frozen := filepath.Join(output, "frozen-manifest.json")
	if err := writeJSON(frozen, m, true); err != nil {
		return err
	}
	digest, err := hashFile(frozen)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(output, "plan.json"), schedule(m, digest), true)
}

func loadFrozen(ctx context.Context, output string) (manifest, plan, error) {
	var m manifest
	var p plan
	path := filepath.Join(output, "frozen-manifest.json")
	if err := readJSON(path, &m); err != nil {
		return m, p, err
	}
	if err := readJSON(filepath.Join(output, "plan.json"), &p); err != nil {
		return m, p, err
	}
	digest, err := hashFile(path)
	if err != nil || digest != p.ManifestSHA256 {
		return m, p, errors.New("frozen manifest no longer matches the plan")
	}
	if err := sealManifest(ctx, &m, output); err != nil {
		return m, p, err
	}
	if !reflect.DeepEqual(p, schedule(m, digest)) {
		return m, p, errors.New("plan differs from deterministic frozen schedule")
	}
	return m, p, nil
}

func inside(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

type gradeSummary struct {
	Infrastructure string `json:"infrastructure_status"`
	Eligibility    struct {
		Passed bool `json:"passed"`
	} `json:"eligibility"`
	Acceptance struct {
		Passed bool `json:"passed"`
	} `json:"acceptance"`
	Regressions struct {
		Passed bool `json:"passed"`
	} `json:"regressions"`
	Integrity struct {
		Passed bool `json:"passed"`
	} `json:"patch_integrity"`
}

type armSummary struct {
	Assigned          int   `json:"assigned"`
	Measured          int   `json:"measured"`
	Results           int   `json:"results"`
	FunctionalPassed  int   `json:"functional_passed"`
	AcceptancePassed  int   `json:"acceptance_passed"`
	RegressionPassed  int   `json:"regression_passed"`
	IntegrityPassed   int   `json:"patch_integrity_passed"`
	InfrastructureBad int   `json:"infrastructure_failed"`
	ProtocolUnknown   int   `json:"protocol_unknown"`
	ProtocolViolated  int   `json:"protocol_violated"`
	UnknownUsage      int   `json:"unknown_usage"`
	ElapsedNS         int64 `json:"model_elapsed_ns"`
	Total             int64 `json:"total"`
	InputUncached     int64 `json:"input_uncached"`
	InputTotal        int64 `json:"input_total"`
	tokens.Counts
}

type pairedResult struct {
	CaseID               string     `json:"case_id"`
	Repeat               int        `json:"repeat"`
	Normal               *runResult `json:"normal,omitempty"`
	Compact              *runResult `json:"compact,omitempty"`
	FunctionalComparison string     `json:"functional_comparison"`
}

type summaryReport struct {
	Version        int                    `json:"version"`
	ManifestSHA256 string                 `json:"manifest_sha256"`
	Topology       string                 `json:"topology"`
	Model          string                 `json:"model"`
	Effort         string                 `json:"effort"`
	Planned        int                    `json:"planned"`
	Controllers    int                    `json:"controllers"`
	Measured       int                    `json:"measured"`
	Results        int                    `json:"results"`
	UnknownUsage   int                    `json:"unknown_usage"`
	Unclaimed      []string               `json:"unclaimed"`
	Interrupted    []string               `json:"claimed_without_result"`
	Arms           map[string]*armSummary `json:"arms"`
	Pairs          []pairedResult         `json:"pairs"`
	Calibration    asset                  `json:"calibration_excluded_from_measured_totals"`
	Cautions       []string               `json:"cautions"`
}

func functionalPass(result *runResult) bool {
	if result == nil || len(result.Grade) == 0 {
		return false
	}
	var grade gradeSummary
	if json.Unmarshal(result.Grade, &grade) != nil {
		return false
	}
	return grade.Infrastructure == "ok" && grade.Eligibility.Passed && grade.Acceptance.Passed && grade.Regressions.Passed && grade.Integrity.Passed
}

func summarize(output string, m manifest, p plan) (summaryReport, error) {
	report := summaryReport{Version: 1, ManifestSHA256: p.ManifestSHA256, Topology: m.Topology, Model: m.Model, Effort: m.Effort, Planned: len(p.Runs), Controllers: m.Controllers,
		Arms: map[string]*armSummary{"normal": {}, "compact": {}}, Calibration: m.Calibration,
		Cautions: []string{"All assigned identities and failed attempts remain in the report; missing usage is not zero.",
			"These are actual model tokens, not monetary costs; price is unknown unless separately reported by the provider.",
			"Functional outcomes, infrastructure, and protocol compliance are distinct; equal pass totals do not prove quality preservation.",
			"Twelve tasks remain twelve task clusters regardless of repetitions; this pilot does not establish general noninferiority.",
			"Protocol status remains unknown until semantic red-test validity, reviewer independence, write checks, and dispositions are independently audited.",
			"Concurrent runs may share hardware; elapsed time is not a clean speed comparison when intervals overlap."}}
	results := map[string]*runResult{}
	for _, selected := range p.Runs {
		arm := report.Arms[selected.Arm]
		arm.Assigned++
		var result runResult
		path := filepath.Join(output, "runs", selected.ID, "result.json")
		if err := readJSON(path, &result); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return report, err
			}
			if _, err := os.Stat(filepath.Dir(path)); err == nil {
				report.Interrupted = append(report.Interrupted, selected.ID)
			} else {
				report.Unclaimed = append(report.Unclaimed, selected.ID)
			}
			continue
		}
		if result.Run != selected || result.ManifestSHA256 != p.ManifestSHA256 {
			return report, fmt.Errorf("result does not match its frozen run identity: %s", selected.ID)
		}
		if err := applyProtocolAudit(filepath.Dir(path), &result); err != nil {
			return report, err
		}
		copy := result
		results[selected.ID] = &copy
		report.Results++
		arm.Results++
		if result.Measured {
			report.Measured++
			arm.Measured++
		}
		if !result.Usage.Complete {
			report.UnknownUsage++
			arm.UnknownUsage++
		}
		addCounts(&arm.Counts, result.Usage.Counts)
		arm.Total += result.Usage.Total
		arm.InputUncached, arm.InputTotal = arm.Input, arm.Input+arm.CacheRead+arm.CacheWrite
		if result.Execution != nil {
			arm.ElapsedNS += result.Execution.ElapsedNS
		}
		var grade gradeSummary
		if len(result.Grade) > 0 {
			if err := json.Unmarshal(result.Grade, &grade); err != nil {
				return report, err
			}
		}
		if grade.Infrastructure != "ok" || result.Status != "completed" {
			arm.InfrastructureBad++
		}
		if grade.Acceptance.Passed {
			arm.AcceptancePassed++
		}
		if grade.Regressions.Passed {
			arm.RegressionPassed++
		}
		if grade.Integrity.Passed {
			arm.IntegrityPassed++
		}
		if functionalPass(&result) {
			arm.FunctionalPassed++
		}
		if result.Method.Status == "violated" {
			arm.ProtocolViolated++
		} else if result.Method.Status != "verified" {
			arm.ProtocolUnknown++
		}
	}
	for _, c := range m.Cases {
		for repeat := 1; repeat <= m.Repeats; repeat++ {
			pair := pairedResult{CaseID: c.ID, Repeat: repeat, Normal: results[fmt.Sprintf("%s-r%d-normal", c.ID, repeat)], Compact: results[fmt.Sprintf("%s-r%d-compact", c.ID, repeat)], FunctionalComparison: "incomplete"}
			if pair.Normal != nil && pair.Compact != nil {
				normal, compact := functionalPass(pair.Normal), functionalPass(pair.Compact)
				switch {
				case normal && compact:
					pair.FunctionalComparison = "both_pass"
				case normal:
					pair.FunctionalComparison = "compact_only_loss"
				case compact:
					pair.FunctionalComparison = "compact_only_win"
				default:
					pair.FunctionalComparison = "both_fail"
				}
			}
			report.Pairs = append(report.Pairs, pair)
		}
	}
	return report, nil
}
