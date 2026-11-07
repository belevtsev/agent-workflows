package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/credentials"
	"github.com/belevtsev/agent-workflows/internal/taskflow"
)

// Contract adviser-switch, revision 4: F1 (task start honours the saved
// preferences' advisers.disabled).

const adviserBindingVersion = 3

const (
	adviserBindingOffOpenAI = `{"version":1,"advisers":{"disabled":["openai_decisions"]}}`
	adviserBindingOffSubQ   = `{"version":1,"advisers":{"disabled":["subq_snap"]}}`
)

// adviserBindingPrivateDir is an owner-only (0700) physical temporary
// directory, as private configuration and task storage require.
func adviserBindingPrivateDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// adviserBindingConfigDir is a fresh --config-dir holding preferences, or no
// preferences file when preferences is empty.
func adviserBindingConfigDir(t *testing.T, preferences string) string {
	t.Helper()
	dir := adviserBindingPrivateDir(t)
	if preferences != "" {
		if err := os.WriteFile(filepath.Join(dir, "preferences.json"), []byte(preferences), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func adviserBindingPins(pins ...fixturePin) map[string]string {
	want := make(map[string]string, len(pins))
	for _, pin := range pins {
		want[pin.provider] = pin.model
	}
	return want
}

// adviserBindingRequire checks the emitted binding, the returned record and
// the stored record.
func adviserBindingRequire(t *testing.T, label string, record taskflow.Record, document map[string]any, want map[string]string) {
	t.Helper()
	requireAdviserBinding(t, label, document["advisers"], want)
	if !maps.Equal(record.Advisers, want) {
		t.Fatalf("%s: record advisers %v, want %v", label, record.Advisers, want)
	}
	loaded, err := taskflow.Load(record.Directory)
	if err != nil {
		t.Fatalf("%s: load %s: %v", label, record.Directory, err)
	}
	if !maps.Equal(loaded.Advisers, want) {
		t.Fatalf("%s: stored advisers %v, want %v", label, loaded.Advisers, want)
	}
}

// adviserBindingRun runs `task start --plan -` with flags and returns the
// emitted JSON document. Failing starts return the error with stderr.
func adviserBindingRun(t *testing.T, f *bindingFixture, flags ...string) (map[string]any, error) {
	t.Helper()
	config := f.config
	var stdout, stderr bytes.Buffer
	config.In = strings.NewReader(bindingSmallPlan)
	config.Out, config.Err = &stdout, &stderr
	if err := Execute(t.Context(), append([]string{"task", "start", "--plan", "-"}, flags...), config); err != nil {
		return nil, fmt.Errorf("%w (stderr %q)", err, stderr.String())
	}
	var document map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("start output %q is not a JSON object: %v", stdout.String(), err)
	}
	return document, nil
}

// adviserBindingDirectories lists the directories below root, relative to it.
func adviserBindingDirectories(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if entry.IsDir() && path != root {
			rel, _ := filepath.Rel(root, path)
			dirs = append(dirs, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

// F1: start never binds a provider the saved preferences disable, with the
// installed policy or a --policy copy; no preferences file disables nothing.
func TestAdviserBindingStartHonoursSavedPreferences(t *testing.T) {
	cases := []struct {
		name        string
		installed   []fixturePin // nil keeps the shipped providers
		policy      []fixturePin // nil starts without --policy
		providers   []string     // credentials set in the environment
		preferences string       // empty: no preferences file
		want        map[string]string
	}{
		{"installed policy, openai disabled", nil, nil,
			[]string{"subq_snap", "openai_decisions"}, adviserBindingOffOpenAI, adviserBindingPins(pinSnap)},
		{"installed policy, subq disabled", nil, nil,
			[]string{"subq_snap", "openai_decisions"}, adviserBindingOffSubQ, adviserBindingPins(pinOpenAI)},
		{"installed policy, no preferences file", nil, nil,
			[]string{"subq_snap", "openai_decisions"}, "", adviserBindingPins(pinSnap, pinOpenAI)},
		{"policy copy, openai disabled", []fixturePin{}, []fixturePin{pinSnap, pinOpenAI},
			[]string{"subq_snap", "openai_decisions"}, adviserBindingOffOpenAI, adviserBindingPins(pinSnap)},
		{"policy copy, subq disabled", []fixturePin{}, []fixturePin{pinSnap, pinOpenAI},
			[]string{"subq_snap", "openai_decisions"}, adviserBindingOffSubQ, adviserBindingPins(pinOpenAI)},
		{"policy copy with jev, jev and openai disabled", []fixturePin{}, []fixturePin{pinJev, pinSnap, pinOpenAI},
			[]string{"jev", "subq_snap", "openai_decisions"},
			`{"version":1,"advisers":{"disabled":["jev","openai_decisions"]}}`, adviserBindingPins(pinSnap)},
		{"policy copy, every provider disabled", []fixturePin{}, []fixturePin{pinSnap, pinOpenAI},
			[]string{"subq_snap", "openai_decisions"},
			`{"version":1,"advisers":{"disabled":["jev","subq_snap","openai_decisions"]}}`, map[string]string{}},
		{"policy copy, no preferences file", []fixturePin{}, []fixturePin{pinSnap, pinOpenAI},
			[]string{"subq_snap", "openai_decisions"}, "", adviserBindingPins(pinSnap, pinOpenAI)},
		{"policy copy, preferences without advisers", []fixturePin{}, []fixturePin{pinSnap, pinOpenAI},
			[]string{"subq_snap", "openai_decisions"}, `{"version":1}`, adviserBindingPins(pinSnap, pinOpenAI)},
		{"policy copy, empty disabled list", []fixturePin{}, []fixturePin{pinSnap, pinOpenAI},
			[]string{"subq_snap", "openai_decisions"}, `{"version":1,"advisers":{"disabled":[]}}`, adviserBindingPins(pinSnap, pinOpenAI)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture := newBindingFixture(t, adviserBindingVersion, c.installed, fixtureCredentials(c.providers...))
			flags := []string{"--config-dir", adviserBindingConfigDir(t, c.preferences)}
			if c.policy != nil {
				flags = append(flags, "--policy", writePolicyFixture(t, t.TempDir(), adviserBindingVersion, c.policy))
			}
			record, document := fixture.start(t, bindingSmallPlan, flags...)
			adviserBindingRequire(t, c.name, record, document, c.want)
		})
	}
}

// F1: the preferences come from the --config-dir given to start. Keys saved
// in that directory (not the environment) show the directory is the one read.
func TestAdviserBindingStartReadsTheGivenConfigDir(t *testing.T) {
	for _, c := range []struct {
		name, preferences string
		want              map[string]string
	}{
		{"no preferences file", "", adviserBindingPins(pinSnap, pinOpenAI)},
		{"openai disabled", adviserBindingOffOpenAI, adviserBindingPins(pinSnap)},
	} {
		t.Run(c.name, func(t *testing.T) {
			fixture := newBindingFixture(t, adviserBindingVersion, nil, map[string]string{})
			dir := adviserBindingConfigDir(t, c.preferences)
			wtStoreKeys(t, dir, map[string]string{
				credentials.SUBQ_API_KEY:   "saved-subq-8801",
				credentials.OPENAI_API_KEY: "saved-openai-8802",
			})
			record, document := fixture.start(t, bindingSmallPlan, "--config-dir", dir)
			adviserBindingRequire(t, c.name, record, document, c.want)
		})
	}
}

// F1: a preferences file that exists but cannot be read or decoded fails
// start before any task directory is created; there is no fallback.
func TestAdviserBindingStartRefusesBadPreferences(t *testing.T) {
	creds := fixtureCredentials("subq_snap", "openai_decisions")

	// Control: the same command with a readable configuration starts a task
	// in the given store, which fixes how deep a task directory sits.
	control := newBindingFixture(t, adviserBindingVersion, nil, creds)
	controlStore := adviserBindingPrivateDir(t)
	document, err := adviserBindingRun(t, control, "--store", controlStore, "--config-dir", adviserBindingConfigDir(t, adviserBindingOffOpenAI))
	if err != nil {
		t.Fatalf("control start: %v", err)
	}
	directory, _ := document["directory"].(string)
	rel, err := filepath.Rel(controlStore, directory)
	if directory == "" || err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		t.Fatalf("control task directory %q is not inside the store %s", directory, controlStore)
	}
	depth := len(strings.Split(rel, string(filepath.Separator)))
	requireAdviserBinding(t, "control", document["advisers"], adviserBindingPins(pinSnap))

	cases := []struct {
		name       string
		content    string // written as preferences.json
		unreadable bool   // preferences.json is a directory instead
		policy     bool
	}{
		{name: "truncated JSON", content: `{"version":1,"advisers":`},
		{name: "unknown adviser", content: `{"version":1,"advisers":{"disabled":["bogus_adviser"]}}`},
		{name: "duplicate adviser", content: `{"version":1,"advisers":{"disabled":["subq_snap","subq_snap"]}}`},
		{name: "unknown member", content: `{"version":1,"advisers":{"enabled":["jev"]}}`},
		{name: "unreadable", unreadable: true},
		{name: "unknown adviser with a policy copy", content: `{"version":1,"advisers":{"disabled":["bogus_adviser"]}}`, policy: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture := newBindingFixture(t, adviserBindingVersion, nil, creds)
			dir := adviserBindingPrivateDir(t)
			path := filepath.Join(dir, "preferences.json")
			if c.unreadable {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("precondition: %s missing: %v", path, err)
			}
			store := adviserBindingPrivateDir(t)
			flags := []string{"--store", store, "--config-dir", dir}
			if c.policy {
				flags = append(flags, "--policy", writePolicyFixture(t, t.TempDir(), adviserBindingVersion, []fixturePin{pinSnap, pinOpenAI}))
			}
			document, err := adviserBindingRun(t, fixture, flags...)
			if err == nil {
				t.Fatalf("start succeeded with bad preferences; advisers %v", document["advisers"])
			}
			for _, d := range adviserBindingDirectories(t, store) {
				if len(strings.Split(d, string(filepath.Separator))) >= depth {
					t.Fatalf("failed start left directory %s in the store (task directories sit %d deep)", d, depth)
				}
			}
		})
	}
}

// ---- F2 / F2a: cw consult honours the saved preferences when ad hoc ----

// adviserBindingConsult runs a dry-run consult of a request with the given
// state under the shipped providers, both keys set, and --config-dir dir. It
// returns the outcome and the output path.
func adviserBindingConsult(t *testing.T, state, dir string) (consultOutcome, string) {
	t.Helper()
	return adviserBindingConsultWith(t, state, dir, "subq_snap", "openai_decisions")
}

// adviserBindingAllKeys are the providers whose keys G3 sets: all three, the
// case in which the credential store skips its own directory check.
var adviserBindingAllKeys = []string{"jev", "subq_snap", "openai_decisions"}

// adviserBindingConsultWith is adviserBindingConsult with keys set for the
// given providers.
func adviserBindingConsultWith(t *testing.T, state, dir string, providers ...string) (consultOutcome, string) {
	t.Helper()
	directory := t.TempDir()
	policy := writePolicyFixture(t, filepath.Join(directory, "policy"), adviserBindingVersion, nil)
	request := writeFixtureFile(t, directory, "request.json", fixtureRequest(state))
	output := filepath.Join(directory, "preview.json")
	getenv := fixtureEnvironment(fixtureCredentials(providers...))
	outcome := executeConsult(t, getenv, "--request", request, "--output", output, "--policy", policy, "--dry-run", "--config-dir", dir)
	return outcome, output
}

// adviserBindingPreview requires a successful dry-run record and returns each
// provider's preview status.
func adviserBindingPreview(t *testing.T, outcome consultOutcome, output string) map[string]string {
	t.Helper()
	if outcome.err != nil {
		t.Fatalf("dry run failed: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
	}
	record, data := readRecordFixture(t, output)
	if strings.Contains(string(data), "fixture-secret") || strings.Contains(outcome.text(), "fixture-secret") {
		t.Fatal("dry run exposed a credential")
	}
	if record["status"] != "dry_run" {
		t.Fatalf("preview status = %v", record["status"])
	}
	providers, isMap := record["providers"].(map[string]any)
	if !isMap {
		t.Fatalf("preview providers = %v", record["providers"])
	}
	statuses := make(map[string]string, len(providers))
	for provider, value := range providers {
		entry, _ := value.(map[string]any)
		status, _ := entry["status"].(string)
		statuses[provider] = status
	}
	return statuses
}

func adviserBindingRequireStatuses(t *testing.T, got, want map[string]string) {
	t.Helper()
	for provider, status := range want {
		if got[provider] != status {
			t.Fatalf("preview %s status = %q, want %q (all %v)", provider, got[provider], status, got)
		}
	}
	for provider, status := range got {
		if _, wanted := want[provider]; !wanted && status == "dry_run" {
			t.Fatalf("preview dispatches to %s, which is not expected (all %v)", provider, got)
		}
	}
}

// F2a: an ad hoc dry run previews a disabled provider as not_configured, never
// dry_run, even with its key set; no preferences file disables nothing.
func TestAdviserBindingAdHocConsultSkipsDisabledProviders(t *testing.T) {
	for _, c := range []struct {
		name, preferences string
		want              map[string]string
	}{
		{"openai disabled", adviserBindingOffOpenAI,
			map[string]string{"subq_snap": "dry_run", "openai_decisions": "not_configured"}},
		{"subq disabled", adviserBindingOffSubQ,
			map[string]string{"subq_snap": "not_configured", "openai_decisions": "dry_run"}},
		{"every provider disabled", `{"version":1,"advisers":{"disabled":["jev","subq_snap","openai_decisions"]}}`,
			map[string]string{"subq_snap": "not_configured", "openai_decisions": "not_configured"}},
		{"no preferences file", "",
			map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"}},
		{"preferences without advisers", `{"version":1}`,
			map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			outcome, output := adviserBindingConsult(t, `"evidence"`, adviserBindingConfigDir(t, c.preferences))
			adviserBindingRequireStatuses(t, adviserBindingPreview(t, outcome, output), c.want)
		})
	}
}

// F2a: a request carrying a task context keeps its bound providers' statuses,
// whatever the preferences say.
func TestAdviserBindingTaskBoundConsultKeepsBinding(t *testing.T) {
	for _, c := range []struct {
		name, state, preferences string
		want                     map[string]string
	}{
		{"both bound, openai disabled",
			`{"task_context":{"advisers":{"subq_snap":"subq-snap-preview","openai_decisions":"gpt-6-luna"}}}`,
			adviserBindingOffOpenAI,
			map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"}},
		{"openai bound, openai disabled",
			`{"task_context":{"advisers":{"openai_decisions":"gpt-6-luna"}}}`,
			adviserBindingOffOpenAI,
			map[string]string{"openai_decisions": "dry_run"}},
		{"both bound, every provider disabled",
			`{"task_context":{"advisers":{"subq_snap":"subq-snap-preview","openai_decisions":"gpt-6-luna"}}}`,
			`{"version":1,"advisers":{"disabled":["jev","subq_snap","openai_decisions"]}}`,
			map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			outcome, output := adviserBindingConsult(t, c.state, adviserBindingConfigDir(t, c.preferences))
			got := adviserBindingPreview(t, outcome, output)
			adviserBindingRequireStatuses(t, got, c.want)
			if len(got) != len(c.want) {
				t.Fatalf("preview providers %v, want exactly %v", got, c.want)
			}
		})
	}
}

// F2a: an invalid preferences file makes an ad hoc consult fail before it
// writes the output.
func TestAdviserBindingAdHocConsultRefusesBadPreferences(t *testing.T) {
	// Control: the same command with a valid file writes a preview.
	outcome, output := adviserBindingConsult(t, `"evidence"`, adviserBindingConfigDir(t, adviserBindingOffOpenAI))
	adviserBindingPreview(t, outcome, output)

	for _, c := range []struct {
		name, content string
		unreadable    bool
	}{
		{name: "truncated JSON", content: `{"version":1,"advisers":`},
		{name: "unknown adviser", content: `{"version":1,"advisers":{"disabled":["bogus_adviser"]}}`},
		{name: "duplicate adviser", content: `{"version":1,"advisers":{"disabled":["subq_snap","subq_snap"]}}`},
		{name: "unknown member", content: `{"version":1,"advisers":{"enabled":["jev"]}}`},
		{name: "unreadable", unreadable: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := adviserBindingPrivateDir(t)
			path := filepath.Join(dir, "preferences.json")
			if c.unreadable {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			outcome, output := adviserBindingConsult(t, `"evidence"`, dir)
			if outcome.err == nil {
				t.Fatalf("ad hoc consult succeeded with bad preferences: %s", outcome.text())
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("failed consult left output %s (stat error %v)", output, err)
			}
		})
	}
}

// ---- Revision 5: G1-G3 ----

const (
	adviserBindingBoundOpenAI = `{"task_context":{"advisers":{"openai_decisions":"gpt-6-luna"}}}`
	adviserBindingOffAll      = `{"version":1,"advisers":{"disabled":["jev","subq_snap","openai_decisions"]}}`
)

// adviserBindingRequireTaskBound requires a task-bound dry run to succeed
// (in particular, not exit 2 for a missing bound key) and to preview exactly
// the bound providers with want's statuses.
func adviserBindingRequireTaskBound(t *testing.T, state, preferences string, want map[string]string) {
	t.Helper()
	outcome, output := adviserBindingConsult(t, state, adviserBindingConfigDir(t, preferences))
	if ExitCode(outcome.err) == 2 {
		t.Fatalf("task-bound dry run exited 2 although every bound key is set: %v: %s", outcome.err, outcome.text())
	}
	got := adviserBindingLenientPreview(t, outcome, output)
	adviserBindingRequireStatuses(t, got, want)
	if len(got) != len(want) {
		t.Fatalf("preview providers %v, want exactly %v", got, want)
	}
}

// adviserBindingLenientPreview is adviserBindingPreview for records that echo
// a request holding a lone surrogate: encoding/json (v1) accepts it where the
// strict readRecordFixture decoder does not.
func adviserBindingLenientPreview(t *testing.T, outcome consultOutcome, output string) map[string]string {
	t.Helper()
	if outcome.err != nil {
		t.Fatalf("dry run failed: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "fixture-secret") || strings.Contains(outcome.text(), "fixture-secret") {
		t.Fatal("dry run exposed a credential")
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("record is not a JSON object: %v", err)
	}
	if record["status"] != "dry_run" {
		t.Fatalf("preview status = %v", record["status"])
	}
	providers, isMap := record["providers"].(map[string]any)
	if !isMap {
		t.Fatalf("preview providers = %v", record["providers"])
	}
	statuses := make(map[string]string, len(providers))
	for provider, value := range providers {
		entry, _ := value.(map[string]any)
		status, _ := entry["status"].(string)
		statuses[provider] = status
	}
	return statuses
}

// G1: consult alone decides whether a request is task-bound; a task-bound
// state carrying a lone surrogate escape keeps its binding although the
// preferences disable the bound provider.
func TestAdviserBindingTaskBoundConsultWithLoneSurrogate(t *testing.T) {
	for _, c := range []struct {
		name, state, preferences string
		want                     map[string]string
	}{
		{"control, plain string in task_context",
			`{"task_context":{"advisers":{"openai_decisions":"gpt-6-luna"},"note":"plain"}}`,
			adviserBindingOffOpenAI, map[string]string{"openai_decisions": "dry_run"}},
		{"surrogate in task_context",
			`{"task_context":{"advisers":{"openai_decisions":"gpt-6-luna"},"note":"\ud800"}}`,
			adviserBindingOffOpenAI, map[string]string{"openai_decisions": "dry_run"}},
		{"control, plain string beside task_context",
			`{"task_context":{"advisers":{"openai_decisions":"gpt-6-luna"}},"note":"plain"}`,
			adviserBindingOffOpenAI, map[string]string{"openai_decisions": "dry_run"}},
		{"surrogate beside task_context",
			`{"task_context":{"advisers":{"openai_decisions":"gpt-6-luna"}},"note":"\ud800"}`,
			adviserBindingOffOpenAI, map[string]string{"openai_decisions": "dry_run"}},
		{"surrogate, both bound and every provider disabled",
			`{"task_context":{"advisers":{"subq_snap":"subq-snap-preview","openai_decisions":"gpt-6-luna"}},"note":"x\ud800y"}`,
			adviserBindingOffAll, map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			adviserBindingRequireTaskBound(t, c.state, c.preferences, c.want)
		})
	}
}

// G2: task_contexts (plural) keeps a binding the preferences disable, like
// task_context.
func TestAdviserBindingTaskContextsKeepBinding(t *testing.T) {
	for _, c := range []struct {
		name, state, preferences string
		want                     map[string]string
	}{
		{"openai bound twice, openai disabled",
			`{"task_contexts":[{"advisers":{"openai_decisions":"gpt-6-luna"}},{"advisers":{"openai_decisions":"gpt-6-luna"}}]}`,
			adviserBindingOffOpenAI, map[string]string{"openai_decisions": "dry_run"}},
		{"both bound, every provider disabled",
			`{"task_contexts":[{"advisers":{"subq_snap":"subq-snap-preview","openai_decisions":"gpt-6-luna"}},{"advisers":{"subq_snap":"subq-snap-preview","openai_decisions":"gpt-6-luna"}}]}`,
			adviserBindingOffAll, map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"}},
		{"one context, subq disabled",
			`{"task_contexts":[{"advisers":{"subq_snap":"subq-snap-preview"}}]}`,
			adviserBindingOffSubQ, map[string]string{"subq_snap": "dry_run"}},
		{"surrogate beside task_contexts, openai disabled",
			`{"task_contexts":[{"advisers":{"openai_decisions":"gpt-6-luna"}}],"note":"\ud800"}`,
			adviserBindingOffOpenAI, map[string]string{"openai_decisions": "dry_run"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			adviserBindingRequireTaskBound(t, c.state, c.preferences, c.want)
		})
	}
}

// adviserBindingRequireStartBoth requires task start with dir to succeed and
// bind both shipped providers; it returns the task directory's depth in store.
func adviserBindingRequireStartBoth(t *testing.T, dir, store string) int {
	t.Helper()
	fixture := newBindingFixture(t, adviserBindingVersion, nil, fixtureCredentials(adviserBindingAllKeys...))
	document, err := adviserBindingRun(t, fixture, "--store", store, "--config-dir", dir)
	if err != nil {
		t.Fatalf("task start with config dir %s: %v", dir, err)
	}
	want := adviserBindingPins(pinSnap, pinOpenAI)
	requireAdviserBinding(t, "start", document["advisers"], want)
	directory, _ := document["directory"].(string)
	loaded, err := taskflow.Load(directory)
	if err != nil {
		t.Fatalf("load %q: %v", directory, err)
	}
	if !maps.Equal(loaded.Advisers, want) {
		t.Fatalf("stored advisers %v, want %v", loaded.Advisers, want)
	}
	rel, err := filepath.Rel(store, directory)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		t.Fatalf("task directory %q is not inside the store %s", directory, store)
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

func adviserBindingRequireConsultBoth(t *testing.T, dir string) {
	t.Helper()
	outcome, output := adviserBindingConsultWith(t, `"evidence"`, dir, adviserBindingAllKeys...)
	got := adviserBindingPreview(t, outcome, output)
	adviserBindingRequireStatuses(t, got, map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"})
	if status, present := got["jev"]; present {
		t.Fatalf("preview lists jev (%s), which the shipped policy does not enable", status)
	}
}

// G3: with no preferences.json, the config directory's permissions are not
// checked, so start and consult work with keys in the environment.
func TestAdviserBindingNoPreferencesFileSkipsDirectoryCheck(t *testing.T) {
	for _, c := range []struct {
		name string
		dir  func(t *testing.T) string
	}{
		{"mode 0755", func(t *testing.T) string {
			dir := adviserBindingPrivateDir(t)
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		{"symlinked", func(t *testing.T) string {
			target := adviserBindingPrivateDir(t)
			link := filepath.Join(adviserBindingPrivateDir(t), "config")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			return link
		}},
		{"missing directory", func(t *testing.T) string {
			return filepath.Join(adviserBindingPrivateDir(t), "missing")
		}},
	} {
		t.Run(c.name+"/start", func(t *testing.T) {
			adviserBindingRequireStartBoth(t, c.dir(t), adviserBindingPrivateDir(t))
		})
		t.Run(c.name+"/consult", func(t *testing.T) {
			adviserBindingRequireConsultBoth(t, c.dir(t))
		})
	}
}

// G3: once preferences.json exists in a 0755 config directory, it is read
// under the private-directory rules, so start and consult both fail.
func TestAdviserBindingPreferencesInOpenDirectoryFail(t *testing.T) {
	// Control in a private directory: how deep a task directory sits in the
	// store, and that the same file content is accepted there.
	depth := adviserBindingRequireStartBoth(t, adviserBindingPrivateDir(t), adviserBindingPrivateDir(t))

	open := func(t *testing.T, content string) string {
		t.Helper()
		dir := adviserBindingPrivateDir(t)
		if err := os.WriteFile(filepath.Join(dir, "preferences.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	for _, c := range []struct{ name, content string }{
		{"valid document", `{"version":1}`},
		{"openai disabled", adviserBindingOffOpenAI},
	} {
		t.Run(c.name+"/start", func(t *testing.T) {
			dir := open(t, c.content)
			fixture := newBindingFixture(t, adviserBindingVersion, nil, fixtureCredentials(adviserBindingAllKeys...))
			store := adviserBindingPrivateDir(t)
			document, err := adviserBindingRun(t, fixture, "--store", store, "--config-dir", dir)
			if err == nil {
				t.Fatalf("task start read preferences from a 0755 directory; advisers %v", document["advisers"])
			}
			for _, d := range adviserBindingDirectories(t, store) {
				if len(strings.Split(d, string(filepath.Separator))) >= depth {
					t.Fatalf("failed start left directory %s in the store", d)
				}
			}
		})
		t.Run(c.name+"/consult", func(t *testing.T) {
			outcome, output := adviserBindingConsultWith(t, `"evidence"`, open(t, c.content), adviserBindingAllKeys...)
			if outcome.err == nil {
				t.Fatalf("consult read preferences from a 0755 directory: %s", outcome.text())
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("failed consult left output %s (stat error %v)", output, err)
			}
		})
	}
}

// ---- Revision 6: H1 ----

// adviserBindingUnusablePreferences are config dirs whose preferences.json
// cannot be used: H1 says only an ad hoc consult reads (and fails on) them.
var adviserBindingUnusablePreferences = []struct {
	name string
	dir  func(t *testing.T) string
}{
	{"truncated", func(t *testing.T) string {
		return adviserBindingConfigDir(t, `{"version":1,"advisers":`)
	}},
	{"unknown ID", func(t *testing.T) string {
		return adviserBindingConfigDir(t, `{"version":1,"advisers":{"disabled":["bogus_adviser"]}}`)
	}},
	{"valid file in a 0755 dir", func(t *testing.T) string {
		dir := adviserBindingConfigDir(t, adviserBindingOffOpenAI)
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}},
	{"valid file in a symlinked dir", func(t *testing.T) string {
		link := filepath.Join(adviserBindingPrivateDir(t), "config")
		if err := os.Symlink(adviserBindingConfigDir(t, adviserBindingOffOpenAI), link); err != nil {
			t.Fatal(err)
		}
		return link
	}},
}

// H1: a task-bound consult never reads the saved preferences, so it succeeds
// and keeps its binding however unusable they are.
func TestAdviserBindingTaskBoundConsultIgnoresUnusablePreferences(t *testing.T) {
	states := []struct {
		name, state string
		want        map[string]string
	}{
		{"task_context",
			`{"task_context":{"advisers":{"subq_snap":"subq-snap-preview","openai_decisions":"gpt-6-luna"}}}`,
			map[string]string{"subq_snap": "dry_run", "openai_decisions": "dry_run"}},
		{"task_contexts",
			`{"task_contexts":[{"advisers":{"openai_decisions":"gpt-6-luna"}},{"advisers":{"openai_decisions":"gpt-6-luna"}}]}`,
			map[string]string{"openai_decisions": "dry_run"}},
	}
	for _, p := range adviserBindingUnusablePreferences {
		for _, s := range states {
			t.Run(p.name+"/"+s.name, func(t *testing.T) {
				outcome, output := adviserBindingConsultWith(t, s.state, p.dir(t), adviserBindingAllKeys...)
				got := adviserBindingLenientPreview(t, outcome, output)
				adviserBindingRequireStatuses(t, got, s.want)
				if len(got) != len(s.want) {
					t.Fatalf("preview providers %v, want exactly %v", got, s.want)
				}
			})
		}
	}
}

// H1: an ad hoc consult in the same cases fails before it writes any output.
func TestAdviserBindingAdHocConsultFailsOnUnusablePreferences(t *testing.T) {
	// Control: the same command with usable preferences writes a preview.
	outcome, output := adviserBindingConsultWith(t, `"evidence"`, adviserBindingConfigDir(t, adviserBindingOffOpenAI), adviserBindingAllKeys...)
	adviserBindingRequireStatuses(t, adviserBindingPreview(t, outcome, output),
		map[string]string{"subq_snap": "dry_run", "openai_decisions": "not_configured"})

	for _, p := range adviserBindingUnusablePreferences {
		t.Run(p.name, func(t *testing.T) {
			outcome, output := adviserBindingConsultWith(t, `"evidence"`, p.dir(t), adviserBindingAllKeys...)
			if outcome.err == nil {
				t.Fatalf("ad hoc consult succeeded with unusable preferences: %s", outcome.text())
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("failed consult left output %s (stat error %v)", output, err)
			}
		})
	}
}

func wtStoreKeys(t *testing.T, dir string, keys map[string]string) {
	t.Helper()
	store := credentials.Store{Directory: dir}
	snapshot, err := store.Read()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read credential store: %v", err)
	}
	generation := snapshot.Generation
	changes := make(map[string]*string, len(keys))
	for name, value := range keys {
		changes[name] = &value
	}
	if _, err := store.Save(t.Context(), changes, &generation); err != nil {
		t.Fatalf("seed credential store: %v", err)
	}
}
