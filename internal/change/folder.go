// Package change reads the record of one task, the folder
// openspec/changes/<task>/. It decides the stage of the task from the files
// of the folder and checks the rules those files must obey. It keeps no
// state of its own: what the files say is the state.
package change

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Dir holds the change folders, from the root of a repository. Archive is
// the one directory below it that holds no change in progress.
const (
	Dir     = "openspec/changes"
	Archive = "archive"
)

// ErrNoFolder says that a task has no change folder.
var ErrNoFolder = errors.New("the change folder does not exist")

// Folder is one change folder as its files give it. A file that is missing
// leaves its field nil.
type Folder struct {
	Task     string
	Rel      string // the folder from the root of the repository, with slashes
	Proposal bool
	Specs    []Spec
	Design   *Design
	Tasks    *Tasks
	Tests    *Tests
	Reviews  *Reviews

	root string // the root of the repository on disk
}

// Scenario is one scenario of a requirement.
type Scenario struct {
	Name       string
	Line       int
	When, Then int // how many lines of each kind it holds
}

// Requirement is one requirement of a specification.
type Requirement struct {
	Name      string
	Line      int
	Section   string // ADDED, MODIFIED or REMOVED; empty under another heading
	Normative bool   // its text holds SHALL or MUST
	Source    bool   // it has a Source: line
	Scenarios []Scenario
}

// Spec is the specification of one capability.
type Spec struct {
	Capability   string
	File         string
	Requirements []Requirement
}

// Task is one entry of tasks.md.
type Task struct {
	ID   string
	Line int
	Done bool
	Text string
}

// Tasks is tasks.md.
type Tasks struct {
	File string
	List []Task
}

// Block is one building block of a design.
type Block struct {
	Name    string
	Line    int
	Depends []string
	New     bool
	Reuse   string // the existing code that was considered for reuse
}

// Rule is one rule of a design, with what checks it.
type Rule struct {
	Text      string
	Line      int
	CheckedBy []string
}

// Design is design.md.
type Design struct {
	File   string
	Blocks []Block
	Rules  []Rule
	flaws  []Problem // what its tables lack
}

// TestRow says which test covers a scenario.
type TestRow struct {
	Scenario, File, Text string
	Line                 int
}

// NotAutomated is what a row holds in place of a test file.
const NotAutomated = "not automated"

// Automated says whether the row names a test.
func (r TestRow) Automated() bool { return !strings.EqualFold(r.File, NotAutomated) }

// Tests is tests.md.
type Tests struct {
	File  string
	Rows  []TestRow
	flaws []Problem
}

// Review is one review that was held.
type Review struct {
	Stage, Round, Date, Content string
	Line                        int
}

// Finding is one finding of a review.
type Finding struct {
	ID, Stage, Round, Severity, Where, Text, Disposition, Reason string
	Line                                                         int
}

// Approval is one approval the developer gave.
type Approval struct {
	What, Date, Content string
	Line                int
}

// Reviews is reviews.md.
type Reviews struct {
	File      string
	Reviews   []Review
	Findings  []Finding
	Approvals []Approval
	flaws     []Problem
}

// List names the change folders of a repository, without the archive.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(Dir)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != Archive && !strings.HasPrefix(entry.Name(), ".") {
			tasks = append(tasks, entry.Name())
		}
	}
	return tasks, nil
}

// read returns a file of the folder, and false when it does not exist.
func (f *Folder) read(name string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.Rel), filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return data, err == nil, err
}

// Read reads the change folder of a task.
func Read(root, task string) (*Folder, error) {
	f := &Folder{Task: task, Rel: path.Join(Dir, task), root: root}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.Rel)))
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
		return nil, fmt.Errorf("%w: %s/", ErrNoFolder, f.Rel)
	}
	if err != nil {
		return nil, err
	}
	if _, f.Proposal, err = f.read("proposal.md"); err != nil {
		return nil, err
	}
	if err := f.readSpecs(); err != nil {
		return nil, err
	}
	for name, parse := range map[string]func(file string, data []byte){
		"design.md":  func(file string, data []byte) { f.Design = parseDesign(file, data) },
		"tasks.md":   func(file string, data []byte) { f.Tasks = parseTasks(file, data) },
		"tests.md":   func(file string, data []byte) { f.Tests = parseTests(file, data) },
		"reviews.md": func(file string, data []byte) { f.Reviews = parseReviews(file, data) },
	} {
		data, found, err := f.read(name)
		if err != nil {
			return nil, err
		}
		if found {
			parse(path.Join(f.Rel, name), data)
		}
	}
	return f, nil
}

func (f *Folder) readSpecs() error {
	entries, err := os.ReadDir(filepath.Join(f.root, filepath.FromSlash(f.Rel), "specs"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		name := path.Join("specs", entry.Name(), "spec.md")
		data, found, err := f.read(name)
		if err != nil {
			return err
		}
		if entry.IsDir() && found {
			f.Specs = append(f.Specs, parseSpec(entry.Name(), path.Join(f.Rel, name), data))
		}
	}
	return nil
}

// current reads the requirements that openspec/specs/ holds for a
// capability.
func (f *Folder) current(capability string) (Spec, error) {
	file := path.Join("openspec/specs", capability, "spec.md")
	data, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(file)))
	if errors.Is(err, os.ErrNotExist) {
		return Spec{File: file}, nil
	}
	return parseSpec(capability, file, data), err
}

var normative = regexp.MustCompile(`\b(SHALL|MUST)\b`)

// title is the text of a heading after its mark.
func title(text, mark string) string { return plain(strings.TrimPrefix(text, mark)) }

// section is the kind of change a `## <kind> Requirements` heading names.
func section(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 3 && fields[2] == "Requirements" && slices.Contains([]string{"ADDED", "MODIFIED", "REMOVED"}, fields[1]) {
		return fields[1]
	}
	return ""
}

func parseSpec(capability, file string, data []byte) Spec {
	spec := Spec{Capability: capability, File: file}
	kind := ""
	var req *Requirement
	var scenario *Scenario
	for _, l := range lines(data) {
		text := strings.TrimSpace(l.text)
		switch {
		case strings.HasPrefix(text, "### Requirement:"):
			spec.Requirements = append(spec.Requirements, Requirement{Name: title(text, "### Requirement:"), Line: l.n, Section: kind})
			req, scenario = &spec.Requirements[len(spec.Requirements)-1], nil
		case strings.HasPrefix(text, "#### Scenario:") && req != nil:
			req.Scenarios = append(req.Scenarios, Scenario{Name: title(text, "#### Scenario:"), Line: l.n})
			scenario = &req.Scenarios[len(req.Scenarios)-1]
		case strings.HasPrefix(text, "## "), strings.HasPrefix(text, "# "):
			kind, req, scenario = section(text), nil, nil
		case strings.HasPrefix(text, "### "):
			req, scenario = nil, nil
		case req == nil:
		case strings.HasPrefix(text, "Source:"):
			req.Source = true
		case scenario != nil:
			if strings.HasPrefix(text, "- **WHEN**") {
				scenario.When++
			}
			if strings.HasPrefix(text, "- **THEN**") {
				scenario.Then++
			}
		case normative.MatchString(text):
			req.Normative = true
		}
	}
	return spec
}

var (
	taskStart = regexp.MustCompile(`^- \[([ xX])\] (\d+\.\d+)\s+(.*)$`)
	taskID    = regexp.MustCompile(`\d+\.\d+`)
)

func parseTasks(file string, data []byte) *Tasks {
	tasks := &Tasks{File: file}
	var task *Task
	for _, l := range lines(data) {
		m := taskStart.FindStringSubmatch(l.text)
		switch {
		case m != nil:
			tasks.List = append(tasks.List, Task{ID: m[2], Line: l.n, Done: m[1] != " ", Text: m[3]})
			task = &tasks.List[len(tasks.List)-1]
		case task != nil && strings.TrimSpace(l.text) != "" && strings.TrimLeft(l.text, " \t") != l.text:
			task.Text += " " + strings.TrimSpace(l.text)
		default:
			task = nil
		}
	}
	return tasks
}

// The fields every task holds.
var taskFields = []string{"Size:", "Depends on:", "Check:", "Source:"}

// depends lists the tasks a task says it depends on.
func (t Task) depends() []string {
	_, rest, found := strings.Cut(t.Text, "Depends on:")
	if !found {
		return nil
	}
	for _, field := range taskFields {
		if i := strings.Index(rest, field); i >= 0 {
			rest = rest[:i]
		}
	}
	return taskID.FindAllString(rest, -1)
}

// columns finds the places of the named columns of a table. It reports the
// ones the table lacks.
func columns(t *table, file string, names ...string) ([]int, []Problem) {
	places := make([]int, len(names))
	var flaws []Problem
	for i, name := range names {
		if places[i] = t.col(name); places[i] < 0 {
			flaws = append(flaws, Problem{File: file, Line: t.line, Text: fmt.Sprintf("the table has no column `%s`", name)})
		}
	}
	return places, flaws
}

// list cuts a cell into its names. The words for nothing give no name.
func list(cell string) []string {
	var names []string
	for _, part := range strings.Split(cell, ",") {
		name := plain(part)
		if name != "" && !slices.Contains([]string{"none", "nothing", "-"}, strings.ToLower(name)) {
			names = append(names, name)
		}
	}
	return names
}

func parseDesign(file string, data []byte) *Design {
	design := &Design{File: file}
	ls := lines(data)
	if t := tableUnder(ls, "Building blocks"); t == nil {
		design.flaws = append(design.flaws, Problem{File: file, Text: "design.md has no table under `## Building blocks`"})
	} else {
		at, flaws := columns(t, file, "Block", "Responsibility", "Depends on", "New", "Reuse considered")
		design.flaws = append(design.flaws, flaws...)
		for _, r := range t.rows {
			block := Block{Name: plain(r.get(at[0])), Line: r.line, Depends: list(r.get(at[2])), Reuse: plain(r.get(at[4]))}
			switch isNew := strings.ToLower(plain(r.get(at[3]))); {
			case len(flaws) > 0:
			case isNew == "yes":
				block.New = true
			case isNew != "no":
				design.flaws = append(design.flaws, Problem{File: file, Line: r.line, Text: fmt.Sprintf("the column New of the block `%s` holds `%s`; it holds `yes` or `no`", block.Name, plain(r.get(at[3])))})
			}
			if len(flaws) == 0 && plain(r.get(at[1])) == "" {
				design.flaws = append(design.flaws, Problem{File: file, Line: r.line, Text: fmt.Sprintf("the block `%s` has no responsibility", block.Name)})
			}
			design.Blocks = append(design.Blocks, block)
		}
	}
	if t := tableUnder(ls, "Rules"); t == nil {
		design.flaws = append(design.flaws, Problem{File: file, Text: "design.md has no table under `## Rules`"})
	} else {
		at, flaws := columns(t, file, "Rule", "Checked by")
		design.flaws = append(design.flaws, flaws...)
		for _, r := range t.rows {
			design.Rules = append(design.Rules, Rule{Text: plain(r.get(at[0])), Line: r.line, CheckedBy: list(r.get(at[1]))})
		}
	}
	return design
}

func parseTests(file string, data []byte) *Tests {
	tests := &Tests{File: file}
	t := tableWith(lines(data), "Scenario")
	if t == nil {
		tests.flaws = []Problem{{File: file, Text: "tests.md has no table with the columns Scenario, Test file and Text"}}
		return tests
	}
	at, flaws := columns(t, file, "Scenario", "Test file", "Text")
	tests.flaws = flaws
	for _, r := range t.rows {
		tests.Rows = append(tests.Rows, TestRow{Scenario: plain(r.get(at[0])), File: plain(r.get(at[1])), Text: plain(r.get(at[2])), Line: r.line})
	}
	return tests
}

func parseReviews(file string, data []byte) *Reviews {
	reviews := &Reviews{File: file}
	ls := lines(data)
	if t := tableUnder(ls, "Reviews"); t != nil {
		at, flaws := columns(t, file, "Stage", "Round", "Date", "Content")
		reviews.flaws = append(reviews.flaws, flaws...)
		for _, r := range t.rows {
			reviews.Reviews = append(reviews.Reviews, Review{Stage: plain(r.get(at[0])), Round: plain(r.get(at[1])), Date: plain(r.get(at[2])), Content: plain(r.get(at[3])), Line: r.line})
		}
	}
	if t := tableUnder(ls, "Findings"); t != nil {
		at, flaws := columns(t, file, "Id", "Stage", "Round", "Severity", "Where", "Finding", "Disposition", "Reason")
		reviews.flaws = append(reviews.flaws, flaws...)
		for _, r := range t.rows {
			reviews.Findings = append(reviews.Findings, Finding{
				ID: plain(r.get(at[0])), Stage: plain(r.get(at[1])), Round: plain(r.get(at[2])), Severity: plain(r.get(at[3])),
				Where: plain(r.get(at[4])), Text: plain(r.get(at[5])), Disposition: plain(r.get(at[6])), Reason: plain(r.get(at[7])), Line: r.line,
			})
		}
	}
	if t := tableUnder(ls, "Approvals"); t != nil {
		at, flaws := columns(t, file, "What", "Date", "Content")
		reviews.flaws = append(reviews.flaws, flaws...)
		for _, r := range t.rows {
			reviews.Approvals = append(reviews.Approvals, Approval{What: plain(r.get(at[0])), Date: plain(r.get(at[1])), Content: plain(r.get(at[2])), Line: r.line})
		}
	}
	return reviews
}

// round says whether a text is the number of a round.
func round(text string) bool {
	n, err := strconv.Atoi(text)
	return err == nil && n >= 1
}

// scenarios lists the scenarios the change adds or modifies.
func (f *Folder) scenarios() []Scenario {
	var all []Scenario
	for _, spec := range f.Specs {
		for _, req := range spec.Requirements {
			if req.Section != "REMOVED" {
				all = append(all, req.Scenarios...)
			}
		}
	}
	return all
}

// finished says whether every task is checked.
func (f *Folder) finished() bool {
	if f.Tasks == nil || len(f.Tasks.List) == 0 {
		return false
	}
	for _, task := range f.Tasks.List {
		if !task.Done {
			return false
		}
	}
	return true
}

// row finds the row of a scenario in tests.md.
func (f *Folder) row(scenario string) (TestRow, bool) {
	if f.Tests != nil {
		for _, r := range f.Tests.Rows {
			if r.Scenario == scenario {
				return r, true
			}
		}
	}
	return TestRow{}, false
}
