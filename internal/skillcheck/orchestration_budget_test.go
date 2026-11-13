package skillcheck

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The orchestration skill's prose budget (D7 as measured by OD-7): the *.md
// files under skills/task-orchestration sum to at most 90,000 bytes, each at
// most 10,240, and each role contract at most 8,192 (S4e).
//
// The budget lands as a ratchet. Each file's cap is its size when the test
// landed until a cut PR lowers it; the target is where the cut must end. A cut
// PR moves its rows from cap to target and lowers orchestrationSumCap by the
// bytes it removed. The last cut sets orchestrationSumCap to
// orchestrationSumTarget.
const (
	orchestrationSkillDir  = "skills/task-orchestration"
	orchestrationFileLimit = 10240
	contractFileLimit      = 8192
	orchestrationSumCap    = 169047
	orchestrationSumTarget = 90000
)

type budgetRow struct {
	cap    int    // bytes the file may hold now
	target int    // bytes the file may hold once its item lands
	item   string // the plan item that cuts the file to its target
}

// orchestrationBudget has one row per *.md file under the skill, keyed by its
// slash path relative to the skill directory. S4f covers the four files S4
// had no item for; S4d gives consultation-helper.md the per-file limit.
var orchestrationBudget = map[string]budgetRow{
	"SKILL.md":                                    {9143, 10240, "S3, S4a"},
	"references/adviser-providers.md":             {10240, 10240, "S4d"},
	"references/base-merge.md":                    {1873, 1800, "S4h"},
	"references/connected-sources.md":             {10240, 10240, "S4f"},
	"references/consultation-helper.md":           {10240, 10240, "S4d"},
	"references/contract-amendment.md":            {10240, 10240, "S3"},
	"references/contracts/README.md":              {8192, 8192, "S3, S4e"},
	"references/contracts/adversarial-tester.md":  {4089, 8192, "S4e"},
	"references/contracts/architect.md":           {7418, 8192, "S4e"},
	"references/contracts/code-review.md":         {8192, 8192, "S4e"},
	"references/contracts/design-review.md":       {6851, 8192, "S4e"},
	"references/contracts/implementer.md":         {5374, 8192, "S4e"},
	"references/contracts/spec-review.md":         {5196, 8192, "S4e"},
	"references/contracts/spec-writer.md":         {4420, 8192, "S4e"},
	"references/contracts/test-engineer.md":       {8192, 8192, "S4e"},
	"references/contracts/test-review.md":         {5638, 8192, "S4e"},
	"references/dispatching-reviews-and-tests.md": {10240, 10240, "S4e"},
	"references/github-worker-fallback.md":        {10240, 10240, "S4f"},
	"references/github-worker.md":                 {10240, 10240, "S4f"},
	"references/global-workflow.md":               {6198, 6000, "S4h"},
	"references/glossary.md":                      {3010, 4096, "S4a"},
	"references/hosts-claude-code.md":             {10240, 10240, "S4f"},
	"references/hosts-codex.md":                   {10240, 10240, "S4f"},
	"references/hosts.md":                         {10240, 10240, "S4f"},
	"references/measure-outcomes.md":              {1028, 800, "S4h"},
	"references/question-patterns.md":             {1883, 10240, "S4a"},
	"references/worker-handoffs.md":               {10240, 10240, "S4c"},
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
// orchestrationSumCap bytes; and every target is within OD-7's per-file limit
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
				"and a target, at most %d bytes (OD-7)", orchestrationSkillDir, rel, size, fileLimit(rel))
			continue
		}
		if size > row.cap {
			t.Errorf("%s/%s holds %d bytes, over its cap of %d: the budget is a ratchet, a file may not grow "+
				"past its cap (OD-7; target %d after %s)", orchestrationSkillDir, rel, size, row.cap, row.target, row.item)
		}
	}
	for rel, row := range orchestrationBudget {
		if _, ok := sizes[rel]; !ok {
			t.Errorf("orchestrationBudget has a row for %s/%s, which does not exist: drop or rename the row",
				orchestrationSkillDir, rel)
		}
		if limit := fileLimit(rel); row.target > limit {
			t.Errorf("orchestrationBudget gives %s/%s a target of %d bytes, over OD-7's limit of %d",
				orchestrationSkillDir, rel, row.target, limit)
		}
	}
	if sum > orchestrationSumCap {
		t.Errorf("the *.md files under %s hold %d bytes, over the sum cap of %d: the budget is a ratchet "+
			"(OD-7; target %d)", orchestrationSkillDir, sum, orchestrationSumCap, orchestrationSumTarget)
	}
}
