package skillcheck

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// claudeAgentAlias is the Agent tool alias for each full model ID a Claude
// Code profile may name. Agent accepts aliases only; references/hosts-claude-code.md
// states the same mapping.
var claudeAgentAlias = map[string]string{
	"claude-opus-5-5":   "opus",
	"claude-sonnet-5-5": "sonnet",
	"claude-haiku-5-5":  "haiku",
}

// hostsProfileTable is the header of the profile table in
// references/hosts-claude-code.md that the coordinator dispatches Claude Code workers by.
const hostsProfileTable = "| Profile | Agent tool `model` | Agent tool `effort` |"

type claudeRouting struct {
	Profiles map[string]struct {
		Model  string `yaml:"model"`
		Effort string `yaml:"effort"`
		Scope  string `yaml:"scope"`
	} `yaml:"profiles"`
	Roles map[string]string `yaml:"roles"`
}

type codexRouting struct {
	Profiles map[string]struct {
		Model                string   `yaml:"model"`
		ReasoningEffort      string   `yaml:"reasoning_effort"`
		UserRequestedEfforts []string `yaml:"user_requested_efforts"`
		Scope                string   `yaml:"scope"`
	} `yaml:"profiles"`
	EffortPolicy struct {
		DefaultSubstantive    string   `yaml:"default_substantive"`
		ConfiguredMaxProfiles []string `yaml:"configured_max_profiles"`
	} `yaml:"effort_policy"`
	Roles              map[string]string `yaml:"roles"`
	UnknownRoleProfile string            `yaml:"unknown_role_profile"`
}

// readRouting decodes the routing part of a shipped policy file.
func readRouting(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, value); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// markdownTableRows returns the body rows of the table whose header line is
// header, keyed by the first cell, with backticks stripped from every cell.
func markdownTableRows(t *testing.T, path, header string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	start := slices.Index(lines, header)
	if start < 0 {
		t.Fatalf("%s: no table with header %q", path, header)
	}
	rows := map[string][]string{}
	// Skip the header and its separator line.
	for _, line := range lines[min(start+2, len(lines)):] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		var cells []string
		for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
			cells = append(cells, strings.Trim(strings.TrimSpace(cell), "`"))
		}
		rows[cells[0]] = cells[1:]
	}
	return rows
}

// TestClaudePolicyRoutesEvidenceRolesToCheaperModels checks the Claude Code
// routing of the read-only evidence roles and of the roles kept on Opus.
//
// Oracle: lookup (locating and quoting sources) runs on Haiku and
// summarization on Sonnet, both at high and with bounded_evidence scope.
// implementer, test_review and spec_review stay on Opus at high, since Sonnet
// for them is an eval variable rather than a shipped default; the decision
// roles stay on Opus at xhigh. The coordinator dispatches through the profile
// table in hosts-claude-code.md, so that table has exactly one row per policy profile,
// naming the profile model's Agent alias and the profile's effort.
func TestClaudePolicyRoutesEvidenceRolesToCheaperModels(t *testing.T) {
	var m manifest
	readJSON(t, "skills-manifest.json", &m)
	var policy claudeRouting
	readRouting(t, m.ClaudeModelPolicy, &policy)

	type route struct{ model, effort, scope string }
	want := map[string]route{
		"lookup":        {"claude-haiku-5-5", "high", "bounded_evidence"},
		"summarization": {"claude-sonnet-5-5", "high", "bounded_evidence"},
		"implementer":   {"claude-opus-5-5", "high", "substantive"},
		"test_review":   {"claude-opus-5-5", "high", "substantive"},
		"spec_review":   {"claude-opus-5-5", "high", "substantive"},
		"planning":      {"claude-opus-5-5", "xhigh", "substantive"},
		"architecture":  {"claude-opus-5-5", "xhigh", "substantive"},
		"architect":     {"claude-opus-5-5", "xhigh", "substantive"},
		"diagnosis":     {"claude-opus-5-5", "xhigh", "substantive"},
	}
	for _, role := range slices.Sorted(maps.Keys(want)) {
		name, ok := policy.Roles[role]
		if !ok {
			t.Errorf("%s: role %s is missing", m.ClaudeModelPolicy, role)
			continue
		}
		profile, ok := policy.Profiles[name]
		if !ok {
			t.Errorf("%s: role %s names undefined profile %q", m.ClaudeModelPolicy, role, name)
			continue
		}
		if got := (route{profile.Model, profile.Effort, profile.Scope}); got != want[role] {
			t.Errorf("%s: role %s routes to %+v, want %+v", m.ClaudeModelPolicy, role, got, want[role])
		}
	}

	const hosts = "skills/task-orchestration/references/hosts-claude-code.md"
	rows := markdownTableRows(t, hosts, hostsProfileTable)
	for _, name := range slices.Sorted(maps.Keys(policy.Profiles)) {
		profile := policy.Profiles[name]
		alias, ok := claudeAgentAlias[profile.Model]
		if !ok {
			t.Errorf("%s: profile %s names %q, which has no Agent tool alias", m.ClaudeModelPolicy, name, profile.Model)
			continue
		}
		row, ok := rows[name]
		if !ok {
			t.Errorf("%s: no row for profile %s", hosts, name)
			continue
		}
		if len(row) != 2 || row[0] != alias || row[1] != profile.Effort {
			t.Errorf("%s: profile %s row is %q, want [%s %s]", hosts, name, row, alias, profile.Effort)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(rows)) {
		if _, ok := policy.Profiles[name]; !ok {
			t.Errorf("%s: row for profile %s, which %s does not define", hosts, name, m.ClaudeModelPolicy)
		}
	}
}

// TestCodexPolicyKeepsMaxForDecisionRolesOnly checks the reasoning effort of
// every Codex role on a substantive profile.
//
// Oracle: planning, architecture, architect and diagnosis run at max; every
// other role on a substantive profile, and a role the policy does not name,
// runs at high. default_substantive is high, configured_max_profiles lists
// exactly the profiles configured at max (the max_for_sol rule in
// docs/workflow.md), and a substantive profile at high still lets the user ask
// for max.
func TestCodexPolicyKeepsMaxForDecisionRolesOnly(t *testing.T) {
	var m manifest
	readJSON(t, "skills-manifest.json", &m)
	var policy codexRouting
	readRouting(t, m.ModelPolicy, &policy)

	decision := []string{"architect", "architecture", "diagnosis", "planning"}
	for _, role := range decision {
		if _, ok := policy.Roles[role]; !ok {
			t.Errorf("%s: decision role %s is missing", m.ModelPolicy, role)
		}
	}
	roles := maps.Clone(policy.Roles)
	roles["(unknown role)"] = policy.UnknownRoleProfile
	for _, role := range slices.Sorted(maps.Keys(roles)) {
		profile, ok := policy.Profiles[roles[role]]
		if !ok {
			t.Errorf("%s: role %s names undefined profile %q", m.ModelPolicy, role, roles[role])
			continue
		}
		if profile.Scope != "substantive" {
			continue
		}
		want := "high"
		if slices.Contains(decision, role) {
			want = "max"
		}
		if profile.ReasoningEffort != want {
			t.Errorf("%s: role %s runs profile %s at %s, want %s", m.ModelPolicy, role, roles[role], profile.ReasoningEffort, want)
		}
	}

	if got := policy.EffortPolicy.DefaultSubstantive; got != "high" {
		t.Errorf("%s: effort_policy.default_substantive = %q, want high", m.ModelPolicy, got)
	}
	var atMax []string
	for _, name := range slices.Sorted(maps.Keys(policy.Profiles)) {
		profile := policy.Profiles[name]
		if profile.ReasoningEffort == "max" {
			atMax = append(atMax, name)
		}
		if profile.Scope == "substantive" && profile.ReasoningEffort == "high" && !slices.Contains(profile.UserRequestedEfforts, "max") {
			t.Errorf("%s: profile %s runs at high but does not list max in user_requested_efforts", m.ModelPolicy, name)
		}
	}
	if got := slices.Sorted(slices.Values(policy.EffortPolicy.ConfiguredMaxProfiles)); !slices.Equal(got, atMax) {
		t.Errorf("%s: effort_policy.configured_max_profiles = %q, want the profiles at max %q", m.ModelPolicy, got, atMax)
	}
}

// TestCodexDefaultsRunMemoryPassesOnLuna checks the Codex settings that
// cw install --host codex recommends.
//
// Oracle: memory extraction and consolidation are background summaries, so
// both name gpt-6-luna, the model the shared policy routes summarization to;
// and the file sets no model_context_window, since no measurement backs
// overriding the model's own window.
func TestCodexDefaultsRunMemoryPassesOnLuna(t *testing.T) {
	const path = "global/codex-defaults.toml"
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	var defaults map[string]any
	if err := toml.Unmarshal(data, &defaults); err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	var m manifest
	readJSON(t, "skills-manifest.json", &m)
	var policy codexRouting
	readRouting(t, m.ModelPolicy, &policy)
	const luna = "gpt-6-luna"
	if got := policy.Profiles[policy.Roles["summarization"]].Model; got != luna {
		t.Fatalf("%s: summarization runs %q, want %s", m.ModelPolicy, got, luna)
	}

	memories, _ := defaults["memories"].(map[string]any)
	for _, key := range []string{"extract_model", "consolidation_model"} {
		if got := memories[key]; got != luna {
			t.Errorf("%s: memories.%s = %v, want %s", path, key, got, luna)
		}
	}
	if window, ok := defaults["model_context_window"]; ok {
		t.Errorf("%s: model_context_window = %v, want it unset", path, window)
	}
}
