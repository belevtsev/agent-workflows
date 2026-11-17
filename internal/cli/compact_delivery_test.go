package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNativeDispatchMinimalReturnIsUsableForBothHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := compactChecksFixture(t, "pass")
			d := protocolPrepare(t, r, host)
			prompt := d["native"].(map[string]any)["prompt"].(string)
			_, tail, ok := strings.Cut(prompt, "required shape and identities:\n")
			if !ok {
				t.Fatal("missing literal minimal return")
			}
			body, _, _ := strings.Cut(tail, "\n")
			request, err := readWorkerReturn(strings.NewReader(body), "-")
			if err != nil || request.Loaded != nil || request.Worker != nil || request.Source != nil {
				t.Fatal("minimal shape requires invented optional claims", err)
			}
			receipt := stageJSON(t, protocolReturn(t, r, d, workflowJSON[map[string]any](t, body)))
			if receipt["authority"] != "untrusted-worker-claims" || !r.Exists(".git/kitsu-stages/active.json") {
				t.Fatal("minimal handback acquired authority or closed the interval")
			}
			stageJSON(t, r.Kitsu("task", "finish", stageID(t, d)))
		})
	}
}

func TestWorkerReturnSupportReadsAndRunnerOutcomesStayRejected(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	d := protocolPrepare(t, r, "codex")
	if result := r.Kitsu("check", "run"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	status := workflowJSON[statusReport](t, r.Kitsu("gate", "status", "--json").Stdout)
	if !status.Verified || status.Checks[0].State != "satisfied" {
		t.Fatal("fixture needs current passing gate evidence")
	}
	for _, kind := range []string{"support read", "runner outcome"} {
		request := protocolRequest(d)
		want := "does not match current declared check evidence"
		if kind == "support read" {
			request["loaded"] = map[string]any{"skills": []workerLoadedSkill{{ID: "skills/task/SKILL.md", SHA256: strings.Repeat("c", 64)}}}
			want = "not a unique original selected skill"
		} else {
			request["checks"] = []stageCheck{{ID: "pass", State: "passed", RunID: status.Checks[0].RunID}}
		}
		result := protocolReturn(t, r, d, request)
		if result.Code == 0 || !strings.Contains(result.Stderr, want) || r.Exists(".git/kitsu-stages/"+stageID(t, d)+".return.json") || !r.Exists(".git/kitsu-stages/active.json") {
			t.Fatal("invalid claim was retained or interval authority changed", kind, result.Output())
		}
	}
	stageJSON(t, protocolReturn(t, r, d, protocolRequest(d)))
	stageJSON(t, r.Kitsu("task", "finish", stageID(t, d), "--expect-checks", "pass"))
}

// Only summary numbers are exported. Full in-memory ledgers are checked; the
// replay does not call a provider, estimate tokens or grade model judgment.
func saveDeliveryReport(t *testing.T, rows []map[string]any) {
	t.Helper()
	path := os.Getenv("KITSU_TEST_DELIVERY_REPORT")
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(map[string]any{
		"version": 1, "scope": "Scripted duplicated compact delivery versus focused compact delivery; not ordinary mode, a historical transcript replay, provider usage or model-quality evidence.",
		"serialization": "Exact canonical CLI payloads and fake-native JSON envelopes, including full packet/skill reads, repeated guide reads, returns, finishes and rejected requests.",
		"profile":       "fake-sol-profile/high", "paid_calls": 0, "provider_tokens": nil,
		"required_facts_equal": true, "rows": rows,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}
