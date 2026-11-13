package skillcheck

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The orchestration skill's prose budget (D7 as measured by OD-7): each *.md
// file under skills/task-orchestration holds at most 10,240 bytes and each
// role contract at most 8,192 (S4e). The 90,000-byte part of the budget
// counts what a default lite task loads; lite_flow_budget_test.go holds it.
//
// The table is a ratchet. Each file's cap is its size after the last cut, and
// orchestrationSumCap is the sum of the files then: a file may shrink, and a
// PR that shrinks one lowers its row and the sum cap. Neither goes up.
const (
	orchestrationSkillDir  = "skills/task-orchestration"
	orchestrationFileLimit = 10240
	contractFileLimit      = 8192
	orchestrationSumCap    = 141780
)

type budgetRow struct {
	cap  int    // bytes the file may hold
	item string // the plan item that last cut the file
}

// orchestrationBudget has one row per *.md file under the skill, keyed by its
// slash path relative to the skill directory. A file no item has cut since
// the table landed keeps the cap it had then.
var orchestrationBudget = map[string]budgetRow{
	"SKILL.md":                                    {6466, "H4"},
	"references/adviser-providers.md":             {8646, "S4d"},
	"references/base-merge.md":                    {1873, "S4h"},
	"references/connected-sources.md":             {8634, "S4f"},
	"references/consultation-helper.md":           {10040, "S4d"},
	"references/contract-amendment.md":            {4803, "S4l"},
	"references/contracts/README.md":              {4813, "S4l"},
	"references/contracts/adversarial-tester.md":  {3191, "S4i"},
	"references/contracts/architect.md":           {6217, "S4k"},
	"references/contracts/code-review.md":         {8183, "S4j"},
	"references/contracts/design-review.md":       {6314, "S4k"},
	"references/contracts/implementer.md":         {5158, "S4j"},
	"references/contracts/spec-review.md":         {4891, "S4k"},
	"references/contracts/spec-writer.md":         {4339, "S4k"},
	"references/contracts/test-engineer.md":       {7297, "S4j"},
	"references/contracts/test-review.md":         {5141, "S4j"},
	"references/dispatching-reviews-and-tests.md": {9270, "S4i"},
	"references/github-worker-fallback.md":        {4132, "S4g"},
	"references/github-worker.md":                 {4000, "S4g"},
	"references/global-workflow.md":               {6311, "S4h"},
	"references/glossary.md":                      {2696, "H4"},
	"references/hosts.md":                         {7336, "S4g"},
	"references/measure-outcomes.md":              {1304, "S4h"},
	"references/question-patterns.md":             {1883, "S4a"},
	"references/worker-handoffs.md":               {8842, "S4i"},
}

// fileLimit is the most bytes OD-7 allows the file at rel.
func fileLimit(rel string) int {
	if strings.HasPrefix(rel, "references/contracts/") {
		return contractFileLimit
	}
	return orchestrationFileLimit
}

// orchestrationMarkdownSizes returns the byte size of every *.md file under
// dir, keyed by slash path relative to dir.
func orchestrationMarkdownSizes(t *testing.T, dir string) map[string]int {
	t.Helper()
	sizes := map[string]int{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sizes[filepath.ToSlash(rel)] = int(info.Size())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sizes
}

// oracle: every *.md file under skills/task-orchestration has a row in
// orchestrationBudget and every row names a file that exists; no file holds
// more bytes than its row's cap; the files together hold at most
// orchestrationSumCap bytes; and every cap is within OD-7's per-file limit
// (8,192 for a contract, 10,240 otherwise). A file growing past its cap, or a
// new file without a row, fails with the file's path and the rule it breaks.
func TestOrchestrationReferenceBudget(t *testing.T) {
	sizes := orchestrationMarkdownSizes(t, filepath.Join(repoRoot, orchestrationSkillDir))
	if len(sizes) == 0 {
		t.Fatalf("found no *.md file under %s", orchestrationSkillDir)
	}

	paths := make([]string, 0, len(sizes))
	for rel := range sizes {
		paths = append(paths, rel)
	}
	sort.Strings(paths)

	sum := 0
	for _, rel := range paths {
		size := sizes[rel]
		sum += size
		row, ok := orchestrationBudget[rel]
		if !ok {
			t.Errorf("%s/%s (%d bytes) has no row in orchestrationBudget: every *.md file under the skill needs a cap "+
				"of at most %d bytes (OD-7)", orchestrationSkillDir, rel, size, fileLimit(rel))
			continue
		}
		if size > row.cap {
			t.Errorf("%s/%s holds %d bytes, over its cap of %d: the budget is a ratchet, a file may not grow "+
				"past its cap (OD-7; last cut by %s)", orchestrationSkillDir, rel, size, row.cap, row.item)
		}
	}
	for rel, row := range orchestrationBudget {
		if _, ok := sizes[rel]; !ok {
			t.Errorf("orchestrationBudget has a row for %s/%s, which does not exist: drop or rename the row",
				orchestrationSkillDir, rel)
		}
		if limit := fileLimit(rel); row.cap > limit {
			t.Errorf("orchestrationBudget gives %s/%s a cap of %d bytes, over OD-7's limit of %d",
				orchestrationSkillDir, rel, row.cap, limit)
		}
	}
	if sum > orchestrationSumCap {
		t.Errorf("the *.md files under %s hold %d bytes, over the sum cap of %d: the budget is a ratchet "+
			"(OD-7)", orchestrationSkillDir, sum, orchestrationSumCap)
	}
}
