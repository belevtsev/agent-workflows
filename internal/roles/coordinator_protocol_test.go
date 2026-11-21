package roles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are workflow contracts presented to the host, rather than a snapshot of
// the prose. Keep the choices and authority boundaries explicit in each path:
// silently dropping a qualification can make the host run bookkeeping twice or
// treat a private receipt as approval.
func TestCoordinatorGuidedProtocol(t *testing.T) {
	t.Parallel()
	coordinator, err := Text(Coordinator)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root(t), ManagedSkill, "references", "compact.md"))
	if err != nil {
		t.Fatal(err)
	}
	compact := string(data)
	tests := []struct {
		name, document, heading string
		required, forbidden     []string
	}{
		{
			name: "managed replaces manual write interval", document: coordinator, heading: "After every role",
			required:  []string{"Unmanaged:", "gate status --json", "role check <role> --from", "Managed: prepare/finish instead", "do not repeat manual baseline capture or role check", "`--finalize`"},
			forbidden: []string{"Before the role starts, note the `tree`", "When it ends, run `kitsu role check"},
		},
		{
			name: "managed write violations retain their interval", document: coordinator, heading: "After every role",
			required: []string{"Unmanaged role-check exit 5 or any managed write violation", "responsible role with violations; repair nothing yourself", "Failed managed intervals stay active; no duplicate role check", "A second write-limit violation by that role stops for the developer", "Never commit read-only role writes"},
		},
		{
			name: "managed role body comes only from its packet", document: coordinator, heading: "Starting a role",
			required:  []string{"On Claude Code use `kitsu-team:<role>`", "on Codex, unmanaged starts get `kitsu role text <role>`", "Managed starts load the full saved packet once; no extra role-text call"},
			forbidden: []string{"otherwise give it `kitsu role text <role>`"},
		},
		{
			name: "policy reuse has invalidation", document: coordinator, heading: "The roles",
			required: []string{"once per session/current policy", "resolve host tools once", "policy, model, config, skill, test or check changes", "no stale reuse", "available fallback", "without repeated failed calls or automatic installation"},
		},
		{
			name: "fresh workers load originals", document: coordinator, heading: "Existing skills",
			required:  []string{"resolve the role's skills before every worker", "Managed preparation resolves them itself; do not duplicate it", "reading original SKILL.md", "every fresh worker", "full role and assignment contract", "receipts alone prove no loading", "Skills grant no extra writes"},
			forbidden: []string{"Before every worker start, resolve its skills"},
		},
		{
			name: "native wait preserves supervision", document: coordinator, heading: "Starting a role",
			required: []string{"native completion notifications", "long bounded native wait", "Do not poll every ten seconds", "cancellation", "deadlines", "user progress updates", "independent host budget monitor", "no CLI daemon watches or starts host workers"},
		},
		{
			name: "managed cadence retains semantic decisions", document: coordinator, heading: "Starting a role",
			required: []string{"Coalesce routine progress", "keep questions/blockers and user updates", "After terminal red/review decisions", "batch finish/prepare on success", "receipts grant no approval"},
		},
		{
			name: "compact stays opt in", document: compact,
			required: []string{"only when the developer requests compact mode", "--compact", "v1 reference contract", "task brief ... --json", "For an unmanaged saved brief", "`role check --from <baseline>`", "separate saved baseline and `--finalize`"},
		},
		{
			name: "native dispatch and optional return", document: compact, heading: "Fresh workers",
			required: []string{"--assignment <file|-> --dispatch", "`native.prompt`", "only the host starts workers", "After the worker has stopped", "Structured return is optional; finish never requires it", "releases no interval, executes no checks", "do not run both"},
		},
		{
			name: "worker delivery and quiet checks", document: compact,
			required:  []string{"Keep coordinator scheduling", "Do not paste this whole reference", "never trim user instructions", "Use ordinary `kitsu check run` for quiet checks", "Do not probe state before that history exists", "rerun checks solely to populate a handback"},
			forbidden: []string{"Give these examples to every fresh worker", "Carry this selective observation policy into every fresh worker's assignment"},
		},
		{
			name: "private records cannot certify workflow", document: compact, heading: "Fresh workers",
			required: []string{"Load all selected original skills in every fresh worker", "receipts do not prove loading", "Inspect the actual red failure", "independent reviews", "canonical reviews/approvals/task records and check freshness", "Git role", "normal hooks and gate", "never replace those requirements"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			text := test.document
			if test.heading != "" {
				text = section(text, test.heading)
			}
			text = strings.Join(strings.Fields(text), " ")
			for _, required := range test.required {
				if !strings.Contains(text, required) {
					t.Errorf("host guidance omits workflow contract %q", required)
				}
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(text, forbidden) {
					t.Errorf("host guidance restores unconditional bookkeeping %q", forbidden)
				}
			}
		})
	}
}
