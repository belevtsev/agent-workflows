package change

import "strings"

// OptionalRow is an original Markdown row; it carries no validation or authority.
type OptionalRow struct {
	Line  int
	Cells []string
}

// OptionalSection exposes a conventional note section without requiring it in
// the task schema. A present section may contain arbitrary prose instead.
type OptionalSection struct {
	Line    int
	Headers []string
	Rows    []OptionalRow
}

// ReadOptionalSection reuses the task Markdown parser, including fenced-block
// exclusion and escaped cells. It never changes structural task validity.
func ReadOptionalSection(data []byte, heading string) *OptionalSection {
	ls := lines(data)
	var result *OptionalSection
	for _, l := range ls {
		if strings.EqualFold(strings.TrimSpace(l.text), "## "+heading) {
			result = &OptionalSection{Line: l.n, Headers: []string{}, Rows: []OptionalRow{}}
			break
		}
	}
	if result == nil {
		return nil
	}
	if t := tableUnder(ls, heading); t != nil {
		result.Headers = append(result.Headers, t.head...)
		for _, row := range t.rows {
			result.Rows = append(result.Rows, OptionalRow{Line: row.line, Cells: row.cells})
		}
	}
	return result
}
