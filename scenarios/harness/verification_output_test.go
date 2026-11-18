package harness_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestBriefVerificationPointsToExecutedLog(t *testing.T) {
	for _, tc := range []struct {
		name, gate, assertion, check, outcome, marker string
		code, exit                                    int
	}{
		{"success", "sh", "exit 0", "task-proof", "passed", "behavior", 0, 0},
		{"assertion failure", "sh", "exit 7", "task-proof", "failed", "behavior", 5, 7},
		{"repository failure", "sh", "exit 0", "lint", "failed", "lint-marker", 5, 19},
		{"unavailable repository tool", "kitsu-missing-verify-tool", "exit 0", "lint", "failed", "could not be started", 5, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := kitsutest.Demo(t)
			r.Write("kitsu.toml", strings.Replace(kitsutest.DemoDeclaration, `"sh", "lint.sh"`, fmt.Sprintf("%q, \"lint.sh\"", tc.gate), 1))
			lint := "echo lint-marker >&2\n"
			if tc.exit == 19 {
				lint += "exit 19\n"
			}
			r.Write("lint.sh", lint)
			f := flow{t: t, repo: r, id: "proof"}
			f.call(0, "open", "--request", "Check the public behavior", "--until", "verify")
			f.record("intake", "host", "Exercise both output streams and the repository checks")
			result := r.Kitsu("task", "test", "proof", "--brief", "--ticket", f.state.Ticket, "--expect", "behavior", "--",
				"sh", "-c", "echo behavior-out; echo behavior-err >&2; "+tc.assertion)
			if result.Code != tc.code || strings.Contains(result.Output(), "behavior-out") || strings.Contains(result.Output(), "behavior-err") || strings.Contains(result.Output(), "lint-marker") {
				t.Fatal(result.Code, result.Output())
			}
			var receipt struct {
				Verified bool
				Log      struct {
					Run           string `json:"run_id"`
					Check         string `json:"check_id"`
					Digest        string `json:"log_sha256"`
					Exit          *int   `json:"exit_status"`
					Outcome, Path string
				} `json:"verification_log"`
			}
			if err := json.Unmarshal([]byte(result.Stdout), &receipt); err != nil {
				t.Fatal(err)
			}
			log := receipt.Log
			if receipt.Verified != (tc.code == 0) || log.Run == "" || log.Path != r.Path(filepath.Join(".git", "kitsu", "logs", log.Run+".log")) || log.Check != tc.check || log.Outcome != tc.outcome {
				t.Fatal(result.Output())
			}
			data, err := os.ReadFile(log.Path)
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != log.Digest || !strings.Contains(string(data), tc.marker) {
				t.Fatal("wrong retained diagnostic", err, string(data))
			}
			if (log.Exit == nil) != (tc.exit == -1) || log.Exit != nil && *log.Exit != tc.exit {
				t.Fatal("wrong original exit", result.Stdout)
			}
			full := r.Kitsu("task", "next", "proof", "--json")
			if tc.code != 0 && (full.Code != 0 || strings.Contains(full.Stdout, "\"checked_at\"")) {
				t.Fatal("failed verification marked complete", full.Output())
			}
		})
	}
}

func TestFullVerificationStillStreamsDiagnostics(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "stream"}
	f.call(0, "open", "--request", "Verify the candidate", "--until", "verify")
	f.record("intake", "host", "Run the public behavior")
	result := r.Kitsu("task", "test", f.id, "--json", "--ticket", f.state.Ticket, "--expect", "behavior", "--",
		"sh", "-c", "echo behavior-out; echo behavior-err >&2")
	if result.Code != 0 || !json.Valid([]byte(result.Stdout)) {
		t.Fatal(result.Output())
	}
	for _, marker := range []string{"behavior-out", "behavior-err", "lint.sh runs", "unit.sh runs"} {
		if !strings.Contains(result.Stderr, marker) {
			t.Fatalf("missing %s from live output: %s", marker, result.Stderr)
		}
	}
}

func TestBriefGateRetainsSourceChangedRunAcrossRetry(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("lint.sh", "if test -f .git/mutate; then echo mutated > README.md; fi\n")
	prior := r.Kitsu("run", "lint")
	if prior.Code != 0 {
		t.Fatal(prior.Output())
	}
	original := r.Read("README.md")
	r.Write(".git/mutate", "1")
	f := flow{t: t, repo: r, id: "changed"}
	f.call(0, "open", "--request", "Verify candidate", "--until", "verify")
	f.record("intake", "host", "Exercise the assertion and the repository declaration")
	try := func() kitsutest.Result {
		return r.Kitsu("task", "test", f.id, "--brief", "--ticket", f.state.Ticket, "--expect", "unit", "--", "sh", "unit.sh")
	}
	failed := try()
	var receipt struct {
		Verified bool
		Run      string `json:"run_id"`
		Log      struct {
			Run           string `json:"run_id"`
			Outcome, Path string
		} `json:"verification_log"`
	}
	if err := json.Unmarshal([]byte(failed.Stdout), &receipt); err != nil || failed.Code != 5 || receipt.Verified || receipt.Log.Outcome != "source_changed" || receipt.Log.Run == receipt.Run {
		t.Fatal("source-changing check inherited other evidence", failed.Output(), err)
	}
	detail, err := os.ReadFile(receipt.Log.Path)
	if err != nil || !strings.Contains(string(detail), "source changed") {
		t.Fatal(err, string(detail))
	}
	run := receipt.Run
	r.Remove(".git/mutate")
	r.Write("README.md", original)
	f.call(0, "next")
	f.want("verify")
	retry := try()
	if err := json.Unmarshal([]byte(retry.Stdout), &receipt); err != nil || retry.Code != 0 || !receipt.Verified || receipt.Log.Outcome != "passed" || receipt.Log.Run != run {
		t.Fatal("retry did not recover the valid assertion and recheck the gate", err, retry.Output())
	}
}
