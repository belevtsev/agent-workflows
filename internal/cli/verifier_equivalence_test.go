package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Generated valid claims must preserve critical evidence exactly. Independently
// varied identity/evidence mutations must still fail the actual binding checks;
// syntactically valid worker assertions never stand in for local observations.
func FuzzWorkerReturnClaimPreservation(f *testing.F) {
	for _, text := range []string{"source evidence", "λ 🐕\nquoted \"fact\"", "", "\x00"} {
		for _, mask := range []uint8{0, 1, 2, 4, 8, 16, 32, 64, 128, 255} {
			f.Add(text, mask)
		}
	}
	f.Fuzz(func(t *testing.T, text string, mask uint8) {
		if len(text) > 1024 || !utf8.ValidString(text) {
			t.Skip("bounded UTF-8 field domain; invalid raw encoding is a separate mutation")
		}
		const task = "verify-role"
		packet, skill := strings.Repeat("b", 64), strings.Repeat("c", 64)
		record := stageRecord{Worktree: "/workspace", Prepared: preparedWorkerBrief{
			Brief:    workerBrief{Task: task, Host: "codex", Skills: []compactSkill{{ID: "local", SHA256: skill}}},
			Dispatch: workerDispatch{SHA256: packet},
		}}
		var request workerReturnRequest
		base := fmt.Sprintf(`{"version":1,"dispatch_id":%q,"task":%q,"packet_sha256":%q,"worktree":"/workspace","changed_paths":["src/fact.go"],"checks":[{"id":"unit","state":"satisfied","run_id":"actual-run"}],"findings":[],"blockers":[],"loaded":{"brief_sha256":%q,"skills":[{"id":"local","sha256":%q}]},"worker":{"host":"codex","id":"offline-worker","completed":true}}`, strings.Repeat("a", 64), task, packet, packet, skill)
		if err := json.Unmarshal([]byte(base), &request); err != nil {
			t.Fatal(err)
		}
		severity := []string{"blocker", "major", "minor"}[int(mask)%3]
		request.Findings = []workerFinding{{Severity: severity, Path: "src/fact.go", Line: int(mask) + 1, Message: text}}
		request.Blockers = []string{text}
		mutateVerifierClaims(&request, mask)
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if mask&32 != 0 {
			data = append(data[:len(data)-1], []byte(`,"approval":true}`)...)
		}
		if mask&128 != 0 {
			data = append(data[:len(data)-1], []byte(`,"VERSION":1}`)...)
		}
		if mask&64 != 0 {
			data = append(data, 0xff)
		}
		got, err := readWorkerReturn(strings.NewReader(string(data)), "-")
		valid := mask&(32|64|128) == 0 && strings.TrimSpace(text) != "" && !strings.ContainsRune(text, 0)
		if (err == nil) != valid {
			t.Fatalf("schema acceptance differs from mutation contract: mask=%d err=%v", mask, err)
		}
		if !valid {
			return
		}
		if !reflect.DeepEqual(got, request) {
			t.Fatal("critical paths, severity, source anchors, blockers or claims changed")
		}
		if err := matchWorkerReturn(record, got); (err == nil) != (mask&15 == 0) {
			t.Fatal("task/packet/selected-skill/host binding accepted a forged claim", mask, err)
		}
		observed := []stageCheck{{ID: "unit", State: "satisfied", RunID: "actual-run"}}
		if err := matchWorkerChecks(got.Checks, observed); (err == nil) != (mask&16 == 0) {
			t.Fatal("runner outcome replaced actual gate evidence", mask, err)
		}
	})
}

func mutateVerifierClaims(r *workerReturnRequest, mask uint8) {
	if mask&1 != 0 {
		r.Task = "other-task"
	}
	if mask&2 != 0 {
		r.PacketSHA256 = strings.Repeat("d", 64)
	}
	if mask&4 != 0 {
		r.Loaded.Skills[0].ID = "unselected-support-guide"
	}
	if mask&8 != 0 {
		r.Worker.Host = "claude"
	}
	if mask&16 != 0 {
		r.Checks[0].State = "passed"
	}
}

// Exercise the public CLI through real state transitions for both hosts. Each
// reconstructed projection is checked against a fresh full view AND independent
// ordinary gate/task/Git evidence; comparing two compact encodings alone is weak.
func TestOfflineCoordinatorDeltaEquivalenceMatrix(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
			r.Git("add", "-A")
			r.Git("commit", "-qm", "offline state equivalence fixture")
			stageJSON(t, r.Kitsu("skill", "resolve", "coordinator", "--host", host, "--task", "fix-login", "--workflow", "small", "--json"))
			view := coordinatorJSON(t, r, "fix-login", "--compact")
			sections := view["sections"].(map[string]any)
			baseline := view["snapshot_id"].(string)
			var dispatch map[string]any
			steps := []struct {
				name   string
				active bool
				apply  func()
			}{
				{"unchanged", false, func() {}},
				{"initial green", false, func() { stageJSON(t, r.Kitsu("check", "run", "--json")) }},
				{"prepare", true, func() {
					args := []string{"task", "prepare", "fix-login", "--role", "quality-engineer", "--stage", "tests", "--round", "1", "--host", host, "--workflow", "small", "--assignment", "-", "--dispatch"}
					dispatch = stageJSON(t, r.Run("Preserve this complete original assignment and λ.\n", filepath.Join(kitsutest.Binary(t), compactBinaryName()), args...))
				}},
				{"worker source change", true, func() { r.Write("tests/regression.txt", "original λ\n") }},
				{"untrusted return", true, func() {
					request := protocolRequest(dispatch)
					request["changed_paths"] = []string{"tests/regression.txt"}
					request["findings"] = []workerFinding{{Severity: "major", Path: "source.txt", Line: 1, Message: "Untrusted observation λ"}}
					request["worker"] = map[string]any{"host": host, "id": "offline-worker", "completed": true}
					stageJSON(t, protocolReturn(t, r, dispatch, request))
				}},
				{"worker green", true, func() { stageJSON(t, r.Kitsu("check", "run", "--json")) }},
				{"finish clears interval", false, func() { stageJSON(t, r.Kitsu("task", "finish", stageID(t, dispatch), "--expect-checks", "pass")) }},
				{"role policy changes", false, func() {
					r.Write("kitsu-roles.toml", fmt.Sprintf("version=1\n[paths]\ntests=['tests/**']\n[%s.quality-engineer]\nmodel='offline-verifier-profile'\neffort='high'\n", host))
				}},
			}
			for _, step := range steps {
				t.Logf("transition: %s", step.name)
				step.apply()
				delta := coordinatorJSON(t, r, "fix-login", "--since", baseline)
				if delta["kind"] != "delta" || delta["since"] != baseline {
					t.Fatal("delta lost its known baseline")
				}
				for key, value := range delta["sections"].(map[string]any) {
					sections[key] = value
				}
				full := coordinatorJSON(t, r, "fix-login", "--compact")
				if !reflect.DeepEqual(sections, full["sections"]) {
					t.Fatal("applying every delta does not reconstruct complete current facts")
				}
				assertVerifierProjection(t, r, full, step.active)
				if step.name == "untrusted return" || step.name == "worker green" {
					assertVerifierUntrustedReturn(t, full, step.name == "untrusted return")
				}
				if step.name == "unchanged" && len(delta["sections"].(map[string]any)) != 0 {
					t.Fatal("unchanged state invented a change")
				}
				if step.name == "role policy changes" {
					if _, exists := delta["sections"].(map[string]any)["policy"]; !exists {
						t.Fatal("new effective host model policy was omitted")
					}
				}
				baseline = delta["snapshot_id"].(string)
			}
		})
	}
}

func assertVerifierProjection(t *testing.T, r *kitsutest.Repo, full map[string]any, active bool) {
	t.Helper()
	gateResult := r.Kitsu("gate", "status", "--json")
	if gateResult.Code != 0 && gateResult.Code != ExitNotVerified {
		t.Fatal(gateResult.Output())
	}
	gate := workflowJSON[statusReport](t, gateResult.Stdout)
	checks := coordinatorSection(t, full, "checks")
	items, present := checks["items"].([]any)
	if !present || checks["verified"] != gate.Verified || len(items) != len(gate.Checks) {
		t.Fatal("compact projection disagrees with original gate verdict")
	}
	for i, raw := range items {
		check := raw.(map[string]any)
		state, ok := check["state"].(string)
		// The ordinary JSON API uses underscores; stage observations retain spaces.
		if !ok || check["id"] != gate.Checks[i].ID || strings.ReplaceAll(state, " ", "_") != gate.Checks[i].State {
			t.Fatal("check identity or current evidence state changed", check, gate.Checks[i])
		}
		run, _ := check["run_id"].(string)
		if run != gate.Checks[i].RunID {
			t.Fatal("deciding check record identity changed", check, gate.Checks[i])
		}
	}
	source := coordinatorSection(t, full, "source")["state"].(map[string]any)
	git := coordinatorSection(t, full, "git")
	if source["tree"] != gate.Tree || source["head"] != r.Git("rev-parse", "HEAD") || git["index_tree"] != r.Git("write-tree") || git["hooks"] != gate.Hooks || git["mode"] != gate.Mode {
		t.Fatal("source/Git projection disagrees with direct original observations")
	}
	if git["index_matches_worktree"] != (gate.IndexTree == gate.Tree) {
		t.Fatal("source edit/index mismatch was lost")
	}
	plain := workflowJSON[uxTaskView](t, r.Kitsu("task", "show", "fix-login", "--json").Stdout)
	if task := coordinatorSection(t, full, "task"); task["workflow"] != plain.Workflow || task["stage"] != plain.Stage {
		t.Fatal("compact task facts diverged from ordinary task records")
	}
	if coordinatorSection(t, full, "prerequisites")["completion"] != "not_certified" || coordinatorSection(t, full, "findings")["status"] != "unknown" {
		t.Fatal("untrusted worker claims certified small-task completeness or review")
	}
	interval := coordinatorSection(t, full, "interval")
	if active {
		if interval["status"] != "active" || interval["role"] != "quality-engineer" || interval["stage"] != "tests" {
			t.Fatal("active original role interval lost")
		}
	} else if interval["status"] != "none" || interval["role"] != nil {
		t.Fatal("cleared interval retained stale role facts")
	}
	if r.Exists("openspec/changes/fix-login/reviews.md") || r.Exists("openspec/changes/fix-login/tasks.md") {
		t.Fatal("observation or untrusted return invented canonical task records")
	}
	if len(full["sections"].(map[string]any)) != len(coordinatorSectionNames) || full["scope"] != coordinatorScope {
		t.Fatal("required sections or authority limit omitted")
	}
}

func assertVerifierUntrustedReturn(t *testing.T, view map[string]any, current bool) {
	t.Helper()
	interval := coordinatorSection(t, view, "interval")
	returned, exists := interval["return"].(map[string]any)
	if !exists || returned["authority"] != "untrusted-worker-claims" || returned["findings"] != float64(1) || returned["observations_current"] != current {
		t.Fatal("worker-claim summary, authority or freshness was lost", interval)
	}
	severity := returned["finding_severities"].(map[string]any)
	if severity["major"] != float64(1) || severity["blocker"] != float64(0) {
		t.Fatal("untrusted finding severity was changed")
	}
	if !strings.Contains(stringMustJSON(coordinatorSection(t, view, "prerequisites")), "inspect_untrusted_worker_claims") {
		t.Fatal("untrusted findings lost their inspection prerequisite")
	}
}
