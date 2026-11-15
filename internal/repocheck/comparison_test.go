package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The comparison of the workflow with one agent is one table under this
// heading of the evaluation document.
const (
	evaluationFile    = "docs/evaluation.md"
	comparisonHeading = "## Comparison"
	notRun            = "not run"
	sumRow            = "Sum"
)

// The columns the table must have. It may have more.
var comparisonColumns = []string{"Task", "Workflow tokens", "Workflow passed", "One agent tokens", "One agent passed", "Difference"}

var digit = regexp.MustCompile(`[0-9]`)

// unfenced returns the lines of a Markdown text that are outside code
// fences.
func unfenced(text string) []string {
	var lines []string
	fence := ""
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if marker := trimmed[:min(3, len(trimmed))]; marker == "```" || marker == "~~~" {
			switch fence {
			case "":
				fence = marker
			case marker:
				fence = ""
			}
			continue
		}
		if fence == "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func tableCells(line string) []string {
	line = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "|"), "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// comparison reads the table of the evaluation document. run says that
// every task has its numbers and the sum of the difference is given.
// problems says what breaks the form of the table; a missing table is one.
func comparison(evaluation string) (run bool, problems []string) {
	lines := unfenced(evaluation)
	at := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == comparisonHeading {
			at = i
			break
		}
	}
	if at < 0 {
		return false, []string{fmt.Sprintf("%s has no heading `%s`", evaluationFile, comparisonHeading)}
	}
	var rows [][]string
	for _, line := range lines[at+1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			break
		}
		if strings.HasPrefix(trimmed, "|") {
			rows = append(rows, tableCells(trimmed))
		} else if len(rows) > 0 {
			break
		}
	}
	if len(rows) < 3 {
		return false, []string{fmt.Sprintf("%s has no table under `%s`", evaluationFile, comparisonHeading)}
	}
	column := map[string]int{}
	for i, name := range rows[0] {
		column[name] = i
	}
	for _, name := range comparisonColumns {
		if _, found := column[name]; !found {
			problems = append(problems, fmt.Sprintf("the comparison table has no column `%s`", name))
		}
	}
	if problems != nil {
		return false, problems
	}
	cell := func(row []string, name string) string {
		if column[name] < len(row) {
			return row[column[name]]
		}
		return ""
	}
	body := rows[2:] // after the header and the line of dashes
	last := body[len(body)-1]
	if cell(last, "Task") != sumRow || len(body) < 2 {
		return false, []string{fmt.Sprintf("the comparison table needs a row for each task and a last row `%s`", sumRow)}
	}
	run = true
	for _, row := range body[:len(body)-1] {
		values := 0
		for _, name := range comparisonColumns[1:] {
			switch value := cell(row, name); {
			case value == notRun:
			case digit.MatchString(value):
				values++
			default:
				problems = append(problems, fmt.Sprintf("task %s: the cell `%s` holds %q; it holds a number or `%s`", cell(row, "Task"), name, value, notRun))
			}
		}
		if values != 0 && values != len(comparisonColumns)-1 {
			problems = append(problems, fmt.Sprintf("task %s: some cells hold a number and others `%s`", cell(row, "Task"), notRun))
		}
		run = run && values == len(comparisonColumns)-1
	}
	switch sum := cell(last, "Difference"); {
	case sum == notRun:
		run = false
	case !digit.MatchString(sum):
		problems = append(problems, fmt.Sprintf("the row `%s` holds %q as the difference; it holds a number or `%s`", sumRow, sum, notRun))
	case !run:
		problems = append(problems, fmt.Sprintf("the row `%s` holds a difference, and a task has no numbers", sumRow))
	}
	return run && problems == nil, problems
}

// A benefit is stated by a sentence that joins one of these verbs or
// comparatives with what the workflow is said to change, and holds no word
// that makes it a question, a condition or a denial.
var (
	benefit = regexp.MustCompile(`(?i)\b(improv(e|es|ed|ing)|rais(e|es|ed)|boosts?|better|cleaner|higher|fewer|sav(e|es|ed|ing)|reduc(e|es|ed|ing)|cheaper|lower)\b[^.!?]*\b(quality|code|architecture|defects?|bugs?|tokens?|costs?)\b`)
	denial  = regexp.MustCompile(`(?i)\b(not|no|never|nothing|none|whether|without|unless|until|if|may|cannot|only with|unmeasured|claims?)\b|n't\b`)
	marks   = regexp.MustCompile("[*_`]")
	// What a sentence may hold without saying anything about a benefit: the
	// target of a link, the name of a role, and "lower-case".
	noClaim = regexp.MustCompile(`\]\([^)]*\)|quality-engineer|(?i)\b(lower|upper)-case\b`)
)

// claims lists the sentences of a Markdown text that state a benefit.
func claims(text string) []string {
	var found []string
	var paragraph []string
	flush := func() {
		joined := marks.ReplaceAllString(noClaim.ReplaceAllString(strings.Join(paragraph, " "), "]"), "")
		paragraph = nil
		for sentence := range strings.SplitSeq(regexp.MustCompile(`[.!?:;]\s+`).ReplaceAllString(joined, "\x00"), "\x00") {
			if benefit.MatchString(sentence) && !denial.MatchString(sentence) {
				found = append(found, strings.TrimSpace(sentence))
			}
		}
	}
	for _, line := range unfenced(text) {
		trimmed := strings.TrimSpace(line)
		item := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "#")
		if trimmed == "" || item {
			flush()
		}
		if trimmed != "" {
			paragraph = append(paragraph, trimmed)
		}
	}
	flush()
	return found
}

// claimProblems lists the benefits that documents state while the
// comparison has not been run.
func claimProblems(documents map[string]string, evaluation string) []string {
	if run, _ := comparison(evaluation); run {
		return nil
	}
	var problems []string
	for name, text := range documents {
		for _, sentence := range claims(text) {
			problems = append(problems, fmt.Sprintf("%s states a benefit, and the comparison table of %s holds no numbers: %q", name, evaluationFile, sentence))
		}
	}
	return problems
}

func comparisonTable(rows ...string) string {
	header := "| Task | Workflow tokens | Workflow passed | One agent tokens | One agent passed | Difference |\n| --- | --- | --- | --- | --- | --- |\n"
	return "# Evaluation\n\n" + comparisonHeading + "\n\n" + header + strings.Join(rows, "\n") + "\n"
}

const (
	rowNotRun = "| 1 | not run | not run | not run | not run | not run |"
	rowRun    = "| 1 | 610,000 | 3 of 3 | 240,000 | 2 of 3 | 370,000 |"
	sumNotRun = "| Sum | | | | | not run |"
	sumRun    = "| Sum | | | | | 370,000 |"
)

// Scenario: A claim without numbers
func TestAClaimWithoutNumbers(t *testing.T) {
	t.Parallel()
	readme := map[string]string{"README.md": "# gatecub\n\nThe workflow improves quality.\n"}
	for name, evaluation := range map[string]string{
		"the table is missing":     "# Evaluation\n\nNot run.\n",
		"the table holds no value": comparisonTable(rowNotRun, sumNotRun),
	} {
		if problems := claimProblems(readme, evaluation); len(problems) != 1 {
			t.Errorf("%s: the claim of the README gives the problems %q, want one", name, problems)
		}
	}
	if problems := claimProblems(readme, comparisonTable(rowRun, sumRun)); problems != nil {
		t.Errorf("with the numbers beside it the claim gives the problems %q, want none", problems)
	}
	plain := map[string]string{"README.md": "Whether seven roles give better code than one agent is **not measured**.\n\n" +
		"`gatecub tokens` shows how many tokens the roles used.\n\n```text\nThe workflow improves quality.\n```\n"}
	if problems := claimProblems(plain, "# Evaluation\n"); problems != nil {
		t.Errorf("a text that states no benefit gives the problems %q", problems)
	}
}

func TestBenefitSentencesAreFound(t *testing.T) {
	t.Parallel()
	for sentence, want := range map[string]bool{
		"The workflow improves quality.":                              true,
		"Seven roles give better code.":                               true,
		"The roles produce fewer defects and use fewer tokens.":       true,
		"The gate reduces the cost of a review.":                      true,
		"- **Roles.** They raise the quality of the architecture.":    true,
		"Nothing here is measured to improve anything.":               false,
		"Whether the roles give better code is not measured.":         false,
		"A benefit may be stated only with the table beside it.":      false,
		"The gate does not improve the code a check does not test.":   false,
		"`gatecub tokens` says what a task cost, role by role.":       false,
		"A commit is allowed only when every check has a passing run": false,
		"Save a copy of the tests as `quality-engineer` left them.":   false,
		"A name is in lower-case words, as [the label](tokens.md).":   false,
	} {
		if got := len(claims(sentence)) > 0; got != want {
			t.Errorf("claims(%q) found a benefit: %v, want %v", sentence, got, want)
		}
	}
}

// Scenario: The numbers of a release
func TestTheNumbersOfARelease(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(evaluationFile)))
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := comparison(string(data)); problems != nil {
		t.Errorf("the comparison table of %s:\n%s", evaluationFile, strings.Join(problems, "\n"))
	}
	for name, c := range map[string]struct {
		evaluation string
		run        bool
		broken     bool
	}{
		"every task has its numbers and the sum": {comparisonTable(rowRun, sumRun), true, false},
		"nothing was run":                        {comparisonTable(rowNotRun, sumNotRun), false, false},
		"no table":                               {"# Evaluation\n\n" + comparisonHeading + "\n\nNot run.\n", false, true},
		"no row for the sum":                     {comparisonTable(rowRun), false, true},
		"a sum and a task without numbers":       {comparisonTable(rowNotRun, sumRun), false, true},
		"a task with half of its numbers":        {comparisonTable("| 1 | 610,000 | 3 of 3 | not run | not run | not run |", sumNotRun), false, true},
		"an empty cell":                          {comparisonTable("| 1 | 610,000 | 3 of 3 | 240,000 | | 370,000 |", sumRun), false, true},
		"a column is missing":                    {strings.ReplaceAll(comparisonTable(rowRun, sumRun), "One agent passed", "Passed"), false, true},
	} {
		run, problems := comparison(c.evaluation)
		if run != c.run || (problems != nil) != c.broken {
			t.Errorf("%s: run %v, problems %q; want run %v, broken %v", name, run, problems, c.run, c.broken)
		}
	}
}

// The documents a reader takes for the project's own account of itself.
func TestDocumentsStateNoBenefitWithoutTheComparison(t *testing.T) {
	t.Parallel()
	evaluation, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(evaluationFile)))
	if err != nil {
		t.Fatal(err)
	}
	documents := map[string]string{}
	for _, file := range linkCheckedDocuments(t) {
		if file != "README.md" && file != "CONTRIBUTING.md" && !strings.HasPrefix(file, "docs/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, file))
		if err != nil {
			t.Fatal(err)
		}
		documents[file] = string(data)
	}
	if len(documents) < 3 {
		t.Fatalf("only %d documents were found to read", len(documents))
	}
	for _, problem := range claimProblems(documents, string(evaluation)) {
		t.Error(problem)
	}
}
