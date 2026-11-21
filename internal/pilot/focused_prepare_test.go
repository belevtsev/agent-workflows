package pilot

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var focusedRoot = flag.String("pilot-focused-root", "", "new private revised-delivery comparison; offline preparation only")

// Reuse the immutable, previously validated runner, not a rebuilt executable
// with an unreviewed accounting path. Its paired-v1 profile describes topology
// and budget; experiment.json identifies the new dirty source/binary separately.
// This entry prepares inputs and compiles Go only. It invokes no model CLI.
func TestFocusedPrepareOffline(t *testing.T) {
	if *focusedRoot == "" {
		t.Skip("explicit new private root required; no model invocation")
	}
	root, err := filepath.Abs(*focusedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !slug.MatchString(filepath.Base(root)) {
		t.Fatal("experiment directory needs a valid unique experiment id")
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal("new experiment root required", err)
	}
	source, err := freezeFocusedSource(t.Context(), filepath.Join("..", ".."), root)
	if err != nil {
		t.Fatal(err)
	}
	prior := "/home/pavlo/.local/state/kitsu/main-paired-20261117"
	var m manifest
	if err := readJSON(filepath.Join(prior, "trial/frozen-manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	if m.Model != "gpt-6.1-sol" || m.Effort != "high" || m.Profile != "main-paired-v1" || len(m.Cases) != 1 || m.Cases[0].ID != "pflag-unknown-flags" || m.Budget == nil || m.Budget.Limit != 10_000_000 {
		t.Fatal("retained comparison foundation differs")
	}
	assets := filepath.Join(root, "canary-assets")
	for _, dir := range []string{"bin", "inputs", "support/skills/task/references", "cases"} {
		if err := os.MkdirAll(filepath.Join(assets, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	replace := func(s string) string { return strings.ReplaceAll(s, prior, root) }
	for i := range m.Host.Argv {
		m.Host.Argv[i] = replace(m.Host.Argv[i])
	}
	for k, v := range m.Host.Env {
		m.Host.Env[k] = replace(v)
	}
	for i := range m.Host.Assets {
		a := &m.Host.Assets[i]
		if !strings.HasPrefix(a.Path, prior+string(filepath.Separator)) {
			if digest, err := hashFile(a.Path); err != nil || digest != a.SHA256 {
				t.Fatal("retained host/runtime input changed", a.Path, err)
			}
		}
		a.Path, a.SHA256 = replace(a.Path), ""
	}
	for i := range m.Host.HomeFiles {
		m.Host.HomeFiles[i].SHA256 = "" // Freeze current credential identity without copying or printing it.
	}
	for i := range m.Prepare {
		for j := range m.Prepare[i].Argv {
			m.Prepare[i].Argv[j] = replace(m.Prepare[i].Argv[j])
		}
	}
	for rel, from := range supportSources {
		if err := copyRegular(filepath.Join(source, from), filepath.Join(assets, "support", rel), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := filepath.Join(assets, "cases", m.Cases[0].ID, "source")
	if err := copySnapshot(m.Cases[0].Snapshot, snapshot); err != nil {
		t.Fatal(err)
	}
	m.Cases[0].Snapshot = snapshot
	if err := copyRegular(filepath.Join(prior, "canary-assets/bin/pilot.test"), filepath.Join(assets, "bin/pilot.test"), 0o500); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", filepath.Join(assets, "bin/kitsu"), "./cmd/kitsu")
	build.Dir = source
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	var gm gradeManifest
	if err := readJSON(m.Grader.Env["KITSU_PILOT_GRADING_MANIFEST"], &gm); err != nil {
		t.Fatal(err)
	}
	gm.WorkRoot, gm.TrustedCandidateRoot, gm.GoCache = filepath.Join(root, "grading-work"), filepath.Join(root, "trial/trusted-candidates"), filepath.Join(root, "grade-cache")
	gradePath := filepath.Join(assets, "inputs/grading-manifest.json")
	if err := writeJSON(gradePath, gm, true); err != nil {
		t.Fatal(err)
	}
	for i := range m.Grader.Argv {
		m.Grader.Argv[i] = replace(m.Grader.Argv[i])
	}
	m.Grader.Env["KITSU_PILOT_GRADING_MANIFEST"] = gradePath
	for i := range m.Grader.Assets {
		a := &m.Grader.Assets[i]
		if !strings.HasPrefix(a.Path, prior+string(filepath.Separator)) {
			if digest, err := hashFile(a.Path); err != nil || digest != a.SHA256 {
				t.Fatal("retained oracle input changed", a.Path, err)
			}
		}
		a.Path, a.SHA256 = replace(a.Path), ""
	}
	for name, a := range m.ObservationPrompts {
		body, err := os.ReadFile(a.Path)
		if err != nil {
			t.Fatal(err)
		}
		if name == "compact" {
			body = []byte(focusedCompactPolicy)
		}
		a.Path, a.SHA256 = filepath.Join(assets, "inputs", name+".txt"), ""
		if err := os.WriteFile(a.Path, body, 0o400); err != nil {
			t.Fatal(err)
		}
		m.ObservationPrompts[name] = a
	}
	common, err := os.ReadFile(m.CoordinatorPrompt.Path)
	if err != nil {
		t.Fatal(err)
	}
	common = []byte(strings.ReplaceAll(string(common), "Original full role instructions, original task contract and selected skills stay complete in each fresh worker.", "Original full role instructions, the complete original behavioral task, selected skills and applicable tool/write restrictions stay complete in each fresh worker. Coordinator scheduling, budget and state/interval supervision stay here; do not copy this entire experimental coordinator prompt into assignments."))
	m.CoordinatorPrompt = asset{Path: filepath.Join(assets, "inputs/coordinator.txt")}
	if err := os.WriteFile(m.CoordinatorPrompt.Path, common, 0o400); err != nil {
		t.Fatal(err)
	}
	// Reuse the completed actual parent/child accounting probe only because the
	// exact Codex/runtime/runner files above are unchanged. Charge no fictitious
	// new probe; every measured coordinator/descendant still has fresh counters.
	if err := calibrationApproved(m.Calibration.Path, m.Model, m.Effort); err != nil {
		t.Fatal(err)
	}
	if err := copyRegular(m.Calibration.Path, filepath.Join(root, "accounting-foundation.json"), 0o400); err != nil {
		t.Fatal(err)
	}
	m.Calibration = asset{Path: filepath.Join(root, "accounting-foundation.json")}
	m.ID, m.Seed = filepath.Base(root), 2026111710
	if err := writeJSON(filepath.Join(root, "draft.json"), m, true); err != nil {
		t.Fatal(err)
	}
	head := exec.Command("git", "rev-parse", "HEAD")
	head.Dir = filepath.Join("..", "..")
	base, err := head.Output()
	if err != nil {
		t.Fatal(err)
	}
	diff := exec.Command("git", "diff", "--binary", "HEAD")
	diff.Dir = head.Dir
	patch, err := diff.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "production-source.diff"), patch, 0o400); err != nil {
		t.Fatal(err)
	}
	binaryHash, err := hashFile(filepath.Join(assets, "bin/kitsu"))
	if err != nil {
		t.Fatal(err)
	}
	patchHash, err := hashFile(filepath.Join(root, "production-source.diff"))
	if err != nil {
		t.Fatal(err)
	}
	sourceHash, err := hashFile(filepath.Join(root, "source-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(root, "experiment.json"), map[string]any{"version": 1, "kind": "current local compact delivery comparison", "base_commit": strings.TrimSpace(string(base)), "source_patch_sha256": patchHash, "source_index_sha256": sourceHash, "source_capture": "complete tracked and non-ignored untracked source snapshot; production binary built from frozen-source", "production_binary_sha256": binaryHash, "runner": "unchanged immutable main-paired-v1 runner", "accounting_foundation": "prior completed actual parent/child probe; exact CLI/runtime/runner hashes unchanged; no new paid probe", "new_probe_tokens": 0, "budget_approval": "pending", "paid_calls": 0}, true); err != nil {
		t.Fatal(err)
	}
	t.Log("new production binary and inputs prepared offline; paid_calls=0; budget approval pending")
}

const focusedCompactPolicy = `OBSERVATION ARM: COMPACT ON, REVISED DELIVERY.
Read /opt/kitsu-support/skills/task/references/compact.md and follow the revised selective policy. Prefer native short-file reads, scoped small searches and ordinary quiet check output. Reserve context collection and compact check envelopes for noisy observations; expand needed evidence. Use managed task prepare --dispatch and the generated native.prompt/model/effort in each fresh worker. Assign the complete original behavioral task and role-specific work; the saved packet supplies the original role, selected skills and worker observation policy. Do not duplicate coordinator supervision or require every worker to reread coordinator guides unless selected as skills or explicitly needed as task references. Keep all original selected skills, task contracts, permissions and write limits complete. Worker returns are optional untrusted claims; prefer the generated minimal shape without inventing optional fields or running extra checks merely to fill them. Finish each interval using actual fail/pass evidence; do not duplicate manual baseline/role checks. Request state snapshots only after task history exists and when additional state is needed; apply deltas only with their complete known baseline. No compact-call quotas or unchanged-state polling. Small-task completeness remains unknown; original meaningful red, independent neutral review and final hooked Git requirements remain mandatory.
`
