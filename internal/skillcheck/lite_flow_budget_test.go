package skillcheck

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"
)

// The 90,000-byte part of the orchestration skill's budget (D7 as measured by
// OD-7) counts what a default lite task loads: the files the coordinator
// reads at its steps and the contracts of the four lite stages. Every other
// *.md file under the skill loads only on a condition, and its per-file cap
// and the folder ratchet in orchestration_budget_test.go hold it.
const liteFlowCap = 90000

// liteFlowFiles are the files a default lite task loads, as slash paths
// relative to the skill directory: SKILL.md, the references its step table
// names for every task, and the implementer, test-engineer, test-review and
// code-review contracts.
var liteFlowFiles = []string{
	"SKILL.md",
	"references/contracts/README.md",
	"references/contracts/code-review.md",
	"references/contracts/implementer.md",
	"references/contracts/test-engineer.md",
	"references/contracts/test-review.md",
	"references/dispatching-reviews-and-tests.md",
	"references/github-worker.md",
	"references/global-workflow.md",
	"references/glossary.md",
	"references/hosts.md",
	"references/worker-handoffs.md",
}

// conditionalReferences maps every other *.md file under the skill to the
// condition under which a task loads it.
var conditionalReferences = map[string]string{
	"references/adviser-providers.md":            "the task binds advisers",
	"references/base-merge.md":                   "the base branch is merged during the task",
	"references/connected-sources.md":            "the task uses Jira, Confluence or GitHub evidence",
	"references/consultation-helper.md":          "the plan requires checkpoints",
	"references/contract-amendment.md":           "a contract changes after its stage",
	"references/contracts/adversarial-tester.md": "the plan opts in to the adversarial tester",
	"references/contracts/architect.md":          "the standard or strict profile",
	"references/contracts/design-review.md":      "the standard or strict profile",
	"references/contracts/spec-review.md":        "the strict profile",
	"references/contracts/spec-writer.md":        "the strict profile",
	"references/github-worker-fallback.md":       "cw publish does not fit the publication",
	"references/measure-outcomes.md":             "the user asks for metrics",
	"references/question-patterns.md":            "a checkpoint or adviser request is written",
}

// liteFlowProblems returns one line per broken rule, sorted: a file that is
// in neither list or in both, a listed file that does not exist, and the lite
// files holding more than limit bytes together.
func liteFlowProblems(sizes map[string]int, lite []string, conditional map[string]string, limit int) []string {
	var problems []string
	inLite := map[string]bool{}
	sum := 0
	for _, rel := range lite {
		if inLite[rel] {
			problems = append(problems, rel+": listed twice in liteFlowFiles")
			continue
		}
		inLite[rel] = true
		size, ok := sizes[rel]
		if !ok {
			problems = append(problems, rel+": in liteFlowFiles, but the file does not exist")
			continue
		}
		sum += size
	}
	for rel := range conditional {
		if inLite[rel] {
			problems = append(problems, rel+": in both liteFlowFiles and conditionalReferences")
		}
		if _, ok := sizes[rel]; !ok {
			problems = append(problems, rel+": in conditionalReferences, but the file does not exist")
		}
	}
	for rel := range sizes {
		if _, ok := conditional[rel]; !ok && !inLite[rel] {
			problems = append(problems, rel+": in neither liteFlowFiles nor conditionalReferences: "+
				"say whether a default lite task loads it")
		}
	}
	if sum > limit {
		problems = append(problems, fmt.Sprintf("the files a default lite task loads hold %d bytes, over the budget of %d (D7, OD-7)",
			sum, limit))
	}
	sort.Strings(problems)
	return problems
}

// oracle: every *.md file under skills/task-orchestration is in exactly one
// of liteFlowFiles and conditionalReferences, every listed file exists, and
// the liteFlowFiles together hold at most liteFlowCap (90,000) bytes. A new
// file fails until it is placed in one list, so nothing enters the default
// flow uncounted.
func TestLiteFlowLoadsAtMost90000Bytes(t *testing.T) {
	sizes := orchestrationMarkdownSizes(t, filepath.Join(repoRoot, orchestrationSkillDir))
	for _, problem := range liteFlowProblems(sizes, liteFlowFiles, conditionalReferences, liteFlowCap) {
		t.Errorf("%s: %s", orchestrationSkillDir, problem)
	}
}

// oracle: on a hand-built tree, liteFlowProblems reports an unlisted file, a
// file in both lists, a listed file that is missing from either list's side,
// a file listed twice, and a lite sum one byte over the limit; and it reports
// nothing when every file is in one list and the lite sum equals the limit.
func TestLiteFlowProblemsAreReported(t *testing.T) {
	sizes := map[string]int{"SKILL.md": 60, "references/a.md": 40, "references/b.md": 500}
	lite := []string{"SKILL.md", "references/a.md"}
	conditional := map[string]string{"references/b.md": "on request"}
	if got := liteFlowProblems(sizes, lite, conditional, 100); len(got) != 0 {
		t.Errorf("a complete split at the limit: got %q, want no problem", got)
	}

	cases := []struct {
		name        string
		sizes       map[string]int
		lite        []string
		conditional map[string]string
		limit       int
		want        string
	}{
		{"one byte over", sizes, lite, conditional, 99,
			"the files a default lite task loads hold 100 bytes, over the budget of 99 (D7, OD-7)"},
		{"unlisted file", map[string]int{"SKILL.md": 60, "references/a.md": 40, "references/b.md": 500, "references/new.md": 1},
			lite, conditional, 100,
			"references/new.md: in neither liteFlowFiles nor conditionalReferences: say whether a default lite task loads it"},
		{"in both lists", sizes, lite, map[string]string{"references/b.md": "on request", "references/a.md": "on request"}, 100,
			"references/a.md: in both liteFlowFiles and conditionalReferences"},
		{"missing lite file", map[string]int{"SKILL.md": 60, "references/b.md": 500}, lite, conditional, 100,
			"references/a.md: in liteFlowFiles, but the file does not exist"},
		{"missing conditional file", map[string]int{"SKILL.md": 60, "references/a.md": 40}, lite, conditional, 100,
			"references/b.md: in conditionalReferences, but the file does not exist"},
		{"listed twice", sizes, []string{"SKILL.md", "references/a.md", "SKILL.md"}, conditional, 100,
			"SKILL.md: listed twice in liteFlowFiles"},
	}
	for _, tc := range cases {
		got := liteFlowProblems(tc.sizes, tc.lite, tc.conditional, tc.limit)
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: got %q, want [%q]", tc.name, got, tc.want)
		}
	}
}
