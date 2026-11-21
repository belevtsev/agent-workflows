package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

// Native launch settings describe a host handoff. They never execute a model.
type nativeWorkerLaunch struct {
	Host         string `json:"host"`
	Model        string `json:"model"`
	Effort       string `json:"effort"`
	FreshContext bool   `json:"fresh_context"`
	ForkTurns    string `json:"fork_turns"`
	Role         string `json:"role,omitempty"`
	Prompt       string `json:"prompt"`
}

type nativeDispatch struct {
	stageDispatch
	Worktree     string             `json:"worktree"`
	State        contextpack.State  `json:"state"`
	Native       nativeWorkerLaunch `json:"native"`
	PromptSHA256 string             `json:"native_prompt_sha256"`
}

func nativeWorkerDispatch(d stageDispatch, state contextpack.State, worktree string) nativeDispatch {
	d.Version = 2
	request := workerReturnRequest{
		Version: 1, DispatchID: d.DispatchID, Task: d.Task, PacketSHA256: d.SHA256,
		ChangedPaths: []string{}, Checks: []stageCheck{}, Findings: []workerFinding{}, Blockers: []string{},
	}
	schema, _ := json.Marshal(request)
	prompt := fmt.Sprintf(`%s

Read the complete original worker packet at %q before work; verify SHA-256 %s. No preview or summary. Follow its full role, assignment, references, selected-skill loading and write limits. Work in %q using the requested model/effort and existing permissions in a fresh context.

Return at most 65536 bytes of JSON with this required shape and identities:
%s
changed_paths are repository-relative. checks are observed {id,state,run_id?}: copy the exact gate evidence state, such as satisfied or failed, not a runner outcome such as passed. An empty checks array makes no claim. findings are {severity,path,line,message}, severity blocker|major|minor; include every finding and blocker.
Omit optional claims unless needed. If reporting loaded.skills, use only selected packet skill ids/hashes; support-guide reads are not selected skills. Do not run extra discovery or checks to fill optional fields. Claims remain unverified and grant no approval or completion proof. Stop work and return to the coordinator; only it may ingest the return and finish the interval.
`, d.Label, d.Path, d.SHA256, worktree, schema)
	native := nativeWorkerLaunch{Host: d.Host, Model: d.Model.Model, Effort: d.Model.Effort, FreshContext: true, ForkTurns: "none", Prompt: prompt}
	if d.Host == "claude" && !d.Configured {
		native.Role = roles.TeamPlugin + ":" + d.Role
	}
	return nativeDispatch{stageDispatch: d, Worktree: worktree, State: state, Native: native, PromptSHA256: workerBriefHash([]byte(prompt))}
}

// Preflight occurs before skill-selection records change the worker's baseline.
func checkNativeDispatchBudget(s *session, b workerBrief, budget int, view string) error {
	b, _, err := briefInputs(s, b)
	if err != nil {
		return compactFailure(err)
	}
	hash := strings.Repeat("0", 64)
	b.Label = fmt.Sprintf("kitsu-task: %s %s %d", b.Task, b.Stage, b.Round)
	b.Baseline, b.Selection = hash, hash
	d := stageDispatch{workerDispatch: workerBriefDispatch(b, filepath.Join(s.repo.GitDir, "kitsu-context", "briefs", hash+".txt"), hash), DispatchID: hash}
	if b.Workflow == "small" {
		b.Warnings = append(b.Warnings, smallTaskBriefWarning)
	}
	full := nativeWorkerDispatch(d, contextpack.State{Tree: hash, Head: hash, Raw: hash, SourcePolicy: hash}, s.repo.Root)
	_, err = boundedStageJSON(nativeDispatchView(full, b.Warnings, view), budget)
	return err
}
