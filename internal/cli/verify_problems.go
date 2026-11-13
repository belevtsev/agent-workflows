package cli

import (
	"fmt"
	"regexp"
	"strings"
)

// maxSummaryProblems bounds the problems a verify summary lists; the rest are
// counted in omitted_problems. cw task verify --full lists every problem.
const maxSummaryProblems = 20

// problemGroup joins verify problems that differ only in one ID. pattern
// matches one problem as taskflow words it: its first group is the ID, and
// problems join only when their further groups are equal. plural takes the
// joined IDs and then the further groups. A problem worded otherwise is
// listed as it is.
type problemGroup struct {
	pattern *regexp.Regexp
	plural  string
}

var problemGroups = []problemGroup{
	{regexp.MustCompile(`^requirement (\S+) is not satisfied$`), "requirements %s are not satisfied"},
	{regexp.MustCompile(`^requirement (\S+) evidence is stale$`), "requirements %s have stale evidence"},
	{regexp.MustCompile(`^requirement (\S+) check references are invalid$`), "requirements %s have invalid check references"},
	{regexp.MustCompile(`^requirement (\S+) lacks permitted non-execution reasoning$`), "requirements %s lack permitted non-execution reasoning"},
	{regexp.MustCompile(`^latest run of (\S+) is (\S+)$`), "latest runs of %s are %s"},
	{regexp.MustCompile(`^worker assignment (\S+) has no terminal handoff$`), "worker assignments %s have no terminal handoff"},
	{regexp.MustCompile(`^worker assignment (\S+) lacks coordinator acceptance or explicit unused rationale$`), "worker assignments %s lack coordinator acceptance or explicit unused rationale"},
	{regexp.MustCompile(`^worker assignment (\S+) is not completed$`), "worker assignments %s are not completed"},
}

// groupProblems joins the problems of each problemGroups pattern that agree
// in their further groups into one line, at the place of the first, keeps
// every other problem as it is and in order, then keeps the first
// maxSummaryProblems lines and counts the rest.
func groupProblems(problems []string) (lines []string, omitted int) {
	type joined struct {
		line  int
		group problemGroup
		ids   []string
		rest  []any
	}
	lines = make([]string, 0, len(problems))
	byKey := map[string]*joined{}
	var order []*joined
	for _, problem := range problems {
		entry, key := (*joined)(nil), ""
		for index, group := range problemGroups {
			if match := group.pattern.FindStringSubmatch(problem); match != nil {
				key = fmt.Sprint(index, match[2:])
				if entry = byKey[key]; entry != nil {
					entry.ids = append(entry.ids, match[1])
				} else {
					entry = &joined{line: len(lines), group: group, ids: []string{match[1]}}
					for _, part := range match[2:] {
						entry.rest = append(entry.rest, part)
					}
					byKey[key] = entry
					order = append(order, entry)
					lines = append(lines, problem)
				}
				break
			}
		}
		if entry == nil {
			lines = append(lines, problem)
		}
	}
	for _, entry := range order {
		if len(entry.ids) > 1 {
			lines[entry.line] = fmt.Sprintf(entry.group.plural, append([]any{strings.Join(entry.ids, ", ")}, entry.rest...)...)
		}
	}
	if len(lines) > maxSummaryProblems {
		return lines[:maxSummaryProblems], len(lines) - maxSummaryProblems
	}
	return lines, 0
}
