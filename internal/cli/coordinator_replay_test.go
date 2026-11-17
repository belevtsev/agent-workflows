package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

// The only subprocess worker is a declared local check. No model/agent CLI,
// provider, network connection or provider-token estimate is involved.
func TestCoordinatorReplayCheckChild(t *testing.T) {
	if os.Getenv("KITSU_TEST_COORDINATOR_REPLAY") != "1" {
		return
	}
	want, err := os.ReadFile("tests/regression.txt")
	if err != nil {
		os.Exit(8)
	}
	body, err := os.ReadFile("src/label.txt")
	if err != nil {
		os.Exit(9)
	}
	quiet := os.Getenv("KITSU_TEST_COORDINATOR_QUIET") == "1"
	if !quiet {
		fmt.Fprint(os.Stderr, strings.Repeat(workflowNoise, 120))
	}
	if !strings.HasPrefix(string(body), strings.TrimSpace(string(want))) {
		fmt.Fprintln(os.Stderr, replayDiagnostic)
		if !quiet {
			fmt.Fprint(os.Stderr, strings.Repeat(workflowNoise, 120))
		}
		os.Exit(7)
	}
	fmt.Fprintln(os.Stderr, "PASS: anonymous fallback")
	os.Exit(0)
}

type coordinatorReplay struct {
	replayMeter
	managed                                      bool
	lean, cadence                                bool
	executed, reused, rejected, returns, retries int
	facts                                        map[string]string
	currentChecks                                []stageCheck
	initialHead                                  string
	workerReturned                               map[string]bool
}

func coordinatorReplayFixture(t *testing.T, managed bool) *coordinatorReplay {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Setenv("CODEX_HOME", filepath.Join(r.Home, ".codex"))
	r.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(r.Home, ".claude"))
	r.Setenv("XDG_CONFIG_HOME", filepath.Join(r.Home, "config"))
	r.Setenv("KITSU_TEST_COORDINATOR_REPLAY", "1")
	r.Setenv("GORACE", "atexit_sleep_ms=0")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r.Write("kitsu.toml", fmt.Sprintf("version=1\n[[check]]\nid='regression'\nrun=[%q,'-test.run=^TestCoordinatorReplayCheckChild$']\npaths=['src/**','tests/**']\ntimeout='10s'\n", filepath.ToSlash(program)))
	var settings strings.Builder
	settings.WriteString("version=1\n[paths]\ntests=['tests/**']\n")
	for _, role := range []string{"quality-engineer", "developer", "reviewer", "git"} {
		fmt.Fprintf(&settings, "[codex.%s]\nmodel='fake-sol-profile'\neffort='high'\nskills=['local']\n", role)
	}
	r.Write("kitsu-roles.toml", settings.String())
	r.Write(workflowSkill, "---\nname: local\ndescription: offline regression fixture\n---\nPreserve anonymous fallback and λ. Read the complete original role.\n")
	r.Write("src/label.txt", "broken\n")
	r.Git("add", "-A")
	r.Git("commit", "-qm", "offline coordinator fixture")
	return &coordinatorReplay{replayMeter: replayMeter{workflowMeter: workflowMeter{t: t, repo: r, retainTransports: true}}, managed: managed, facts: map[string]string{"provider_usage": "unknown", "model_quality": "unknown", "profile": "fake-sol-profile/high"}, initialHead: r.Git("rev-parse", "HEAD"), workerReturned: map[string]bool{}}
}

// A fake host's complete request/response envelopes are charged independently
// from the canonical-cli requests. The fake accepts an actual native dispatch,
// reads the saved complete packet, then reads the skill at its original path.
func (h *coordinatorReplay) prepare(role, stage string, round int, assignment string) (map[string]any, workerBrief) {
	h.t.Helper()
	d := h.preparePacket(role, stage, round, assignment)
	h.dispatch(d)
	return d, h.readWorkerPacket(d, role, stage, round, assignment)
}

// Preparing a packet is separate from actual native host dispatch. The v1
// reference already resolves role/skill inputs inside prepare; it needs no
// extra role text/show/skill resolve calls to establish the same contract.
func (h *coordinatorReplay) preparePacket(role, stage string, round int, assignment string) map[string]any {
	h.t.Helper()
	args := []string{"task", "prepare", workflowTask, "--role", role, "--stage", stage, "--round", strconv.Itoa(round), "--host", "codex", "--workflow", "small", "--assignment", "-"}
	if h.managed {
		args = append(args, "--dispatch")
		if h.lean {
			args = append(args, "--view", "launch")
		}
	}
	return workflowJSON[map[string]any](h.t, h.run(assignment, "kitsu", 0, args...).Stdout)
}

func (h *coordinatorReplay) readWorkerPacket(d map[string]any, role, stage string, round int, assignment string) workerBrief {
	h.t.Helper()
	body := h.read(d["path"].(string))
	if workerBriefHash([]byte(body)) != d["sha256"] || !strings.HasSuffix(body, "Original assignment:\n"+assignment) {
		h.t.Fatal("packet hash or original assignment changed")
	}
	_, metadata, ok := strings.Cut(body, "Worker brief metadata:\n")
	var b workerBrief
	if !ok {
		h.t.Fatal("complete metadata absent")
	}
	if err := json.NewDecoder(strings.NewReader(metadata)).Decode(&b); err != nil {
		h.t.Fatal(err)
	}
	if b.Role != role || b.Stage != stage || b.Round != round || b.LoadStatus != "unverified" || b.State.Stage != "unknown" || b.Model.Model != "fake-sol-profile" || b.Model.Effort != "high" || b.Baseline == "" || b.Selection == "" || len(b.Skills) != 1 || len(b.Constraints) < 5 {
		h.t.Fatal("worker authority/unknown state changed", b)
	}
	if !h.lean && (b.Baseline != d["baseline"] || b.Selection != d["selection_id"]) {
		h.t.Fatal("full dispatch identity differs from packet", b)
	}
	if h.lean {
		// Role/round are recovered from the complete hash-verified worker packet,
		// without another host response or a full dispatch expansion.
		d["stage"], d["round"] = b.Stage, float64(b.Round)
	}
	_, instructions, _ := strings.Cut(body, "Original role instructions:\n")
	instructions, _, _ = strings.Cut(instructions, "\n\nOriginal assignment:\n")
	if original, err := roles.Text(role); err != nil || instructions != original {
		h.t.Fatal("original role was not preserved", err)
	}
	skill := h.read(b.Skills[0].Path)
	if fmt.Sprintf("%x", sha256.Sum256([]byte(skill))) != b.Skills[0].SHA256 {
		h.t.Fatal("skill hash mismatch")
	}
	key := stage + "/" + strconv.Itoa(round)
	h.facts[key] = stringMustJSON(map[string]any{"role": b.Role, "model": b.Model, "writes": b.Writes, "tests": b.TestPaths, "assignment": assignment, "instructions": instructions, "skill_body": skill, "load_status": b.LoadStatus, "state": b.State.Stage})
	return b
}

func (h *coordinatorReplay) dispatch(d map[string]any) {
	h.t.Helper()
	native := d["native"]
	if !h.managed {
		// The legacy fake host assembles native arguments locally from v1.
		// Share the formatter to hold the entire worker-facing contract (including
		// return schema) constant. Managed obtains those arguments from the CLI.
		legacy := workflowJSON[stageDispatch](h.t, stringMustJSON(d))
		native = workflowJSON[map[string]any](h.t, stringMustJSON(nativeWorkerDispatch(legacy, contextpack.State{}, h.repo.Dir).Native))
	}
	if native == nil {
		h.t.Fatal("missing native dispatch")
	}
	settings := native.(map[string]any)
	if settings["model"] != "fake-sol-profile" || settings["effort"] != "high" || settings["fresh_context"] != true || settings["fork_turns"] != "none" {
		h.t.Fatal("native fake-host profile or fresh context changed", settings)
	}
	if h.managed && workerBriefHash([]byte(settings["prompt"].(string))) != d["native_prompt_sha256"] {
		h.t.Fatal("native prompt hash changed")
	}
	h.recordTransport("fake-native-dispatch", stringMustJSON(map[string]any{"tool": "spawn_worker", "native": native}), `{"worker_id":"offline-script","provider_usage":"unknown","profile":"fake-sol-profile/high"}`, "")
}

func (h *coordinatorReplay) write(path, body string) {
	h.repo.Write(path, body)
	h.recordTransport("fake-native-write", stringMustJSON(map[string]string{"tool": "write_file", "path": path, "body": body}), `{"written":true}`, "")
}

func (h *coordinatorReplay) check(expect int, force, expand bool) {
	h.t.Helper()
	before := len(h.repo.RecordFiles("regression"))
	args := []string{"check", "run"}
	if force {
		args = append(args, "--force")
	}
	if h.managed {
		args = append(args, "--compact", "--json", "--max-bytes", "4096")
	}
	result := h.run("", "kitsu", expect, args...)
	after := len(h.repo.RecordFiles("regression"))
	if after > before {
		h.executed += after - before
	} else {
		h.reused++
	}
	if h.managed {
		page := h.decodePage(result.Stdout)
		meta := workflowJSON[compactCheckMeta](h.t, string(page.Metadata))
		if len(meta.Checks) != 1 || meta.Checks[0].Record == nil || meta.Verified != (expect == 0) || meta.Checks[0].Skipped != (after == before) {
			h.t.Fatal("check execution/reuse or actual verdict lost", meta)
		}
		state := "failed"
		if meta.Verified {
			state = "satisfied"
		}
		h.currentChecks = []stageCheck{{ID: meta.Checks[0].ID, State: state, RunID: meta.Checks[0].Record.RunID}}
		if expand {
			// Charge the rejected request, its error, and the subsequent retry.
			h.run("", "kitsu", 2, "context", "show", page.ID, "--item", "regression", "--cursor", "invalid", "--json")
			h.rejected++
			h.retries++
			if log := replayPages(&h.replayMeter, page, false); !strings.Contains(log, replayDiagnostic) {
				h.t.Fatal("buried red diagnostic lost")
			}
		}
	} else {
		status := workflowJSON[statusReport](h.t, h.run("", "kitsu", expect, "gate", "status", "--json").Stdout)
		if status.Verified != (expect == 0) {
			h.t.Fatal("baseline check verdict lost")
		}
		h.currentChecks = []stageCheck{{ID: status.Checks[0].ID, State: status.Checks[0].State, RunID: status.Checks[0].RunID}}
		if expand && !strings.Contains(result.Output(), replayDiagnostic) {
			h.t.Fatal("baseline red diagnostic absent")
		}
	}
}

func (h *coordinatorReplay) complete(d map[string]any, changed []string, findings []map[string]any, expected string) stageReceipt {
	h.t.Helper()
	if !h.cadence {
		h.recordTransport("fake-native-followup", `{"worker_id":"offline-script","message":"Return observed paths, checks, findings and blockers; do not claim approval."}`, `{"accepted":true}`, "")
	}
	request := map[string]any{"version": 1, "dispatch_id": d["dispatch_id"], "task": workflowTask, "packet_sha256": d["sha256"], "changed_paths": changed, "checks": h.currentChecks, "findings": findings, "blockers": []string{}}
	returned := stringMustJSON(request)
	h.recordTransport("fake-native-worker-return", `{"worker_id":"offline-script","operation":"wait"}`, returned, "")
	h.returns++
	h.workerReturned[d["dispatch_id"].(string)] = true
	if h.managed {
		result := h.run(returned, "kitsu", 0, "task", "return", d["dispatch_id"].(string), "--request", "-")
		receipt := workflowJSON[workerReturnReceipt](h.t, result.Stdout)
		claims := workflowJSON[workerReturnRequest](h.t, returned)
		if receipt.Authority != "untrusted-worker-claims" || receipt.Observed.Status != "observed" || receipt.Scope != workerReturnScope || contextpack.Key(receipt.Claims) != contextpack.Key(claims) {
			h.t.Fatal("worker claims or findings severity changed, or lost untrusted authority", receipt)
		}
	}
	h.facts["findings:"+d["stage"].(string)+"/"+strconv.Itoa(int(d["round"].(float64)))] = stringMustJSON(findings)
	r := workflowJSON[stageReceipt](h.t, h.run("", "kitsu", 0, "task", "finish", d["dispatch_id"].(string), "--expect-checks", expected).Stdout)
	if r.Status != "finished" || !r.Writes || r.Verified != (expected == "pass") || r.Scope != stageReceiptScope {
		h.t.Fatal("finish receipt authority changed", r)
	}
	return r
}

func (h *coordinatorReplay) throughFirstReview(needsRepair bool) []stageReceipt {
	h.t.Helper()
	d, _ := h.prepare("quality-engineer", "tests", 1, "Write the regression for anonymous fallback. Preserve λ.\n")
	h.write("tests/regression.txt", "anonymous\n")
	h.check(5, true, true)
	qe := h.complete(d, []string{"tests/regression.txt"}, []map[string]any{}, "fail")
	d, _ = h.prepare("developer", "code", 1, "Implement the anonymous fallback; preserve λ.\n")
	source := "anonymous λ\n"
	if needsRepair {
		source = "anonymous\n"
	}
	h.write("src/label.txt", source)
	h.check(0, true, false)
	h.check(0, false, false)
	dev := h.complete(d, []string{"src/label.txt"}, []map[string]any{}, "pass")
	d, _ = h.prepare("reviewer", "review", 1, "Review source and regression independently for fallback and λ.\n")
	if strings.Contains(h.read(h.repo.Path("src/label.txt")), "λ") == needsRepair {
		h.t.Fatal("review fixture disagrees with actual source")
	}
	h.read(h.repo.Path("tests/regression.txt"))
	findings := []map[string]any{}
	if needsRepair {
		findings = append(findings, replayReviewFinding())
	}
	review := h.complete(d, []string{}, findings, "pass")
	return []stageReceipt{qe, dev, review}
}

func replayReviewFinding() map[string]any {
	return map[string]any{"path": "src/label.txt", "line": 1, "severity": "major", "message": "Required λ marker is missing."}
}

func (h *coordinatorReplay) lifecycle() {
	h.throughFirstReview(true)
	finding := replayReviewFinding()
	d, _ := h.prepare("developer", "code", 2, "Repair this review finding: "+stringMustJSON(finding)+"\n")
	h.write("src/label.txt", "anonymous λ\n")
	h.check(0, true, false)
	h.check(0, false, false)
	h.complete(d, []string{"src/label.txt"}, []map[string]any{}, "pass")
	d, _ = h.prepare("reviewer", "review", 2, "Review current source and regression independently for fallback and λ.\n")
	if !strings.Contains(h.read(h.repo.Path("src/label.txt")), "λ") {
		h.t.Fatal("repair lost")
	}
	h.read(h.repo.Path("tests/regression.txt"))
	h.complete(d, []string{}, []map[string]any{}, "pass")
	d, _ = h.prepare("git", "commit", 1, "Commit only after the actual current gate passes; report commit and clean status.\n")
	h.run("", "kitsu", 0, "gate", "init")
	h.run("", "git", 0, "add", "-A")
	h.run("", "kitsu", 0, "gate", "status", "--json")
	h.run("", "git", 0, "commit", "-qm", "Offline scripted repair\n\nCo-Authored-By: fake-sol-profile <fake@example.invalid>")
	h.complete(d, []string{}, []map[string]any{}, "pass")
	if audit := h.auditCompletion(d); !audit.complete() {
		h.t.Fatal("actual Git worker completion, current gate, hook, new task commit and clean tree required", audit)
	}
	h.facts["done"] = "current_gate_passed;hook_installed;commit_created;clean"
}

type replayCompletion struct {
	WorkerReturned, GitFinished, IntervalActive              bool
	CurrentGateVerified, HookInstalled, CommitCreated, Clean bool
}

func (a replayCompletion) complete() bool {
	return a.WorkerReturned && a.GitFinished && !a.IntervalActive && a.CurrentGateVerified && a.HookInstalled && a.CommitCreated && a.Clean
}

func (h *coordinatorReplay) exists(path string) bool {
	h.t.Helper()
	_, err := os.Stat(h.repo.Path(path))
	if err != nil && !os.IsNotExist(err) {
		h.t.Fatal(err)
	}
	found := err == nil
	h.recordTransport("fake-native-stat", stringMustJSON(map[string]string{"tool": "stat", "path": path}), stringMustJSON(map[string]bool{"exists": found}), "")
	return found
}

// The same evidence oracle checks both completed and interrupted endpoints.
// Passing role finishes or a prepared Git packet alone cannot satisfy it.
func (h *coordinatorReplay) auditCompletion(git map[string]any) replayCompletion {
	h.t.Helper()
	id := git["dispatch_id"].(string)
	a := replayCompletion{WorkerReturned: h.workerReturned[id], IntervalActive: h.exists(".git/kitsu-stages/active.json")}
	name := ".git/kitsu-stages/" + id + ".finish.json"
	if h.exists(name) {
		receipt := workflowJSON[stageReceipt](h.t, h.read(h.repo.Path(name)))
		a.GitFinished = receipt.DispatchID == id && receipt.Status == "finished" && receipt.Writes && receipt.Verified
	}
	status := workflowJSON[statusReport](h.t, h.run("", "kitsu", 0, "gate", "status", "--json").Stdout)
	a.CurrentGateVerified, a.HookInstalled = status.Verified, status.Hooks == "installed"
	a.Clean = h.run("", "git", 0, "status", "--porcelain").Stdout == ""
	head := strings.TrimSpace(h.run("", "git", 0, "rev-parse", "HEAD").Stdout)
	a.CommitCreated = len(head) == 40 && head != h.initialHead
	return a
}

func TestCoordinatorReplayInterruptedBeforeGit(t *testing.T) {
	for _, managed := range []bool{false, true} {
		arm := "v1_reference"
		if managed {
			arm = "managed_v2"
		}
		t.Run(arm, func(t *testing.T) {
			t.Parallel()
			h := coordinatorReplayFixture(t, managed)
			receipts := h.throughFirstReview(false)
			if len(receipts) != 3 || receipts[0].Verified || !receipts[1].Verified || !receipts[2].Verified {
				t.Fatal("endpoint requires actual QE red, developer green and reviewer finishes", receipts)
			}
			// Reproduce the historic interrupted endpoint: only prepare Git's
			// packet, with no host dispatch, worker return, finish or task commit.
			d := h.preparePacket("git", "commit", 1, "Commit after the actual current gate passes; report commit and clean status.\n")
			audit := h.auditCompletion(d)
			if audit.complete() || audit.WorkerReturned || audit.GitFinished || !audit.IntervalActive || audit.CommitCreated || audit.Clean {
				t.Fatal("prepared Git packet or three role finishes became task completion", audit)
			}
			counts := map[string]int{}
			for _, entry := range h.transports {
				counts[entry.Boundary]++
			}
			if counts["fake-native-dispatch"] != 3 || h.returns != 3 || h.facts["commit/1"] != "" || h.facts["done"] != "" {
				t.Fatal("Git worker was unexpectedly dispatched/completed")
			}
			verifyReplayLedger(t, &h.workflowMeter)
			t.Logf("arm=%s workflow=incomplete current_gate_verified=%v Git_dispatch=false Git_return=false Git_finish=false task_commit=false clean=false calls=%d", arm, audit.CurrentGateVerified, h.calls)
		})
	}
}

func TestCoordinatorReplayLifecycle(t *testing.T) {
	baseline, managed := coordinatorReplayFixture(t, false), coordinatorReplayFixture(t, true)
	baseline.lifecycle()
	managed.lifecycle()
	if !maps.Equal(baseline.facts, managed.facts) {
		t.Fatalf("required facts differ:\nbaseline=%v\nmanaged=%v", baseline.facts, managed.facts)
	}
	for _, h := range []*coordinatorReplay{baseline, managed} {
		verifyReplayLedger(t, &h.workflowMeter)
		if h.executed != 3 || h.reused != 2 || h.returns != 6 || h.facts["provider_usage"] != "unknown" || h.facts["model_quality"] != "unknown" {
			t.Fatal("execution or unknown-evidence accounting changed", h.executed, h.reused, h.returns)
		}
		counts := map[string]int{}
		for _, entry := range h.transports {
			counts[entry.Boundary]++
		}
		for boundary, want := range map[string]int{"fake-native-read": 17, "fake-native-write": 3, "fake-native-dispatch": 6, "fake-native-followup": 6, "fake-native-worker-return": 6, "fake-native-stat": 2} {
			if counts[boundary] != want {
				t.Fatalf("managed=%v boundary=%s count=%d want=%d", h.managed, boundary, counts[boundary], want)
			}
		}
		t.Logf("arm=%s calls=%d request_bytes=%d response_bytes=%d executed=%d reused=%d returns=%d expansions=%d rejected=%d retries=%d fake_profile=fake-sol-profile/high provider_usage=unknown model_quality=unknown", h.arm(), h.calls, h.requestBytes, h.responseBytes, h.executed, h.reused, h.returns, h.expansions, h.rejected, h.retries)
		h.saveLedger()
	}
	if managed.expansions == 0 || managed.rejected != 1 || managed.retries != 1 {
		t.Fatal("managed replay did not charge expansion/rejected request/retry")
	}
}

func (h *coordinatorReplay) arm() string {
	if h.managed {
		return "managed_v2"
	}
	return "v1_reference"
}

func (h *coordinatorReplay) saveLedger() {
	h.t.Helper()
	dir := os.Getenv("KITSU_TEST_COORDINATOR_REPLAY_RECEIPT_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		h.t.Fatal(err)
	}
	arm := h.arm()
	data, err := json.MarshalIndent(map[string]any{"version": 1, "arm": arm, "reference_contract": "v1 task prepare already resolves roles and skills; no redundant introspection. v1 host locally formats the same native worker contract; v2 obtains it via --dispatch and ingests task return.", "scope": "offline scripted fake-host calls and bytes; not coordinator LLM turns, real model quality, provider tokens, or paid calibration; no savings claim", "serialization": map[string]string{"canonical-cli": "program + space + words separated by space + newline + exact stdin", "fake-native": "exact serialized JSON request/response envelopes"}, "profile": "fake-sol-profile/high", "calls": h.calls, "request_bytes": h.requestBytes, "response_bytes": h.responseBytes, "checks_executed": h.executed, "checks_reused": h.reused, "expansions": h.expansions, "rejected_calls": h.rejected, "retries": h.retries, "worker_returns": h.returns, "facts": h.facts, "transports": h.transports}, "", "  ")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, arm+".json"), append(data, '\n'), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func TestCoordinatorReplayFailures(t *testing.T) {
	for _, name := range []string{"cancel", "unavailable", "stale-source", "unknown"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := coordinatorReplayFixture(t, true)
			d, b := h.prepare("developer", "code", 1, "Inspect current source. Report blockers and uncertainty explicitly.\n")
			id := d["dispatch_id"].(string)
			switch name {
			case "cancel", "unavailable":
				h.recordTransport("fake-native-worker-return", `{"worker_id":"offline-script","operation":"wait"}`, stringMustJSON(map[string]string{"state": name, "completion": "unknown"}), "")
				abandoned := workflowJSON[stageReceipt](t, h.run("", "kitsu", 0, "task", "finish", id, "--abandon", "--reason", "Fake worker "+name+"; no successful return.").Stdout)
				if abandoned.Status != "abandoned" || abandoned.Writes || abandoned.Verified {
					t.Fatal("cancelled/unavailable worker gained approval", abandoned)
				}
			case "stale-source":
				before := h.snapshot()
				h.write("src/label.txt", "source changed after worker observation\n")
				request := map[string]any{"version": 1, "dispatch_id": id, "task": workflowTask, "packet_sha256": d["sha256"], "source": before, "changed_paths": []string{"src/label.txt"}, "checks": []any{}, "findings": []any{}, "blockers": []string{}}
				h.recordTransport("fake-native-worker-return", `{"worker_id":"offline-script","operation":"wait"}`, stringMustJSON(request), "")
				result := h.run(stringMustJSON(request), "kitsu", 1, "task", "return", id, "--request", "-")
				if !strings.Contains(result.Stderr, "stale") || !h.repo.Exists(".git/kitsu-stages/active.json") {
					t.Fatal("stale source accepted or interval released", result.Output())
				}
				h.rejected++
				h.run("", "kitsu", 0, "task", "finish", id, "--abandon", "--reason", "Fresh source observation required.")
			case "unknown":
				request := map[string]any{"version": 1, "dispatch_id": id, "task": workflowTask, "packet_sha256": d["sha256"], "changed_paths": []string{}, "checks": []any{}, "findings": []any{}, "blockers": []string{}, "loaded": map[string]any{"brief_sha256": d["sha256"], "skills": []map[string]string{{"id": b.Skills[0].ID, "sha256": b.Skills[0].SHA256}}}, "worker": map[string]any{"host": "codex", "id": "offline-script", "completed": true}}
				h.recordTransport("fake-native-worker-return", `{"worker_id":"offline-script","operation":"wait"}`, stringMustJSON(request), "")
				receipt := workflowJSON[workerReturnReceipt](t, h.run(stringMustJSON(request), "kitsu", 0, "task", "return", id, "--request", "-").Stdout)
				if receipt.Authority != "untrusted-worker-claims" || receipt.Observed.Verified || receipt.Observed.Status != "observed" || !h.repo.Exists(".git/kitsu-stages/active.json") {
					t.Fatal("host completion/load claim became approval", receipt)
				}
				h.run("", "kitsu", 1, "task", "finish", id, "--expect-checks", "pass")
				h.rejected++
				h.run("", "kitsu", 0, "task", "finish", id, "--abandon", "--reason", "Current checks are unknown.")
			}
			verifyReplayLedger(t, &h.workflowMeter)
			if h.facts["done"] != "" || h.facts["provider_usage"] != "unknown" || h.facts["model_quality"] != "unknown" {
				t.Fatal("failed or uncertain replay claimed completion or measured quality")
			}
		})
	}
}

// Source identity is captured by real Kitsu commands rather than fabricated
// to make a fixture green. This helper is also useful for failure replays.
func (h *coordinatorReplay) snapshot() contextpack.State {
	p := replayCollect(&h.replayMeter, contextpack.Operation{ID: "state", Kind: "git"}, "16384")
	return p.State
}
