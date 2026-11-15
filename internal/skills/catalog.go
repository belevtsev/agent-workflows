// Package skills discovers existing local host skills without executing them.
package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

const maxFile = 1 << 20

// Entry identifies a skill at its original SKILL.md, including its byte hash.
// ManualOnly and UserInvocable describe native host invocation restrictions.
type Entry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Host          string `json:"host"`
	Scope         string `json:"scope"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	Disabled      bool   `json:"disabled,omitempty"`
	Reason        string `json:"reason,omitempty"`
	ManualOnly    bool   `json:"manual_only,omitempty"`
	UserInvocable bool   `json:"user_invocable"`
}

// Catalog holds the local inventory and diagnostics for sources it cannot read.
type Catalog struct {
	Skills      []Entry  `json:"skills"`
	Warnings    []string `json:"warnings"`
	host        string
	blocked     []block
	overrides   map[string]string
	policyError string
	codexRules  []codexRule
}

type block struct{ path, reason string }
type codexRule struct {
	path, name string
	enabled    bool
}

// Resolve preflights explicit worker selections. Exact IDs take precedence over
// unique short names. Absolute SKILL.md and skill-directory paths are accepted.
// Manual-only skills cannot be preloaded into an automatic worker.
func (c Catalog) Resolve(refs []string) ([]Entry, error) {
	out := make([]Entry, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		entry, err := c.resolve(ref)
		if err != nil {
			return nil, err
		}
		if entry.Disabled {
			return nil, fmt.Errorf("skill %q is unavailable: %s", ref, entry.Reason)
		}
		fresh, err := readSkill(entry.Path, entry.Host)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", ref, err)
		}
		entry.Name, entry.Description, entry.SHA256 = fresh.Name, fresh.Description, fresh.SHA256
		entry.ManualOnly, entry.UserInvocable = fresh.ManualOnly, fresh.UserInvocable
		entry = c.applyPolicy(entry)
		if entry.Disabled {
			return nil, fmt.Errorf("skill %q is unavailable: %s", ref, entry.Reason)
		}
		if entry.ManualOnly {
			return nil, fmt.Errorf("skill %q is manual-only and cannot be preloaded into a worker", ref)
		}
		key := realPath(entry.Path)
		if !seen[key] {
			out = append(out, entry)
			seen[key] = true
		}
	}
	return out, nil
}

func (c Catalog) resolve(ref string) (Entry, error) {
	matches := []Entry{}
	for _, entry := range c.Skills {
		if entry.ID == ref {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 && filepath.IsAbs(ref) {
		path := skillPath(ref)
		for _, entry := range c.Skills {
			if realPath(entry.Path) == realPath(path) {
				return entry, nil
			}
		}
		entry, err := readSkill(path, c.host)
		entry.ID, entry.Scope = explicitID(entry), "explicit"
		return c.applyPolicy(entry), err
	}
	if len(matches) == 0 {
		for _, entry := range c.Skills {
			if entry.Name == ref {
				matches = append(matches, entry)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return Entry{}, fmt.Errorf("skill %q was not found; list skills or provide an absolute SKILL.md path", ref)
	}
	ids := make([]string, 0, len(matches))
	for _, entry := range matches {
		ids = append(ids, entry.ID)
	}
	slices.Sort(ids)
	return Entry{}, fmt.Errorf("skill %q is ambiguous; select one of: %s", ref, strings.Join(ids, ", "))
}

func (c Catalog) applyPolicy(entry Entry) Entry {
	if c.policyError != "" {
		entry.Disabled, entry.Reason = true, c.policyError
	}
	disabled := false
	for _, rule := range c.codexRules {
		if rule.name == entry.Name || (rule.path != "" && realPath(rule.path) == realPath(entry.Path)) {
			disabled = !rule.enabled
		}
	}
	if disabled {
		entry.Disabled, entry.Reason = true, "disabled by Codex skills.config"
	}
	for _, denied := range c.blocked {
		if within(denied.path, entry.Path) || realPath(denied.path) == realPath(entry.Path) {
			entry.Disabled, entry.Reason = true, denied.reason
		}
	}
	if entry.Host == "claude" && entry.Scope != "plugin" {
		switch c.overrides[entry.Name] {
		case "off":
			entry.Disabled, entry.Reason = true, "disabled by Claude skillOverrides"
		case "user-invocable-only":
			entry.ManualOnly = true
		}
	}
	return entry
}

func readSkill(path, host string) (Entry, error) {
	entry := Entry{Host: host, Path: path, Name: filepath.Base(filepath.Dir(path)), UserInvocable: true}
	data, err := readFile(path)
	if err != nil {
		return entry, err
	}
	hash := sha256.Sum256(data)
	entry.SHA256 = hex.EncodeToString(hash[:])
	fields, body, err := frontmatter(data)
	if err != nil {
		return entry, err
	}
	if value, ok := fields["name"]; ok {
		name, valid := value.(string)
		if !valid || strings.TrimSpace(name) == "" || strings.ContainsFunc(name, unicode.IsControl) {
			return entry, errors.New("frontmatter name must be non-empty text without control characters")
		}
		entry.Name = strings.TrimSpace(name)
	} else if host != "claude" {
		return entry, errors.New("frontmatter requires name and description")
	}
	if value, ok := fields["description"]; ok {
		text, valid := value.(string)
		if !valid || strings.TrimSpace(text) == "" {
			return entry, errors.New("frontmatter description must be non-empty text")
		}
		entry.Description = strings.TrimSpace(text)
	} else if host != "claude" {
		return entry, errors.New("frontmatter requires name and description")
	} else {
		entry.Description = firstLine(body)
	}
	entry.ManualOnly, err = boolField(fields, "disable-model-invocation", false)
	if err == nil {
		entry.UserInvocable, err = boolField(fields, "user-invocable", true)
	}
	if err == nil && host == "codex" {
		manual, policyErr := codexInvocationPolicy(path)
		entry.ManualOnly, err = entry.ManualOnly || manual, policyErr
	}
	return entry, err
}

func codexInvocationPolicy(path string) (bool, error) {
	data, err := readFile(filepath.Join(filepath.Dir(path), "agents", "openai.yaml"))
	if missing(err) {
		return false, nil
	}
	var metadata struct {
		Policy struct {
			AllowImplicit *bool `yaml:"allow_implicit_invocation"`
		} `yaml:"policy"`
	}
	if err != nil || yaml.Load(data, &metadata) != nil {
		return false, errors.New("cannot read valid agents/openai.yaml policy metadata")
	}
	return metadata.Policy.AllowImplicit != nil && !*metadata.Policy.AllowImplicit, nil
}

func frontmatter(data []byte) (map[string]any, string, error) {
	if !utf8.Valid(data) {
		return nil, "", errors.New("SKILL.md must be UTF-8")
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.Split(text, "\n")
	fields := map[string]any{}
	if strings.TrimSuffix(lines[0], "\r") != "---" {
		return fields, text, nil
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSuffix(lines[i], "\r") != "---" {
			continue
		}
		if err := yaml.Load([]byte(strings.Join(lines[1:i], "\n")), &fields); err != nil {
			return nil, "", errors.New("invalid YAML frontmatter")
		}
		return fields, strings.Join(lines[i+1:], "\n"), nil
	}
	return nil, "", errors.New("frontmatter is missing its closing ---")
}

func boolField(fields map[string]any, key string, fallback bool) (bool, error) {
	value, ok := fields[key]
	if !ok {
		return fallback, nil
	}
	if flag, ok := value.(bool); ok {
		return flag, nil
	}
	switch strings.ToLower(fmt.Sprint(value)) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("frontmatter %s must be a boolean", key)
}

func readFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFile {
		return nil, errors.New("file must be regular and at most 1 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxFile+1))
	if err == nil && len(data) > maxFile {
		err = errors.New("file exceeds 1 MiB")
	}
	return data, err
}

func skillPath(path string) string {
	if filepath.Base(path) == "SKILL.md" {
		return filepath.Clean(path)
	}
	return filepath.Join(path, "SKILL.md")
}

func explicitID(entry Entry) string {
	hash := sha256.Sum256([]byte(entry.Path))
	return entry.Host + ":explicit:" + hex.EncodeToString(hash[:6])
}

func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(realPath(root), realPath(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func firstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func missing(err error) bool { return errors.Is(err, fs.ErrNotExist) }
