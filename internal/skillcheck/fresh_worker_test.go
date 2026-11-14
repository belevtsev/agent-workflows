package skillcheck

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Fresh workers and capped results (D15): the coordinator loads one file per
// step, and every worker returns at most the capped result and writes the
// rest to its report file.

const (
	orchestrationSkillFile = orchestrationSkillDir + "/SKILL.md"
	stepTableHeading       = "## Read when the step starts"
	handoffCapPhrase       = "at most 6 KiB"
	reportFilePhrase       = "report file"
)

// stepRow is one body row of the step table: its first cell and the link
// destinations in the row.
type stepRow struct {
	step    string
	targets []string
}

// stepTableRows returns the body rows of the table under stepTableHeading in
// the skill's Markdown: the lines that start with "|" after the heading, up
// to the next heading, without the header row and its separator.
func stepTableRows(skill []byte) []stepRow {
	var rows []stepRow
	inSection, seen := false, 0
	for line := range strings.SplitSeq(string(skill), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			inSection = trimmed == stepTableHeading
			continue
		}
		if !inSection || !strings.HasPrefix(trimmed, "|") {
			continue
		}
		if seen++; seen <= 2 {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		rows = append(rows, stepRow{step: strings.TrimSpace(cells[0]), targets: markdownTargets([]byte(line))})
	}
	return rows
}

// stepTableProblems reports each row of the step table that does not link
// exactly one Markdown file under references/, and a missing table.
func stepTableProblems(skill []byte) []string {
	rows := stepTableRows(skill)
	if len(rows) == 0 {
		return []string{fmt.Sprintf("no table rows under %q", stepTableHeading)}
	}
	var problems []string
	for _, row := range rows {
		if len(row.targets) != 1 {
			problems = append(problems, fmt.Sprintf("step %q links %d files, want exactly 1: %q", row.step, len(row.targets), row.targets))
			continue
		}
		file, _, _ := strings.Cut(row.targets[0], "#")
		if !strings.HasPrefix(file, "references/") || !strings.HasSuffix(file, ".md") {
			problems = append(problems, fmt.Sprintf("step %q links %q, want a Markdown file under references/", row.step, row.targets[0]))
		}
	}
	return problems
}

// oracle: under "Read when the step starts" in the task-orchestration
// SKILL.md, every table row holds exactly one link, to a Markdown file under
// references/. A step that names two files makes the coordinator load both.
func TestEveryStepRowLinksOneReference(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(orchestrationSkillFile)))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range stepTableProblems(skill) {
		t.Errorf("%s: %s", orchestrationSkillFile, problem)
	}
}

// oracle: a row with two links, a row with none, a row that links outside
// references/ and a skill without the table are each reported; a table whose
// rows link one reference each is clean.
func TestStepTableProblemsAreReported(t *testing.T) {
	table := func(rows ...string) []byte {
		return []byte("# Skill\n\n[flow](references/a.md)\n\n" + stepTableHeading + "\n\n| Step | Reference |\n| --- | --- |\n" +
			strings.Join(rows, "\n") + "\n\n## Next\n\n| x | [b](references/b.md), [c](references/c.md) |\n")
	}
	if problems := stepTableProblems(table("| One | [a](references/a.md) |", "| Two | [b](references/sub/b.md#part) |")); len(problems) != 0 {
		t.Errorf("a table with one reference per row was rejected: %q", problems)
	}
	for name, tc := range map[string]struct {
		skill []byte
		want  string
	}{
		"two links":          {table("| Tests | [a](references/a.md), [b](references/b.md) |"), `step "Tests" links 2 files`},
		"no link":            {table("| Tests | read the contract |"), `step "Tests" links 0 files`},
		"outside references": {table("| Tests | [docs](../../docs/operations.md) |"), `step "Tests" links "../../docs/operations.md"`},
		"no table":           {[]byte("# Skill\n\n" + stepTableHeading + "\n\nNo table.\n"), "no table rows"},
	} {
		t.Run(name, func(t *testing.T) {
			wantProblem(t, stepTableProblems(tc.skill), tc.want)
		})
	}
}

// referenceTargets returns the Markdown files under the skill directory that
// the document at rel (a slash path relative to the skill directory) links,
// as slash paths relative to the skill directory, without anchors.
func referenceTargets(t *testing.T, rel string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(orchestrationSkillDir), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, target := range markdownTargets(data) {
		file, _, _ := strings.Cut(target, "#")
		if file == "" || strings.Contains(file, "://") || !strings.HasSuffix(file, ".md") {
			continue
		}
		files = append(files, path.Join(path.Dir(rel), file))
	}
	return files
}

// oracle: every *.md file under the skill's references/ is linked from
// SKILL.md, or from a file that a row of the step table links. A reference
// further away cannot be found from the one file a step names.
func TestEveryReferenceIsOneLinkFromItsStep(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(orchestrationSkillFile)))
	if err != nil {
		t.Fatal(err)
	}
	reachable := map[string]bool{}
	for _, file := range referenceTargets(t, "SKILL.md") {
		reachable[file] = true
	}
	for _, row := range stepTableRows(skill) {
		for _, target := range row.targets {
			file, _, _ := strings.Cut(target, "#")
			for _, linked := range referenceTargets(t, file) {
				reachable[linked] = true
			}
		}
	}
	found := 0
	for rel := range orchestrationMarkdownSizes(t, filepath.Join(repoRoot, orchestrationSkillDir)) {
		if !strings.HasPrefix(rel, "references/") {
			continue
		}
		found++
		if !reachable[rel] {
			t.Errorf("%s/%s is linked neither from SKILL.md nor from a file its step table names", orchestrationSkillDir, rel)
		}
	}
	if found == 0 {
		t.Fatalf("found no *.md file under %s/references", orchestrationSkillDir)
	}
}

// workerFiles returns the files a dispatched worker reads as its assignment:
// every role contract but the roster README, the GitHub worker's contract and
// every plugin agent definition. Paths are slash paths relative to root.
func workerFiles(root string) ([]string, error) {
	files := []string{orchestrationSkillDir + "/references/github-worker-fallback.md"}
	for _, dir := range []string{orchestrationSkillDir + "/references/contracts", "agents"} {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Type()&fs.ModeType == 0 && strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "README.md" {
				files = append(files, dir+"/"+entry.Name())
			}
		}
	}
	sort.Strings(files)
	return files, nil
}

// prose joins the words of a hard-wrapped document with single spaces, so a
// phrase split over two lines still matches.
func prose(data []byte) string {
	return strings.Join(strings.Fields(string(data)), " ")
}

// oracle: every worker file (workerFiles) contains both "at most 6 KiB" and
// "report file", so each worker is told the cap on its result and where the
// rest goes by the one file it reads, whichever of the two that is.
func TestEveryWorkerFileStatesTheHandoffCap(t *testing.T) {
	files, err := workerFiles(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Nine contracts, the GitHub worker's contract and seven agents.
	if len(files) < 17 {
		t.Fatalf("found %d worker files, want at least 17: %q", len(files), files)
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		text := prose(data)
		for _, phrase := range []string{handoffCapPhrase, reportFilePhrase} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s does not say %q: state the cap on the worker's result and the report file that takes the rest", file, phrase)
			}
		}
	}
}

var (
	sentenceEnd  = regexp.MustCompile(`[.;:?!]\s+`)
	inlineWord   = regexp.MustCompile(`(?i)\binline\b`)
	negation     = regexp.MustCompile(`(?i)\b(?:never|not|no)\b`)
	wholeProduct = regexp.MustCompile(`(?i)\b(?:full|whole|complete|entire)\s+(?:review|report|reasoning|findings|analysis)\b`)
)

// inlineReturnProblems reports each sentence of a worker file that asks for
// the long form of the work in the result itself. A sentence ends at ". ",
// "; ", ": ", "? " or "! ". It is reported when it says "inline" without
// "never", "not" or "no", or when it names the full, whole, complete or
// entire review, report, reasoning, findings or analysis without naming the
// report file as its place.
//
// The check reads words, not meaning. It misses a paraphrase ("reply with
// everything you found"), a request spread over two sentences, and a contract
// that asks for each finding's reasoning in its summary; cw task handoff
// refuses those results by size (internal/taskflow/report.go, handoffCaps).
func inlineReturnProblems(file string, data []byte) []string {
	var problems []string
	for _, sentence := range sentenceEnd.Split(prose(data), -1) {
		switch {
		case inlineWord.MatchString(sentence) && !negation.MatchString(sentence):
			problems = append(problems, fmt.Sprintf("%s asks for an inline return: %q", file, sentence))
		case wholeProduct.MatchString(sentence) && !strings.Contains(sentence, reportFilePhrase):
			problems = append(problems, fmt.Sprintf("%s names the full work without the report file: %q", file, sentence))
		}
	}
	return problems
}

// oracle: no worker file (workerFiles) has a sentence that inlineReturnProblems
// reports, so no contract or agent definition tells a worker to return a full
// review or report in its result.
func TestNoWorkerFileAsksForAnInlineReview(t *testing.T) {
	files, err := workerFiles(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range inlineReturnProblems(file, data) {
			t.Error(problem)
		}
	}
}

// oracle: a sentence that asks for the review inline, or for the full review
// in the reply, is reported with its file; the cap sentence of the contracts,
// a negated "inline" and a full review sent to the report file are not.
func TestInlineReturnProblemsAreReported(t *testing.T) {
	const file = "contracts/code-review.md"
	for name, text := range map[string]string{
		"inline review":       "Read the bundle. Return the review inline, with\nevery finding.",
		"full review":         "Reply with the full review and its evidence.",
		"complete report":     "Stop when done; the complete report goes in your\nfinal message.",
		"whole reasoning":     "Give the whole reasoning for each finding in `summary`.",
		"cap, then full text": "The result is at most 6 KiB, with the rest in the report file. Return the entire analysis to the coordinator.",
	} {
		t.Run(name, func(t *testing.T) {
			if problems := inlineReturnProblems(file, []byte(text)); len(problems) != 1 {
				t.Errorf("got %d problems, want 1: %q", len(problems), problems)
			} else {
				wantProblem(t, problems, file)
			}
		})
	}
	for name, text := range map[string]string{
		"cap sentence":       "The result is at most 6 KiB. Put the full reasoning in the report\nfile that the result's `report` names; `cw task handoff` refuses a larger result.",
		"negated inline":     "Never return the review inline.",
		"review to the file": "Write the full review to the report file that the result's `report` names.",
		"unrelated":          "Review the whole package against the contract.",
	} {
		t.Run(name, func(t *testing.T) {
			if problems := inlineReturnProblems(file, []byte(text)); len(problems) != 0 {
				t.Errorf("a compliant sentence was reported: %q", problems)
			}
		})
	}
}
