package taskflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// FixRef names what a fix-round brief answers: one finding of a recorded
// worker result, as "<assignment id>/<finding id>", or one plan requirement.
type FixRef struct {
	Finding     string `json:"finding,omitempty"`
	Requirement string `json:"requirement,omitempty"`
}

// authorStages are the stages whose briefs produce work that a later round
// revises; a later brief of one names what it fixes or which assignment it
// repeats.
var authorStages = []string{StageSpec, StageDesign, StageTests, StageImplementation}

// validateBriefStage checks the brief's stage, fixes and repeats against the
// record and the assignments recorded before it (prior). Under a profile a
// brief needs a stage of that profile, and an author-stage brief after the
// first of its stage needs fixes or repeats. A record without a profile
// accepts a brief without them, as before profiles existed; what a brief does
// carry is checked either way.
func validateBriefStage(record Record, prior []WorkerAssignment, brief WorkerBrief) error {
	if brief.Stage != "" && !slices.Contains(stageOrder, brief.Stage) {
		return fmt.Errorf("worker brief stage %q is not one of %s", brief.Stage, strings.Join(stageOrder, ", "))
	}
	if profile := record.Profile; profile != nil {
		if brief.Stage == "" {
			return fmt.Errorf("worker brief needs a stage under profile %s: one of %s", profile.Name, strings.Join(profile.Stages, ", "))
		}
		if !slices.Contains(profile.Stages, brief.Stage) {
			return fmt.Errorf("worker brief stage %s is not in profile %s: use one of %s", brief.Stage, profile.Name, strings.Join(profile.Stages, ", "))
		}
	}
	if err := validateFixes(record, brief.Fixes); err != nil {
		return err
	}
	if brief.Repeats != "" {
		var repeated *WorkerAssignment
		for i := range prior {
			if prior[i].Brief.ID == brief.Repeats {
				repeated = &prior[i]
			}
		}
		if repeated == nil {
			return fmt.Errorf("worker brief repeats %s, which is not an earlier assignment of the task", brief.Repeats)
		}
		if repeated.Brief.Stage != brief.Stage {
			return fmt.Errorf("worker brief repeats %s of stage %q, but its stage is %q; a replacement keeps the stage", brief.Repeats, repeated.Brief.Stage, brief.Stage)
		}
	}
	if record.Profile == nil || !slices.Contains(authorStages, brief.Stage) || len(brief.Fixes) > 0 || brief.Repeats != "" {
		return nil
	}
	for _, earlier := range prior {
		if earlier.Brief.Stage == brief.Stage {
			return fmt.Errorf("worker brief of stage %s needs fixes or repeats: assignment %s already took that stage, so name the findings or requirements this round fixes, or the assignment it repeats", brief.Stage, earlier.Brief.ID)
		}
	}
	return nil
}

// validateFixes checks that each entry names exactly one finding of a
// recorded worker result or one plan requirement, with no entry twice.
func validateFixes(record Record, fixes []FixRef) error {
	if len(fixes) > 256 {
		return errors.New("worker brief fixes holds at most 256 entries")
	}
	seen := map[FixRef]bool{}
	for _, fix := range fixes {
		if (fix.Finding == "") == (fix.Requirement == "") {
			return errors.New("each worker brief fixes entry names exactly one finding or requirement")
		}
		if seen[fix] {
			return errors.New("worker brief fixes names an entry twice")
		}
		seen[fix] = true
		if fix.Requirement != "" {
			if !slices.ContainsFunc(record.Plan.Requirements, func(r Requirement) bool { return r.ID == fix.Requirement }) {
				return fmt.Errorf("worker brief fixes requirement %s, which the plan does not hold", fix.Requirement)
			}
			continue
		}
		assignment, finding, ok := strings.Cut(fix.Finding, "/")
		result := findWorkerResult(record, assignment)
		if !ok || result == nil || !slices.ContainsFunc(result.Result.Findings, func(f WorkerFinding) bool { return f.ID == finding }) {
			return fmt.Errorf("worker brief fixes finding %s, which no recorded worker result holds; write it as <assignment id>/<finding id>", fix.Finding)
		}
	}
	return nil
}
