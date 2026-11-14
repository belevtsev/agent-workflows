// Package tokens counts what a task cost in tokens. The counts come from
// the record the host keeps of a session, never from what a model says. A
// count that cannot be read is unknown, and never zero.
package tokens

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// The hosts whose records cw reads.
const (
	Claude = "claude"
	Codex  = "codex"
)

// Counts are the tokens of the four kinds a host bills apart.
type Counts struct {
	Input      int64 `json:"input"`       // input that was not read from the cache
	CacheWrite int64 `json:"cache_write"` // input written to the cache
	CacheRead  int64 `json:"cache_read"`  // input read from the cache
	Output     int64 `json:"output"`
}

// Sum is the four kinds together.
func (c Counts) Sum() int64 { return c.Input + c.CacheWrite + c.CacheRead + c.Output }

func (c *Counts) add(o Counts) {
	c.Input += o.Input
	c.CacheWrite += o.CacheWrite
	c.CacheRead += o.CacheRead
	c.Output += o.Output
}

// most keeps the larger count of each kind. A host may write one answer in
// several lines, and the last of them holds the whole answer.
func (c *Counts) most(o Counts) {
	c.Input = max(c.Input, o.Input)
	c.CacheWrite = max(c.CacheWrite, o.CacheWrite)
	c.CacheRead = max(c.CacheRead, o.CacheRead)
	c.Output = max(c.Output, o.Output)
}

func (c Counts) valid() bool {
	return c.Input >= 0 && c.CacheWrite >= 0 && c.CacheRead >= 0 && c.Output >= 0
}

// Label is the first line of a brief: it gives the tokens of a start to a
// task, a stage and a round.
type Label struct {
	Task  string
	Stage string
	Round int
}

// The stages a label may name, each with the role that works in it.
var stageRoles = []struct{ stage, role string }{
	{"lookup", "lookup"}, {"spec", "spec-writer"}, {"spec-review", "reviewer"},
	{"design", "architect"}, {"design-review", "reviewer"}, {"tests", "quality-engineer"},
	{"code", "developer"}, {"review", "reviewer"}, {"commit", "git"},
}

// RoleOf is the role of a stage, empty for a stage no label may name.
func RoleOf(stage string) string {
	for _, s := range stageRoles {
		if s.stage == stage {
			return s.role
		}
	}
	return ""
}

var labelLine = regexp.MustCompile(`^cw-task: ([a-z0-9]+(?:-[a-z0-9]+)*) ([a-z-]+) ([1-9][0-9]{0,5})$`)

// ParseLabel reads the label from the text of a brief. ok is false when the
// first line is not a label.
func ParseLabel(brief string) (label Label, ok bool) {
	first, _, _ := strings.Cut(strings.TrimLeft(brief, " \t\r\n"), "\n")
	m := labelLine.FindStringSubmatch(strings.TrimSpace(first))
	if m == nil || RoleOf(m[2]) == "" {
		return Label{}, false
	}
	round, _ := strconv.Atoi(m[3])
	return Label{Task: m[1], Stage: m[2], Round: round}, true
}

// reading is what one record of a host gives.
type reading struct {
	models map[string]Counts // per model the record names
	brief  string            // the first message, which holds the label
	why    string            // why the counts are unknown; empty when they are known
	size   int64             // the size of the record before it was read
}

// maxLine bounds one line of a record. A longer line cannot be read.
const maxLine = 64 << 20

// eachLine calls visit for every line of a file. whole is false for a last
// line that does not end: the host may still be writing it.
func eachLine(path string, visit func(n int, line []byte, whole bool)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxLine)
	whole := true
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		whole = !atEOF || bytes.IndexByte(data, '\n') >= 0
		return bufio.ScanLines(data, atEOF)
	})
	for n := 1; scanner.Scan(); n++ {
		if line := bytes.TrimSpace(scanner.Bytes()); len(line) > 0 {
			visit(n, line, whole)
		}
	}
	return scanner.Err()
}

// text is the text of a message, which a host writes as a string or as a
// list of parts.
func text(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) == nil {
		for _, part := range parts {
			if part.Text != "" {
				return part.Text
			}
		}
	}
	return ""
}

// collect sums the counts of a record, each answer once.
type collect struct {
	answers map[string]*answer
	order   int
	broken  string
}

type answer struct {
	model  string
	counts Counts
}

func (c *collect) add(key, model string, counts Counts) {
	if c.answers == nil {
		c.answers = map[string]*answer{}
	}
	if key == "" {
		c.order++
		key = fmt.Sprintf("\x00%d", c.order)
	}
	if c.answers[key] == nil {
		c.answers[key] = &answer{model: model}
	}
	c.answers[key].counts.most(counts)
}

// result turns what was collected into a reading. A record that holds no
// count, or a line that cannot be read, makes the counts unknown.
func (c *collect) result(r reading, err error) reading {
	switch {
	case errors.Is(err, os.ErrNotExist):
		r.why = "the record does not exist"
	case errors.Is(err, bufio.ErrTooLong):
		r.why = "a line of the record is longer than cw reads"
	case err != nil:
		r.why = "the record cannot be read: " + err.Error()
	case c.broken != "":
		r.why = c.broken
	case len(c.answers) == 0:
		r.why = "the record holds no token count"
	}
	if r.why != "" {
		return r
	}
	r.models = map[string]Counts{}
	for _, a := range c.answers {
		sum := r.models[a.model]
		sum.add(a.counts)
		r.models[a.model] = sum
	}
	return r
}

// readClaude reads the transcript Claude Code keeps of a session or of a
// role: one JSON object in each line.
func readClaude(path string) reading {
	var r reading
	var c collect
	briefFound := false
	err := eachLine(path, func(n int, line []byte, whole bool) {
		// Most lines hold no count. They are not decoded, and they must
		// still be JSON objects: a damaged line may be a lost answer.
		if briefFound && !bytes.Contains(line, []byte(`"usage"`)) {
			if whole && (line[0] != '{' || !json.Valid(line)) {
				c.broken = fmt.Sprintf("line %d of the record is not a JSON object", n)
			}
			return
		}
		var entry struct {
			Type    string `json:"type"`
			Message *struct {
				ID      string          `json:"id"`
				Model   string          `json:"model"`
				Content json.RawMessage `json:"content"`
				Usage   *struct {
					Input      *int64 `json:"input_tokens"`
					CacheWrite *int64 `json:"cache_creation_input_tokens"`
					CacheRead  *int64 `json:"cache_read_input_tokens"`
					Output     *int64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &entry) != nil {
			if whole {
				c.broken = fmt.Sprintf("line %d of the record is not a JSON object", n)
			}
			return
		}
		if entry.Message == nil {
			return
		}
		if entry.Type == "user" && !briefFound {
			r.brief, briefFound = text(entry.Message.Content), true
		}
		usage := entry.Message.Usage
		if entry.Type != "assistant" || usage == nil || entry.Message.Model == "<synthetic>" {
			return
		}
		if usage.Input == nil || usage.Output == nil {
			c.broken = fmt.Sprintf("line %d of the record holds an answer without its token counts", n)
			return
		}
		counts := Counts{Input: *usage.Input, Output: *usage.Output}
		if usage.CacheWrite != nil {
			counts.CacheWrite = *usage.CacheWrite
		}
		if usage.CacheRead != nil {
			counts.CacheRead = *usage.CacheRead
		}
		if !counts.valid() {
			c.broken = fmt.Sprintf("line %d of the record holds a negative token count", n)
			return
		}
		c.add(entry.Message.ID, entry.Message.Model, counts)
	})
	return c.result(r, err)
}

// codexUsage is a usage object of a Codex token_count event.
type codexUsage struct {
	Input      *int64 `json:"input_tokens"`
	Cached     *int64 `json:"cached_input_tokens"`
	CacheWrite *int64 `json:"cache_write_input_tokens"`
	Output     *int64 `json:"output_tokens"`
}

// counts are the four kinds of a usage object. Codex counts cached and
// written input inside its input. ok is false without an input or output
// count.
func (u *codexUsage) counts() (c Counts, ok bool) {
	if u == nil || u.Input == nil || u.Output == nil {
		return Counts{}, false
	}
	c.Output = *u.Output
	if u.Cached != nil {
		c.CacheRead = *u.Cached
	}
	if u.CacheWrite != nil {
		c.CacheWrite = *u.CacheWrite
	}
	c.Input = *u.Input - c.CacheRead - c.CacheWrite
	return c, true
}

func (c Counts) minus(o Counts) Counts {
	return Counts{c.Input - o.Input, c.CacheWrite - o.CacheWrite, c.CacheRead - o.CacheRead, c.Output - o.Output}
}

// hostContext says whether the text of a user message is what Codex adds
// before the first prompt of a thread: the AGENTS.md instructions or the
// environment. Such a message is not the brief.
func hostContext(s string) bool {
	s = strings.TrimLeft(s, " \t\r\n")
	return strings.HasPrefix(s, "# AGENTS.md instructions") || strings.HasPrefix(s, "<environment_context>")
}

// readCodex reads the record Codex keeps of one thread. Codex writes an
// event_msg of payload.type token_count after each request: info holds the
// running total of the thread in total_token_usage, and the request itself
// in last_token_usage. The thread is counted once, from its totals: each
// rise of the total goes to the model of the turn_context before it, so the
// sum is the last total. A total that falls is a reset: the count goes on
// from it, so the sum is the totals before each fall and the last total. A
// first total that is more than its own request is not understood, and the
// counts are unknown.
func readCodex(path string) reading {
	var r reading
	var c collect
	model := ""
	var prev *Counts
	err := eachLine(path, func(n int, line []byte, whole bool) {
		var entry struct {
			Type    string `json:"type"`
			Payload *struct {
				Model   string          `json:"model"`
				Type    string          `json:"type"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Info    *struct {
					Total *codexUsage `json:"total_token_usage"`
					Last  *codexUsage `json:"last_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &entry) != nil {
			if whole {
				c.broken = fmt.Sprintf("line %d of the record is not a JSON object", n)
			}
			return
		}
		p := entry.Payload
		switch {
		case p == nil:
		case entry.Type == "turn_context":
			model = p.Model
		case entry.Type == "response_item" && p.Type == "message" && p.Role == "user" && r.brief == "":
			if brief := text(p.Content); !hostContext(brief) {
				r.brief = brief
			}
		case entry.Type == "event_msg" && p.Type == "token_count" && p.Info != nil:
			total, ok := p.Info.Total.counts()
			last, lastOK := p.Info.Last.counts()
			if !ok || !total.valid() {
				c.broken = fmt.Sprintf("line %d of the record holds a running total without its token counts", n)
				return
			}
			if prev == nil {
				// The first total is the first request alone. A larger one
				// holds tokens this record does not show being spent.
				if !lastOK || last != total {
					c.broken = fmt.Sprintf("line %d of the record starts from a running total of earlier requests", n)
					return
				}
				prev = &Counts{}
			}
			rise := total.minus(*prev)
			if !rise.valid() {
				// Codex resets the total after a context window overflows:
				// the total before the fall stays counted.
				rise = total
			}
			*prev = total
			c.add("", model, rise)
		}
	})
	return c.result(r, err)
}
