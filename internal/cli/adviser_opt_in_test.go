package cli

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
	"github.com/belevtsev/agent-workflows/v3/internal/doctor"
)

// Advisers bind only when the user opts in: advisers.enabled in the saved
// preferences, a key saved with cw keys set, or --policy. A key variable in
// the environment alone binds nothing.

const optInEnabledOpenAI = `{"version":1,"advisers":{"enabled":["openai_decisions"]}}`

// optInEveryAdviser opts in to every adviser, for tests of what else decides
// a binding: the selected policy and the credentials present.
const optInEveryAdviser = `{"version":1,"advisers":{"enabled":["jev","subq_snap","openai_decisions"]}}`

// optInKeyWarning is the doctor warning for an OPENAI_API_KEY without an opt-in.
const optInKeyWarning = "openai_decisions: key present, adviser not enabled; OPENAI_API_KEY alone does not bind it (opt in with advisers.enabled or cw keys set)"

// optInUnreadableKeys is the stderr line of a start or ad hoc consult whose
// saved keys cannot be read.
const optInUnreadableKeys = "warning: saved adviser keys were not read, so none of them opts in an adviser: " +
	"credential storage is unsafe; use an owner-only physical directory and regular private files\n"

// optInOpenConfigDir is a config dir with mode 0755: no preferences file, so
// only the key store refuses it.
func optInOpenConfigDir(t *testing.T) string {
	t.Helper()
	dir := adviserBindingPrivateDir(t)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// optInDoctorKeyWarnings runs cw doctor in f with env and returns the
// warnings that report a key without an opt-in.
func optInDoctorKeyWarnings(t *testing.T, f compactDoctorFixture, env map[string]string) []string {
	t.Helper()
	outcome := f.run(t, env)
	if outcome.err != nil {
		t.Fatalf("doctor: %v: %s", outcome.err, outcome.text())
	}
	var report doctor.DoctorReport
	if err := json.Unmarshal([]byte(outcome.stdout), &report); err != nil {
		t.Fatalf("doctor output is not a report: %v", err)
	}
	var keyWarnings []string
	for _, warning := range report.Warnings {
		if strings.Contains(warning, "key present, adviser not enabled") {
			keyWarnings = append(keyWarnings, warning)
		}
	}
	return keyWarnings
}

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
			keyWarnings := optInDoctorKeyWarnings(t, f, fixtureCredentials("openai_decisions"))
			if wanted := []string{optInKeyWarning}; c.want && !slices.Equal(keyWarnings, wanted) || !c.want && len(keyWarnings) != 0 {
				t.Fatalf("doctor key warnings = %q, want the warning: %v", keyWarnings, c.want)
			}
		})
	}
}

// The doctor warns only about a provider that the policy selected without
// --policy enables: a TypeSafe key is no warning under the shipped policy,
// which leaves Jev out, and is one under a personal policy that enables Jev.
func TestDoctorWarnsOnlyForAdvisersThePolicyEnables(t *testing.T) {
	// oracle: the exact key warnings for a TYPESAFE_API_KEY set without an opt-in.
	jevWarning := "jev: key present, adviser not enabled; TYPESAFE_API_KEY alone does not bind it (opt in with advisers.enabled or cw keys set)"
	for _, c := range []struct {
		name string
		pins []fixturePin // nil: no personal policy, so the embedded one applies
		want []string
	}{
		{name: "shipped policy without jev"},
		{name: "personal policy with jev", pins: []fixturePin{pinJev}, want: []string{jevWarning}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCompactDoctorFixture(t)
			if c.pins != nil {
				personalPolicyFixture(t, f.home, adviserBindingVersion, c.pins)
			}
			if got := optInDoctorKeyWarnings(t, f, fixtureCredentials("jev")); !slices.Equal(got, c.want) {
				t.Fatalf("doctor key warnings = %q, want %q", got, c.want)
			}
		})
	}
}

// When the saved keys cannot be read, any key may be saved, so the doctor
// does not call one "not enabled"; credentials_stored reports the store.
func TestDoctorDoesNotGuessOptInWhenSavedKeysAreUnreadable(t *testing.T) {
	// oracle: no key warning, and credentials_stored is degraded.
	f := newCompactDoctorFixture(t)
	if err := os.MkdirAll(filepath.Join(f.home, ".config", "agent-workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(f.home, ".config", "agent-workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := optInDoctorKeyWarnings(t, f, fixtureCredentials("openai_decisions")); len(got) != 0 {
		t.Fatalf("doctor key warnings = %q with an unreadable key store, want none", got)
	}
	outcome := f.run(t, fixtureCredentials("openai_decisions"))
	var report doctor.DoctorReport
	if err := json.Unmarshal([]byte(outcome.stdout), &report); err != nil {
		t.Fatalf("doctor output is not a report: %v", err)
	}
	if status := report.Checks["credentials_stored"].Status; status != "degraded" {
		t.Fatalf("fixture: credentials_stored = %q, want degraded", status)
	}
}

// The key warning is information: alone it leaves the doctor summary ok,
// while any other warning, or the same text from the project profile, still
// asks for attention.
func TestDoctorSummaryStaysOkWithOnlyAKeyWithoutAnOptIn(t *testing.T) {
	// oracle: the summary status, and that the warning is still listed.
	for _, c := range []struct {
		name              string
		warnings, profile []string
		want              string
	}{
		{name: "only the key warning", warnings: []string{optInKeyWarning}, want: "ok"},
		{name: "key warning and another", warnings: []string{optInKeyWarning, "Doctor reached its time limit"}, want: "attention"},
		{name: "same text from the profile", profile: []string{optInKeyWarning}, want: "attention"},
	} {
		t.Run(c.name, func(t *testing.T) {
			report := doctor.DoctorReport{Checks: map[string]doctor.Check{}, Warnings: c.warnings}
			report.Profile.Warnings = c.profile
			summary := summarizeDoctor(report)
			if summary.Status != c.want || !slices.Contains(summary.Warnings, optInKeyWarning) {
				t.Fatalf("summary status %q, warnings %q; want status %q with the key warning listed", summary.Status, summary.Warnings, c.want)
			}
		})
	}
}

// Saved keys that cannot be read opt in to nothing, and the command goes on:
// with all three key variables set and a 0755 config dir, start binds no
// adviser and an ad hoc consult contacts none, each with one stderr line.
func TestUnreadableSavedKeysOptInToNothing(t *testing.T) {
	// oracle: exit 0, advisers {} (or every preview not_configured), and the exact stderr line.
	t.Run("task start", func(t *testing.T) {
		fixture := newBindingFixture(t, adviserBindingVersion, nil, fixtureCredentials(adviserBindingAllKeys...))
		record, document := fixture.start(t, bindingSmallPlan, "--config-dir", optInOpenConfigDir(t))
		adviserBindingRequire(t, "task start", record, document, map[string]string{})
		if got := fixture.stderr.String(); got != optInUnreadableKeys {
			t.Fatalf("stderr = %q, want %q", got, optInUnreadableKeys)
		}
	})
	t.Run("ad hoc consult", func(t *testing.T) {
		directory := t.TempDir()
		request := writeFixtureFile(t, directory, "request.json", fixtureRequest(`"evidence"`))
		output := filepath.Join(directory, "preview.json")
		outcome := executeConsult(t, fixtureEnvironment(fixtureCredentials(adviserBindingAllKeys...)),
			"--request", request, "--output", output, "--dry-run", "--config-dir", optInOpenConfigDir(t))
		got := adviserBindingPreview(t, outcome, output)
		adviserBindingRequireStatuses(t, got, map[string]string{"subq_snap": "not_configured", "openai_decisions": "not_configured"})
		if !strings.Contains(outcome.stderr, optInUnreadableKeys) {
			t.Fatalf("stderr = %q, want it to contain %q", outcome.stderr, optInUnreadableKeys)
		}
	})
}
