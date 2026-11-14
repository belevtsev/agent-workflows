package roles

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/pelletier/go-toml/v2"
)

// FileName is the roles file at the root of a repository. It is optional.
const FileName = "cw-roles.toml"

// Config is what cw-roles.toml sets.
type Config struct {
	// Tests are the patterns of the test paths; nil when the file names
	// none.
	Tests []string
	// Models holds, by host and role, the fields the file sets. An empty
	// field keeps the default.
	Models map[string]map[string]Model
}

// Problem is one broken rule of the roles file.
type Problem struct {
	Line int // 0 when the line is unknown
	Text string
}

// Error lists every broken rule found in the roles file.
type Error struct {
	Problems []Problem
}

func (e *Error) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		if p.Line > 0 {
			lines[i] = fmt.Sprintf("%s:%d: %s", FileName, p.Line, p.Text)
		} else {
			lines[i] = fmt.Sprintf("%s: %s", FileName, p.Text)
		}
	}
	return strings.Join(lines, "\n")
}

// Load reads the roles file at the root of a working tree. A missing file
// is an empty configuration.
func Load(root string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse reads a roles file. It refuses an unknown key, host or role and a
// pattern cw.toml would refuse. It does not judge a model name: hosts
// change them. Every error it returns is an *Error.
func Parse(data []byte) (*Config, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			line, _ := decodeErr.Position()
			return nil, &Error{Problems: []Problem{{Line: line, Text: "not valid TOML: " + decodeErr.Error()}}}
		}
		return nil, &Error{Problems: []Problem{{Text: "not valid TOML: " + err.Error()}}}
	}
	p := parser{data: data, cfg: &Config{Models: map[string]map[string]Model{}}}
	if v, ok := raw["version"].(int64); !ok || v != 1 {
		p.fail(p.line(`version\s*=`), "`version` must be 1")
	}
	for _, key := range sortedKeys(raw) {
		switch {
		case key == "version":
		case key == "paths":
			p.paths(raw[key])
		case slices.Contains(Hosts, key):
			p.host(key, raw[key])
		default:
			p.fail(p.line(`\[?\s*`+regexp.QuoteMeta(key)+`\b`), "unknown key `%s`; the file holds `version`, `[paths]`, and `[claude.<role>]` and `[codex.<role>]`", key)
		}
	}
	if len(p.problems) > 0 {
		return nil, &Error{Problems: p.problems}
	}
	return p.cfg, nil
}

type parser struct {
	data     []byte
	cfg      *Config
	problems []Problem
}

func (p *parser) fail(line int, format string, args ...any) {
	p.problems = append(p.problems, Problem{Line: line, Text: fmt.Sprintf(format, args...)})
}

// line is the first line that starts with the expression, or 0.
func (p *parser) line(expr string) int {
	re, err := regexp.Compile(`^\s*` + expr)
	if err != nil {
		return 0
	}
	for i, text := range bytes.Split(p.data, []byte("\n")) {
		if re.Match(text) {
			return i + 1
		}
	}
	return 0
}

func (p *parser) paths(value any) {
	table, ok := value.(map[string]any)
	if !ok {
		p.fail(p.line(`paths\b`), "`paths` must be a table: [paths]")
		return
	}
	for _, key := range sortedKeys(table) {
		line := p.line(regexp.QuoteMeta(key) + `\s*=`)
		if key != "tests" {
			p.fail(line, "unknown key `%s` under [paths]; it holds `tests`", key)
			continue
		}
		list, ok := table[key].([]any)
		if !ok {
			p.fail(line, "`tests` must be an array of path patterns")
			continue
		}
		p.cfg.Tests = []string{}
		for _, item := range list {
			pattern, ok := item.(string)
			if !ok {
				p.fail(line, "`tests` must be an array of path patterns")
				continue
			}
			if err := decl.ValidPattern(pattern); err != nil {
				p.fail(line, "pattern %q: %v", pattern, err)
				continue
			}
			p.cfg.Tests = append(p.cfg.Tests, pattern)
		}
	}
}

func (p *parser) host(host string, value any) {
	table, ok := value.(map[string]any)
	if !ok {
		p.fail(p.line(regexp.QuoteMeta(host)+`\b`), "`%s` must hold one table for each role: [%s.<role>]", host, host)
		return
	}
	p.cfg.Models[host] = map[string]Model{}
	for _, role := range sortedKeys(table) {
		header := p.line(`\[\s*` + host + `\s*\.\s*"?` + regexp.QuoteMeta(role) + `"?\s*\]`)
		if !Known(role) {
			p.fail(header, "unknown role `%s`; the roles are %s", role, strings.Join(IDs(), ", "))
			continue
		}
		fields, ok := table[role].(map[string]any)
		if !ok {
			p.fail(header, "`%s.%s` must be a table with `model` and `effort`", host, role)
			continue
		}
		var m Model
		for _, key := range sortedKeys(fields) {
			text, isText := fields[key].(string)
			switch {
			case key != "model" && key != "effort":
				p.fail(header, "unknown key `%s` under [%s.%s]; it holds `model` and `effort`", key, host, role)
			case !isText || text == "":
				p.fail(header, "`%s` under [%s.%s] must be a text that is not empty", key, host, role)
			case key == "model":
				m.Model = text
			default:
				m.Effort = text
			}
		}
		p.cfg.Models[host][role] = m
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
