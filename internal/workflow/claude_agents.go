package workflow

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Claude agent definitions are a Claude-only installed artifact. The release
// manifest's optional claude_agents mapping names each definition; a Claude
// host links <claude>/agents/<name>.md to the file through the current release
// pointer, like a skill registration. The links exist so workers can be
// dispatched without the Skill tool and its listing. Every link replaces an
// absent path, so removing one always restores what was there before.

const claudeAgentsKey = "claude_agents"

// claudeAgentFile is the only release path a definition may use. A fixed layout
// keeps every definition inside the bundle's portable global resources.
func claudeAgentFile(name string) string { return "global/claude-agents/" + name + ".md" }

func claudeAgentName(name string) bool { return len(name) <= 64 && suiteName.MatchString(name) }

// manifestClaudeAgents returns a validated manifest's definitions. Releases
// made before agent definitions existed have none.
func manifestClaudeAgents(m Object) map[string]string {
	agents := map[string]string{}
	for name, root := range object(m[claudeAgentsKey]) {
		agents[name] = text(root)
	}
	return agents
}

// agentDirectory refuses a symlinked agents directory, as skills/ is refused:
// links written through it would land outside the Claude config directory.
func agentDirectory(h hostTarget) error {
	if err := realDirectory(h.agentDir, false); err != nil {
		return fmt.Errorf("Claude agent definitions need %s to be a real directory, not a symlink: %w", h.agentDir, err)
	}
	return nil
}

// protectClaudeAgents makes a compiled snapshot's definitions read-only. Claude
// Code's agent editor follows the link, so an edit fails instead of changing
// the immutable snapshot that every later command verifies.
func protectClaudeAgents(root string, m Object) error {
	for _, file := range manifestClaudeAgents(m) {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(file)), 0o444); err != nil {
			return err
		}
	}
	return nil
}

func (i *Installer) agentPath(h hostTarget, name string) string {
	return filepath.Join(h.agentDir, name+".md")
}

func (i *Installer) isAgentLink(path string) bool {
	if i.Claude == "" {
		return false
	}
	name, ok := strings.CutSuffix(filepath.Base(path), ".md")
	return ok && filepath.Dir(path) == i.host(hostClaude).agentDir && claudeAgentName(name)
}

// agentPlan reconciles one Claude host's agent links with a target manifest:
// it adds missing definitions over absent paths, repoints a definition whose
// release path changed and removes definitions the target no longer names.
// Update and rollback both use it; owned links must be unchanged.
func (i *Installer) agentPlan(h hostTarget, hs, m Object) (Object, []Object, error) {
	records, ops := Object{}, []Object{}
	if h.id != hostClaude {
		return records, ops, nil
	}
	next := manifestClaudeAgents(m)
	owned := object(hs["agents"])
	if len(next) == 0 && len(owned) == 0 {
		return records, ops, nil
	}
	if err := agentDirectory(h); err != nil {
		return nil, nil, err
	}
	for _, name := range keys(owned) {
		item := object(owned[name])
		path := i.agentPath(h, name)
		before := Object{"kind": "symlink", "target": item["target"]}
		actual, err := observe(path)
		if err != nil {
			return nil, nil, err
		}
		if !equal(actual, before) {
			return nil, nil, fmt.Errorf("owned agent definition changed: %s", path)
		}
		root, keep := next[name]
		if !keep {
			ops = append(ops, pathOperation(path, before, Object{"kind": "absent"}, hostLabel(h, "agent:"+name)))
			continue
		}
		record := clone(item)
		if target := i.target(root); target != item["target"] {
			record["target"] = target
			ops = append(ops, pathOperation(path, before, Object{"kind": "symlink", "target": target}, hostLabel(h, "agent:"+name)))
		}
		records[name] = record
	}
	for _, name := range slices.Sorted(maps.Keys(next)) {
		if owned[name] != nil {
			continue
		}
		path := i.agentPath(h, name)
		before, err := observe(path)
		if err != nil {
			return nil, nil, err
		}
		if before["kind"] != "absent" {
			return nil, nil, fmt.Errorf("unrelated occupied agent definition: %s", path)
		}
		target := i.target(next[name])
		records[name] = Object{"target": target, "original": before}
		ops = append(ops, pathOperation(path, before, Object{"kind": "symlink", "target": target}, hostLabel(h, "agent:"+name)))
	}
	return records, ops, nil
}

// setAgentRecords stores a Claude host's agent ownership. A host without
// definitions keeps no field, so releases without them leave state unchanged.
func setAgentRecords(hs, records Object) {
	if len(records) == 0 {
		delete(hs, "agents")
		return
	}
	hs["agents"] = records
}

// agentRemoval restores every owned agent link of a host to its absent origin.
func (i *Installer) agentRemoval(h hostTarget, hs Object) []Object {
	ops := []Object{}
	owned := object(hs["agents"])
	for _, name := range keys(owned) {
		before := Object{"kind": "symlink", "target": object(owned[name])["target"]}
		ops = append(ops, pathOperation(i.agentPath(h, name), before, Object{"kind": "absent"}, hostLabel(h, "agent:"+name)))
	}
	return ops
}

// validateAgentStructure checks a host record's agent ownership in isolation.
func (i *Installer) validateAgentStructure(h hostTarget, hs Object) error {
	raw, present := hs["agents"]
	if !present {
		return nil
	}
	agents, ok := raw.(map[string]any)
	if h.id != hostClaude || !ok || len(agents) == 0 {
		return errors.New("corrupt agent definition ownership")
	}
	prefix := i.current + string(filepath.Separator)
	for name, value := range agents {
		item, ok := value.(map[string]any)
		target := text(item["target"])
		relative, inside := strings.CutPrefix(target, prefix)
		if !ok || !claudeAgentName(name) || !inside || relative != claudeAgentFile(name) || len(item) != 2 {
			return errors.New("corrupt agent definition ownership record")
		}
		original := object(item["original"])
		if !equal(original, Object{"kind": "absent"}) {
			return errors.New("corrupt original agent definition")
		}
	}
	return nil
}

// verifyAgents checks that a Claude host owns exactly the release's definitions
// and that every link is still the one it created.
func (i *Installer) verifyAgents(h hostTarget, hs, m Object) error {
	if h.id != hostClaude {
		if hs["agents"] != nil {
			return errors.New("agent definitions apply to the Claude host")
		}
		return nil
	}
	owned, next := object(hs["agents"]), manifestClaudeAgents(m)
	if len(owned) != len(next) {
		return errors.New("agent definition ownership differs from the release")
	}
	if len(next) == 0 {
		return nil
	}
	if err := agentDirectory(h); err != nil {
		return err
	}
	for name, root := range next {
		item := object(owned[name])
		if item == nil || item["target"] != i.target(root) {
			return errors.New("agent definition ownership differs from the release")
		}
		actual, err := observe(i.agentPath(h, name))
		if err != nil {
			return err
		}
		if !equal(actual, Object{"kind": "symlink", "target": item["target"]}) {
			return fmt.Errorf("owned agent definition changed: %s", name)
		}
	}
	return nil
}

// suiteClaudeAgents validates the manifest's optional agent definitions. Each
// one names itself, keeps the Skill tool out and leaves model and effort to
// the dispatching coordinator's policy.
func suiteClaudeAgents(root string, value any) error {
	agents, err := suiteMapping(value, claudeAgentsKey)
	if err != nil || len(agents) == 0 {
		return errors.New("claude_agents must be a nonempty mapping")
	}
	for name, file := range agents {
		if !claudeAgentName(name) || file != claudeAgentFile(name) {
			return fmt.Errorf("claude agent %q must use %s", name, claudeAgentFile(name))
		}
		path, err := suiteRelativePath(root, file, "claude agent definition")
		if err != nil {
			return err
		}
		if !suiteRegular(path) {
			return fmt.Errorf("claude agent definition is missing: %s", file)
		}
		data, err := suiteRead(path, text(file))
		if err != nil {
			return err
		}
		if err := suiteClaudeAgentDefinition(name, string(data)); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	return nil
}

func suiteClaudeAgentDefinition(name, data string) error {
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return errors.New("missing agent frontmatter")
	}
	end := slices.Index(lines[1:], "---")
	if end < 0 {
		return errors.New("unterminated agent frontmatter")
	}
	value, err := suiteYAML([]byte(strings.Join(lines[1:end+1], "\n")))
	if err != nil {
		return fmt.Errorf("invalid agent frontmatter: %w", err)
	}
	fields, err := suiteMapping(value, "agent frontmatter", "name", "description", "disallowedTools")
	if err != nil {
		return err
	}
	if fields["name"] != name {
		return errors.New("agent name differs from its manifest entry")
	}
	if !suiteText(fields["description"]) {
		return errors.New("agent description is empty")
	}
	if fields["disallowedTools"] != "Skill" {
		return errors.New("agent definitions must disallow exactly the Skill tool")
	}
	if strings.TrimSpace(strings.Join(lines[end+2:], "\n")) == "" {
		return errors.New("agent prompt is empty")
	}
	return nil
}
