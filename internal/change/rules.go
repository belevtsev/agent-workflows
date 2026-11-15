package change

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/gatecub/v3/internal/decl"
)

// The parts of a change folder a problem can belong to.
const (
	partSpec    = "spec"
	partDesign  = "design"
	partTests   = "tests"
	partReviews = "reviews"
)

// The values the tables of reviews.md may hold.
var (
	Stages       = []string{"spec", "design", "code"}
	Approved     = []string{"spec", "design"}
	Severities   = []string{"blocker", "major", "minor"}
	Dispositions = []string{"open", "resolved", "rejected", "deferred"}
)

// Problem is one broken rule, with its place.
type Problem struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Text string `json:"text"`
	part string
}

func (p Problem) String() string {
	if p.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Text)
	}
	return fmt.Sprintf("%s: %s", p.File, p.Text)
}

// Check lists the rules the files of a folder break. A file that is missing
// breaks none: a folder at an earlier stage passes when the files it has
// obey their rules.
func Check(f *Folder) ([]Problem, error) {
	var problems []Problem
	add := func(part string, found []Problem) {
		for _, p := range found {
			p.part = part
			problems = append(problems, p)
		}
	}
	spec, err := f.specRules()
	if err != nil {
		return nil, err
	}
	design, err := f.designRules()
	if err != nil {
		return nil, err
	}
	tests, err := f.testRules()
	if err != nil {
		return nil, err
	}
	add(partSpec, spec)
	add(partDesign, append(design, f.taskRules()...))
	add(partTests, tests)
	add(partReviews, f.reviewRules())
	return problems, nil
}

func (f *Folder) specRules() ([]Problem, error) {
	var problems []Problem
	scenarios := map[string]string{}
	for _, spec := range f.Specs {
		current, err := f.current(spec.Capability)
		if err != nil {
			return nil, err
		}
		seen := map[string]int{}
		for _, req := range spec.Requirements {
			for _, flaw := range req.flaws(seen[req.Name], current) {
				flaw.File = spec.File
				problems = append(problems, flaw)
			}
			seen[req.Name] = req.Line
			if req.Section == "REMOVED" {
				continue
			}
			for _, s := range req.Scenarios {
				if first, found := scenarios[s.Name]; found {
					text := fmt.Sprintf("the scenario `%s` has the name of the scenario at %s; rename it and update tests.md so scenario names are unique within this change", s.Name, first)
					problems = append(problems, Problem{File: spec.File, Line: s.Line, Text: text})
				} else {
					scenarios[s.Name] = fmt.Sprintf("%s:%d", spec.File, s.Line)
				}
			}
		}
	}
	return problems, nil
}

// flaws lists the rules one requirement breaks. first is the line of an
// earlier requirement of the same name, or 0; current holds the
// requirements openspec/specs/ has for the capability.
func (req Requirement) flaws(first int, current Spec) []Problem {
	var flaws []Problem
	at := func(line int, format string, args ...any) {
		flaws = append(flaws, Problem{Line: line, Text: fmt.Sprintf(format, args...)})
	}
	if first > 0 {
		at(req.Line, "the requirement `%s` has the name of the requirement at line %d", req.Name, first)
	}
	if req.Section == "ADDED" && slices.ContainsFunc(current.Requirements, func(r Requirement) bool { return r.Name == req.Name }) {
		at(req.Line, "the requirement `%s` exists in %s, so it must be under `## MODIFIED Requirements`", req.Name, current.File)
	}
	if req.Section == "REMOVED" {
		return flaws
	}
	if !req.Normative {
		at(req.Line, "the requirement `%s` holds neither SHALL nor MUST", req.Name)
	}
	if !req.Source {
		at(req.Line, "the requirement `%s` has no `Source:` line", req.Name)
	}
	if len(req.Scenarios) == 0 {
		at(req.Line, "the requirement `%s` has no scenario", req.Name)
	}
	for _, s := range req.Scenarios {
		if s.When != 1 || s.Then != 1 {
			at(s.Line, "the scenario `%s` holds %d `WHEN` and %d `THEN`; it holds exactly one of each", s.Name, s.When, s.Then)
		}
	}
	return flaws
}

func (f *Folder) taskRules() []Problem {
	if f.Tasks == nil {
		return nil
	}
	var problems []Problem
	at := func(line int, format string, args ...any) {
		problems = append(problems, Problem{File: f.Tasks.File, Line: line, Text: fmt.Sprintf(format, args...)})
	}
	if len(f.Tasks.List) == 0 {
		at(0, "tasks.md holds no task of the form `- [ ] 1.1 <text>`")
	}
	earlier := map[string]bool{}
	for _, task := range f.Tasks.List {
		for _, field := range taskFields {
			if !strings.Contains(task.Text, field) {
				at(task.Line, "task %s has no `%s`", task.ID, field)
			}
		}
		for _, id := range task.depends() {
			if !earlier[id] {
				at(task.Line, "task %s depends on %s, which is not earlier in the list", task.ID, id)
			}
		}
		earlier[task.ID] = true
	}
	for _, spec := range f.Specs {
		for _, req := range spec.Requirements {
			if !slices.ContainsFunc(f.Tasks.List, func(t Task) bool { return strings.Contains(t.Text, req.Name) }) {
				problems = append(problems, Problem{File: spec.File, Line: req.Line, Text: fmt.Sprintf("no task of tasks.md names the requirement `%s`", req.Name)})
			}
		}
	}
	return problems
}

func (f *Folder) testRules() ([]Problem, error) {
	if f.Tests == nil {
		if f.finished() && len(f.scenarios()) > 0 {
			return []Problem{{File: f.Rel + "/tests.md", Text: "every task is checked, and tests.md is missing: no scenario has a test"}}, nil
		}
		return nil, nil
	}
	problems := slices.Clone(f.Tests.flaws)
	at := func(line int, format string, args ...any) {
		problems = append(problems, Problem{File: f.Tests.File, Line: line, Text: fmt.Sprintf(format, args...)})
	}
	for _, r := range f.Tests.Rows {
		switch {
		case !r.Automated() && r.Text == "":
			at(r.Line, "the scenario `%s` is not automated and its row gives no reason", r.Scenario)
		case !r.Automated():
		case r.File == "" || r.Text == "":
			at(r.Line, "the row of the scenario `%s` names no test file or no text", r.Scenario)
		default:
			data, err := readInside(f.root, r.File)
			switch {
			case errors.Is(err, errOutside):
				at(r.Line, "the test file `%s` of the scenario `%s` is outside the repository", r.File, r.Scenario)
			case errors.Is(err, os.ErrNotExist):
				at(r.Line, "the test file `%s` of the scenario `%s` does not exist", r.File, r.Scenario)
			case err != nil:
				return nil, err
			case !bytes.Contains(data, []byte(r.Text)):
				at(r.Line, "the test file `%s` of the scenario `%s` does not hold the text `%s`", r.File, r.Scenario, r.Text)
			}
		}
	}
	if f.finished() {
		for _, spec := range f.Specs {
			for _, req := range spec.Requirements {
				for _, s := range req.Scenarios {
					if _, found := f.row(s.Name); !found && req.Section != "REMOVED" {
						problems = append(problems, Problem{File: spec.File, Line: s.Line, Text: fmt.Sprintf("every task is checked, and tests.md has no row for the scenario `%s`", s.Name)})
					}
				}
			}
		}
	}
	return problems, nil
}

// errOutside is the reason a test file is not read: its path leaves the
// repository.
var errOutside = errors.New("the path leaves the repository")

// readInside reads a file named by a path from the root of the repository.
// A path that is absolute or leaves the root, also through a symbolic link,
// gives errOutside, so that the check says nothing about other files.
func readInside(root, rel string) ([]byte, error) {
	leaves := func(path string) bool {
		return filepath.IsAbs(path) || filepath.VolumeName(path) != "" || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if leaves(clean) {
		return nil, errOutside
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(realRoot, clean))
	if err != nil {
		return nil, err
	}
	if within, err := filepath.Rel(realRoot, real); err != nil || leaves(within) {
		return nil, errOutside
	}
	return os.ReadFile(real)
}

// declared reads the ids of the checks gatecub.toml declares. A repository
// without the file declares none.
func declared(root string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(root, decl.FileName))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	file, err := decl.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("the checks a rule names cannot be looked up: %w", err)
	}
	ids := map[string]bool{}
	for _, check := range file.Checks {
		ids[check.ID] = true
	}
	return ids, nil
}

func (f *Folder) designRules() ([]Problem, error) {
	if f.Design == nil {
		return nil, nil
	}
	problems := slices.Clone(f.Design.flaws)
	at := func(line int, format string, args ...any) {
		problems = append(problems, Problem{File: f.Design.File, Line: line, Text: fmt.Sprintf(format, args...)})
	}
	if cycle := cycleOf(f.Design.Blocks); cycle != nil {
		first := slices.IndexFunc(f.Design.Blocks, func(b Block) bool { return b.Name == cycle[0] })
		at(f.Design.Blocks[first].Line, "the building blocks depend on each other in a cycle: `%s`", strings.Join(cycle, "`, `"))
	}
	for _, block := range f.Design.Blocks {
		if block.New && block.Reuse == "" {
			at(block.Line, "the new block `%s` names no existing code that was considered for reuse", block.Name)
		}
	}
	var checks map[string]bool
	for _, rule := range f.Design.Rules {
		if len(rule.CheckedBy) == 0 {
			at(rule.Line, "the rule does not say how it is checked: by `review`, or by a check of gatecub.toml")
		}
		for _, by := range rule.CheckedBy {
			if by == "review" {
				continue
			}
			if checks == nil {
				var err error
				if checks, err = declared(f.root); err != nil {
					return nil, err
				}
			}
			if !checks[by] {
				at(rule.Line, "gatecub.toml declares no check `%s`", by)
			}
		}
	}
	return problems, nil
}

// cycleOf returns the blocks of one cycle of dependencies, in order, or nil.
func cycleOf(blocks []Block) []string {
	depends := map[string][]string{}
	for _, block := range blocks {
		depends[block.Name] = block.Depends
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var path []string
	var visit func(name string) []string
	visit = func(name string) []string {
		switch state[name] {
		case done:
			return nil
		case visiting:
			return slices.Clone(path[slices.Index(path, name):])
		}
		state[name] = visiting
		path = append(path, name)
		for _, next := range depends[name] {
			if _, known := depends[next]; !known {
				continue
			}
			if cycle := visit(next); cycle != nil {
				return cycle
			}
		}
		path = path[:len(path)-1]
		state[name] = done
		return nil
	}
	for _, block := range blocks {
		if cycle := visit(block.Name); cycle != nil {
			return cycle
		}
	}
	return nil
}

func (f *Folder) reviewRules() []Problem {
	if f.Reviews == nil {
		return nil
	}
	problems := slices.Clone(f.Reviews.flaws)
	at := func(line int, format string, args ...any) {
		problems = append(problems, Problem{File: f.Reviews.File, Line: line, Text: fmt.Sprintf(format, args...)})
	}
	value := func(line int, what, column, got string, allowed []string) {
		if !slices.Contains(allowed, got) {
			at(line, "%s holds `%s` as its %s; it holds `%s`", what, got, column, strings.Join(allowed, "`, `"))
		}
	}
	number := func(line int, what, got string) {
		if !round(got) {
			at(line, "%s holds `%s` as its round; a round is a number from 1", what, got)
		}
	}
	for _, r := range f.Reviews.Reviews {
		value(r.Line, "the review", "stage", r.Stage, Stages)
		number(r.Line, "the review", r.Round)
	}
	for _, a := range f.Reviews.Approvals {
		value(a.Line, "the approval", "subject", a.What, Approved)
	}
	finished := f.finished()
	for _, finding := range f.Reviews.Findings {
		what := fmt.Sprintf("the finding `%s`", finding.ID)
		value(finding.Line, what, "stage", finding.Stage, Stages)
		value(finding.Line, what, "severity", finding.Severity, Severities)
		value(finding.Line, what, "disposition", finding.Disposition, Dispositions)
		number(finding.Line, what, finding.Round)
		switch {
		case (finding.Disposition == "rejected" || finding.Disposition == "deferred") && finding.Reason == "":
			at(finding.Line, "%s is `%s` without a reason", what, finding.Disposition)
		case finding.Disposition == "open" && finished:
			at(finding.Line, "every task is checked, and %s is `open`", what)
		}
		if finding.Disposition == "deferred" && (finding.Severity == "blocker" || finding.Severity == "major") {
			at(finding.Line, "%s is `%s` and `deferred`; a `blocker` or a `major` finding is never deferred", what, finding.Severity)
		}
	}
	return problems
}
