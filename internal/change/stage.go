package change

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The stages of a change folder, in order.
const (
	StageSpec   = "spec"
	StageDesign = "design"
	StageTests  = "tests"
	StageCode   = "code"
	StageReview = "review"
	StageCommit = "commit"
	StageDone   = "done"
)

// Content holds the content ids of a change: of what a review or an
// approval was given for. An id is empty when no file gives it.
type Content struct {
	Spec   string `json:"spec"`
	Design string `json:"design"`
	Code   string `json:"code"`
}

// Report is the stage of one change folder.
type Report struct {
	Task  string `json:"task"`
	Stage string `json:"stage"`
	// Missing says what the stage still lacks. It is empty when the stage
	// is done.
	Missing []string `json:"missing"`
	// NotAutomated counts the scenarios whose row names no test.
	NotAutomated int     `json:"not_automated"`
	Content      Content `json:"content"`
}

// Git is what the stage needs to know from git.
type Git struct {
	// Code is the content id of the code in the working tree.
	Code string
	// InHead says that HEAD holds the folder as the working tree does.
	InHead bool
	// Committed is the content id of the code in the last commit that
	// changed the folder. It is set only when InHead is.
	Committed string
}

// shortID is a content id: the first 12 characters of a SHA-256.
func shortID(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil))[:12] }

// Code makes the content id of the code from the entries of a tree: every
// path outside openspec/changes/.
type Code struct{ hash hash.Hash }

// NewCode starts a content id.
func NewCode() *Code { return &Code{sha256.New()} }

// Add takes one entry of a tree as git prints it.
func (c *Code) Add(raw []byte, entry string) {
	if !strings.HasPrefix(entry, Dir+"/") {
		c.hash.Write(raw)
	}
}

// ID is the content id of the entries added.
func (c *Code) ID() string { return shortID(c.hash) }

var taskMark = regexp.MustCompile(`(?m)^- \[[xX]\] `)

// content makes the ids of the specification and of the design from the
// files of the folder. A line end of another system and the mark of a
// finished task change neither.
func (f *Folder) content() (spec, design string, err error) {
	names := []string{"proposal.md"}
	dir := filepath.Join(f.root, filepath.FromSlash(f.Rel))
	err = filepath.WalkDir(filepath.Join(dir, "specs"), func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, file)
		names = append(names, filepath.ToSlash(rel))
		return err
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	slices.Sort(names)
	if spec, err = f.idOf(names, nil); err != nil {
		return "", "", err
	}
	design, err = f.idOf([]string{"design.md", "tasks.md"}, func(name, text string) string {
		if name == "tasks.md" {
			return taskMark.ReplaceAllString(text, "- [ ] ")
		}
		return text
	})
	return spec, design, err
}

// idOf is the content id of some files of the folder, empty when none of
// them exists.
func (f *Folder) idOf(names []string, change func(name, text string) string) (string, error) {
	h := sha256.New()
	exists := false
	for _, name := range names {
		data, found, err := f.read(name)
		if err != nil {
			return "", err
		}
		if !found {
			continue
		}
		exists = true
		text := unix(data)
		if change != nil {
			text = change(name, text)
		}
		fmt.Fprintf(h, "%s\x00%s\x00", name, text)
	}
	if !exists {
		return "", nil
	}
	return shortID(h), nil
}

// Status decides the stage of a folder: the first stage whose condition
// does not hold, with what is missing.
func Status(f *Folder, git Git) (*Report, error) {
	problems, err := Check(f)
	if err != nil {
		return nil, err
	}
	spec, design, err := f.content()
	if err != nil {
		return nil, err
	}
	report := &Report{Task: f.Task, Stage: StageDone, Missing: []string{}, Content: Content{Spec: spec, Design: design, Code: git.Code}}
	for _, s := range f.scenarios() {
		if r, found := f.row(s.Name); found && !r.Automated() {
			report.NotAutomated++
		}
	}
	broken := func(part string) []string {
		var texts []string
		for _, p := range problems {
			if p.part == part {
				texts = append(texts, p.String())
			}
		}
		return texts
	}
	stages := []struct {
		name    string
		missing func() []string
	}{
		{StageSpec, func() []string {
			missing := lacks(!f.Proposal, "proposal.md is missing")
			missing = append(missing, lacks(len(f.Specs) == 0, "specs/<capability>/spec.md is missing")...)
			return append(append(missing, broken(partSpec)...), f.reviewed(StageSpec, "the specification", true, spec)...)
		}},
		{StageDesign, func() []string {
			missing := lacks(f.Design == nil, "design.md is missing")
			missing = append(missing, lacks(f.Tasks == nil, "tasks.md is missing")...)
			return append(append(missing, broken(partDesign)...), f.reviewed(StageDesign, "the design", true, design)...)
		}},
		{StageTests, f.untested},
		{StageCode, func() []string {
			var missing []string
			for _, task := range f.Tasks.List {
				if !task.Done {
					missing = append(missing, fmt.Sprintf("task %s is not checked", task.ID))
				}
			}
			return missing
		}},
		{StageReview, func() []string { return f.reviewed("code", "the code", false, git.Code, git.Committed) }},
		{StageCommit, func() []string {
			return lacks(!git.InHead, fmt.Sprintf("the folder %s/ differs from `HEAD`: its files are not committed", f.Rel))
		}},
	}
	for _, stage := range stages {
		if missing := stage.missing(); len(missing) > 0 {
			report.Stage, report.Missing = stage.name, missing
			break
		}
	}
	return report, nil
}

// lacks is the one thing that is missing when the condition holds.
func lacks(condition bool, text string) []string {
	if condition {
		return []string{text}
	}
	return nil
}

// untested lists the scenarios that have no row in tests.md.
func (f *Folder) untested() []string {
	scenarios := f.scenarios()
	if f.Tests == nil {
		return lacks(len(scenarios) > 0, "tests.md is missing")
	}
	var missing []string
	for _, s := range scenarios {
		if _, found := f.row(s.Name); !found {
			missing = append(missing, fmt.Sprintf("%s has no row for the scenario `%s`", path.Base(f.Tests.File), s.Name))
		}
	}
	return missing
}

// reviewed says what the review of a stage, and its approval where the
// stage needs one, still lack. A review and an approval count only when
// they were given for one of the content ids; the first id is that of the
// working tree, and it is empty when the files of the stage are missing.
func (f *Folder) reviewed(stage, what string, approval bool, ids ...string) []string {
	if f.Reviews == nil {
		f = &Folder{Reviews: &Reviews{}}
	}
	counts := func(content string) bool { return content != "" && slices.Contains(ids, content) }
	var held, heldNow, given, givenNow bool
	for _, r := range f.Reviews.Reviews {
		if r.Stage == stage {
			held, heldNow = true, heldNow || counts(r.Content)
		}
	}
	for _, a := range f.Reviews.Approvals {
		if a.What == stage {
			given, givenNow = true, givenNow || counts(a.Content)
		}
	}
	missing := lacks(!held, fmt.Sprintf("reviews.md holds no review of %s", what))
	missing = append(missing, lacks(approval && !given, fmt.Sprintf("the approval of %s is not recorded in reviews.md", what))...)
	who := ""
	switch reviewOld, approvalOld := held && !heldNow, approval && given && !givenNow; {
	case reviewOld && approvalOld:
		who = "the review and the approval of %s were"
	case reviewOld:
		who = "the review of %s was"
	case approvalOld:
		who = "the approval of %s was"
	}
	if who != "" && ids[0] != "" {
		missing = append(missing, fmt.Sprintf(who+" given for other content; the content id is now `%s`", what, ids[0]))
	}
	return append(missing, f.open(stage, what)...)
}

// open names the findings of a stage that nobody has settled.
func (f *Folder) open(stage, what string) []string {
	var open []string
	for _, finding := range f.Reviews.Findings {
		if finding.Stage == stage && finding.Disposition == "open" {
			open = append(open, fmt.Sprintf("the finding `%s` of the review of %s is `open`", finding.ID, what))
		}
	}
	return open
}
