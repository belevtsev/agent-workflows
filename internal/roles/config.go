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

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/pelletier/go-toml/v2"
)

// FileName is the roles file at the root of a repository. It is optional.
const FileName = "kitsu-roles.toml"

// WorkflowConfigVersion assigns each repository specialist to one host.
const WorkflowConfigVersion = 2

// Config holds repository policy and effective role selections.
type Config struct {
	// Version identifies the repository schema. Version 1 uses host tables.
	Version int
	// Tests are the patterns of the test paths; nil when the file names
	// none.
	Tests []string
	// Models holds, by host and role, the fields the file sets. An empty
	// field keeps the default.
	Models map[string]map[string]Model
	// Skills holds selected references by host and role. An absent role inherits
	// personal choices; an explicit empty list suppresses them.
	Skills      map[string]map[string][]string
	assignments map[string]string
}

// ConfiguredHost returns the repository's assigned host for a specialist.
// Legacy configuration and an omitted optional specialist return an empty host.
func (c *Config) ConfiguredHost(role string) string {
	if c == nil || c.Version != WorkflowConfigVersion {
		return ""
	}
	return c.assignments[role]
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

// Load reads repository policy and merges its skill selections over personal
// choices. An empty root reads only personal choices; missing files are empty.
func Load(root string) (*Config, error) {
	return load(root, nil)
}

// LoadWithWorkflowDefault uses a one-host policy when the repository has no roles file.
func LoadWithWorkflowDefault(root, host string) (*Config, error) {
	return load(root, WorkflowTemplate(host))
}

// WorkflowTemplate assigns the required specialists to one host.
func WorkflowTemplate(host string) []byte {
	data := fmt.Appendf(nil, "version = %d\n", WorkflowConfigVersion)
	for _, role := range IDs() {
		if role != "git" {
			data = fmt.Appendf(data, "\n[roles.%s]\nhost = %q\n", role, host)
		}
	}
	return data
}

func load(root string, fallback []byte) (*Config, error) {
	personal, err := LoadPersonal()
	if err != nil || root == "" {
		return personal, err
	}
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		if fallback == nil {
			return personal, nil
		}
		data = fallback
	} else if err != nil {
		return nil, err
	}
	repository, err := Parse(data)
	if err != nil {
		return nil, err
	}
	mergeSkills(personal, repository)
	return repository, nil
}

// Parse reads a roles file. It refuses an unknown key, host or role and a
// pattern kitsu.toml would refuse. It does not judge a model name: hosts
// change them. Every error it returns is an *Error.
func Parse(data []byte) (*Config, error) {
	return parseConfig(data, false)
}

func parseConfig(data []byte, personal bool) (*Config, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			line, _ := decodeErr.Position()
			return nil, &Error{Problems: []Problem{{Line: line, Text: "not valid TOML: " + decodeErr.Error()}}}
		}
		return nil, &Error{Problems: []Problem{{Text: "not valid TOML: " + err.Error()}}}
	}
	p := parser{data: data, personal: personal, cfg: &Config{
		Models: map[string]map[string]Model{}, Skills: map[string]map[string][]string{},
	}}
	version, _ := raw["version"].(int64)
	switch {
	case version == 1:
		p.cfg.Version = 1
	case version == WorkflowConfigVersion && !personal:
		p.cfg.Version = WorkflowConfigVersion
		p.cfg.assignments = map[string]string{}
	case personal:
		p.fail(p.line(`version\s*=`), "`version` must be 1")
	default:
		p.fail(p.line(`version\s*=`), "`version` must be 1 or 2")
	}
	for _, key := range sortedKeys(raw) {
		switch {
		case key == "version":
		case key == "paths" && !personal:
			p.paths(raw[key])
		case key == "roles" && p.cfg.Version == WorkflowConfigVersion:
			p.configuredRoles(raw[key])
		case p.cfg.Version == WorkflowConfigVersion:
			p.fail(p.line(`\[?\s*`+regexp.QuoteMeta(key)+`\b`),
				"unknown key `%s`; version 2 holds `version`, `[paths]`, and `[roles.<role>]` tables", key)
		case slices.Contains(Hosts, key):
			p.host(key, raw[key])
		default:
			allowed := "`version`, `[paths]`, and host role tables"
			if personal {
				allowed = "only `version` and host role `skills` tables"
			}
			p.fail(p.line(`\[?\s*`+regexp.QuoteMeta(key)+`\b`), "unknown key `%s`; the file holds %s", key, allowed)
		}
	}
	if p.cfg.Version == WorkflowConfigVersion {
		p.requireConfiguredRoles(raw["roles"])
	}
	if len(p.problems) > 0 {
		return nil, &Error{Problems: p.problems}
	}
	return p.cfg, nil
}

func (p *parser) requireConfiguredRoles(value any) {
	table, _ := value.(map[string]any)
	line := p.line(`\[?\s*roles\b`)
	for _, role := range IDs() {
		if _, found := table[role]; !found && role != "git" {
			p.fail(line, "version 2 requires a host assignment in [roles.%s]", role)
		}
	}
}

type parser struct {
	data     []byte
	cfg      *Config
	problems []Problem
	personal bool
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
	p.cfg.Skills[host] = map[string][]string{}
	for _, role := range sortedKeys(table) {
		header := p.line(`\[\s*` + host + `\s*\.\s*"?` + regexp.QuoteMeta(role) + `"?\s*\]`)
		if !Known(role) && role != Coordinator {
			p.fail(header, "unknown role `%s`; the roles are %s", role, strings.Join(IDs(), ", "))
			continue
		}
		fields, ok := table[role].(map[string]any)
		if !ok {
			p.fail(header, "`%s.%s` must be a table with role settings", host, role)
			continue
		}
		p.role(host, role, host+"."+role, fields, header)
	}
}

func (p *parser) configuredRoles(value any) {
	table, ok := value.(map[string]any)
	if !ok {
		p.fail(p.line(`roles\b`), "`roles` must hold specialist tables: [roles.<role>]")
		return
	}
	for _, role := range sortedKeys(table) {
		name := "roles." + role
		header := p.line(`\[\s*roles\s*\.\s*"?` + regexp.QuoteMeta(role) + `"?\s*\]`)
		if !Known(role) {
			p.fail(header, "unknown specialist `%s`; the roles are %s", role, strings.Join(IDs(), ", "))
			continue
		}
		fields, ok := table[role].(map[string]any)
		if !ok {
			p.fail(header, "`%s` must be a table with role settings", name)
			continue
		}
		host, _ := fields["host"].(string)
		if !slices.Contains(Hosts, host) {
			p.fail(header, "`host` under [%s] must be `codex` or `claude`", name)
			continue
		}
		p.cfg.assignments[role] = host
		p.role(host, role, name, fields, header)
	}
}

func (p *parser) role(host, role, table string, fields map[string]any, line int) {
	if p.cfg.Models[host] == nil {
		p.cfg.Models[host] = map[string]Model{}
		p.cfg.Skills[host] = map[string][]string{}
	}
	var model Model
	for _, key := range sortedKeys(fields) {
		if key == "skills" {
			p.skillList(host, role, table, fields[key], line)
			continue
		}
		value, isText := fields[key].(string)
		switch {
		case key == "host" && p.cfg.Version == WorkflowConfigVersion:
		case p.personal || role == Coordinator:
			p.fail(line, "[%s] permits only `skills` in this scope", table)
		case key != "model" && key != "effort":
			p.fail(line, "unknown key `%s` under [%s]", key, table)
		case !isText || value == "" || p.cfg.Version == WorkflowConfigVersion && strings.TrimSpace(value) == "":
			p.fail(line, "`%s` under [%s] must be a text that is not empty", key, table)
		case key == "model":
			model.Model = value
		default:
			model.Effort = value
		}
	}
	p.cfg.Models[host][role] = model
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
