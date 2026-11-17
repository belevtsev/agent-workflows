package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
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
	prompt := fmt.Sprintf("%s\n\nRead the complete original worker packet at %q before doing any work. Verify its SHA-256 is %s. Do not use a preview or summary in place of the full packet. Follow its original assignment, full role instructions, write limits and references. Load selected skills from their original paths and verify their recorded hashes; skill loading and host execution remain unverified by kitsu.\n\nWork in %q in a fresh worker context without inherited coordinator turns. The host must apply the requested model and effort and its existing permissions. Return one bounded JSON object (at most 65536 bytes), using this exact required shape and identities:\n%s\nchanged_paths lists repository-relative paths; checks lists claimed {id,state,run_id?}; findings lists {severity,path,line,message}, where severity is blocker, major or minor, with original source references; blockers lists strings. Include every finding and blocker. Optional loaded:{brief_sha256,skills:[{id,sha256}]} and worker:{host,id,completed} describe only what you actually loaded or observed; these are unverified claims. Optional worktree and source assert the current worktree and full context state; no extra command is required to obtain them. Do not add approval fields. Return the JSON to the coordinator after stopping work; only the coordinator may ingest it with task return and finalize the interval with task finish. This handback grants no approval or completion proof.\n", d.Label, d.Path, d.SHA256, worktree, schema)
	native := nativeWorkerLaunch{Host: d.Host, Model: d.Model.Model, Effort: d.Model.Effort, FreshContext: true, ForkTurns: "none", Prompt: prompt}
	if d.Host == "claude" {
		native.Role = "kitsu:" + d.Role
	}
	return nativeDispatch{stageDispatch: d, Worktree: worktree, State: state, Native: native, PromptSHA256: workerBriefHash([]byte(prompt))}
}

// Preflight occurs before skill-selection records change the worker's baseline.
func checkNativeDispatchBudget(s *session, b workerBrief, budget int) error {
	b, _, err := briefInputs(s, b)
	if err != nil {
		return compactFailure(err)
	}
	hash := strings.Repeat("0", 64)
	b.Label = fmt.Sprintf("kitsu-task: %s %s %d", b.Task, b.Stage, b.Round)
	b.Baseline, b.Selection = hash, hash
	d := stageDispatch{workerDispatch: workerBriefDispatch(b, filepath.Join(s.repo.GitDir, "kitsu-context", "briefs", hash+".txt"), hash), DispatchID: hash}
	_, err = boundedStageJSON(nativeWorkerDispatch(d, contextpack.State{Tree: hash, Head: hash, Raw: hash, SourcePolicy: hash}, s.repo.Root), budget)
	return err
}
