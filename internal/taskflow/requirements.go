package taskflow

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// ContractRequirement is one interface-contract requirement a contract-bearing
// (test-engineer) brief covers, with the plan requirements it implements.
type ContractRequirement struct {
	Requirement string   `json:"requirement"`
	Criteria    []string `json:"criteria"`
}

// RequirementMapping maps one declared requirement to the tests that fail when
// it is violated, or marks it uncovered with the reason.
type RequirementMapping struct {
	Requirement string       `json:"requirement"`
	Tests       []MappedTest `json:"tests,omitempty"`
	Uncovered   string       `json:"uncovered,omitempty"`
}

// MappedTest names a Go test by its _test.go file, relative to the project
// root, and its name as go test reports it (TestX or TestX/sub_case).
type MappedTest struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

var contractRequirementID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

// validateBriefRequirements checks the requirements a brief declares: only a
// brief with a contract declares them, each with distinct plan requirement IDs
// as its criteria.
func validateBriefRequirements(record Record, brief WorkerBrief) error {
	if len(brief.Requirements) == 0 {
		return nil
	}
	if brief.Contract == nil {
		return errors.New("only a brief with a contract declares requirements")
	}
	shape := errors.New("worker brief requirements need 1..256 distinct requirement IDs, each with 1..256 distinct criteria")
	if len(brief.Requirements) > 256 {
		return shape
	}
	seen := map[string]bool{}
	for _, requirement := range brief.Requirements {
		if !contractRequirementID.MatchString(requirement.Requirement) || seen[requirement.Requirement] || len(requirement.Criteria) == 0 || len(requirement.Criteria) > 256 {
			return shape
		}
		seen[requirement.Requirement] = true
		criteria := map[string]bool{}
		for _, criterion := range requirement.Criteria {
			if criteria[criterion] {
				return shape
			}
			criteria[criterion] = true
		}
	}
	for _, requirement := range brief.Requirements {
		for _, criterion := range requirement.Criteria {
			if !slices.ContainsFunc(record.Plan.Requirements, func(plan Requirement) bool { return plan.ID == criterion }) {
				return fmt.Errorf("brief requirement %s names unknown plan requirement %s", requirement.Requirement, criterion)
			}
		}
	}
	return nil
}

// planProvesTests reports whether some planned check earns per-test credit.
func planProvesTests(plan Plan) bool {
	return slices.ContainsFunc(plan.Checks, func(check Check) bool { return langpack.GoTestsCredited(langpack.Run{Argv: check.Argv}) })
}

// requirementsFollowPlan requires requirements on a brief with a contract
// when the plan can prove tests, and refuses them when it cannot.
func requirementsFollowPlan(record Record, brief WorkerBrief) error {
	proves := planProvesTests(record.Plan)
	if proves && brief.Contract != nil && len(brief.Requirements) == 0 {
		return errors.New("a brief with a contract must declare requirements: each contract requirement with the plan requirements it implements")
	}
	if !proves && len(brief.Requirements) > 0 {
		return errors.New("brief declares requirements, but the plan has no go test check whose argv earns per-test credit (it needs -json in its own argv and only flags the Go evidence rule allows: no -args, -run, -skip or other refused flag)")
	}
	return nil
}

// validateRequirementMap checks a result's requirement_map against its
// assignment's brief: who may carry one, entry and test shape, and that a
// completed result answers every declared requirement.
func validateRequirementMap(assigned WorkerAssignment, result WorkerResult) error {
	declared := assigned.Brief.Requirements
	if len(result.RequirementMap) > 0 && (result.Status != "completed" || len(declared) == 0) {
		return errors.New("only a completed result of an assignment whose brief declares requirements carries requirement_map")
	}
	answered := map[string]bool{}
	for _, entry := range result.RequirementMap {
		if !slices.ContainsFunc(declared, func(requirement ContractRequirement) bool { return requirement.Requirement == entry.Requirement }) {
			return fmt.Errorf("requirement_map names %s, which the brief does not declare", entry.Requirement)
		}
		if answered[entry.Requirement] {
			return fmt.Errorf("requirement_map repeats %s", entry.Requirement)
		}
		answered[entry.Requirement] = true
		tested := len(entry.Tests) > 0 && len(entry.Tests) <= 256 && entry.Uncovered == ""
		uncovered := len(entry.Tests) == 0 && boundedText(entry.Uncovered)
		if !tested && !uncovered {
			return fmt.Errorf("requirement_map entry %s needs 1..256 tests or an uncovered reason, not both", entry.Requirement)
		}
		seen := map[MappedTest]bool{}
		for _, test := range entry.Tests {
			if !cleanWorkerPath(test.Path, false) || !strings.HasSuffix(test.Path, "_test.go") || !slices.ContainsFunc(assigned.Brief.OwnedPaths, func(owned string) bool { return scopeContains(owned, test.Path) }) {
				return fmt.Errorf("requirement_map entry %s test %s %s: path must be a _test.go file inside the brief's owned_paths", entry.Requirement, test.Path, test.Name)
			}
			if !langpack.GoTestName(test.Name) {
				return fmt.Errorf("requirement_map entry %s test %s %s: name must be a Go test name (a Test, Fuzz or Example function, then optional /subtest parts)", entry.Requirement, test.Path, test.Name)
			}
			if seen[test] {
				return fmt.Errorf("requirement_map entry %s repeats test %s %s", entry.Requirement, test.Path, test.Name)
			}
			seen[test] = true
		}
	}
	if result.Status == "completed" {
		for _, requirement := range declared {
			if !answered[requirement.Requirement] {
				return fmt.Errorf("completed result's requirement_map has no entry for %s", requirement.Requirement)
			}
		}
	}
	return nil
}

// mappedTestAuthorship requires each mapped test's file to be one this
// result, or an earlier test-engineer result not rejected, changed.
func mappedTestAuthorship(record Record, result WorkerResult) error {
	written := map[string]bool{}
	add := func(changes []WorkerChangedFile) {
		for _, changed := range changes {
			if !changed.Deleted {
				written[changed.Path] = true
			}
		}
	}
	add(result.ChangedFiles)
	for _, earlier := range record.WorkerResults {
		assigned := findAssignment(record, earlier.Result.AssignmentID)
		if assigned == nil || assigned.Brief.Contract == nil {
			continue
		}
		if accepted := latestAcceptance(record, assigned.Brief.ID); accepted != nil && accepted.Acceptance.Status == "rejected" {
			continue
		}
		add(earlier.Result.ChangedFiles)
	}
	for _, entry := range result.RequirementMap {
		for _, test := range entry.Tests {
			if !written[test.Path] {
				return fmt.Errorf("requirement_map entry %s test %s %s: path is not a file this result or an earlier test-engineer result changed", entry.Requirement, test.Path, test.Name)
			}
		}
	}
	return nil
}

// mappedTestsLive requires each mapped test to exist at the current source.
func mappedTestsLive(source testSource, result WorkerResult) error {
	for _, entry := range result.RequirementMap {
		for _, test := range entry.Tests {
			if _, missing := source.resolve(test); missing != "" {
				return fmt.Errorf("requirement_map entry %s test %s %s is missing at the current source: %s", entry.Requirement, test.Path, test.Name, missing)
			}
		}
	}
	return nil
}
