package usagelog

import "testing"

// Contract roster-tests, revision 1, requirement C1: with an empty
// description, the shipped agent types and their removed cw- predecessors,
// bare or plugin-qualified, map to a composition role with basis agent_type.
// A mapping that misses any of these spellings fails here.
func TestC1_DescribedRoleMapsAgentTypes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		agentType string
		role      string
	}{
		{"reviewer", "review"},
		{"agent-workflows:reviewer", "review"},
		{"cw-reviewer", "review"},
		{"agent-workflows:cw-reviewer", "review"},
		{"test-engineer", "test_author"},
		{"agent-workflows:test-engineer", "test_author"},
		{"cw-test-author", "test_author"},
		{"agent-workflows:cw-test-author", "test_author"},
		{"github-worker", "github"},
		{"agent-workflows:github-worker", "github"},
		{"cw-github-worker", "github"},
		{"agent-workflows:cw-github-worker", "github"},
	} {
		t.Run(test.agentType, func(t *testing.T) {
			t.Parallel()
			want := roleChoice{role: test.role, basis: "agent_type"}
			if got := describedRole("", test.agentType); got != want {
				t.Fatalf("describedRole(%q, %q) = %+v; want %+v", "", test.agentType, got, want)
			}
		})
	}
}

// Requirement C2: spec-reviewer (a removed agent) and unknown custom types
// map to no role; the built-in Explore and Plan types keep their roles.
// Treating spec-reviewer as a reviewer, or dropping Explore/Plan, fails here.
func TestC2_DescribedRoleOtherAndBuiltInTypes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		agentType string
		want      roleChoice
	}{
		{"spec-reviewer", roleChoice{role: "other", basis: "none"}},
		{"agent-workflows:spec-reviewer", roleChoice{role: "other", basis: "none"}},
		{"my-custom-agent", roleChoice{role: "other", basis: "none"}},
		{"Explore", roleChoice{role: "evidence", basis: "agent_type"}},
		{"Plan", roleChoice{role: "planning", basis: "agent_type"}},
	} {
		t.Run(test.agentType, func(t *testing.T) {
			t.Parallel()
			if got := describedRole("", test.agentType); got != test.want {
				t.Fatalf("describedRole(%q, %q) = %+v; want %+v", "", test.agentType, got, test.want)
			}
		})
	}
}

// Requirement C3: a description that names a role wins over the agent type.
// Letting the reviewer type override the description fails here.
func TestC3_DescriptionWinsOverAgentType(t *testing.T) {
	t.Parallel()
	want := roleChoice{role: "review_followup", basis: "description"}
	if got := describedRole("Follow-up review of the fix", "agent-workflows:reviewer"); got != want {
		t.Fatalf("describedRole(%q, %q) = %+v; want %+v", "Follow-up review of the fix", "agent-workflows:reviewer", got, want)
	}
}
