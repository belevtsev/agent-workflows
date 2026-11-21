package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func protocolPrepare(t *testing.T, r *kitsutest.Repo, host string) map[string]any {
	t.Helper()
	return stageJSON(t, r.Run("Preserve this original assignment exactly.\n", filepath.Join(kitsutest.Binary(t), compactBinaryName()), "task", "prepare", "fix-login", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", host, "--workflow", "small", "--assignment", "-", "--dispatch"))
}

func protocolRequest(d map[string]any) map[string]any {
	return map[string]any{"version": 1, "dispatch_id": d["dispatch_id"], "task": d["task"], "packet_sha256": d["sha256"], "changed_paths": []string{}, "checks": []stageCheck{}, "findings": []workerFinding{}, "blockers": []string{}}
}

func protocolReturn(t *testing.T, r *kitsutest.Repo, d, request map[string]any, extra ...string) kitsutest.Result {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"task", "return", stageID(t, d), "--request", "-"}
	return r.Run(string(data), filepath.Join(kitsutest.Binary(t), compactBinaryName()), append(args, extra...)...)
}

func TestNativeDispatchPreservesCompleteContractForBothHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			r.Write("kitsu-roles.toml", "version=1\n["+host+".reviewer]\nmodel='fake-sol-profile'\neffort='high'\n")
			d := protocolPrepare(t, r, host)
			native := d["native"].(map[string]any)
			prompt := native["prompt"].(string)
			body, err := roles.Text("reviewer")
			if err != nil {
				t.Fatal(err)
			}
			packet, err := os.ReadFile(d["path"].(string))
			if err != nil {
				t.Fatal(err)
			}
			if d["version"] != float64(2) || d["native_prompt_sha256"] != workerBriefHash([]byte(prompt)) || native["fresh_context"] != true || native["fork_turns"] != "none" || native["model"] != "fake-sol-profile" || native["effort"] != "high" {
				t.Fatal("incomplete native handoff", d)
			}
			if !strings.HasPrefix(prompt, d["label"].(string)+"\n") || !strings.Contains(prompt, strconv.Quote(d["path"].(string))) || !strings.Contains(prompt, d["sha256"].(string)) || !strings.Contains(prompt, "complete original") || !strings.Contains(prompt, "unverified") || !strings.HasSuffix(string(packet), "Original assignment:\nPreserve this original assignment exactly.\n") {
				t.Fatal("original instructions or prompt integrity lost")
			}
			if host == "claude" && native["role"] != "kitsu-team:reviewer" {
				t.Fatal("native Claude role missing", native)
			}
			if host == "codex" && strings.Count(string(packet), body) != 1 || host == "claude" && strings.Contains(string(packet), body) {
				t.Fatal("host role instructions duplicated or missing")
			}
			if host == "codex" && native["role"] != nil {
				t.Fatal("Codex handoff claims a Claude native role", native)
			}
			stageJSON(t, r.Kitsu("task", "finish", stageID(t, d)))
		})
	}
}

func TestWorkerReturnRetainsUntrustedClaimsWithoutFinishingOrSourceMutation(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	d := protocolPrepare(t, r, "codex")
	request := protocolRequest(d)
	request["findings"] = []workerFinding{
		{Severity: "blocker", Path: "README.md", Line: 1, Message: "Resolve this blocker using independent source evidence."},
		{Severity: "major", Path: "README.md", Line: 2, Message: "Resolve this major finding using independent source evidence."},
		{Severity: "minor", Path: "README.md", Line: 3, Message: "Independently inspect this source reference."},
	}
	request["blockers"] = []string{"Host loading is not verified."}
	request["loaded"] = map[string]any{"brief_sha256": d["sha256"], "skills": []workerLoadedSkill{}}
	request["worker"] = map[string]any{"host": "codex", "id": "fake-worker-17", "completed": true}
	before := r.Git("status", "--porcelain")
	receipt := stageJSON(t, protocolReturn(t, r, d, request))
	if receipt["authority"] != "untrusted-worker-claims" || !strings.Contains(receipt["scope"].(string), "no model execution") || !r.Exists(".git/kitsu-stages/active.json") || r.Exists(".git/kitsu-stages/"+stageID(t, d)+".finish.json") || before != r.Git("status", "--porcelain") {
		t.Fatal("return changed source, finished the interval or granted authority", receipt)
	}
	claims := receipt["claims"].(map[string]any)
	if len(claims["findings"].([]any)) != 3 || len(claims["blockers"].([]any)) != 1 || receipt["observed"].(map[string]any)["status"] != "observed" {
		t.Fatal("claim metadata omitted", receipt)
	}
	for i, severity := range []string{"blocker", "major", "minor"} {
		finding := claims["findings"].([]any)[i].(map[string]any)
		if finding["severity"] != severity || finding["disposition"] != nil || finding["approval"] != nil {
			t.Fatal("finding severity lost or given disposition authority", finding)
		}
	}
	again := stageJSON(t, protocolReturn(t, r, d, request))
	if receipt["return_id"] != again["return_id"] {
		t.Fatal("retry changed retained handback identity")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(r.Path(".git/kitsu-stages/" + stageID(t, d) + ".return.json"))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("worker return is not private", info, err)
		}
	}
	stageJSON(t, r.Kitsu("task", "finish", stageID(t, d)))
	if result := protocolReturn(t, r, d, request); result.Code == 0 {
		t.Fatal("return accepted after interval closed")
	}
}

func TestWorkerReturnRejectsInvalidSchemaBeforeMutation(t *testing.T) {
	t.Parallel()
	base, _ := json.Marshal(protocolRequest(map[string]any{"dispatch_id": strings.Repeat("a", 64), "task": "fix-login", "sha256": strings.Repeat("b", 64)}))
	for _, tc := range []struct{ name, input string }{
		{"unknown", strings.Replace(string(base), `"version":1`, `"version":1,"approval":true`, 1)},
		{"duplicate", strings.Replace(string(base), `"version":1`, `"version":1,"version":1`, 1)},
		{"case duplicate", strings.Replace(string(base), `"version":1`, `"version":1,"VERSION":1`, 1)},
		{"missing findings", strings.Replace(string(base), `"findings":[],`, "", 1)},
		{"null blockers", strings.Replace(string(base), `"blockers":[]`, `"blockers":null`, 1)},
		{"trailing", string(base) + " {}"},
		{"UTF8", string(base) + "\xff"},
		{"oversized", strings.Repeat("x", maxWorkerReturn+1)},
		{"NUL", strings.Replace(string(base), `"blockers":[]`, `"blockers":["bad\u0000"]`, 1)},
		{"path traversal", strings.Replace(string(base), `"changed_paths":[]`, `"changed_paths":["../escape"]`, 1)},
		{"missing severity", strings.Replace(string(base), `"findings":[]`, `"findings":[{"path":"README.md","line":1,"message":"Inspect source"}]`, 1)},
		{"unknown severity", strings.Replace(string(base), `"findings":[]`, `"findings":[{"severity":"approved","path":"README.md","line":1,"message":"Inspect source"}]`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readWorkerReturn(strings.NewReader(tc.input), "-"); err == nil {
				t.Fatal("invalid worker claim accepted")
			}
		})
	}
	r := compactChecksFixture(t, "pass")
	result := r.Run(`{"version":1,"approval":true}`, filepath.Join(kitsutest.Binary(t), compactBinaryName()), "task", "return", strings.Repeat("a", 64), "--request", "-")
	if result.Code != 2 || r.Exists(".git/kitsu-stages") || r.Exists(".git/kitsu-context") {
		t.Fatal("invalid schema mutated private state", result.Output())
	}
}

func TestWorkerReturnRejectsIdentityPolicyWritesAndStaleChecks(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"task", "packet", "worktree", "source", "loaded skill", "host", "write", "policy", "packet tamper", "checks", "budget"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			d := protocolPrepare(t, r, "codex")
			request := protocolRequest(d)
			var extra []string
			switch kind {
			case "task":
				request["task"] = "different-task"
			case "packet":
				request["packet_sha256"] = strings.Repeat("c", 64)
			case "worktree":
				request["worktree"] = r.Path(".") + string(filepath.Separator) + "."
			case "source":
				request["source"] = map[string]string{"tree": "stale", "head": "stale"}
			case "loaded skill":
				request["loaded"] = map[string]any{"skills": []workerLoadedSkill{{ID: "unselected", SHA256: strings.Repeat("c", 64)}}}
			case "host":
				request["worker"] = map[string]any{"host": "claude", "id": "fake-worker", "completed": true}
			case "write":
				r.Write("main.go", "package main\n")
			case "policy":
				r.Write("kitsu-roles.toml", "version=1\n[codex.reviewer]\nmodel='drift'\n")
			case "packet tamper":
				if err := os.WriteFile(d["path"].(string), []byte("changed packet"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "checks":
				request["checks"] = []stageCheck{{ID: "pass", State: "satisfied", RunID: "invented"}}
			case "budget":
				extra = []string{"--max-bytes", "512"}
			}
			result := protocolReturn(t, r, d, request, extra...)
			if result.Code == 0 || r.Exists(".git/kitsu-stages/"+stageID(t, d)+".return.json") || !r.Exists(".git/kitsu-stages/active.json") {
				t.Fatal("invalid handback accepted or interval released", result.Output())
			}
		})
	}
}

func TestWorkerReturnRejectsTamperingAndChangedEvidenceOnRetry(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"tamper", "evidence", "different handback"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			d := protocolPrepare(t, r, "codex")
			request := protocolRequest(d)
			stageJSON(t, protocolReturn(t, r, d, request))
			switch mode {
			case "tamper":
				name := ".git/kitsu-stages/" + stageID(t, d) + ".return.json"
				r.Write(name, strings.Replace(r.Read(name), `"untrusted-worker-claims"`, `"approved"`, 1))
			case "evidence":
				if result := r.Kitsu("check", "run"); result.Code != 0 {
					t.Fatal(result.Output())
				}
			case "different handback":
				request["blockers"] = []string{"different"}
			}
			if result := protocolReturn(t, r, d, request); result.Code == 0 || !r.Exists(".git/kitsu-stages/active.json") {
				t.Fatal("unsafe return retry accepted", result.Output())
			}
		})
	}
}

func TestNativeDispatchSmallBudgetDoesNotWriteSelections(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	result := r.Run("Original contract", filepath.Join(kitsutest.Binary(t), compactBinaryName()), "task", "prepare", "fix-login", "--role", "reviewer", "--stage", "review", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--dispatch", "--max-bytes", "2048")
	if result.Code == 0 || r.Exists("openspec/changes/.skills/fix-login") || r.Exists(".git/kitsu-stages/active.json") {
		t.Fatal("native dispatch budget failure changed selection or interval", result.Output())
	}
}

func TestWorkerProtocolHelpAndCompletionAreOffline(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("return.json", "{}")
	for _, tc := range []struct {
		words []string
		want  string
	}{
		{[]string{"task", "prepare", "--d"}, "--dispatch"},
		{[]string{"task", "ret"}, "return"},
		{[]string{"task", "return", "--request", "ret"}, "return.json"},
	} {
		values := uxCompletionTestValues(uxQueryRepoCompletion(t, r, tc.words))
		if !slices.Contains(values, tc.want) {
			t.Fatal("protocol completion missing", tc.want, values)
		}
	}
	for _, command := range []string{"prepare", "return"} {
		result := r.Kitsu("task", command, "--help")
		if result.Code != 0 || !strings.Contains(result.Stdout, "usage: kitsu task "+command) {
			t.Fatal(result.Output())
		}
	}
	if r.Exists(".git/kitsu-stages") || r.Exists("openspec/changes/.skills") {
		t.Fatal("help or completion created protocol state")
	}
}
