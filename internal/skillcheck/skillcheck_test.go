package skillcheck

import (
	"bufio"
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
	"gopkg.in/yaml.v3"
)

// repoRoot is the repository checkout this package lives in.
var repoRoot = filepath.Join("..", "..")

type manifest struct {
	Registrations      map[string]string `json:"registrations"`
	GlobalInstructions string            `json:"global_instructions"`
	RequiredLicenses   []string          `json:"required_licenses"`
	ModelPolicy        string            `json:"model_policy"`
	ClaudeModelPolicy  string            `json:"claude_model_policy"`
}

func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func requireExists(t *testing.T, field, path string) {
	t.Helper()
	if path == "./" {
		path = "."
	}
	if path == "" || !filepath.IsLocal(filepath.FromSlash(path)) {
		t.Errorf("%s: %q is not a relative path inside the repository", field, path)
		return
	}
	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(path))); err != nil {
		t.Errorf("%s: %v", field, err)
	}
}

// repoFiles lists tracked and untracked, not ignored files, slash-separated.
func repoFiles(t *testing.T) []string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", "-C", repoRoot, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	output, err := command.Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	var files []string
	for name := range strings.SplitSeq(strings.TrimSuffix(string(output), "\x00"), "\x00") {
		if _, err := os.Lstat(filepath.Join(repoRoot, name)); err == nil {
			files = append(files, name)
		}
	}
	return files
}

// skillRoots are the directories the hosts load skills from: every manifest
// registration and every Claude Code plugin skill path.
func skillRoots(t *testing.T) []string {
	var m manifest
	readJSON(t, "skills-manifest.json", &m)
	var plugin struct {
		Skills []string `json:"skills"`
	}
	readJSON(t, ".claude-plugin/plugin.json", &plugin)
	roots := slices.Collect(func(yield func(string) bool) {
		for _, dir := range m.Registrations {
			yield(dir)
		}
	})
	for _, dir := range plugin.Skills {
		roots = append(roots, strings.TrimSuffix(strings.TrimPrefix(dir, "./"), "/"))
	}
	slices.Sort(roots)
	return slices.Compact(roots)
}

func skillEntrypoints(t *testing.T) []string {
	var entrypoints []string
	for _, root := range skillRoots(t) {
		err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Name() == "SKILL.md" {
				entrypoints = append(entrypoints, path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(entrypoints)
	return slices.Compact(entrypoints)
}

type frontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation *bool  `yaml:"disable-model-invocation"`
}

var skillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// listedSkills are the entry points the workflow depends on being found
// without the user naming them; none may disable model invocation.
var listedSkills = []string{"archify", "code-review", "graphify", "task-orchestration", "test-strategy"}

func TestSkillMetadata(t *testing.T) {
	entrypoints := skillEntrypoints(t)
	if len(entrypoints) < 50 {
		t.Fatalf("found only %d skill entrypoints", len(entrypoints))
	}
	names := map[string]string{}
	for _, path := range entrypoints {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		head, ok := bytes.CutPrefix(data, []byte("---\n"))
		end := bytes.Index(head, []byte("\n---\n"))
		if !ok || end < 0 {
			t.Errorf("%s: missing frontmatter", path)
			continue
		}
		var fields frontmatter
		if err := yaml.Unmarshal(head[:end], &fields); err != nil {
			t.Errorf("%s: invalid frontmatter: %v", path, err)
			continue
		}
		switch {
		case !skillName.MatchString(fields.Name):
			t.Errorf("%s: invalid name %q", path, fields.Name)
		case fields.Name != filepath.Base(filepath.Dir(path)):
			t.Errorf("%s: name %q differs from its directory", path, fields.Name)
		case names[fields.Name] != "":
			t.Errorf("%s: name %q also used by %s", path, fields.Name, names[fields.Name])
		}
		names[fields.Name] = path
		if description := strings.TrimSpace(fields.Description); description == "" || len(description) > 1024 {
			t.Errorf("%s: description must have 1 to 1024 characters, has %d", path, len(description))
		}
		hidden := fields.DisableModelInvocation != nil && *fields.DisableModelInvocation
		if hidden && slices.Contains(listedSkills, fields.Name) {
			t.Errorf("%s: %s must stay discoverable without being named", path, fields.Name)
		}
		if implicit := codexImplicit(t, filepath.Join(filepath.Dir(path), "agents", "openai.yaml")); hidden == implicit {
			t.Errorf("%s: disable-model-invocation=%v disagrees with agents/openai.yaml allow_implicit_invocation=%v", path, hidden, implicit)
		}
	}
	for _, name := range listedSkills {
		if names[name] == "" {
			t.Errorf("listed skill %s is not registered", name)
		}
	}
}

// codexImplicit reads policy.allow_implicit_invocation, which defaults to true.
func codexImplicit(t *testing.T, path string) bool {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Policy struct {
			AllowImplicitInvocation *bool `yaml:"allow_implicit_invocation"`
		} `yaml:"policy"`
	}
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return metadata.Policy.AllowImplicitInvocation == nil || *metadata.Policy.AllowImplicitInvocation
}

var (
	// A link's text starts at a bracket that does not follow a word or a
	// closing bracket, so Go generics such as Map[T](x) are not links.
	inlineLink    = regexp.MustCompile(`(?:^|[^\w\]\\])!?\[[^\[\]]*\]\(\s*(<[^>]*>|[^)\s]+)`)
	referenceLink = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*(<[^>]*>|\S+)`)
	codeSpan      = regexp.MustCompile("`+[^`]*`+")
)

// markdownTargets returns the link destinations outside code fences and
// code spans.
func markdownTargets(data []byte) []string {
	var targets []string
	fence := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if marker := trimmed[:min(3, len(trimmed))]; marker == "```" || marker == "~~~" {
			switch {
			case fence == "":
				fence = marker
			case fence == marker:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		line = codeSpan.ReplaceAllString(line, "")
		for _, match := range inlineLink.FindAllStringSubmatch(line, -1) {
			targets = append(targets, match[1])
		}
		if match := referenceLink.FindStringSubmatch(line); match != nil {
			targets = append(targets, match[1])
		}
	}
	return targets
}

func TestRelativeMarkdownLinksResolve(t *testing.T) {
	var documents []string
	for _, file := range repoFiles(t) {
		inSkill := slices.ContainsFunc(skillRoots(t), func(root string) bool { return strings.HasPrefix(file, root+"/") })
		topLevel := !strings.Contains(file, "/") || strings.HasPrefix(file, "docs/") || strings.HasPrefix(file, ".github/")
		// Templates link to files of the project they are copied into.
		template := strings.Contains(file, "/assets/templates/")
		if strings.HasSuffix(file, ".md") && (inSkill || topLevel) && !template {
			documents = append(documents, file)
		}
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range documents {
		data, err := os.ReadFile(filepath.Join(repoRoot, document))
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range markdownTargets(data) {
			target = strings.Trim(target, "<>")
			parsed, err := url.Parse(target)
			if err != nil {
				t.Errorf("%s: invalid link %q", document, target)
				continue
			}
			if parsed.Scheme != "" || parsed.Host != "" || parsed.Path == "" {
				continue
			}
			resolved := filepath.Join(root, filepath.Dir(document), filepath.FromSlash(parsed.Path))
			if strings.HasPrefix(parsed.Path, "/") || !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
				t.Errorf("%s: link %q leaves the repository", document, target)
			} else if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s: broken link %q", document, target)
			}
		}
	}
}

func TestManifestAndPluginPathsResolve(t *testing.T) {
	var m manifest
	readJSON(t, "skills-manifest.json", &m)
	if len(m.Registrations) == 0 {
		t.Fatal("skills-manifest.json has no registrations")
	}
	for name, dir := range m.Registrations {
		if !skillName.MatchString(name) {
			t.Errorf("registration %q: invalid name", name)
		}
		requireExists(t, "registration "+name, dir)
		entrypoints := 0
		_ = filepath.WalkDir(filepath.Join(repoRoot, filepath.FromSlash(dir)), func(_ string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Name() == "SKILL.md" {
				entrypoints++
			}
			return nil
		})
		if entrypoints == 0 {
			t.Errorf("registration %q has no SKILL.md under %s", name, dir)
		}
		if _, err := os.Stat(filepath.Join(repoRoot, dir, "SKILL.md")); err == nil {
			if filepath.Base(dir) != name {
				t.Errorf("registration %q names directory %s", name, dir)
			}
		}
	}
	requireExists(t, "global_instructions", m.GlobalInstructions)
	requireExists(t, "model_policy", m.ModelPolicy)
	requireExists(t, "claude_model_policy", m.ClaudeModelPolicy)
	for _, path := range m.RequiredLicenses {
		requireExists(t, "required_licenses", path)
	}

	var claude struct {
		Skills []string `json:"skills"`
		Agents []string `json:"agents"`
	}
	readJSON(t, ".claude-plugin/plugin.json", &claude)
	for _, path := range slices.Concat(claude.Skills, claude.Agents) {
		requireExists(t, ".claude-plugin/plugin.json", path)
	}
	var codex struct {
		Skills string `json:"skills"`
	}
	readJSON(t, ".codex-plugin/plugin.json", &codex)
	requireExists(t, ".codex-plugin/plugin.json skills", codex.Skills)
	for _, marketplace := range []string{".claude-plugin/marketplace.json", ".agents/plugins/marketplace.json"} {
		var listing struct {
			Plugins []struct {
				Source jsontext.Value `json:"source"`
			} `json:"plugins"`
		}
		readJSON(t, marketplace, &listing)
		for _, plugin := range listing.Plugins {
			var local string
			var object struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(plugin.Source, &local) == nil {
				requireExists(t, marketplace, local)
			} else if json.Unmarshal(plugin.Source, &object) == nil && object.Path != "" {
				requireExists(t, marketplace, object.Path)
			}
		}
	}
}

// The Claude Code plugin, the Codex plugin and the Claude Code marketplace
// carry one version string: the next release tag without its leading "v"
// (docs/development.md, Releases).
// oracle: the three strings read from the manifests are non-empty and equal.
func TestPluginVersionsAgree(t *testing.T) {
	var claude, codex struct {
		Version string `json:"version"`
	}
	var marketplace struct {
		Metadata struct {
			Version string `json:"version"`
		} `json:"metadata"`
	}
	readJSON(t, ".claude-plugin/plugin.json", &claude)
	readJSON(t, ".codex-plugin/plugin.json", &codex)
	readJSON(t, ".claude-plugin/marketplace.json", &marketplace)
	if claude.Version == "" || codex.Version != claude.Version || marketplace.Metadata.Version != claude.Version {
		t.Errorf("plugin versions disagree: .claude-plugin/plugin.json version %q, .codex-plugin/plugin.json version %q, .claude-plugin/marketplace.json metadata.version %q",
			claude.Version, codex.Version, marketplace.Metadata.Version)
	}
}

// pinnedInstall matches a cw install command pinned to a concrete tag; a
// placeholder such as @vX does not match.
var pinnedInstall = regexp.MustCompile("agent-workflows/v3/cmd/cw@(v[0-9][^\\s`'\")\\]]*)")

// installCommandFiles tell a user without cw how to install it: the
// quick start, Get cw, the always-on conventions, the task-orchestration
// skill's first step and the session-start hook's missing-cw line.
var installCommandFiles = []string{"README.md", "docs/operations.md", "global/AGENTS.md", "skills/task-orchestration/SKILL.md", "hooks/hooks.json"}

// Every place that tells a user without cw how to install it gives the
// go install command for the next release tag, and no first-party file but
// the changelog pins another tag (docs/development.md, Releases).
// oracle: each of installCommandFiles contains the go install command for
// "v" + the .claude-plugin/plugin.json version, and every pinned cw install
// command outside CHANGELOG.md and third_party/ names that same tag.
func TestInstallCommandsNameTheRelease(t *testing.T) {
	var plugin struct {
		Version string `json:"version"`
	}
	readJSON(t, ".claude-plugin/plugin.json", &plugin)
	if plugin.Version == "" {
		t.Fatal(".claude-plugin/plugin.json has no version")
	}
	tag := "v" + plugin.Version
	want := "go install github.com/belevtsev/agent-workflows/v3/cmd/cw@" + tag
	for _, file := range installCommandFiles {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s does not give the install command %q", file, want)
		}
	}
	for _, file := range repoFiles(t) {
		if file == "CHANGELOG.md" || strings.HasPrefix(file, "third_party/") {
			continue
		}
		path := filepath.Join(repoRoot, filepath.FromSlash(file))
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pinnedInstall.FindAllStringSubmatch(string(data), -1) {
			if match[1] != tag {
				t.Errorf("%s: a cw install command names %s, want %s", file, match[1], tag)
			}
		}
	}
}

// Contract roster-tests, revision 1, requirement A5: the manifest no longer
// carries claude_agents; the plugin's agents array is the only roster.
func TestA5_ManifestHasNoClaudeAgents(t *testing.T) {
	var members map[string]jsontext.Value
	readJSON(t, "skills-manifest.json", &members)
	if len(members) == 0 {
		t.Fatal("skills-manifest.json has no members")
	}
	if _, ok := members["claude_agents"]; ok {
		t.Error("skills-manifest.json still has a claude_agents member")
	}
}

func TestModelPoliciesLoad(t *testing.T) {
	var m manifest
	readJSON(t, "skills-manifest.json", &m)
	policy, err := modelpolicy.Load(filepath.Join(repoRoot, m.ModelPolicy))
	if err != nil {
		t.Fatal(err)
	}
	for provider := range policy.Providers {
		if !modelpolicy.Known(provider) {
			t.Errorf("unknown provider %s", provider)
		}
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, m.ClaudeModelPolicy))
	if err != nil {
		t.Fatal(err)
	}
	var claude map[string]any
	if err := yaml.Unmarshal(data, &claude); err != nil || len(claude) == 0 {
		t.Fatalf("%s: %v", m.ClaudeModelPolicy, err)
	}
}

// Automation lives only inside skills and vendored packs; the repository's
// own tooling is Go, and first-party skills carry no Python. The one exception
// is an eval case's scaffold.sh, the workspace seed `claude plugin eval` runs.
// .agents holds only the Codex marketplace listing.
func TestNoAutomationOutsideSkills(t *testing.T) {
	for _, file := range repoFiles(t) {
		if problem := automationProblem(file, readShebang(t, file)); problem != "" {
			t.Errorf("%s: %s", file, problem)
		}
		if strings.HasPrefix(file, ".agents/") && file != ".agents/plugins/marketplace.json" {
			t.Errorf("unexpected file under .agents: %s", file)
		}
	}
}

func TestAutomationProblem(t *testing.T) {
	for file, shebang := range map[string]string{
		"skills/foo/helper.py":         "",
		"tools/release.sh":             "",
		"tools/x.bash":                 "",
		"tools/x.zsh":                  "",
		"tools/requirements-dev.txt":   "",
		"scripts/release":              "#!/bin/bash",
		"scripts/check":                "#!/usr/bin/env python3",
		"scripts/run":                  "#!/bin/sh -e",
		"evals/case/other.sh":          "",
		"evals/case/scaffold.sh/x.zsh": "",
	} {
		if automationProblem(file, shebang) == "" {
			t.Errorf("%s (%q) was accepted", file, shebang)
		}
	}
	for file, shebang := range map[string]string{
		"skills/foo/run.sh":      "#!/bin/sh",
		"third_party/x/tool.py":  "",
		"evals/case/scaffold.sh": "#!/bin/bash",
		"cmd/cw/main.go":         "",
		"docs/requirements.md":   "",
		"tools/notes.txt":        "#!not a script interpreter",
	} {
		if problem := automationProblem(file, shebang); problem != "" {
			t.Errorf("%s (%q) was rejected: %s", file, shebang, problem)
		}
	}
}

// automationProblem reports why a tracked file breaks the automation rule,
// given its first line when that starts with #!.
func automationProblem(file, shebang string) string {
	ext := strings.ToLower(filepath.Ext(file))
	if strings.HasPrefix(file, "skills/") && ext == ".py" {
		return "first-party skills must use Go"
	}
	if strings.HasPrefix(file, "skills/") || strings.HasPrefix(file, "third_party/") ||
		strings.HasPrefix(file, "evals/") && path.Base(file) == "scaffold.sh" {
		return ""
	}
	base := strings.ToLower(path.Base(file))
	if slices.Contains([]string{".sh", ".py", ".bash", ".zsh"}, ext) || strings.HasPrefix(base, "requirements") && ext == ".txt" {
		return "first-party automation must use Go"
	}
	for word := range strings.FieldsSeq(strings.TrimPrefix(shebang, "#!")) {
		interpreter := path.Base(word)
		if strings.HasPrefix(interpreter, "python") || slices.Contains([]string{"sh", "bash", "zsh"}, interpreter) {
			return "first-party script interpreter must use Go"
		}
	}
	return ""
}

// readShebang returns a file's first line when it starts with #!.
func readShebang(t *testing.T, file string) string {
	t.Helper()
	f, err := os.Open(filepath.Join(repoRoot, file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	head := make([]byte, 256)
	n, _ := io.ReadFull(f, head)
	line, _, _ := bytes.Cut(head[:n], []byte("\n"))
	if !bytes.HasPrefix(line, []byte("#!")) {
		return ""
	}
	return string(line)
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
