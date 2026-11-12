package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
)

// fixtureSkillSource holds the shipped task-orchestration policy (D1) and its
// references.
var fixtureSkillSource = filepath.Join("..", "..", "skills", "task-orchestration")

// fixturePin is one consultation.providers entry (D1): provider ID and pinned
// model.
type fixturePin struct{ provider, model string }

var (
	pinJev    = fixturePin{"jev", "jev-1.14.0"}
	pinSnap   = fixturePin{"subq_snap", "subq-snap-preview"}
	pinOpenAI = fixturePin{"openai_decisions", "gpt-6-luna"}
)

// shippedPolicyPath is the shipped version-3 policy (D1).
var shippedPolicyPath = filepath.Join(fixtureSkillSource, "model-policy.yaml")

// fixtureCredentialVariable is the D3 credential variable of each provider.
var fixtureCredentialVariable = map[string]string{
	"jev": "TYPESAFE_API_KEY", "subq_snap": "SUBQ_API_KEY", "openai_decisions": "OPENAI_API_KEY",
}

// fixtureProviderOrder is the D3 fixed provider order.
var fixtureProviderOrder = []string{"jev", "subq_snap", "openai_decisions"}

// rewriteProviders replaces the shipped consultation.providers mapping (block
// or flow style) with pins, written in flow style in the given order.
func rewriteProviders(text string, pins []fixturePin) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	indent := func(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }
	ignorable := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		return trimmed == "" || strings.HasPrefix(trimmed, "#")
	}
	start := slices.IndexFunc(lines, func(line string) bool {
		return indent(line) == 0 && strings.TrimSpace(line) == "consultation:"
	})
	if start < 0 {
		return "", errors.New("the shipped policy has no top-level consultation mapping")
	}
	end, child := len(lines), -1
	for index := start + 1; index < len(lines); index++ {
		if ignorable(lines[index]) {
			continue
		}
		if indent(lines[index]) == 0 {
			end = index
			break
		}
		if child < 0 {
			child = indent(lines[index])
		}
	}
	at := -1
	for index := start + 1; index < end; index++ {
		if !ignorable(lines[index]) && indent(lines[index]) == child && strings.HasPrefix(strings.TrimSpace(lines[index]), "providers:") {
			if at >= 0 {
				return "", errors.New("the shipped policy repeats consultation.providers")
			}
			at = index
		}
	}
	if at < 0 {
		return "", errors.New("the shipped policy has no consultation.providers")
	}
	stop := at + 1
	if rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[at]), "providers:")); rest == "" || strings.HasPrefix(rest, "#") {
		for stop < end && (strings.TrimSpace(lines[stop]) == "" || indent(lines[stop]) > child) {
			stop++
		}
	}
	entries := make([]string, len(pins))
	for index, pin := range pins {
		entries[index] = pin.provider + ": " + pin.model
	}
	replacement := strings.Repeat(" ", child) + "providers: {" + strings.Join(entries, ", ") + "}\n"
	return strings.Join(lines[:at], "") + replacement + strings.Join(lines[stop:], ""), nil
}

var fixturePolicyVersion = regexp.MustCompile(`(?m)^version:[ \t]*3[ \t]*(#.*)?$`)

// policyFixtureText returns the shipped version-3 policy, with its version
// changed to version and, when pins is non-nil, its providers replaced.
func policyFixtureText(t *testing.T, version int, pins []fixturePin) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureSkillSource, "model-policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if matches := fixturePolicyVersion.FindAllStringIndex(text, -1); len(matches) != 1 {
		t.Fatalf("the shipped policy does not declare version 3 exactly once (D1): %d matches", len(matches))
	}
	if pins != nil {
		if text, err = rewriteProviders(text, pins); err != nil {
			t.Fatal(err)
		}
	}
	if version != 3 {
		text = fixturePolicyVersion.ReplaceAllLiteralString(text, "version: "+strconv.Itoa(version))
	}
	return text
}

// writePolicyFixture writes a policy and a copy of the shipped references into
// dir, returning the policy path.
func writePolicyFixture(t *testing.T, dir string, version int, pins []fixturePin) string {
	t.Helper()
	references := filepath.Join(fixtureSkillSource, "references")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(references)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(references, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "references", entry.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	policy := filepath.Join(dir, "model-policy.yaml")
	if err := os.WriteFile(policy, []byte(policyFixtureText(t, version, pins)), 0o600); err != nil {
		t.Fatal(err)
	}
	return policy
}

// personalPolicyFixture writes the given policy as the personal
// task-orchestration skill's, <home>/.agents/skills/task-orchestration/model-policy.yaml,
// which consult and task start read when no policy is selected, and returns
// the policy path.
func personalPolicyFixture(t *testing.T, home string, version int, pins []fixturePin) string {
	t.Helper()
	return writePolicyFixture(t, filepath.Join(home, ".agents", "skills", "task-orchestration"), version, pins)
}

// fixtureQuestions is one choice question that includes insufficient_evidence,
// written with sorted keys so fixtureRequest("\"evidence\"") is canonical.
const fixtureQuestions = `{"claim":{"criteria":{"contradicted":"The cited source contradicts the claim.","insufficient_evidence":"The cited evidence does not settle the claim.","supported":"The cited source supports the claim."},"instructions":"Does the cited source support the claim?","type":"choice"}}`

func fixtureRequest(state string) string {
	return `{"questions":` + fixtureQuestions + `,"state":` + state + `}`
}

func writeFixtureFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixtureEnvironment serves only the given variables; values is read on every
// call, so later changes to it are visible.
func fixtureEnvironment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// fixtureCredentials sets a fake credential for each provider. No test lets a
// set credential reach a request: they are used for dry runs and bindings.
func fixtureCredentials(providers ...string) map[string]string {
	values := map[string]string{}
	for _, provider := range providers {
		values[fixtureCredentialVariable[provider]] = "fixture-secret-" + provider
	}
	return values
}

type consultOutcome struct {
	err            error
	stdout, stderr string
}

func (o consultOutcome) text() string {
	message := ""
	if o.err != nil {
		message = o.err.Error()
	}
	return message + "\n" + o.stdout + "\n" + o.stderr
}

func executeConsult(t *testing.T, getenv func(string) string, args ...string) consultOutcome {
	t.Helper()
	config, out := testConfig(t)
	stderr := new(bytes.Buffer)
	config.Err = stderr
	config.Getenv = getenv
	err := Execute(t.Context(), append([]string{"consult"}, args...), config)
	return consultOutcome{err: err, stdout: out.String(), stderr: stderr.String()}
}

func requireNotWritten(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s was written: %v", path, err)
	}
}

func readRecordFixture(t *testing.T, path string) (map[string]any, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("record is not a JSON object: %v: %s", err, data)
	}
	return record, data
}

// memberKeys returns, in document order, the keys of the object held by a
// top-level member.
func memberKeys(t *testing.T, data []byte, member string) []string {
	t.Helper()
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.ReadToken(); err != nil || token.Kind() != '{' {
		t.Fatalf("record is not an object: %v", err)
	}
	for decoder.PeekKind() != '}' {
		name, err := decoder.ReadToken()
		if err != nil {
			t.Fatal(err)
		}
		if name.String() != member {
			if err := decoder.SkipValue(); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if token, err := decoder.ReadToken(); err != nil || token.Kind() != '{' {
			t.Fatalf("record member %q is not an object: %v", member, err)
		}
		keys := []string{}
		for decoder.PeekKind() != '}' {
			key, err := decoder.ReadToken()
			if err != nil {
				t.Fatal(err)
			}
			keys = append(keys, key.String())
			if err := decoder.SkipValue(); err != nil {
				t.Fatal(err)
			}
		}
		return keys
	}
	t.Fatalf("record has no %q member", member)
	return nil
}

// orderedProviders returns the providers of pins in the D3 fixed order.
func orderedProviders(pins []fixturePin) []string {
	providers := []string{}
	for _, provider := range fixtureProviderOrder {
		if slices.ContainsFunc(pins, func(pin fixturePin) bool { return pin.provider == provider }) {
			providers = append(providers, provider)
		}
	}
	return providers
}

func modelOf(pins []fixturePin, provider string) string {
	for _, pin := range pins {
		if pin.provider == provider {
			return pin.model
		}
	}
	return ""
}

var lowerHexSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// D3 (Removed): --with-openai and --without-openai are unknown flags, a usage
// error raised before credentials are read or a record is written, in every
// consult mode.
func TestConsultRejectsRemovedOpenAIFlags(t *testing.T) {
	directory := t.TempDir()
	policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, nil)
	request := writeFixtureFile(t, directory, "request.json", fixtureRequest(`"evidence"`))
	decision := writeFixtureFile(t, directory, "decision.json", `{}`)
	for _, flag := range []string{"--with-openai", "--without-openai"} {
		for name, args := range map[string][]string{
			"request":    {"--request", request, "--policy", policy},
			"dry_run":    {"--request", request, "--policy", policy, "--dry-run"},
			"adjudicate": {"--adjudicate", request, "--decision", decision},
		} {
			t.Run(flag+"_"+name, func(t *testing.T) {
				output := filepath.Join(t.TempDir(), "record.json")
				args := append(slices.Clone(args), "--output", output, flag)
				outcome := executeConsult(t, func(string) string { t.Fatal("removed flag read the credential environment"); return "" }, args...)
				if ExitCode(outcome.err) != 2 {
					t.Fatalf("%s accepted: %v (exit %d)", flag, outcome.err, ExitCode(outcome.err))
				}
				if !strings.Contains(outcome.text(), "unknown flag: "+flag) {
					t.Fatalf("%s is not reported as an unknown flag: %s", flag, outcome.text())
				}
				requireNotWritten(t, output)
			})
		}
	}
}

// D3 (participation set, not_configured, record version 4, zero attempted
// providers, comparison no_advisers, exit 0) and Revision 2 (request
// fingerprint): with no credential, every policy provider is not_configured,
// no request is made, and a no_advisers_configured record is written.
func TestConsultWithoutCredentialsWritesNoAdvisersConfiguredRecord(t *testing.T) {
	for _, test := range []struct {
		name string
		pins []fixturePin // nil keeps the shipped providers
		want []fixturePin
	}{
		{"shipped_providers", nil, []fixturePin{pinSnap, pinOpenAI}},
		{"jev_opted_in", []fixturePin{pinOpenAI, pinJev, pinSnap}, []fixturePin{pinJev, pinSnap, pinOpenAI}},
		{"no_providers", []fixturePin{}, []fixturePin{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, test.pins)
			requestText := fixtureRequest(`"evidence"`)
			request := writeFixtureFile(t, directory, "request.json", requestText)
			output := filepath.Join(directory, "record.json")
			outcome := executeConsult(t, fixtureEnvironment(nil), "--request", request, "--output", output, "--policy", policy)
			if outcome.err != nil {
				t.Fatalf("no configured adviser is not exit 0: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
			}
			record, data := readRecordFixture(t, output)
			if record["version"] != float64(4) || record["status"] != "no_advisers_configured" {
				t.Fatalf("record version %v status %v, want 4 no_advisers_configured", record["version"], record["status"])
			}
			digest := sha256.Sum256([]byte(requestText))
			if record["request_sha256"] != hex.EncodeToString(digest[:]) {
				t.Fatalf("request_sha256 = %v, want %s", record["request_sha256"], hex.EncodeToString(digest[:]))
			}
			var parsedRequest any
			if err := json.Unmarshal([]byte(requestText), &parsedRequest); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(record["request"], parsedRequest) {
				t.Fatalf("record request = %v", record["request"])
			}
			wantProviders := orderedProviders(test.want)
			if keys := memberKeys(t, data, "providers"); !slices.Equal(keys, wantProviders) {
				t.Fatalf("provider keys %v, want %v in fixed order", keys, wantProviders)
			}
			providers, _ := record["providers"].(map[string]any)
			for _, provider := range wantProviders {
				entry, _ := providers[provider].(map[string]any)
				keys := slices.Sorted(maps.Keys(entry))
				if !slices.Equal(keys, []string{"duration_seconds", "request_sha256", "requested_model", "status"}) {
					t.Fatalf("%s entry fields %v: %v", provider, keys, entry)
				}
				if entry["status"] != "not_configured" || entry["requested_model"] != modelOf(test.want, provider) || entry["duration_seconds"] != float64(0) {
					t.Fatalf("%s entry = %v", provider, entry)
				}
				if digest, _ := entry["request_sha256"].(string); !lowerHexSHA256.MatchString(digest) {
					t.Fatalf("%s request_sha256 = %v", provider, entry["request_sha256"])
				}
			}
			comparisons, _ := record["comparison"].(map[string]any)
			comparison, _ := comparisons["claim"].(map[string]any)
			wantMissing := map[string]any{}
			for _, provider := range wantProviders {
				wantMissing[provider] = "not_configured"
			}
			choices, isMap := comparison["choices"].(map[string]any)
			if comparison["status"] != "no_advisers" || !isMap || len(choices) != 0 || !reflect.DeepEqual(comparison["missing"], wantMissing) {
				t.Fatalf("comparison = %v, want no_advisers with missing %v", comparison, wantMissing)
			}
			validated, err := consult.ValidateRecord(data)
			if err != nil {
				t.Fatalf("written record does not revalidate: %v", err)
			}
			if validated.Version != 4 || validated.Status != "no_advisers_configured" || validated.DecisionStatus != "pending" || len(validated.Providers) != len(wantProviders) {
				t.Fatalf("validated record = %+v", validated)
			}
			for _, provider := range wantProviders {
				if got := validated.Providers[provider]; got.Status != "not_configured" || got.RequestedModel != modelOf(test.want, provider) {
					t.Fatalf("validated %s = %+v", provider, got)
				}
			}
		})
	}
}

// boundAdviserLine is the Revision 3 stderr line for a bound provider that has
// no credential.
func boundAdviserLine(provider string) string {
	return "Adviser " + provider + " is bound to this task but not configured; set " + fixtureCredentialVariable[provider] + " and consult again."
}

// D3 (dry run, participation set), Revision 2 (batched bindings) and Revision
// 3 (task-bound dry run with an unconfigured bound adviser exits 5): a dry run
// previews each participating provider as dry_run or not_configured, taking
// the participation set from a task binding when the request carries one.
func TestConsultDryRunPreviewsParticipatingProviders(t *testing.T) {
	for _, test := range []struct {
		name        string
		state       string
		credentials map[string]string
		want        map[string][2]string // provider -> requested model, preview status
	}{
		{
			"policy_with_snap_credential", `"evidence"`, fixtureCredentials("subq_snap"),
			map[string][2]string{"subq_snap": {"subq-snap-preview", "dry_run"}, "openai_decisions": {"gpt-6-luna", "not_configured"}},
		},
		{
			"policy_without_credentials", `"evidence"`, nil,
			map[string][2]string{"subq_snap": {"subq-snap-preview", "not_configured"}, "openai_decisions": {"gpt-6-luna", "not_configured"}},
		},
		{
			"task_binding_narrows_policy", `{"task_context":{"advisers":{"openai_decisions":"gpt-6-luna"}}}`, fixtureCredentials("jev", "subq_snap", "openai_decisions"),
			map[string][2]string{"openai_decisions": {"gpt-6-luna", "dry_run"}},
		},
		{
			"task_binding_outside_policy", `{"task_context":{"advisers":{"jev":"jev-1.14.0"}}}`, fixtureCredentials("subq_snap", "openai_decisions"),
			map[string][2]string{"jev": {"jev-1.14.0", "not_configured"}},
		},
		{
			"empty_task_binding", `{"task_context":{"advisers":{}}}`, fixtureCredentials("jev", "subq_snap", "openai_decisions"),
			map[string][2]string{},
		},
		{
			"identical_batched_bindings", `{"task_contexts":[{"advisers":{"subq_snap":"subq-snap-preview"}},{"advisers":{"subq_snap":"subq-snap-preview"}}]}`, fixtureCredentials("subq_snap", "openai_decisions"),
			map[string][2]string{"subq_snap": {"subq-snap-preview", "dry_run"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, nil)
			request := writeFixtureFile(t, directory, "request.json", fixtureRequest(test.state))
			output := filepath.Join(directory, "preview.json")
			outcome := executeConsult(t, fixtureEnvironment(test.credentials), "--request", request, "--output", output, "--policy", policy, "--dry-run")
			// Only a task-bound provider without a credential makes the dry run exit 5.
			var unconfiguredBound []string
			if strings.Contains(test.state, "task_context") {
				for provider, want := range test.want {
					if want[1] == "not_configured" {
						unconfiguredBound = append(unconfiguredBound, provider)
					}
				}
			}
			if len(unconfiguredBound) == 0 && outcome.err != nil {
				t.Fatalf("dry run failed: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
			}
			if len(unconfiguredBound) != 0 && ExitCode(outcome.err) != 5 {
				t.Fatalf("dry run with an unconfigured bound adviser is not exit 5: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
			}
			for _, provider := range unconfiguredBound {
				if !strings.Contains(outcome.stderr, boundAdviserLine(provider)) {
					t.Fatalf("stderr lacks %q: %q", boundAdviserLine(provider), outcome.stderr)
				}
			}
			if strings.Contains(outcome.text(), "fixture-secret") {
				t.Fatal("dry run printed a credential")
			}
			record, data := readRecordFixture(t, output)
			if strings.Contains(string(data), "fixture-secret") {
				t.Fatal("dry run preview stored a credential")
			}
			if record["status"] != "dry_run" {
				t.Fatalf("preview status = %v", record["status"])
			}
			providers, isMap := record["providers"].(map[string]any)
			if !isMap || len(providers) != len(test.want) {
				t.Fatalf("preview providers = %v, want %v", record["providers"], test.want)
			}
			for provider, want := range test.want {
				entry, _ := providers[provider].(map[string]any)
				if entry["requested_model"] != want[0] || entry["status"] != want[1] {
					t.Fatalf("preview %s = %v, want model %s status %s", provider, entry, want[0], want[1])
				}
			}
		})
	}
}

// D3 (participation set, model pin validation, exit 1) and Revision 2
// (missing and differing bindings): an invalid task binding is invalid input,
// so consult exits 1 and writes nothing.
func TestConsultRejectsInvalidTaskBindings(t *testing.T) {
	const missingBinding = "task context has no adviser binding; regenerate it with this cw's 'cw task context'"
	for _, test := range []struct {
		name, state, message string
	}{
		{"context_without_binding", `{"task_context":{"task_id":"fixture"}}`, missingBinding},
		{"batched_context_without_binding", `{"task_contexts":[{"advisers":{"subq_snap":"subq-snap-preview"}},{"task_id":"fixture"}]}`, missingBinding},
		{"differing_batched_bindings", `{"task_contexts":[{"advisers":{"subq_snap":"subq-snap-preview"}},{"advisers":{"openai_decisions":"gpt-6-luna"}}]}`, ""},
		{"jev_alias", `{"task_context":{"advisers":{"jev":"jev-latest"}}}`, ""},
		{"wrong_snap_model", `{"task_context":{"advisers":{"subq_snap":"subq-snap-2"}}}`, ""},
		{"wrong_openai_model", `{"task_context":{"advisers":{"openai_decisions":"gpt-5"}}}`, ""},
		{"unknown_provider", `{"task_context":{"advisers":{"gemini":"gemini-pro"}}}`, ""},
		{"non_string_model", `{"task_context":{"advisers":{"subq_snap":1}}}`, ""},
		{"binding_not_a_mapping", `{"task_context":{"advisers":["subq_snap"]}}`, ""},
		// Revision 3: task_context and task_contexts together are rejected.
		{"single_and_batched_contexts", `{"task_context":{"advisers":{}},"task_contexts":[{"advisers":{}}]}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, nil)
			request := writeFixtureFile(t, directory, "request.json", fixtureRequest(test.state))
			output := filepath.Join(directory, "record.json")
			// No credential is set, so a request that passes validation makes no
			// provider call and would write a record.
			outcome := executeConsult(t, fixtureEnvironment(nil), "--request", request, "--output", output, "--policy", policy)
			if ExitCode(outcome.err) != 1 {
				t.Fatalf("invalid binding accepted: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
			}
			if !strings.Contains(outcome.text(), test.message) {
				t.Fatalf("refusal does not say %q: %s", test.message, outcome.text())
			}
			requireNotWritten(t, output)
		})
	}
}

// D3 (a task-bound request whose bound provider is not_configured is a
// negative result, exit 5) and Revision 3 (the stderr line): stderr names each
// variable to set and the record is still written, without a provider request.
func TestConsultTaskBoundMissingCredentialIsNegativeResult(t *testing.T) {
	for _, test := range []struct {
		name  string
		bound []fixturePin
	}{
		{"snap_bound", []fixturePin{pinSnap}},
		{"jev_and_openai_bound", []fixturePin{pinJev, pinOpenAI}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, nil)
			entries := make([]string, len(test.bound))
			for index, pin := range test.bound {
				entries[index] = strconv.Quote(pin.provider) + ":" + strconv.Quote(pin.model)
			}
			state := `{"task_context":{"advisers":{` + strings.Join(entries, ",") + `}}}`
			request := writeFixtureFile(t, directory, "request.json", fixtureRequest(state))
			output := filepath.Join(directory, "record.json")
			outcome := executeConsult(t, fixtureEnvironment(nil), "--request", request, "--output", output, "--policy", policy)
			if ExitCode(outcome.err) != 5 {
				t.Fatalf("bound provider without a credential is not exit 5: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
			}
			for _, pin := range test.bound {
				if line := boundAdviserLine(pin.provider); !strings.Contains(outcome.stderr, line) {
					t.Fatalf("stderr lacks %q: %q", line, outcome.stderr)
				}
			}
			record, data := readRecordFixture(t, output)
			if record["status"] != "no_advisers_configured" {
				t.Fatalf("record status = %v", record["status"])
			}
			if keys := memberKeys(t, data, "providers"); !slices.Equal(keys, orderedProviders(test.bound)) {
				t.Fatalf("record providers %v, want the binding %v", keys, orderedProviders(test.bound))
			}
			validated, err := consult.ValidateRecord(data)
			if err != nil {
				t.Fatalf("written record does not revalidate: %v", err)
			}
			for _, pin := range test.bound {
				if got := validated.Providers[pin.provider]; got.Status != "not_configured" || got.RequestedModel != pin.model {
					t.Fatalf("%s = %+v", pin.provider, got)
				}
			}
		})
	}
}

// decisionFixture writes a decision document: Revision 3's
// {"questions": {question ID: D4 item}}.
func decisionFixture(t *testing.T, dir string, items map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"questions": items})
	if err != nil {
		t.Fatal(err)
	}
	return writeFixtureFile(t, dir, "decision.json", string(data))
}

func resolvedItem(choice string, evidence, verification []string) map[string]any {
	return map[string]any{
		"status": "resolved", "choice": choice, "reason": "The cited source states the claim directly.",
		"evidence": evidence, "verification": verification,
		"rejected_judgments": map[string]any{}, "investigations": map[string]any{}, "agreement_not_independent": false,
	}
}

func unresolvedItem() map[string]any {
	return map[string]any{
		"status": "unresolved", "choice": nil, "reason": "The available source does not settle the claim.",
		"evidence": []string{}, "verification": []string{},
		"rejected_judgments": map[string]any{}, "investigations": map[string]any{}, "agreement_not_independent": false,
	}
}

var recordVersionMember = regexp.MustCompile(`"version":(\s*)4([\s,}])`)

// D3 (exit codes, record version 4 only) and D4 (adjudication rules applied
// in full with no adviser answer): adjudication of a no_advisers_configured
// record resolves (exit 0) or stays unresolved (exit 5); an older or newer
// record version and adviser output used as evidence are invalid input (exit
// 1) and write nothing.
func TestConsultAdjudicationExitCodesAndRecordVersion(t *testing.T) {
	directory := t.TempDir()
	policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, nil)
	request := writeFixtureFile(t, directory, "request.json", fixtureRequest(`"evidence"`))
	recordPath := filepath.Join(directory, "record.json")
	if outcome := executeConsult(t, fixtureEnvironment(nil), "--request", request, "--output", recordPath, "--policy", policy); outcome.err != nil {
		t.Fatalf("fixture record: %v: %s", outcome.err, outcome.text())
	}
	_, recordData := readRecordFixture(t, recordPath)
	adjudicate := func(t *testing.T, record string, items map[string]any) (consultOutcome, string) {
		t.Helper()
		scratch := t.TempDir()
		decision := decisionFixture(t, scratch, items)
		output := filepath.Join(scratch, "adjudicated.json")
		return executeConsult(t, fixtureEnvironment(nil), "--adjudicate", record, "--decision", decision, "--output", output), output
	}
	resolved := map[string]any{"claim": resolvedItem("supported", []string{"source:docs/claim.md:3-5@0123abc"}, []string{"check:fixture-check@run-1"})}

	t.Run("resolved_exits_zero", func(t *testing.T) {
		outcome, output := adjudicate(t, recordPath, resolved)
		if outcome.err != nil {
			t.Fatalf("resolved adjudication is not exit 0: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
		}
		_, data := readRecordFixture(t, output)
		validated, err := consult.ValidateRecord(data)
		if err != nil || validated.DecisionStatus != "resolved" {
			t.Fatalf("adjudicated record = %+v, %v", validated, err)
		}
	})
	// Revision 3: agreement_not_independent may be true on any resolved item.
	t.Run("resolved_with_agreement_flag_exits_zero", func(t *testing.T) {
		item := resolvedItem("supported", []string{"observation:The cited paragraph states the claim."}, []string{"check:fixture-check"})
		item["agreement_not_independent"] = true
		outcome, output := adjudicate(t, recordPath, map[string]any{"claim": item})
		if outcome.err != nil {
			t.Fatalf("resolved adjudication with agreement_not_independent is not exit 0: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
		}
		_, data := readRecordFixture(t, output)
		if validated, err := consult.ValidateRecord(data); err != nil || validated.DecisionStatus != "resolved" {
			t.Fatalf("adjudicated record = %+v, %v", validated, err)
		}
	})
	t.Run("unresolved_exits_negative", func(t *testing.T) {
		outcome, output := adjudicate(t, recordPath, map[string]any{"claim": unresolvedItem()})
		if ExitCode(outcome.err) != 5 {
			t.Fatalf("unresolved adjudication is not exit 5: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
		}
		_, data := readRecordFixture(t, output)
		validated, err := consult.ValidateRecord(data)
		if err != nil || validated.DecisionStatus != "unresolved" {
			t.Fatalf("adjudicated record = %+v, %v", validated, err)
		}
	})
	for _, version := range []int{1, 2, 3, 5} {
		t.Run("record_version_"+strconv.Itoa(version), func(t *testing.T) {
			if matches := recordVersionMember.FindAllIndex(recordData, -1); len(matches) != 1 {
				t.Fatalf("fixture record holds %d version members", len(matches))
			}
			changed := recordVersionMember.ReplaceAll(recordData, []byte(`"version":${1}`+strconv.Itoa(version)+`${2}`))
			record := writeFixtureFile(t, t.TempDir(), "record.json", string(changed))
			outcome, output := adjudicate(t, record, resolved)
			if ExitCode(outcome.err) != 1 {
				t.Fatalf("record version %d accepted: %v (exit %d): %s", version, outcome.err, ExitCode(outcome.err), outcome.text())
			}
			message := "consultation record version " + strconv.Itoa(version) + " is not supported; this cw reads version 4"
			if !strings.Contains(outcome.text(), message) {
				t.Fatalf("refusal does not say %q: %s", message, outcome.text())
			}
			requireNotWritten(t, output)
		})
	}
	for name, item := range map[string]map[string]any{
		"adviser_output_entry":       resolvedItem("supported", []string{"subq_snap:supported"}, []string{"check:fixture-check"}),
		"observation_naming_adviser": resolvedItem("supported", []string{"observation:SubQ Snap agreed with the claim."}, []string{"check:fixture-check"}),
		// Revision 3: the plural adviser words are rejected too.
		"observation_naming_advisers":  resolvedItem("supported", []string{"observation:The advisers agreed with the claim."}, []string{"check:fixture-check"}),
		"observation_naming_advisors":  resolvedItem("supported", []string{"observation:Both Advisors agreed with the claim."}, []string{"check:fixture-check"}),
		"insufficient_evidence_choice": resolvedItem("insufficient_evidence", []string{"source:docs/claim.md:3@0123abc"}, []string{"check:fixture-check"}),
		"resolved_without_evidence":    resolvedItem("supported", []string{}, []string{"check:fixture-check"}),
	} {
		t.Run(name, func(t *testing.T) {
			outcome, output := adjudicate(t, recordPath, map[string]any{"claim": item})
			if ExitCode(outcome.err) != 1 {
				t.Fatalf("invalid decision accepted: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
			}
			if name == "adviser_output_entry" && !strings.Contains(outcome.text(), "evidence and verification must reference source, checks or observations, not adviser output") {
				t.Fatalf("refusal does not explain the evidence rule: %s", outcome.text())
			}
			requireNotWritten(t, output)
		})
	}
}
