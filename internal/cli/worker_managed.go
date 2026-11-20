package cli

import (
	"github.com/belevtsev/kitsu/v3/internal/change"
)

// managedStage remains readable for dispatches from the former local launcher.
type managedStage struct {
	Profile    string `json:"profile"`
	Index      string `json:"index"`
	Head       string `json:"head"`
	MergeHeads string `json:"merge_heads"`
	Target     string `json:"commit_target"`
}

func validateConfiguredStage(b workerBrief) error {
	if !b.Configured || b.Workflow != "full" || b.Stage == "lookup" {
		return nil
	}
	report := b.State.Report
	if report == nil {
		return failf("configured full task has no OpenSpec change record")
	}
	stage := report.Stage
	allowed := false
	switch b.Stage {
	case "spec", "spec-review":
		allowed = stage == change.StageSpec
	case "design", "design-review":
		allowed = stage == change.StageDesign
	case "tests":
		allowed = stage == change.StageTests || stage == change.StageCode || stage == change.StageReview
	case "code":
		// An approved design can authorize an empty interface before tests.
		allowed = stage == change.StageTests || stage == change.StageCode || stage == change.StageReview
	case "review":
		allowed = stage == change.StageCode || stage == change.StageReview
	case "commit":
		allowed = stage == change.StageCommit && report.BrokenRules == 0
	}
	if !allowed {
		return failf("OpenSpec task %s is at %s; resolve its outstanding work and approvals before starting %s", b.Task, stage, b.Stage)
	}
	return nil
}
