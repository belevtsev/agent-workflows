package cli

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Advisers bind only when the user opts in: advisers.enabled in the saved
// preferences, a key saved with cw keys set, or --policy. A key variable in
// the environment alone binds nothing.

const optInEnabledOpenAI = `{"version":1,"advisers":{"enabled":["openai_decisions"]}}`

// optInEveryAdviser opts in to every adviser, for tests of what else decides
// a binding: the selected policy and the credentials present.
const optInEveryAdviser = `{"version":1,"advisers":{"enabled":["jev","subq_snap","openai_decisions"]}}`

// optInKeyWarning is the doctor warning for an OPENAI_API_KEY without an opt-in.
const optInKeyWarning = "openai_decisions: key present, adviser not enabled; OPENAI_API_KEY alone does not bind it (opt in with advisers.enabled, cw keys set or --policy)"

// With adviser keys in the environment and no opt-in, cw task start binds no
// adviser, whether the policy is the installed one, the one embedded in cw, or
// one chosen through $CW_MODEL_POLICY rather than --policy.
func TestTaskStartWithOnlyKeyVariablesBindsNoAdviser(t *testing.T) {
	// oracle: the bound advisers of the emitted, returned and stored record are empty.
	for _, c := range []struct {
		name    string
		release int  // 0: no personal policy, so the embedded one applies
		viaEnv  bool // select a policy copy through $CW_MODEL_POLICY
	}{
		{name: "installed policy", release: adviserBindingVersion},
		{name: "embedded policy"},
		{name: "policy from CW_MODEL_POLICY", release: adviserBindingVersion, viaEnv: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			environment := fixtureCredentials("subq_snap", "openai_decisions")
			if c.viaEnv {
				environment[policyEnvironment] = writePolicyFixture(t, t.TempDir(), adviserBindingVersion, []fixturePin{pinSnap, pinOpenAI})
			}
			fixture := newBindingFixture(t, c.release, nil, environment)
			record, document := fixture.start(t, bindingSmallPlan, "--config-dir", adviserBindingConfigDir(t, ""))
			adviserBindingRequire(t, c.name, record, document, map[string]string{})
		})
	}
}

// Each opt-in on its own binds the provider it names. Both shipped providers
// have a key in the environment and the installed policy enables both; only
// openai_decisions is opted in to, so only it is bound.
func TestTaskStartBindsAnAdviserForEachOptIn(t *testing.T) {
	// oracle: the bound set is exactly {openai_decisions: gpt-6-luna}; for
	// --policy, the same start without the flag binds nothing.
	want := adviserBindingPins(pinOpenAI)
	t.Run("advisers.enabled", func(t *testing.T) {
		fixture := newBindingFixture(t, adviserBindingVersion, nil, fixtureCredentials("subq_snap", "openai_decisions"))
		record, document := fixture.start(t, bindingSmallPlan, "--config-dir", adviserBindingConfigDir(t, optInEnabledOpenAI))
		adviserBindingRequire(t, "advisers.enabled", record, document, want)
	})
	t.Run("cw keys set", func(t *testing.T) {
		fixture := newBindingFixture(t, adviserBindingVersion, nil, fixtureCredentials("subq_snap", "openai_decisions"))
		dir := adviserBindingConfigDir(t, "")
		if err := fixture.run(t, "saved-openai-key\n", "keys", "set", credentials.OPENAI_API_KEY, "--config-dir", dir); err != nil {
			t.Fatalf("cw keys set: %v: %s", err, fixture.stderr)
		}
		record, document := fixture.start(t, bindingSmallPlan, "--config-dir", dir)
		adviserBindingRequire(t, "cw keys set", record, document, want)
	})
	t.Run("--policy", func(t *testing.T) {
		fixture := newBindingFixture(t, adviserBindingVersion, []fixturePin{pinOpenAI}, fixtureCredentials("subq_snap", "openai_decisions"))
		dir := adviserBindingConfigDir(t, "")
		record, document := fixture.start(t, bindingSmallPlan, "--config-dir", dir)
		adviserBindingRequire(t, "same policy without --policy", record, document, map[string]string{})
		record, document = fixture.start(t, bindingSmallPlan, "--config-dir", dir, "--policy", fixture.policy)
		adviserBindingRequire(t, "--policy", record, document, want)
	})
}

// An ad hoc consult applies the same rule: without --policy, an adviser not
// opted in to is previewed as not_configured although its key is set.
func TestAdHocConsultContactsOnlyOptedInAdvisers(t *testing.T) {
	// oracle: each provider's dry-run preview status.
	for _, c := range []struct {
		name, preferences string
		policy            bool
		want              map[string]string
	}{
		{"key variables only", "", false, map[string]string{"subq_snap": "not_configured", "openai_decisions": "not_configured"}},
		{"openai enabled", optInEnabledOpenAI, false, map[string]string{"subq_snap": "not_configured", "openai_decisions": "dry_run"}},
		{"--policy", "", true, map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			directory := t.TempDir()
			request := writeFixtureFile(t, directory, "request.json", fixtureRequest(`"evidence"`))
			output := filepath.Join(directory, "preview.json")
			args := []string{"--request", request, "--output", output, "--dry-run", "--config-dir", adviserBindingConfigDir(t, c.preferences)}
			if c.policy {
				args = append(args, "--policy", writePolicyFixture(t, filepath.Join(directory, "policy"), adviserBindingVersion, nil))
			}
			outcome := executeConsult(t, fixtureEnvironment(fixtureCredentials("subq_snap", "openai_decisions")), args...)
			got := adviserBindingPreview(t, outcome, output)
			adviserBindingRequireStatuses(t, got, c.want)
		})
	}
}

// cw doctor warns about a key variable set without an opt-in, and only then.
func TestDoctorWarnsAboutAKeyWithoutAnOptIn(t *testing.T) {
	// oracle: the exact warning text in the doctor report's warnings.
	for _, c := range []struct {
		name        string
		preferences string
		saved       bool // save OPENAI_API_KEY in the configuration directory
		want        bool
	}{
		{name: "key variable only", want: true},
		{name: "advisers.enabled", preferences: optInEnabledOpenAI},
		{name: "saved key", saved: true},
		{name: "disabled", preferences: adviserBindingOffOpenAI},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCompactDoctorFixture(t)
			dir := filepath.Join(f.home, ".config", "agent-workflows")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if c.preferences != "" {
				if err := os.WriteFile(filepath.Join(dir, "preferences.json"), []byte(c.preferences), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c.saved {
				resolved, err := filepath.EvalSymlinks(dir)
				if err != nil {
					t.Fatal(err)
				}
				wtStoreKeys(t, resolved, map[string]string{credentials.OPENAI_API_KEY: "saved-openai-key"})
			}
			outcome := f.run(t, fixtureCredentials("openai_decisions"))
			if outcome.err != nil {
				t.Fatalf("doctor: %v: %s", outcome.err, outcome.text())
			}
			var report projectctx.DoctorReport
			if err := json.Unmarshal([]byte(outcome.stdout), &report); err != nil {
				t.Fatalf("doctor output is not a report: %v", err)
			}
			var keyWarnings []string
			for _, warning := range report.Warnings {
				if strings.Contains(warning, "key present, adviser not enabled") {
					keyWarnings = append(keyWarnings, warning)
				}
			}
			if wanted := []string{optInKeyWarning}; c.want && !slices.Equal(keyWarnings, wanted) || !c.want && len(keyWarnings) != 0 {
				t.Fatalf("doctor key warnings = %q, want the warning: %v", keyWarnings, c.want)
			}
		})
	}
}
