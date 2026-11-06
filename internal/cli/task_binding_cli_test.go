package cli

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/taskflow"
)

const (
	bindingSmallPlan       = `{"title":"Inspect source with bound advisers","kind":"small","requirements":[{"id":"source","description":"Read source","allow_non_execution":true}],"checks":[]}`
	bindingSubstantialPlan = `{"title":"Decide with bound advisers","kind":"substantial","requirements":[{"id":"source","description":"Inspect actual source","allow_non_execution":true}],"checks":[]}`
)

// taskContextArgs runs cw task context for the before checkpoint (Revision 3).
func taskContextArgs(task string) []string {
	return []string{"task", "context", "--task", task, "--phase", "before"}
}

type bindingFixture struct {
	config      Config
	out, stderr *bytes.Buffer
	home        string
	policy      string // the active release's policy, if one is installed
	environment map[string]string
}

// newBindingFixture isolates a home and serves environment as the credential
// environment. When release is non-zero, the default state directory holds an
// active release whose policy declares that version, with pins (nil keeps the
// shipped providers).
func newBindingFixture(t *testing.T, release int, pins []fixturePin, environment map[string]string) *bindingFixture {
	t.Helper()
	config, out, _, _ := testConfig(t)
	home, repo := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("verified source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := &bindingFixture{out: out, stderr: new(bytes.Buffer), home: home, environment: environment}
	if release != 0 {
		state := filepath.Join(home, ".local", "state", "agent-workflows")
		if err := os.MkdirAll(state, 0o700); err != nil {
			t.Fatal(err)
		}
		fixture.policy = activeReleaseFixture(t, state, release, pins)
	}
	config.Err = fixture.stderr
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	config.Getenv = func(name string) string { return fixture.environment[name] }
	fixture.config = config
	return fixture
}

func (f *bindingFixture) run(t *testing.T, input string, args ...string) error {
	t.Helper()
	f.config.In = strings.NewReader(input)
	f.out.Reset()
	f.stderr.Reset()
	err := Execute(t.Context(), args, f.config)
	if strings.Contains(f.out.String()+f.stderr.String(), "fixture-secret") {
		t.Fatalf("%v printed a credential", args)
	}
	return err
}

func (f *bindingFixture) execute(t *testing.T, input string, args ...string) map[string]any {
	t.Helper()
	if err := f.run(t, input, args...); err != nil {
		t.Fatalf("%v: %v: %s %s", args, err, f.out, f.stderr)
	}
	var document map[string]any
	if err := json.Unmarshal(f.out.Bytes(), &document); err != nil {
		t.Fatalf("%v output is not a JSON object: %v: %s", args, err, f.out)
	}
	return document
}

func (f *bindingFixture) start(t *testing.T, plan string, flags ...string) (taskflow.Record, map[string]any) {
	t.Helper()
	document := f.execute(t, plan, append([]string{"task", "start", "--plan", "-"}, flags...)...)
	var record taskflow.Record
	if err := json.Unmarshal(f.out.Bytes(), &record); err != nil || record.Directory == "" {
		t.Fatalf("task start record: %v: %s", err, f.out)
	}
	return record, document
}

// requireNoTask fails when any task directory exists in the default store.
func (f *bindingFixture) requireNoTask(t *testing.T) {
	t.Helper()
	store := filepath.Join(f.home, ".local", "state", "agent-workflows", "workflow-tasks")
	entries, err := os.ReadDir(store)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a refused task start created %d task directories in %s", len(entries), store)
	}
}

// requireAdviserBinding checks an emitted "advisers" member: a JSON object
// holding exactly want.
func requireAdviserBinding(t *testing.T, label string, got any, want map[string]string) {
	t.Helper()
	binding, isObject := got.(map[string]any)
	if !isObject || len(binding) != len(want) {
		t.Fatalf("%s advisers = %#v, want %v", label, got, want)
	}
	for provider, model := range want {
		if binding[provider] != model {
			t.Fatalf("%s advisers = %v, want %v", label, binding, want)
		}
	}
}

// D5 (binding at start), D3 (configured = in the policy's providers and
// credentialed) and Revision 3 (task start --policy, defaulting to the
// installed release's policy): cw task start binds the providers of the
// selected policy whose credential is present, stores the binding and emits
// it.
func TestTaskStartBindsConfiguredAdvisersFromPolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		pins        []fixturePin // the active release's providers; nil keeps the shipped ones
		explicit    []fixturePin // non-nil passes --policy with these providers
		credentials map[string]string
		want        map[string]string
	}{
		{"snap_credential_only", nil, nil, fixtureCredentials("subq_snap"), map[string]string{"subq_snap": "subq-snap-preview"}},
		{"openai_credential_only", nil, nil, fixtureCredentials("openai_decisions"), map[string]string{"openai_decisions": "gpt-6-luna"}},
		{"no_credentials", nil, nil, nil, map[string]string{}},
		{"every_credential_jev_not_enabled", nil, nil, fixtureCredentials("jev", "subq_snap", "openai_decisions"), map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}},
		{"jev_enabled_and_credentialed", []fixturePin{pinJev, pinSnap}, nil, fixtureCredentials("jev", "openai_decisions"), map[string]string{"jev": "jev-1.14.0"}},
		{"no_enabled_providers", []fixturePin{}, nil, fixtureCredentials("jev", "subq_snap", "openai_decisions"), map[string]string{}},
		{"explicit_policy_overrides_installed", nil, []fixturePin{pinJev}, fixtureCredentials("jev", "subq_snap", "openai_decisions"), map[string]string{"jev": "jev-1.14.0"}},
		{"explicit_policy_without_credentials", nil, []fixturePin{pinJev, pinSnap, pinOpenAI}, nil, map[string]string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBindingFixture(t, 3, test.pins, test.credentials)
			var flags []string
			if test.explicit != nil {
				flags = []string{"--policy", writePolicyFixture(t, filepath.Join(t.TempDir(), "explicit"), 3, test.explicit)}
			}
			record, document := fixture.start(t, bindingSmallPlan, flags...)
			requireAdviserBinding(t, "task start", document["advisers"], test.want)
			if !maps.Equal(record.Advisers, test.want) {
				t.Fatalf("Record.Advisers = %v, want %v", record.Advisers, test.want)
			}
			stored, err := taskflow.Load(record.Directory)
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(stored.Advisers, test.want) {
				t.Fatalf("stored advisers = %v, want %v", stored.Advisers, test.want)
			}
		})
	}
}

// Revision 3 with D1: when the selected policy cannot be read or is invalid,
// including an older version, task start fails before creating any task
// directory and never binds an empty set silently.
func TestTaskStartRefusesUnusablePolicyBeforeCreatingATask(t *testing.T) {
	for _, test := range []struct {
		name     string
		release  int // 0 installs no release
		explicit int // 0 passes no --policy; -1 passes a missing path
		message  string
	}{
		{"no_installed_policy", 0, 0, ""},
		{"installed_policy_version_2", 2, 0, "model policy version 2 is not supported; this cw requires version 3"},
		{"installed_policy_version_1", 1, 0, "model policy version 1 is not supported; this cw requires version 3"},
		{"explicit_policy_version_2", 3, 2, "model policy version 2 is not supported; this cw requires version 3"},
		{"explicit_policy_missing", 3, -1, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBindingFixture(t, test.release, nil, fixtureCredentials("subq_snap", "openai_decisions"))
			args := []string{"task", "start", "--plan", "-"}
			switch {
			case test.explicit > 0:
				args = append(args, "--policy", writePolicyFixture(t, filepath.Join(t.TempDir(), "explicit"), test.explicit, nil))
			case test.explicit < 0:
				args = append(args, "--policy", filepath.Join(t.TempDir(), "missing", "model-policy.yaml"))
			}
			err := fixture.run(t, bindingSmallPlan, args...)
			if err == nil {
				t.Fatalf("task start accepted an unusable policy: %s", fixture.out)
			}
			if text := err.Error() + fixture.out.String() + fixture.stderr.String(); !strings.Contains(text, test.message) {
				t.Fatalf("refusal does not say %q: %s", test.message, text)
			}
			fixture.requireNoTask(t)
		})
	}
}

// D5 (binding immutable for the task's lifetime; cw task context emits the
// binding) with Revision 3 (Context JSON on stdout): after the active policy
// and the credentials change, the stored binding and the emitted context still
// hold the providers bound at start.
func TestTaskContextEmitsTheBindingFromStart(t *testing.T) {
	fixture := newBindingFixture(t, 3, nil, fixtureCredentials("subq_snap"))
	record, _ := fixture.start(t, bindingSubstantialPlan)
	want := map[string]string{"subq_snap": "subq-snap-preview"}
	if !maps.Equal(record.Advisers, want) {
		t.Fatalf("binding at start = %v, want %v", record.Advisers, want)
	}
	// Enable every provider and provide every credential after the start.
	if err := os.WriteFile(fixture.policy, []byte(policyFixtureText(t, 3, []fixturePin{pinJev, pinSnap, pinOpenAI})), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.environment = fixtureCredentials("jev", "subq_snap", "openai_decisions")
	context := fixture.execute(t, "", taskContextArgs(record.Directory)...)
	if context["phase"] != "before" {
		t.Fatalf("context phase = %v", context["phase"])
	}
	requireAdviserBinding(t, "task context", context["advisers"], want)
	stored, err := taskflow.Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(stored.Advisers, want) {
		t.Fatalf("stored binding changed to %v, want %v", stored.Advisers, want)
	}
	// A task started now binds the changed configuration, so the unchanged
	// binding above is not an artifact of the fixture.
	later, _ := fixture.start(t, bindingSubstantialPlan)
	if all := map[string]string{"jev": "jev-1.14.0", "subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}; !maps.Equal(later.Advisers, all) {
		t.Fatalf("a later task bound %v, want %v", later.Advisers, all)
	}
}
