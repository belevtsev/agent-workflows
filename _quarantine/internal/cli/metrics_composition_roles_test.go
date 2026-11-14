package cli

// Contract roster-tests, revision 1, requirements C4 and C5: end-to-end role
// attribution through `cw metrics import --claude-transcript ... --composition`
// when an implementation assignment from `cw task assign` meets a subagent
// whose agent type does or does not identify a test author.
//
// Fixture (one per case), following TestMetricsCompositionRolesBriefIDPairing:
// Main (opus): user prompt 400 B; k1 T=10000 (w1h 10000) out 20 issuing two
// calls, a cw task assign Bash call (brief id impl-1, role implementation)
// and an Agent call whose prompt names impl-1; 2 results x 50 B = 100 B.
// k2 T=10045 (read 10000, w1h 45) out 10: d=45=20+25, R=25, sample 100/25=4.
// Main $ = 10000*8e-6 + 10000*0.2e-6 + 45*8e-6 + 30*20e-6
//        = 0.08 + 0.002 + 0.00036 + 0.0006 = 0.08296.
// One subagent (sonnet): brief 400 B, one request T=1000 (w1h) out 10:
// 1000*4e-6 + 10*10e-6 = 0.0041. Session 0.08706.

import (
	"path/filepath"
	"testing"
)

// rosterRoleComposition imports a session in which the subagent agent-w1 is
// paired with an implementation brief and carries the given description and
// agent type (both on the Agent call and in its subagent meta).
func rosterRoleComposition(t *testing.T, brief, description, agentType string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	type call struct {
		id, name string
		input    map[string]any
		agentID  string
	}
	calls := []call{
		{"a1", "Bash", map[string]any{"command": compAssignCmd("impl-1", "implementation", brief)}, ""},
		{"a2", "Agent", map[string]any{"description": description, "subagent_type": agentType, "prompt": "Handle assignment impl-1."}, "w1"},
	}
	lines := []string{compUserPrompt(t, compFill(400, 'a'))}
	for _, c := range calls {
		lines = append(lines, compAssistant(t, "k1", opusModel, compUsage{w1h: 10000, out: 20}, compToolUse(c.id, c.name, c.input)))
	}
	for _, c := range calls {
		var res map[string]any
		if c.agentID != "" {
			res = map[string]any{"agentId": c.agentID, "status": "completed"}
		}
		lines = append(lines, compToolResult(t, c.id, compFill(50, 'r'), res))
	}
	lines = append(lines,
		compAssistant(t, "k2", opusModel, compUsage{read: 10000, w1h: 45, out: 10}, compTextItem("done")),
		compCostState(t, 0.08706, false, map[string]compModelUsage{
			opusModel:   {read: 10000, w1h: 10045, out: 30, cost: 0.08296},
			sonnetModel: {w1h: 1000, out: 10, cost: 0.0041},
		}),
	)
	writeLines(t, mainPath, lines...)
	writeLines(t, compSubagentPath(dir, "w1"),
		compUserPrompt(t, compFill(400, 'p')),
		compAssistant(t, "w1-q1", sonnetModel, compUsage{w1h: 1000, out: 10}, compTextItem("ok")),
	)
	writeLines(t, compMetaPath(dir, "w1"), mustJSON(t, map[string]any{"agentType": agentType, "description": description}))
	prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	return comp
}

// C4: a test-engineer agent type on an implementation brief that never says
// "test author" is reported as test_author, still based on cw_task_assign.
// Reporting it as implementation (type ignored) or with a description or
// agent_type basis (assignment ignored) fails here.
func TestC4_TestEngineerTypeOnImplementationBriefIsTestAuthor(t *testing.T) {
	for _, agentType := range []string{"agent-workflows:test-engineer", "test-engineer"} {
		t.Run(agentType, func(t *testing.T) {
			comp := rosterRoleComposition(t, "apply the parser change", "helper t", agentType)
			agent := compAgent(t, comp, "agent-w1")
			compWantStr(t, agent, "role", "test_author", "agent-w1")
			compWantStr(t, agent, "role_basis", "cw_task_assign", "agent-w1")
		})
	}
}

// C5: mentioning the test engineer in an implementation brief does not make a
// general-purpose or implementer worker a test author. Reporting test_author,
// or losing the cw_task_assign basis, fails here.
func TestC5_TestEngineerMentionInBriefStaysImplementation(t *testing.T) {
	for _, agentType := range []string{"general-purpose", "agent-workflows:implementer"} {
		t.Run(agentType, func(t *testing.T) {
			comp := rosterRoleComposition(t, "apply the parser change; the test engineer owns the tests", "helper i", agentType)
			agent := compAgent(t, comp, "agent-w1")
			compWantStr(t, agent, "role", "implementation", "agent-w1")
			compWantStr(t, agent, "role_basis", "cw_task_assign", "agent-w1")
		})
	}
}
