package cli

// Contract: verify-adviser-claim, interface contract revision 4, R3
// (cw task start refuses a contradiction). The plan, policy, credentials and
// preferences are fixtures; credential values are fake.

import (
	json "encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"
)

// acCLIReason is the first R1 required-true example.
const acCLIReason = "No advisers are configured on this host (0 of 3 credentials)."

// acCLIPlan is a substantial plan whose checkpoints are not required, with reason.
func acCLIPlan(t *testing.T, reason string) string {
	t.Helper()
	plan := map[string]any{
		"title": "Decide with bound advisers",
		"kind":  "substantial",
		"requirements": []any{map[string]any{
			"id": "source", "description": "Inspect actual source", "check_ids": []any{"unit"}, "allow_non_execution": true,
		}},
		"checks":      []any{map[string]any{"id": "unit", "argv": []any{"go", "test", "./..."}}},
		"checkpoints": map[string]any{"required": false, "reason": reason},
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// acCLIStartMessage is the exact R2 message for the bound IDs.
func acCLIStartMessage(ids string) string {
	return "plan checkpoints reason claims no advisers are configured, but this task binds " + ids +
		`; set "required": true and run the checkpoints, or, if checkpoints are truly unneeded, give the real reason`
}

type acCLICase struct {
	name        string
	credentials []string     // providers whose fake credential is in cw's environment
	preferences string       // preferences.json content; empty writes none
	policy      []fixturePin // non-nil passes --policy with these providers
	reason      string
	ids         string            // non-empty: start must fail naming these bound IDs
	bound       map[string]string // ids empty: start must succeed with this binding
}

// acCLIStart runs `cw task start --plan - --store S --config-dir D [--policy P]`.
// The credentials are also saved in D, which opts in to their advisers.
func acCLIStart(t *testing.T, c acCLICase) (*bindingFixture, string, error) {
	t.Helper()
	fixture := newBindingFixture(t, adviserBindingVersion, nil, fixtureCredentials(c.credentials...))
	store := adviserBindingPrivateDir(t)
	dir := adviserBindingConfigDir(t, c.preferences)
	if len(c.credentials) > 0 {
		wtStoreKeys(t, dir, fixtureCredentials(c.credentials...))
	}
	args := []string{"task", "start", "--plan", "-", "--store", store, "--config-dir", dir}
	if c.policy != nil {
		args = append(args, "--policy", writePolicyFixture(t, t.TempDir(), adviserBindingVersion, c.policy))
	}
	err := fixture.run(t, acCLIPlan(t, c.reason), args...)
	return fixture, store, err
}

// R3: with a non-empty computed binding, cw task start fails with the R2
// message, exits non-zero and creates no task directory; a provider that is
// disabled in preferences.json or lacks its credential is not bound, so when
// nothing is bound the same plan starts. Fails when the command accepts a
// contradiction, words or orders the IDs differently, counts an unbound
// provider, or refuses a plan with nothing bound.
func TestTaskStartRefusesNoAdvisersClaimWithBoundAdvisers(t *testing.T) {
	cases := []acCLICase{
		{name: "both_credentialed", credentials: []string{"subq_snap", "openai_decisions"}, reason: acCLIReason, ids: "openai_decisions, subq_snap"},
		{name: "only_subq_credentialed", credentials: []string{"subq_snap"}, reason: acCLIReason, ids: "subq_snap"},
		{name: "openai_disabled_in_preferences", credentials: []string{"subq_snap", "openai_decisions"},
			preferences: `{"version":1,"advisers":{"disabled":["openai_decisions"]}}`, reason: acCLIReason, ids: "subq_snap"},
		{name: "jev_policy", credentials: []string{"jev"}, policy: []fixturePin{pinJev}, reason: "zero advisers bound", ids: "jev"},
		{name: "all_three_policy", credentials: []string{"jev", "subq_snap", "openai_decisions"}, policy: []fixturePin{pinJev, pinSnap, pinOpenAI},
			reason: "no adviser credentials are configured and no alternative approach is open", ids: "jev, openai_decisions, subq_snap"},
		{name: "nothing_credentialed", credentials: nil, reason: acCLIReason, bound: map[string]string{}},
		{name: "every_provider_disabled", credentials: []string{"subq_snap", "openai_decisions"},
			preferences: `{"version":1,"advisers":{"disabled":["jev","subq_snap","openai_decisions"]}}`, reason: acCLIReason, bound: map[string]string{}},
		{name: "credential_only_for_unenabled_jev", credentials: []string{"jev"}, reason: acCLIReason, bound: map[string]string{}},
		{name: "non_claiming_reason_bound", credentials: []string{"subq_snap", "openai_decisions"},
			reason: "the owner fixed the design", bound: map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}},
	}
	// Control: a successful start in the same layout fixes how deep a task
	// directory sits below the store.
	control := cases[len(cases)-1]
	fixture, store, err := acCLIStart(t, control)
	if err != nil {
		t.Fatalf("fixture invariant: control start failed: %v: %s", err, fixture.stderr)
	}
	var controlRecord struct {
		Directory string `json:"directory"`
	}
	if err := json.Unmarshal(fixture.out.Bytes(), &controlRecord); err != nil || controlRecord.Directory == "" {
		t.Fatalf("fixture invariant: control start output: %v: %s", err, fixture.out)
	}
	rel, err := filepath.Rel(store, controlRecord.Directory)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		t.Fatalf("fixture invariant: control task %q is not inside the store %s", controlRecord.Directory, store)
	}
	depth := len(strings.Split(rel, string(filepath.Separator)))

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture, store, err := acCLIStart(t, c)
			if c.ids == "" {
				if err != nil {
					t.Fatalf("R3: start refused a plan with binding %v: %v: %s", c.bound, err, fixture.stderr)
				}
				var document map[string]any
				if err := json.Unmarshal(fixture.out.Bytes(), &document); err != nil {
					t.Fatalf("start output is not JSON: %v: %s", err, fixture.out)
				}
				requireAdviserBinding(t, "task start", document["advisers"], c.bound)
				return
			}
			if err == nil {
				t.Fatalf("R3: start accepted a contradiction: %s", fixture.out)
			}
			if code := ExitCode(err); code == 0 {
				t.Fatalf("R3: refused start exit code is 0: %v", err)
			}
			want := acCLIStartMessage(c.ids)
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("R3: error text\n got %q\nwant it to contain %q", err.Error(), want)
			}
			for _, pin := range []fixturePin{pinJev, pinSnap, pinOpenAI} {
				if strings.Contains(err.Error(), pin.model) {
					t.Fatalf("R3: error names the model %q: %q", pin.model, err.Error())
				}
			}
			for _, d := range adviserBindingDirectories(t, store) {
				if len(strings.Split(d, string(filepath.Separator))) >= depth {
					t.Fatalf("R3: refused start left directory %s in the store (task directories sit %d deep)", d, depth)
				}
			}
		})
	}
}

// R3 with R5: a refused contradiction does not stop a corrected plan from
// starting with the same configuration, and required checkpoints with the
// same reason start too. Fails when the refusal depends on anything but the
// contradiction.
func TestTaskStartAcceptsCorrectedPlanAfterRefusal(t *testing.T) {
	fixture := newBindingFixture(t, adviserBindingVersion, nil, fixtureCredentials("subq_snap", "openai_decisions"))
	store := adviserBindingPrivateDir(t)
	dir := adviserBindingConfigDir(t, optInEveryAdviser)
	args := []string{"task", "start", "--plan", "-", "--store", store, "--config-dir", dir}
	if err := fixture.run(t, acCLIPlan(t, acCLIReason), args...); err == nil {
		t.Errorf("R3: start accepted a contradiction: %s", fixture.out)
	}
	required := strings.Replace(acCLIPlan(t, acCLIReason), `"required":false`, `"required":true`, 1)
	if !strings.Contains(required, `"required":true`) {
		t.Fatalf("fixture invariant: plan has no required member to flip: %s", required)
	}
	want := map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}
	for name, plan := range map[string]string{"required_true": required, "real_reason": acCLIPlan(t, "the owner fixed the design")} {
		if err := fixture.run(t, plan, args...); err != nil {
			t.Fatalf("R3 %s: corrected plan refused: %v: %s", name, err, fixture.stderr)
		}
		var document map[string]any
		if err := json.Unmarshal(fixture.out.Bytes(), &document); err != nil {
			t.Fatalf("start output is not JSON: %v", err)
		}
		requireAdviserBinding(t, name, document["advisers"], want)
	}
}
