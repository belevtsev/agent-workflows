package usagelog

import "testing"

// Contract claude-agents-tests, revision 2, requirement A18: the shipped
// Claude agent types identify a composition role when the description names
// none, and a description that names a role still wins.
func TestA18_DescribedRoleUsesClaudeAgentTypes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, description, agentType string
		want                         roleChoice
	}{
		{"reviewer type", "", "cw-reviewer", roleChoice{role: "review", basis: "agent_type"}},
		{"test author type", "", "cw-test-author", roleChoice{role: "test_author", basis: "agent_type"}},
		{"GitHub worker type", "", "cw-github-worker", roleChoice{role: "github", basis: "agent_type"}},
		{"description wins over the type", "Follow-up review of the fix", "cw-reviewer", roleChoice{role: "review_followup", basis: "description"}},
		{"unknown custom type", "", "my-custom-agent", roleChoice{role: "other", basis: "none"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := describedRole(test.description, test.agentType); got != test.want {
				t.Fatalf("describedRole(%q, %q) = %+v; want %+v", test.description, test.agentType, got, test.want)
			}
		})
	}
}
