package workflow

import (
	"strings"
	"testing"
)

func TestClaudeModelDefaultsUseCoordinatorProfile(t *testing.T) {
	fixture := newSuiteFixture(t)
	defaults, err := ClaudeModelDefaults(fixture.root, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if defaults["model"] != "claude-opus-5-5" || defaults["effortLevel"] != "xhigh" || len(defaults) != 2 {
		t.Fatalf("wrong Claude defaults: %v", defaults)
	}
}

func TestClaudeModelPolicyRejectsInvalidMappings(t *testing.T) {
	for _, test := range []struct{ name, old, replacement, message string }{
		{"unknown model", "model: claude-opus-5-5", "model: gpt-6-luna", "unsupported Claude model"},
		{"max effort", "effort: xhigh", "effort: max", "unsupported Claude effort"},
		{"unknown scope", "scope: substantive", "scope: anything", "unsupported Claude scope"},
		{"missing role", "  summarization: bounded_evidence\n", "", "must match the shared policy"},
		{"extra role", "  summarization: bounded_evidence\n", "  summarization: bounded_evidence\n  extra: substantive\n", "must match the shared policy"},
		{"unknown profile", "  lookup: bounded_evidence", "  lookup: missing", "unknown profile"},
		{"wrong scope", "  lookup: bounded_evidence", "  lookup: substantive", "requires bounded_evidence scope"},
		{"bounded unknown role", "unknown_role_profile: substantive", "unknown_role_profile: bounded_evidence", "must use a substantive profile"},
		{"extra field", "version: 1\n", "version: 1\nextra: true\n", "invalid fields"},
		{"wrong version", "version: 1\n", "version: 2\n", "version has an invalid value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			path := "skills/task-orchestration/model-policy.claude.yaml"
			original := installerRead(t, fixture.root+"/"+path)
			if !strings.Contains(original, test.old) {
				t.Fatalf("fixture lacks %q", test.old)
			}
			fixture.write(t, path, strings.ReplaceAll(original, test.old, test.replacement))
			if _, err := ValidateSuite(fixture.root); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("ValidateSuite() error = %v; want containing %q", err, test.message)
			}
		})
	}
}
