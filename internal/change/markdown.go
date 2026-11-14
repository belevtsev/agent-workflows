package change

import "strings"

// line is one line of a Markdown file that stands outside a fenced block.
type line struct {
	n    int // counted from 1
	text string
}

// unix reads a text with the line ends of any system.
func unix(data []byte) string {
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// lines returns the lines of a Markdown text that are not inside a fenced
// code block, so that an example in a file is never read as its content.
func lines(data []byte) []line {
	var out []line
	fence := ""
	for i, text := range strings.Split(unix(data), "\n") {
		trimmed := strings.TrimSpace(text)
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
		case strings.HasPrefix(trimmed, "```"), strings.HasPrefix(trimmed, "~~~"):
			fence = trimmed[:3]
		default:
			out = append(out, line{i + 1, text})
		}
	}
	return out
}

// plain is a cell or a name without the marks around it.
func plain(s string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "`"))
}

// table is a Markdown table.
type table struct {
	line int // the line of its header
	head []string
	rows []row
}

type row struct {
	line  int
	cells []string
}

// col is the place of a column, or -1 when the table has none of that name.
func (t *table) col(name string) int {
	for i, h := range t.head {
		if strings.EqualFold(plain(h), name) {
			return i
		}
	}
	return -1
}

// get is the cell at a place, empty when the row is shorter.
func (r row) get(i int) string {
	if i < 0 || i >= len(r.cells) {
		return ""
	}
	return r.cells[i]
}

// cells cuts a table line into its cells. `\|` is a bar inside a cell.
func cells(text string) []string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(strings.TrimPrefix(text, "|"), "|")
	var out []string
	var cell strings.Builder
	for i := 0; i < len(text); i++ {
		switch {
		case text[i] == '\\' && i+1 < len(text) && text[i+1] == '|':
			cell.WriteByte('|')
			i++
		case text[i] == '|':
			out = append(out, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteByte(text[i])
		}
	}
	return append(out, strings.TrimSpace(cell.String()))
}

func isRow(l line) bool { return strings.HasPrefix(strings.TrimSpace(l.text), "|") }

// isRule says whether a line is the rule below a table's header.
func isRule(l line) bool {
	if !isRow(l) {
		return false
	}
	for _, cell := range cells(l.text) {
		if cell == "" || strings.Trim(cell, "-: ") != "" {
			return false
		}
	}
	return true
}

// tableAt reads the table that starts at ls[i], or returns nil.
func tableAt(ls []line, i int) *table {
	if i+1 >= len(ls) || !isRow(ls[i]) || !isRule(ls[i+1]) {
		return nil
	}
	t := &table{line: ls[i].n, head: cells(ls[i].text)}
	for _, l := range ls[i+2:] {
		if !isRow(l) {
			break
		}
		t.rows = append(t.rows, row{l.n, cells(l.text)})
	}
	return t
}

// tableUnder is the first table below the heading `## <name>` and above the
// next heading of that level or a higher one.
func tableUnder(ls []line, name string) *table {
	inside := false
	for i, l := range ls {
		text := strings.TrimSpace(l.text)
		switch {
		case strings.HasPrefix(text, "## "):
			inside = strings.EqualFold(strings.TrimSpace(text[3:]), name)
		case strings.HasPrefix(text, "# "):
			inside = false
		case inside:
			if t := tableAt(ls, i); t != nil {
				return t
			}
		}
	}
	return nil
}

// tableWith is the first table of a file that has the column.
func tableWith(ls []line, column string) *table {
	for i := range ls {
		if t := tableAt(ls, i); t != nil && t.col(column) >= 0 {
			return t
		}
	}
	return nil
}
