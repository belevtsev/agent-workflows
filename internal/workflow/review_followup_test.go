package workflow

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Tests for the review_followup role contract (review-followup-role, version 3).

func reviewFollowupPolicyPath(name string) string {
	return filepath.Join("..", "..", "skills", "task-orchestration", name)
}

// reviewFollowupMap converts a decoded policy node into a string-keyed map.
func reviewFollowupMap(t *testing.T, label string, value any) map[string]any {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		converted := make(map[string]any, len(typed))
		for key, item := range typed {
			converted[key] = item
		}
		return converted
	default:
		t.Fatalf("%s: got %T, want a string-keyed map", label, value)
		return nil
	}
}

func reviewFollowupString(t *testing.T, label string, value any) string {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("%s: got %T (%v), want string", label, value, value)
	}
	return text
}

// reviewFollowupProfile returns the profile name a role routes to and that
// profile's definition. Profile fields differ between policies: Claude
// profiles carry "effort", shared (Codex) profiles carry "reasoning_effort".
func reviewFollowupProfile(t *testing.T, policy Object, role string) (string, map[string]any) {
	t.Helper()
	roles := reviewFollowupMap(t, "policy roles", policy["roles"])
	rawProfile, ok := roles[role]
	if !ok {
		t.Fatalf("policy roles has no %q; roles: %v", role, slices.Sorted(maps.Keys(roles)))
	}
	profile := reviewFollowupString(t, "roles."+role, rawProfile)
	profiles := reviewFollowupMap(t, "policy profiles", policy["profiles"])
	rawSpec, ok := profiles[profile]
	if !ok {
		t.Fatalf("role %q routes to profile %q, which policy profiles does not define", role, profile)
	}
	return profile, reviewFollowupMap(t, "profiles."+profile, rawSpec)
}

// reviewFollowupClaudeRoute returns the Claude profile name a role routes to
// and that profile's model, effort and scope.
func reviewFollowupClaudeRoute(t *testing.T, policy Object, role string) (profile, model, effort, scope string) {
	t.Helper()
	profile, spec := reviewFollowupProfile(t, policy, role)
	model = reviewFollowupString(t, "profiles."+profile+".model", spec["model"])
	effort = reviewFollowupString(t, "profiles."+profile+".effort", spec["effort"])
	if rawScope, present := spec["scope"]; present {
		scope = reviewFollowupString(t, "profiles."+profile+".scope", rawScope)
	}
	return profile, model, effort, scope
}

func loadReviewFollowupPolicies(t *testing.T) (shared, claude Object) {
	t.Helper()
	shared, err := suiteModelPolicy(reviewFollowupPolicyPath("model-policy.yaml"))
	if err != nil {
		t.Fatalf("suiteModelPolicy(shipped shared policy): %v", err)
	}
	claude, err = suiteClaudeModelPolicy(reviewFollowupPolicyPath("model-policy.claude.yaml"), shared)
	if err != nil {
		t.Fatalf("suiteClaudeModelPolicy(shipped Claude policy): %v", err)
	}
	return shared, claude
}

// B1: the shipped Claude policy routes review_followup to Sonnet and keeps
// review on Opus.
func TestShippedClaudePolicyRoutesReviewFollowupToSonnet(t *testing.T) {
	_, claude := loadReviewFollowupPolicies(t)

	profile, model, effort, scope := reviewFollowupClaudeRoute(t, claude, "review_followup")
	if model != "claude-sonnet-5-5" {
		t.Errorf("review_followup (profile %q) model = %q, want %q", profile, model, "claude-sonnet-5-5")
	}
	if effort != "high" {
		t.Errorf("review_followup (profile %q) effort = %q, want %q", profile, effort, "high")
	}
	if scope != "substantive" {
		t.Errorf("review_followup (profile %q) scope = %q, want %q", profile, scope, "substantive")
	}

	reviewProfile, reviewModel, reviewEffort, _ := reviewFollowupClaudeRoute(t, claude, "review")
	if reviewModel != "claude-opus-5-5" {
		t.Errorf("review (profile %q) model = %q, want %q", reviewProfile, reviewModel, "claude-opus-5-5")
	}
	if reviewEffort != "high" {
		t.Errorf("review (profile %q) effort = %q, want %q", reviewProfile, reviewEffort, "high")
	}
	if reviewProfile == profile {
		t.Errorf("review and review_followup share profile %q on Claude; review_followup must use Sonnet while review stays on Opus", profile)
	}
}

// reviewFollowupShippedClaudeText returns the shipped Claude policy text.
func reviewFollowupShippedClaudeText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(reviewFollowupPolicyPath("model-policy.claude.yaml"))
	if err != nil {
		t.Fatalf("read shipped Claude policy: %v", err)
	}
	return string(data)
}

// reviewFollowupWithoutRole removes the single two-space-indented
// "role: profile" line for role from the policy text, failing if there is not
// exactly one such line.
func reviewFollowupWithoutRole(t *testing.T, text, role string) string {
	t.Helper()
	prefix := "  " + role + ":"
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	removed := 0
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		// A profile definition with the same name ("  review_followup:" with
		// nested fields) is also indented by two spaces; only a role line
		// carries a profile name after the colon.
		if value, found := strings.CutPrefix(trimmed, prefix); found && strings.TrimSpace(value) != "" {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed != 1 {
		t.Fatalf("expected exactly one %q line in the shipped Claude policy, found %d", prefix+" <profile>", removed)
	}
	return strings.Join(kept, "\n")
}

// reviewFollowupWithExtraRole adds a role line directly under "roles:".
func reviewFollowupWithExtraRole(t *testing.T, text, line string) string {
	t.Helper()
	const header = "\nroles:\n"
	if strings.Count(text, header) != 1 {
		t.Fatalf("expected exactly one %q section in the shipped Claude policy", "roles:")
	}
	return strings.Replace(text, header, header+line+"\n", 1)
}

func reviewFollowupWritePolicy(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model-policy.claude.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write edited Claude policy: %v", err)
	}
	return path
}

// B2: review_followup is a Claude-only role. The shared policy does not have
// it; the Claude validator accepts it (or its absence) but no other extra role,
// and still requires every shared role.
func TestReviewFollowupIsClaudeOnlyRole(t *testing.T) {
	shared, _ := loadReviewFollowupPolicies(t)
	shipped := reviewFollowupShippedClaudeText(t)

	t.Run("shared policy has no review_followup", func(t *testing.T) {
		roles := reviewFollowupMap(t, "shared policy roles", shared["roles"])
		if profile, present := roles["review_followup"]; present {
			t.Errorf("shared policy routes review_followup to %v; want no review_followup role", profile)
		}
		if _, present := roles["review"]; !present {
			t.Errorf("shared policy has no review role; roles: %v", slices.Sorted(maps.Keys(roles)))
		}
	})

	t.Run("accepts Claude policy without review_followup", func(t *testing.T) {
		edited := reviewFollowupWithoutRole(t, shipped, "review_followup")
		policy, err := suiteClaudeModelPolicy(reviewFollowupWritePolicy(t, edited), shared)
		if err != nil {
			t.Fatalf("suiteClaudeModelPolicy(without review_followup) error = %v, want nil", err)
		}
		roles := reviewFollowupMap(t, "Claude policy roles", policy["roles"])
		if profile, present := roles["review_followup"]; present {
			t.Errorf("policy without the review_followup line still routes it to %v", profile)
		}
	})

	rejected := map[string]string{
		"rejects an arbitrary extra role": reviewFollowupWithExtraRole(t, shipped, "  extra_role: substantive"),
		"rejects a missing shared role":   reviewFollowupWithoutRole(t, shipped, "review"),
	}
	for name, edited := range rejected {
		t.Run(name, func(t *testing.T) {
			_, err := suiteClaudeModelPolicy(reviewFollowupWritePolicy(t, edited), shared)
			if err == nil {
				t.Fatal("suiteClaudeModelPolicy error = nil, want a role mismatch error")
			}
			if want := "must match the shared policy"; !strings.Contains(err.Error(), want) {
				t.Errorf("suiteClaudeModelPolicy error = %q, want it to contain %q", err.Error(), want)
			}
		})
	}
}

func reviewFollowupClaudeChoices() []ModelChoice {
	return []ModelChoice{
		{Model: "claude-opus-5-5", Efforts: []string{"low", "medium", "high", "xhigh"}},
		{Model: "claude-sonnet-5-5", Efforts: []string{"low", "medium", "high", "xhigh"}},
	}
}

func reviewFollowupCodexChoices() []ModelChoice {
	return []ModelChoice{
		{Model: "gpt-6.1-sol", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
		{Model: "gpt-6-luna", Efforts: []string{"high", "xhigh"}},
	}
}

func reviewFollowupRole(role, profile, scope, model, effort string, choices []ModelChoice) RoleConfiguration {
	selection := ModelSelection{Model: model, Effort: effort}
	return RoleConfiguration{
		Role:      role,
		Profile:   profile,
		Scope:     scope,
		Default:   selection,
		Effective: selection,
		Choices:   choices,
	}
}

func reviewFollowupView() ConfigurationView {
	claudeChoices := reviewFollowupClaudeChoices()
	codexChoices := reviewFollowupCodexChoices()
	return ConfigurationView{
		Hosts: []HostConfiguration{
			{
				ID: "claude",
				Roles: []RoleConfiguration{
					reviewFollowupRole("coordinator", "substantive", "substantive", "claude-opus-5-5", "high", claudeChoices),
					reviewFollowupRole("review", "review", "substantive", "claude-opus-5-5", "high", claudeChoices),
					reviewFollowupRole("review_followup", "review_followup", "substantive", "claude-sonnet-5-5", "high", claudeChoices),
					reviewFollowupRole("implementation", "implementation", "bounded_execution", "claude-sonnet-5-5", "medium", claudeChoices),
					reviewFollowupRole("github", "github", "bounded_execution", "claude-sonnet-5-5", "medium", claudeChoices),
					reviewFollowupRole("explorer", "explorer", "bounded_evidence", "claude-sonnet-5-5", "low", claudeChoices),
				},
				Fallback: reviewFollowupRole("fallback", "fallback", "substantive", "claude-opus-5-5", "high", claudeChoices),
			},
			{
				ID: "codex",
				Roles: []RoleConfiguration{
					reviewFollowupRole("coordinator", "substantive", "substantive", "gpt-6.1-sol", "high", codexChoices),
					reviewFollowupRole("review", "review", "substantive", "gpt-6.1-sol", "high", codexChoices),
					reviewFollowupRole("planning", "decision", "substantive", "gpt-6.1-sol", "medium", codexChoices),
					reviewFollowupRole("implementation", "implementation", "bounded_execution", "gpt-6-luna", "high", codexChoices),
					reviewFollowupRole("github", "github", "bounded_execution", "gpt-6-luna", "high", codexChoices),
					reviewFollowupRole("explorer", "explorer", "bounded_evidence", "gpt-6-luna", "high", codexChoices),
				},
				Fallback: reviewFollowupRole("fallback", "fallback", "substantive", "gpt-6.1-sol", "high", codexChoices),
			},
		},
		Preferences:     Preferences{Version: 1},
		SharedDefaults:  Object{},
		SharedEffective: Object{},
	}
}

func reviewFollowupDeref(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return *value
}

func checkReviewFollowupOverride(t *testing.T, label string, got ModelOverride, model, effort string) {
	t.Helper()
	if got.Model == nil || *got.Model != model {
		t.Errorf("%s model = %s, want %q", label, reviewFollowupDeref(got.Model), model)
	}
	if got.Effort == nil || *got.Effort != effort {
		t.Errorf("%s effort = %s, want %q", label, reviewFollowupDeref(got.Effort), effort)
	}
}

func checkReviewFollowupHost(t *testing.T, prefs Preferences, host string, pinned, unpinned []string, model, effort string) {
	t.Helper()
	hostPrefs, ok := prefs.Hosts[host]
	if !ok {
		t.Fatalf("balanced preferences have no host %q; hosts: %v", host, slices.Sorted(maps.Keys(prefs.Hosts)))
	}
	for _, role := range pinned {
		override, present := hostPrefs.Roles[role]
		if !present {
			t.Errorf("%s: balanced preset does not pin role %q; roles: %v", host, role, slices.Sorted(maps.Keys(hostPrefs.Roles)))
			continue
		}
		checkReviewFollowupOverride(t, host+" role "+role, override, model, effort)
	}
	for _, role := range unpinned {
		if override, present := hostPrefs.Roles[role]; present {
			t.Errorf("%s: balanced preset pins role %q to {%s, %s}; want no entry",
				host, role, reviewFollowupDeref(override.Model), reviewFollowupDeref(override.Effort))
		}
	}
	if got, want := slices.Sorted(maps.Keys(hostPrefs.Roles)), slices.Sorted(slices.Values(pinned)); !slices.Equal(got, want) {
		t.Errorf("%s: balanced pinned roles = %v, want exactly %v", host, got, want)
	}
	if hostPrefs.Fallback == nil {
		t.Errorf("%s: balanced fallback is nil, want {%s, %s}", host, model, effort)
	} else {
		checkReviewFollowupOverride(t, host+" fallback", *hostPrefs.Fallback, model, effort)
	}
}

// B3: the Balanced preset pins Opus-default substantive roles on Claude but
// leaves the Sonnet review_followup role alone, and pins every substantive
// role on Codex, whose view has no review_followup role.
func TestBalancedPresetReviewFollowupPins(t *testing.T) {
	prefs, err := PreferencesForSelection(reviewFollowupView(), ConfigurationSelection{Preset: PresetBalanced})
	if err != nil {
		t.Fatalf("PreferencesForSelection(balanced): %v", err)
	}

	t.Run("claude", func(t *testing.T) {
		checkReviewFollowupHost(t, prefs, "claude",
			[]string{"coordinator", "review"},
			[]string{"review_followup", "implementation", "github", "explorer"},
			"claude-opus-5-5", "high")
	})
	t.Run("codex", func(t *testing.T) {
		checkReviewFollowupHost(t, prefs, "codex",
			[]string{"coordinator", "review", "planning"},
			[]string{"implementation", "github", "explorer"},
			"gpt-6.1-sol", "xhigh")
	})
}
