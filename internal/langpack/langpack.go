// Package langpack holds the language-specific evidence rules that decide
// what a changed task must have executed before it completes. The task core
// stays language-neutral: it describes the changed source and the passed
// check runs, and asks Rules which rules apply.
package langpack

// Source describes the selected project at the current source. Paths are
// slash-separated and relative to the selected path.
type Source struct {
	// Current lists every eligible file now present.
	Current []string
	// Changed lists every file added, modified or deleted since the task
	// baseline; nil when the baseline manifest is unavailable.
	Changed []string
}

// EvidenceRule reports what a changed task lacks in executed evidence.
// passed holds the argv of every check run that passed at the current source.
type EvidenceRule interface {
	Applies(source Source) bool
	Problems(source Source, passed [][]string) []string
}

// languageRules are the installed language packs, in report order.
var languageRules = []EvidenceRule{GoTests{}}

// Rules returns the rules that apply to source: AnyPassedRun, which applies
// to every project, then each applicable language rule.
func Rules(source Source) []EvidenceRule {
	rules := []EvidenceRule{AnyPassedRun{}}
	for _, rule := range languageRules {
		if rule.Applies(source) {
			rules = append(rules, rule)
		}
	}
	return rules
}

// AnyPassedRun requires at least one passed check run at the current source.
// It is the only rule for a project no language pack recognises.
type AnyPassedRun struct{}

func (AnyPassedRun) Applies(Source) bool { return true }

func (AnyPassedRun) Problems(_ Source, passed [][]string) []string {
	if len(passed) == 0 {
		return []string{"changed substantial task has no passed check run at the current source"}
	}
	return nil
}
