package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func TestSelectiveCompactTaskStartRequiresExplicitOptIn(t *testing.T) {
	onboardingFixture(t)
	request := "Inspect λ and preserve `literal` instructions.\nNext line."
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			plainPrompt := "Use the task skill from the Kitsu plugin to handle this request. Keep this host session as the workflow orchestrator and follow the skill's workflow.\n\nRequest:\n" + request
			if host == "claude" {
				plainPrompt = "/kitsu:task " + request
			}
			for _, compact := range []bool{false, true} {
				args := []string{"task", "start", request, "--host", host, "--json"}
				if compact {
					args = append(args, "--compact")
				}
				var out, diagnostics bytes.Buffer
				if err := Execute(t.Context(), args, Config{Out: &out, Err: &diagnostics}); err != nil || diagnostics.Len() != 0 {
					t.Fatalf("handoff failed: %v; %s; %s", err, &out, &diagnostics)
				}
				var handoff taskHandoff
				if err := json.Unmarshal(out.Bytes(), &handoff); err != nil {
					t.Fatal(err)
				}
				if len(handoff.Argv) != 2 || handoff.Argv[1] != handoff.Prompt {
					t.Fatal("launch prompt differs from printed handoff", handoff)
				}
				if !compact {
					if handoff.Prompt != plainPrompt {
						t.Fatalf("plain start changed: %q", handoff.Prompt)
					}
					continue
				}
				if !strings.HasPrefix(handoff.Prompt, plainPrompt+"\n\nKitsu compact mode (selective opt-in):") {
					t.Fatalf("compact mode changed the original request: %q", handoff.Prompt)
				}
				for _, instruction := range []string{
					"references/compact.md", "native reads for short files", "noisy searches and logs",
					"Expand retained evidence", "Keep the requested workflow",
				} {
					if !strings.Contains(handoff.Prompt, instruction) {
						t.Errorf("compact handoff lost %q", instruction)
					}
				}
			}
		})
	}
}

func TestSelectiveCompactSavedWorkersRetainPolicyAndOriginalInstructions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, host, role, stage string
	}{
		{name: "Codex developer", host: "codex", role: "developer", stage: "code"},
		{name: "Codex reviewer", host: "codex", role: "reviewer", stage: "review"},
		{name: "Claude lookup", host: "claude", role: "lookup", stage: "lookup"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r, _ := skillRepo(t)
			r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
			assignment := "Investigate the assigned evidence; preserve λ and this complete instruction.\n"
			d := savedDispatch(t, r, assignment, "--host", test.host, "--role", test.role, "--stage", test.stage)
			data, err := os.ReadFile(d.Path)
			if err != nil {
				t.Fatal(err)
			}
			b := savedBriefMetadata(t, string(data))
			constraints := strings.Join(b.Constraints, "\n")
			for _, instruction := range []string{
				"original full role instructions and selected skills uncompressed",
				"original paths and verify hashes", "native host reads for short whole files",
				"scoped native search when output is small", "do not route every read or command through the collector",
				"broad or noisy retrieval", "kitsu context read", "kitsu context search",
				"context collect for independent JSON batches", "errors and omissions before conclusions",
				"noisy declared checks use kitsu check run --compact", "expand retained logs with context show",
				"Expansion never reruns a check", "local rendered text, not provider prompt cache",
				"all requests, responses and expansions", "never proves passing checks",
				"ordinary kitsu check run for quiet declared checks", "Do not reread the coordinator's managed skill",
				"unless selected as a skill or needed by an explicit assignment reference",
				"do not duplicate managed baseline or role checks",
			} {
				if !strings.Contains(constraints, instruction) {
					t.Errorf("fresh worker lost %q", instruction)
				}
			}
			if !b.Compact || b.LoadStatus != "unverified" || !strings.HasSuffix(string(data), "Original assignment:\n"+assignment) {
				t.Fatal("worker opt-in, loading uncertainty or original assignment lost", b)
			}
			body, err := roles.Text(test.role)
			if err != nil {
				t.Fatal(err)
			}
			if test.host == "codex" && strings.Count(string(data), body) != 1 {
				t.Fatal("full original role instructions were changed or repeated")
			}
			if test.host == "claude" && (b.NativeRole != "kitsu-team:"+test.role || strings.Contains(string(data), body)) {
				t.Fatal("native role instructions were duplicated or lost")
			}
			if test.role == "reviewer" && !strings.Contains(constraints, "Review sources independently") {
				t.Fatal("selective observations lost independent review")
			}
		})
	}
}
