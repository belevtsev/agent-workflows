package evals

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCases validates every case under evals/ against the `claude plugin eval`
// schema (https://code.claude.com/docs/en/plugin-evals.md) and this
// repository's conventions. It runs offline and calls no model.
func TestCases(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := loadPluginInfo(root)
	if err != nil {
		t.Fatal(err)
	}
	cases, err := findCases(filepath.Join(root, "evals"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no eval cases found under evals/")
	}
	for _, dir := range cases {
		rel, _ := filepath.Rel(root, dir)
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			for _, err := range validateCase(root, dir, plugin) {
				t.Error(err)
			}
		})
	}
}

// pluginInfo is the plugin manifest's name and the agent names (frontmatter
// `name`) of the files its `agents` array lists.
type pluginInfo struct {
	Name   string
	Agents map[string]bool
}

func loadPluginInfo(repoRoot string) (pluginInfo, error) {
	manifestPath := filepath.Join(repoRoot, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return pluginInfo{}, err
	}
	var manifest struct {
		Name   string   `json:"name"`
		Agents []string `json:"agents"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return pluginInfo{}, fmt.Errorf(".claude-plugin/plugin.json: %w", err)
	}
	if manifest.Name == "" {
		return pluginInfo{}, errors.New(".claude-plugin/plugin.json: no name")
	}
	info := pluginInfo{Name: manifest.Name, Agents: map[string]bool{}}
	for _, agent := range manifest.Agents {
		file := filepath.Join(repoRoot, filepath.FromSlash(agent))
		data, err := os.ReadFile(file)
		if err != nil {
			return pluginInfo{}, fmt.Errorf(".claude-plugin/plugin.json agent %s: %w", agent, err)
		}
		front, _, err := splitFrontmatter(data)
		if err != nil {
			return pluginInfo{}, fmt.Errorf("%s: %w", agent, err)
		}
		var fields struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(front, &fields); err != nil || fields.Name == "" {
			return pluginInfo{}, fmt.Errorf("%s: frontmatter has no name (%v)", agent, err)
		}
		info.Agents[fields.Name] = true
	}
	return info, nil
}

// findCases returns, sorted, every directory under evalsDir that holds a
// prompt.md or case.yaml, skipping results, testdata, mocks and hidden
// directories.
func findCases(evalsDir string) ([]string, error) {
	var cases []string
	err := filepath.WalkDir(evalsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if p != evalsDir && (name == "results" || name == "testdata" || name == "mocks" || strings.HasPrefix(name, ".")) {
			return filepath.SkipDir
		}
		if fileExists(filepath.Join(p, "prompt.md")) || fileExists(filepath.Join(p, "case.yaml")) {
			cases = append(cases, p)
			// Anything inside a case directory belongs to that case.
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(cases)
	return cases, err
}

var (
	caseYAMLKeys   = []string{"schema_version", "name", "description", "tags", "plugins", "runs", "expected_outcome", "execution", "context", "graders"}
	executionKeys  = []string{"prompt", "model", "max_turns", "timeout_seconds", "allowed_tools", "append_system_prompt", "env"}
	contextKeys    = []string{"scaffold_script", "history_file", "add_dirs"}
	promptKeys     = []string{"schema_version", "name", "description", "tags", "plugins", "runs", "expected_outcome", "model", "max_turns", "timeout_seconds", "allowed_tools", "append_system_prompt", "env"}
	graderBaseKeys = []string{"type", "weight", "arm"}
	graderTypeKeys = map[string][]string{
		"regex":       {"pattern", "flags", "match", "target"},
		"tool_used":   {"tool", "input_match", "min", "max"},
		"tool_order":  {"before", "after"},
		"file_exists": {"path", "exists"},
		"llm":         {"criteria", "focus"},
		"baseline":    {"baseline_file", "criteria"},
	}
	graderRequiredKeys = map[string][]string{
		"regex":       {"pattern"},
		"tool_used":   {"tool"},
		"tool_order":  {"before", "after"},
		"file_exists": {"path"},
		"baseline":    {"baseline_file"},
	}
	graderViews  = []string{"last_message", "trace", "files", "mock_calls"}
	matchPattern = regexp.MustCompile(`^(contains|not_contains|count:[0-9]+)$`)
	envKey       = regexp.MustCompile(`^EVAL_[A-Z0-9_]*$`)
	dispatchRef  = regexp.MustCompile(`subagent_type\\?"?\s*:\s*\\?"([^"\\]+)\\?"`)
	ownedBullet  = regexp.MustCompile(`^\s*-\s*Owned paths:`)
	folderBullet = regexp.MustCompile(`^\s*-\s*Change folder:`)
	backticked   = regexp.MustCompile("`([^`]+)`")
	pathLike     = regexp.MustCompile(`/|\.[A-Za-z0-9]+$`)
)

// caseCheck collects the problems of one case, each prefixed with the
// offending file's slash path relative to the repository root.
type caseCheck struct {
	root, dir, suite string
	// changeFolder is the folder the brief's "Change folder:" bullet names,
	// with a trailing slash, or "" when it names none.
	changeFolder string
	errs         []error
}

func (c *caseCheck) rel(name string) string {
	rel, err := filepath.Rel(c.root, filepath.Join(c.dir, filepath.FromSlash(name)))
	if err != nil {
		return filepath.ToSlash(filepath.Join(c.dir, name))
	}
	return filepath.ToSlash(rel)
}

func (c *caseCheck) fail(file, format string, args ...any) {
	c.errs = append(c.errs, fmt.Errorf("%s: %s", c.rel(file), fmt.Sprintf(format, args...)))
}

// validateCase checks one case directory and returns every problem found.
func validateCase(repoRoot, caseDir string, p pluginInfo) []error {
	c := &caseCheck{root: repoRoot, dir: caseDir, suite: filepath.Base(filepath.Dir(caseDir))}
	graderCount := c.checkCaseYAML()
	body := c.checkPrompt()
	owned := ownedPaths(body)
	dispatched := c.checkDispatch(body, p)
	// Only the spec writer writes its change folder; the reviewer (spec-review
	// contract) reads one and must never edit it.
	if slices.ContainsFunc(dispatched, func(d string) bool {
		_, agent, namespaced := strings.Cut(d, ":")
		return d == "spec-writer" || namespaced && agent == "spec-writer"
	}) {
		c.changeFolder = changeFolder(body)
	}

	graderDir := filepath.Join(caseDir, "graders")
	files, _ := filepath.Glob(filepath.Join(graderDir, "*.md"))
	sort.Strings(files)
	if len(files) == 0 && graderCount == 0 {
		c.fail("graders", "missing: a case needs at least one graders/*.md file or case.yaml graders entry")
	}
	for _, file := range files {
		name := "graders/" + filepath.Base(file)
		data, err := os.ReadFile(file)
		if err != nil {
			c.fail(name, "%v", err)
			continue
		}
		front, graderBody, err := splitFrontmatter(data)
		if err != nil {
			c.fail(name, "cannot parse frontmatter: %v", err)
			continue
		}
		fields, err := decodeMapping(front)
		if err != nil {
			c.fail(name, "cannot parse frontmatter YAML: %v", err)
			continue
		}
		c.checkGrader(name, fields, strings.TrimSpace(string(graderBody)) != "", owned, dispatched, false)
	}
	return c.errs
}

func (c *caseCheck) checkCaseYAML() (graders int) {
	const name = "case.yaml"
	data, err := os.ReadFile(filepath.Join(c.dir, name))
	if err != nil {
		c.fail(name, "missing: every case in this repository keeps a case.yaml (%v)", err)
		return 0
	}
	fields, err := decodeMapping(data)
	if err != nil {
		c.fail(name, "cannot parse YAML: %v", err)
		return 0
	}
	c.checkKeys(name, "", fields, caseYAMLKeys, []string{"schema_version", "name"})
	if v, ok := fields["schema_version"]; ok {
		if s, isString := v.(string); !isString || s != "1.1" {
			c.fail(name, `schema_version must be the string "1.1", got %#v`, v)
		}
	}
	if v, ok := fields["name"]; ok {
		if s, _ := v.(string); s != filepath.Base(c.dir) {
			c.fail(name, "name %#v differs from the case directory name %q", v, filepath.Base(c.dir))
		}
	}
	c.checkCommonFields(name, fields)
	if v, ok := fields["execution"]; ok {
		execution, isMap := v.(map[string]any)
		if !isMap {
			c.fail(name, "execution must be a mapping")
		} else {
			c.checkKeys(name, "execution.", execution, executionKeys, nil)
			c.checkRunLimits(name, execution)
		}
	}
	if v, ok := fields["context"]; ok {
		context, isMap := v.(map[string]any)
		if !isMap {
			c.fail(name, "context must be a mapping")
		} else {
			c.checkKeys(name, "context.", context, contextKeys, nil)
			for _, key := range []string{"scaffold_script", "history_file"} {
				if v, ok := context[key]; ok {
					c.checkCaseFile(name, "context."+key, v)
				}
			}
			if v, ok := context["add_dirs"]; ok {
				list, isList := v.([]any)
				if !isList {
					c.fail(name, "context.add_dirs must be a list")
				}
				for _, dir := range list {
					if s, ok := dir.(string); !ok || !cleanRelative(s) || !dirExists(filepath.Join(c.dir, filepath.FromSlash(s))) {
						c.fail(name, "context.add_dirs entry %#v is not a directory inside the case", dir)
					}
				}
			}
		}
	}
	if v, ok := fields["graders"]; ok {
		list, isList := v.([]any)
		if !isList {
			c.fail(name, "graders must be a list")
			return 0
		}
		for i, item := range list {
			grader, isMap := item.(map[string]any)
			if !isMap {
				c.fail(name, "graders[%d] must be a mapping", i)
				continue
			}
			label := fmt.Sprintf("%s graders[%d]", name, i)
			if _, ok := grader["name"]; !ok {
				c.fail(name, `graders[%d]: missing required key "name"`, i)
			}
			delete(grader, "name")
			// Paths and dispatch are checked against the prompt body only for
			// graders/*.md files; inline graders get the schema checks.
			c.checkGrader(label, grader, false, nil, nil, true)
		}
		return len(list)
	}
	return 0
}

func (c *caseCheck) checkCaseFile(file, key string, v any) {
	s, ok := v.(string)
	if !ok || !cleanRelative(s) {
		c.fail(file, "%s must be a relative path inside the case, got %#v", key, v)
		return
	}
	if !fileExists(filepath.Join(c.dir, filepath.FromSlash(s))) {
		c.fail(file, "%s names %q, which is missing from the case directory", key, s)
	}
}

func (c *caseCheck) checkPrompt() string {
	const name = "prompt.md"
	data, err := os.ReadFile(filepath.Join(c.dir, name))
	if err != nil {
		c.fail(name, "missing: every case in this repository keeps its prompt in prompt.md (%v)", err)
		return ""
	}
	front, body, err := splitFrontmatter(data)
	if err != nil {
		c.fail(name, "cannot parse frontmatter: %v", err)
		return string(data)
	}
	fields, err := decodeMapping(front)
	if err != nil {
		c.fail(name, "cannot parse frontmatter YAML: %v", err)
	} else {
		c.checkKeys(name, "", fields, promptKeys, nil)
		c.checkCommonFields(name, fields)
		c.checkRunLimits(name, fields)
		tags, _ := fields["tags"].([]any)
		if !slices.Contains(tags, any(c.suite)) {
			c.fail(name, "tags %v must include the suite name %q so `--tag %s` selects the case", fields["tags"], c.suite, c.suite)
		}
	}
	if strings.TrimSpace(string(body)) == "" {
		c.fail(name, "empty prompt body")
	}
	return string(body)
}

// checkCommonFields checks the fields prompt.md and case.yaml share.
func (c *caseCheck) checkCommonFields(file string, fields map[string]any) {
	if v, ok := fields["runs"]; ok {
		c.checkIntRange(file, "runs", v, 1, 50)
	}
	if v, ok := fields["tags"]; ok {
		c.checkStringList(file, "tags", v)
	}
	if v, ok := fields["plugins"]; ok {
		c.checkStringList(file, "plugins", v)
	}
}

// checkRunLimits checks the fields prompt.md frontmatter and case.yaml
// `execution` share.
func (c *caseCheck) checkRunLimits(file string, fields map[string]any) {
	if v, ok := fields["max_turns"]; ok {
		c.checkIntRange(file, "max_turns", v, 1, 200)
	}
	if v, ok := fields["timeout_seconds"]; ok {
		c.checkIntRange(file, "timeout_seconds", v, 1, 3600)
	}
	if v, ok := fields["allowed_tools"]; ok {
		c.checkStringList(file, "allowed_tools", v)
	}
	if v, ok := fields["env"]; ok {
		env, isMap := v.(map[string]any)
		if !isMap {
			c.fail(file, "env must be a mapping")
		}
		for key := range env {
			if !envKey.MatchString(key) {
				c.fail(file, "env key %q must match EVAL_[A-Z0-9_]*", key)
			}
		}
	}
}

func (c *caseCheck) checkIntRange(file, key string, v any, low, high int) {
	n, ok := v.(int)
	if !ok || n < low || n > high {
		c.fail(file, "%s must be an integer from %d to %d, got %#v", key, low, high, v)
	}
}

func (c *caseCheck) checkStringList(file, key string, v any) {
	list, ok := v.([]any)
	if !ok {
		c.fail(file, "%s must be a list of strings, got %#v", key, v)
		return
	}
	for _, item := range list {
		if _, ok := item.(string); !ok {
			c.fail(file, "%s entry %#v must be a string", key, item)
		}
	}
}

func (c *caseCheck) checkKeys(file, prefix string, fields map[string]any, allowed, required []string) {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	where := ""
	if prefix != "" {
		where = " in " + strings.TrimSuffix(prefix, ".")
	}
	for _, key := range keys {
		if !slices.Contains(allowed, key) {
			c.fail(file, "unknown key %q%s", key, where)
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			c.fail(file, "missing required key %q%s", key, where)
		}
	}
}

// checkGrader checks one grader's keys and values. inline marks a case.yaml
// grader, whose paths and dispatch are not checked against the prompt.
func (c *caseCheck) checkGrader(file string, fields map[string]any, hasBody bool, owned, dispatched []string, inline bool) {
	typ, ok := fields["type"]
	if !ok {
		c.checkKeys(file, "", fields, append(slices.Clone(graderBaseKeys), allGraderKeys()...), []string{"type"})
		return
	}
	kind, _ := typ.(string)
	typeKeys, known := graderTypeKeys[kind]
	if !known {
		c.fail(file, "type %#v is not one of regex, tool_used, tool_order, file_exists, llm, baseline", typ)
		return
	}
	c.checkKeys(file, "", fields, append(slices.Clone(graderBaseKeys), typeKeys...), graderRequiredKeys[kind])
	if v, ok := fields["weight"]; ok {
		if w, isNumber := number(v); !isNumber || w <= 0 || math.IsInf(w, 0) {
			c.fail(file, "weight must be a positive number, got %#v", v)
		}
	}
	if v, ok := fields["arm"]; ok && v != "with-only" && v != "both" {
		c.fail(file, "arm must be with-only or both, got %#v", v)
	}
	switch kind {
	case "regex":
		flags := ""
		if v, ok := fields["flags"]; ok {
			s, isString := v.(string)
			if !isString || !validFlags(s) {
				c.fail(file, "flags %#v must be distinct JavaScript regex flags from dgimsuyv, not both u and v", v)
			}
			flags = s
		}
		if v, ok := fields["match"]; ok {
			if s, _ := v.(string); !matchPattern.MatchString(s) {
				c.fail(file, "match must be contains, not_contains or count:<n>, got %#v", v)
			}
		}
		if v, ok := fields["pattern"]; ok {
			c.checkRegex(file, "pattern", v, flags)
		}
		if v, ok := fields["target"]; ok {
			c.checkView(file, "target", v, owned, inline)
		}
	case "tool_used":
		if v, ok := fields["tool"]; ok {
			if s, _ := v.(string); s == "" {
				c.fail(file, "tool must be a tool name, got %#v", v)
			}
		}
		var matcher *jsRegex
		if v, ok := fields["input_match"]; ok {
			matcher = c.checkRegex(file, "input_match", v, "")
		}
		minimum, maximum := 1, -1
		for _, key := range []string{"min", "max"} {
			if v, ok := fields[key]; ok {
				n, isInt := v.(int)
				if !isInt || n < 0 {
					c.fail(file, "%s must be a non-negative integer, got %#v", key, v)
					continue
				}
				if key == "min" {
					minimum = n
				} else {
					maximum = n
				}
			}
		}
		if maximum >= 0 && minimum > maximum {
			// min defaults to 1, so `max: 0` alone can never pass.
			c.fail(file, "min %d is greater than max %d (min defaults to 1; set min: 0 to require no calls)", minimum, maximum)
		}
		if !inline && fields["tool"] == "Agent" && matcher != nil && minimum > 0 {
			if !matchesDispatch(matcher, dispatched) {
				c.fail(file, "input_match %q does not match the dispatched agent (%s)", fields["input_match"], strings.Join(dispatched, ", "))
			}
		}
	case "tool_order":
		for _, key := range []string{"before", "after"} {
			v, ok := fields[key]
			if !ok {
				continue
			}
			switch step := v.(type) {
			case string:
				if step == "" {
					c.fail(file, "%s must name a tool", key)
				}
			case map[string]any:
				c.checkKeys(file, key+".", step, []string{"tool", "input_match"}, []string{"tool"})
				if m, ok := step["input_match"]; ok {
					c.checkRegex(file, key+".input_match", m, "")
				}
			default:
				c.fail(file, "%s must be a tool name or {tool, input_match}, got %#v", key, v)
			}
		}
	case "file_exists":
		if v, ok := fields["path"]; ok {
			s, isString := v.(string)
			if !isString || !cleanRelative(s) {
				c.fail(file, "path must be a relative path, got %#v", v)
			} else if !inline {
				c.checkOwned(file, s, owned)
			}
		}
		if v, ok := fields["exists"]; ok {
			if _, isBool := v.(bool); !isBool {
				c.fail(file, "exists must be true or false, got %#v", v)
			}
		}
	case "llm":
		criteria, _ := fields["criteria"].(string)
		if !hasBody && strings.TrimSpace(criteria) == "" {
			c.fail(file, "an llm grader needs a rubric in its body or criteria")
		}
		if v, ok := fields["focus"]; ok {
			c.checkView(file, "focus", v, owned, inline)
		}
	case "baseline":
		if v, ok := fields["baseline_file"]; ok {
			c.checkCaseFile(file, "baseline_file", v)
		}
		criteria, _ := fields["criteria"].(string)
		if !hasBody && strings.TrimSpace(criteria) == "" {
			c.fail(file, "a baseline grader needs criteria in its body or criteria")
		}
	}
}

func allGraderKeys() []string {
	var keys []string
	for _, typeKeys := range graderTypeKeys {
		keys = append(keys, typeKeys...)
	}
	return keys
}

// checkView checks a regex `target` or llm `focus`.
func (c *caseCheck) checkView(file, key string, v any, owned []string, inline bool) {
	switch view := v.(type) {
	case string:
		if !slices.Contains(graderViews, view) {
			c.fail(file, "%s %q is not one of %s or {source: file, path: ...}", key, view, strings.Join(graderViews, ", "))
		}
	case map[string]any:
		c.checkKeys(file, key+".", view, []string{"source", "path"}, []string{"source", "path"})
		if view["source"] != "file" {
			c.fail(file, "%s.source must be file, got %#v", key, view["source"])
		}
		p, isString := view["path"].(string)
		if !isString || !cleanRelative(p) {
			c.fail(file, "%s.path must be a relative path, got %#v", key, view["path"])
			return
		}
		if !inline {
			c.checkOwned(file, p, owned)
		}
	default:
		c.fail(file, "%s must be one of %s or {source: file, path: ...}, got %#v", key, strings.Join(graderViews, ", "), v)
	}
}

// checkOwned rejects a graded path the brief does not let the agent write: a
// path under the brief's change folder, one of the brief's owned paths, or
// anything under artefacts/ when the brief names no owned paths.
func (c *caseCheck) checkOwned(file, graded string, owned []string) {
	if c.changeFolder != "" && strings.HasPrefix(path.Clean(graded), c.changeFolder) {
		return
	}
	if owned == nil {
		if !strings.HasPrefix(graded, "artefacts/") {
			c.fail(file, "path %q is not an owned path: the brief names no owned paths, so graded files must be under artefacts/%s", graded, c.folderHint())
		}
		return
	}
	for _, o := range owned {
		if o == graded {
			return
		}
		if matched, err := path.Match(graded, o); err == nil && matched {
			return
		}
	}
	c.fail(file, "path %q is not an owned path of the brief (%s)%s", graded, strings.Join(owned, ", "), c.folderHint())
}

// folderHint names the change folder in a failure, when the brief has one.
func (c *caseCheck) folderHint() string {
	if c.changeFolder == "" {
		return ""
	}
	return " or the change folder " + c.changeFolder
}

func (c *caseCheck) checkRegex(file, key string, v any, flags string) *jsRegex {
	s, ok := v.(string)
	if !ok || s == "" {
		c.fail(file, "%s must be a non-empty regex string, got %#v", key, v)
		return nil
	}
	re, err := compileJS(s, flags)
	if err != nil {
		c.fail(file, "%s %q: %v", key, s, err)
		return nil
	}
	return re
}

// checkDispatch returns the subagent_type values the prompt dispatches and
// reports any that the plugin does not ship.
func (c *caseCheck) checkDispatch(body string, p pluginInfo) []string {
	var dispatched []string
	for _, m := range dispatchRef.FindAllStringSubmatch(body, -1) {
		value := m[1]
		if !slices.Contains(dispatched, value) {
			dispatched = append(dispatched, value)
		}
		plugin, agent, namespaced := strings.Cut(value, ":")
		if !namespaced {
			plugin, agent = p.Name, value
		}
		if plugin != p.Name || !p.Agents[agent] {
			c.fail("prompt.md", "dispatched agent %q is not listed in plugin.json agents of plugin %q", value, p.Name)
		}
	}
	if len(dispatched) == 0 && fileExists(filepath.Join(c.dir, "prompt.md")) {
		c.fail("prompt.md", "dispatches no agent: every case asks the coordinator to dispatch a plugin agent through subagent_type")
	}
	return dispatched
}

func matchesDispatch(re *jsRegex, dispatched []string) bool {
	if re.approximate {
		// Lookarounds were dropped, so the compiled form cannot decide a match.
		return true
	}
	for _, value := range dispatched {
		input, _ := json.Marshal(map[string]string{"subagent_type": value})
		if re.MatchString(string(input)) {
			return true
		}
	}
	return false
}

// changeFolder returns the first backticked folder in the brief's first
// "- Change folder:" bullet, which may wrap onto indented lines, cleaned and
// with a trailing slash; "" when the brief has no such bullet or that folder
// is not relative inside the workspace.
func changeFolder(body string) string {
	m := backticked.FindStringSubmatch(bulletText(body, folderBullet))
	if m == nil {
		return ""
	}
	dir := path.Clean(m[1])
	if dir == "." || path.IsAbs(dir) || dir == ".." || strings.HasPrefix(dir, "../") {
		return ""
	}
	return dir + "/"
}

// bulletText returns the first line matching bullet joined with its indented
// continuation lines, or "" when no line matches.
func bulletText(body string, bullet *regexp.Regexp) string {
	var text strings.Builder
	found := false
	for _, line := range strings.Split(body, "\n") {
		if !found {
			if bullet.MatchString(line) {
				found = true
				text.WriteString(line)
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "- ") || !strings.HasPrefix(line, " ") {
			break
		}
		text.WriteString(" " + trimmed)
	}
	return text.String()
}

// ownedPaths returns the paths in the brief's "Owned paths:" bullet, nil when
// the brief has none.
func ownedPaths(body string) []string {
	text := bulletText(body, ownedBullet)
	if text == "" {
		return nil
	}
	owned := []string{}
	for _, m := range backticked.FindAllStringSubmatch(text, -1) {
		if pathLike.MatchString(m[1]) && !strings.ContainsAny(m[1], " ") {
			owned = append(owned, m[1])
		}
	}
	return owned
}

// validFlags reports whether flags is a valid JavaScript flags string: known
// flags, none repeated, and not both u and v.
func validFlags(flags string) bool {
	for i, f := range flags {
		if !strings.ContainsRune("dgimsuyv", f) || strings.ContainsRune(flags[:i], f) {
			return false
		}
	}
	return !strings.ContainsRune(flags, 'u') || !strings.ContainsRune(flags, 'v')
}

// splitFrontmatter splits a Markdown file into its YAML frontmatter (between
// the leading and the next "---" line) and the body after it.
func splitFrontmatter(data []byte) (front, body []byte, err error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, nil, errors.New("no YAML frontmatter: the file must start with a --- line")
	}
	rest := data[len("---\n"):]
	if bytes.HasPrefix(rest, []byte("---\n")) || bytes.Equal(rest, []byte("---")) {
		return nil, rest[min(len(rest), len("---\n")):], nil
	}
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		if bytes.HasSuffix(rest, []byte("\n---")) {
			return rest[:len(rest)-len("\n---")], nil, nil
		}
		return nil, nil, errors.New("unterminated YAML frontmatter: no closing --- line")
	}
	return rest[:end+1], rest[end+len("\n---\n"):], nil
}

// decodeMapping decodes YAML that must be a mapping (empty input is an empty
// mapping).
func decodeMapping(data []byte) (map[string]any, error) {
	fields := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return fields, nil
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("not a YAML mapping")
	}
	if err := node.Content[0].Decode(&fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func cleanRelative(p string) bool {
	return p != "" && !path.IsAbs(p) && !strings.Contains(p, "\\") && path.Clean(p) == p && p != "." && !strings.HasPrefix(p, "../")
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
