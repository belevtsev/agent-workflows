package pilot

import (
	"path/filepath"
	"testing"
)

func TestPilotDeclaredProfilesFreezeAndRequireMatchingCalibration(t *testing.T) {
	for _, profile := range []struct {
		name    string
		effort  string
		repeats int
		runs    int
	}{
		{name: "high-single-trial", effort: "high", repeats: 1, runs: 24},
		{name: "max-three-trials", effort: "max", repeats: 3, runs: 72},
	} {
		t.Run(profile.name, func(t *testing.T) {
			path, output := unitManifest(t)
			var m manifest
			if err := readJSON(path, &m); err != nil {
				t.Fatal(err)
			}
			m.Effort, m.Repeats, m.TimeoutSeconds = profile.effort, profile.repeats, 480
			if err := writeJSON(path, m, false); err != nil {
				t.Fatal(err)
			}
			if err := freeze(t.Context(), path, output); err != nil {
				t.Fatal(err)
			}
			frozen, p, err := loadFrozen(t.Context(), output)
			if err != nil || len(p.Runs) != profile.runs || frozen.Effort != profile.effort || frozen.TimeoutSeconds != 480 {
				t.Fatal("declared profile was not frozen intact", frozen, p, err)
			}
			seen := map[string]int{}
			for i, selected := range p.Runs {
				seen[selected.CaseID]++
				if i%2 == 0 {
					next := p.Runs[i+1]
					if next.CaseID != selected.CaseID || next.Repeat != selected.Repeat || next.Arm == selected.Arm || next.Controller != selected.Controller {
						t.Fatal("profile split a paired assignment", selected, next)
					}
				}
			}
			for id, count := range seen {
				if count != profile.repeats*2 {
					t.Fatal("case assignment missing or duplicated", id, count)
				}
			}
			// The existing offline seed approves max. A high study must not
			// inherit that approval merely because the model is unchanged.
			err = calibrationApproved(frozen.Calibration.Path, frozen.Model, frozen.Effort)
			if (err == nil) != (profile.effort == "max") {
				t.Fatal("calibration did not match declared effort", err)
			}
			calibration := map[string]any{"version": 1, "approved": true, "model": frozen.Model, "effort": profile.effort,
				"subagents_verified": true, "usage_verified": true, "isolation_verified": true, "cli_version": "offline-fake"}
			calibrationPath := filepath.Join(t.TempDir(), "calibration.json")
			if err := writeJSON(calibrationPath, calibration, false); err != nil {
				t.Fatal(err)
			}
			if err := calibrationApproved(calibrationPath, frozen.Model, frozen.Effort); err != nil {
				t.Fatal("matching calibration rejected", err)
			}
			if err := calibrationApproved(calibrationPath, "different-model", frozen.Effort); err == nil {
				t.Fatal("another model inherited calibration approval")
			}
		})
	}
}
