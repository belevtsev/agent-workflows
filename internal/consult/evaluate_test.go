package consult

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Multilingual evaluation (design D3, cmd/cwdev bullet) through Evaluation.Run with
// its collect seam: only configured providers run; others are reported not_run
// with reason not_configured; offline mode is unchanged.

var fxLanguages = []string{"en", "uk", "de"}

func fxEvalSourceDir() string { return filepath.Join("testdata", "evaluation", "source") }

// fxEvalSource copies the evaluation source tree, optionally replacing its policy.
func fxEvalSource(t *testing.T, root, policy string) string {
	t.Helper()
	err := filepath.WalkDir(fxEvalSourceDir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(fxEvalSourceDir(), path)
		if err != nil {
			return err
		}
		fxWrite(t, filepath.Join(root, relative), fxRead(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy != "" {
		fxWrite(t, filepath.Join(root, "skills", "task-orchestration", "model-policy.yaml"), fxRead(t, fxPolicyPath(policy)))
	}
	return root
}

func fxEvaluation(t *testing.T, source string, env func(string) string) (Evaluation, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	return Evaluation{Source: source, Stdout: &stdout, Stderr: &stderr, Getenv: env, Home: func() (string, error) { return home, nil }, Executable: os.Executable}, &stdout, &stderr
}

func fxNativeBytes(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return value
}

func fxContainsText(value any, text string) bool {
	switch item := value.(type) {
	case string:
		return item == text
	case map[string]any:
		for _, member := range item {
			if fxContainsText(member, text) {
				return true
			}
		}
	case []any:
		for _, member := range item {
			if fxContainsText(member, text) {
				return true
			}
		}
	}
	return false
}

// fxReportedAs reports whether any object member named provider holds all texts.
func fxReportedAs(value any, provider string, texts ...string) bool {
	switch item := value.(type) {
	case map[string]any:
		for name, member := range item {
			if name == provider {
				matched := true
				for _, text := range texts {
					matched = matched && fxContainsText(member, text)
				}
				if matched {
					return true
				}
			}
			if fxReportedAs(member, provider, texts...) {
				return true
			}
		}
	case []any:
		for _, member := range item {
			if fxReportedAs(member, provider, texts...) {
				return true
			}
		}
	}
	return false
}

func fxField(value any, path ...string) any {
	for _, name := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[name]
	}
	return value
}

// Offline mode is unchanged: the plan matches the helper's and nothing is read or sent.
func TestEvaluationOfflineMatchesPlan(t *testing.T) {
	t.Parallel()
	run, stdout, stderr := fxEvaluation(t, fxEvalSourceDir(), fxNoEnv(t))
	run.collect = fxNoCollect(t)
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if want := fxTestdata(t, "evaluation", "offline.json"); !bytes.Equal(stdout.Bytes(), want) {
		t.Fatalf("offline plan differs:\n%s\n%s", stdout, want)
	}
}

// fxStableReport drops timing and runner identity, the only parts that differ by run.
func fxStableReport(t *testing.T, data []byte) any {
	t.Helper()
	report := fxNativeBytes(t, data).(map[string]any)
	for _, name := range []string{"started_at", "finished_at", "duration_seconds"} {
		delete(report, name)
	}
	if provenance, ok := report["provenance"].(map[string]any); ok {
		delete(provenance, "runtime")
		if artifacts, ok := provenance["artifacts"].(map[string]any); ok {
			delete(artifacts, "runner")
		}
	}
	if batches, ok := report["batches"].(map[string]any); ok {
		for _, batch := range batches {
			if item, ok := batch.(map[string]any); ok {
				delete(item, "duration_seconds")
			}
		}
	}
	return report
}

// D3: with every provider configured the evaluation is unchanged apart from
// version 4 records and the version 3 policy digest.
func TestEvaluationLiveAllConfiguredMatchesGoldens(t *testing.T) {
	t.Parallel()
	run, stdout, stderr := fxEvaluation(t, fxEvalSourceDir(), fxEnv(fxProviders...))
	scripted := fxParse(t, fxTestdata(t, "evaluation", "raw_records.json")).(*object)
	var mutex sync.Mutex
	calls := 0
	run.collect = func(_ context.Context, prepared *object, options batch) (map[string]any, error) {
		mutex.Lock()
		defer mutex.Unlock()
		if options.deadline != 30*time.Second {
			t.Errorf("deadline = %v", options.deadline)
		}
		if calls >= len(fxLanguages) {
			t.Errorf("unexpected batch %d", calls+1)
			return nil, errors.New("unexpected batch")
		}
		language := fxLanguages[calls]
		calls++
		raw := map[string]any{}
		for _, name := range fxProviders {
			if options.credentials[name] == "" {
				t.Errorf("%s batch lacks the %s credential", language, name)
			}
			raw[name] = scripted.get(language).(*object).get(name).(*object).with("request", prepared.get(name))
		}
		return raw, nil
	}
	run.Live, run.OutputDir = true, filepath.Join(t.TempDir(), "baseline with spaces")
	if code := run.Run(t.Context()); code != 2 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(run.OutputDir))
	if err != nil {
		t.Fatal(err)
	}
	if want := "Synthetic evaluation incomplete; private report: " + filepath.Join(resolved, filepath.Base(run.OutputDir), "report.json") + "\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	for _, name := range []string{"summary.md", "en.record.json", "uk.record.json", "de.record.json"} {
		if got := fxRead(t, filepath.Join(run.OutputDir, name)); !bytes.Equal(got, fxTestdata(t, "evaluation", name)) {
			t.Fatalf("%s differs from golden:\n%s", name, got)
		}
	}
	got := fxStableReport(t, fxRead(t, filepath.Join(run.OutputDir, "report.json")))
	if want := fxNativeBytes(t, fxTestdata(t, "evaluation", "report.json")); !reflect.DeepEqual(got, want) {
		t.Fatalf("report differs:\n%v\n--- want\n%v", got, want)
	}
	entries, err := os.ReadDir(run.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	var want []string
	if err := json.Unmarshal(fxTestdata(t, "evaluation", "files.json"), &want); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, want) {
		t.Fatalf("published files = %v", names)
	}
}

// D3: the live evaluation runs only configured providers (in the policy and with a
// credential); every known provider is reported and non-configured ones are
// not_run/not_configured, are never sent a request, and are not a failure.
func TestEvaluationRunsOnlyConfiguredProviders(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, policy string
		credentials  []string
		configured   []string
	}{
		{"zero", "all", nil, nil},
		{"one", "all", []string{fxSnap}, []string{fxSnap}},
		{"all", "all", fxProviders, fxProviders},
		{"policy subset", "snap_openai", fxProviders, []string{fxSnap, fxOpenAI}},
		{"empty policy", "none", fxProviders, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := fxEvalSource(t, t.TempDir(), test.policy)
			run, _, stderr := fxEvaluation(t, source, fxEnv(test.credentials...))
			server := fxServe(t, fxHandler(t, nil, nil))
			run.collect = server.sender.collect
			run.Live, run.OutputDir = true, filepath.Join(t.TempDir(), "report")
			code := run.Run(t.Context())
			for _, name := range fxProviders {
				want := 0
				if slices.Contains(test.configured, name) {
					want = len(fxLanguages)
				}
				if server.count(name) != want {
					t.Fatalf("%s received %d requests, want %d", name, server.count(name), want)
				}
			}
			if len(test.configured) == 0 {
				if code == 1 || code == 130 {
					t.Fatalf("exit %d with no configured provider: %s", code, stderr)
				}
			} else if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			report := fxNativeBytes(t, fxRead(t, filepath.Join(run.OutputDir, "report.json")))
			if fxField(report, "status") == "error" {
				t.Fatalf("report status = error")
			}
			for _, name := range fxProviders {
				if !fxReportedAs(report, name) {
					t.Fatalf("report omits %s", name)
				}
				notRun := fxReportedAs(report, name, "not_run", "not_configured")
				if configured := slices.Contains(test.configured, name); configured == notRun || (configured && fxReportedAs(report, name, "not_run")) {
					t.Fatalf("%s configured=%v reported not_run=%v", name, configured, notRun)
				}
			}
		})
	}
}

// Unchanged: --live and an output directory are required together.
func TestLiveFlagAndOutputAreRequiredTogether(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		live   bool
		output string
	}{{true, ""}, {false, "/unused"}} {
		run, _, stderr := fxEvaluation(t, t.TempDir(), fxEnv())
		run.collect = fxNoCollect(t)
		run.Live, run.OutputDir = test.live, test.output
		if code := run.Run(t.Context()); code != 1 || stderr.String() != "Evaluation validation or output preflight failed; no exception details retained.\n" {
			t.Fatalf("code %d stderr %q", code, stderr)
		}
	}
}

// Unchanged: an existing output directory is refused before any request.
func TestExistingOutputIsNeverOverwritten(t *testing.T) {
	t.Parallel()
	source := fxEvalSource(t, t.TempDir(), "")
	destination := filepath.Join(t.TempDir(), "existing")
	sentinel := filepath.Join(destination, "report.json")
	fxWrite(t, sentinel, []byte("original"))
	run, _, _ := fxEvaluation(t, source, fxEnv(fxProviders...))
	run.collect = fxNoCollect(t)
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if string(fxRead(t, sentinel)) != "original" {
		t.Fatal("sentinel changed")
	}
}

// Output inside the evaluated skill or its corpus is refused before any request.
func TestOutputInsideSourceIsRejected(t *testing.T) {
	t.Parallel()
	source := fxEvalSource(t, t.TempDir(), "")
	for _, output := range []string{filepath.Join(source, "skills", "task-orchestration", "output"), filepath.Join(source, "evals", "multilingual", "output")} {
		run, _, _ := fxEvaluation(t, source, fxEnv(fxProviders...))
		server := fxServe(t, fxHandler(t, nil, nil))
		run.collect = server.sender.collect
		run.Live, run.OutputDir = true, output
		if code := run.Run(t.Context()); code != 1 || server.total() != 0 {
			t.Fatalf("%s: exit %d after %d requests", output, code, server.total())
		}
		fxAbsent(t, output)
	}
}

// Unchanged: an interrupt keeps completed batches and marks the rest missing.
func TestInterruptRetainsCompletedBatch(t *testing.T) {
	t.Parallel()
	source := fxEvalSource(t, t.TempDir(), "")
	destination := filepath.Join(t.TempDir(), "interrupted")
	run, _, _ := fxEvaluation(t, source, fxEnv(fxProviders...))
	server := fxServe(t, fxHandler(t, nil, nil))
	var mutex sync.Mutex
	calls := 0
	run.collect = func(ctx context.Context, prepared *object, options batch) (map[string]any, error) {
		mutex.Lock()
		calls++
		current := calls
		mutex.Unlock()
		if current == 2 {
			return nil, context.Canceled
		}
		return server.sender.collect(ctx, prepared, options)
	}
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 130 {
		t.Fatalf("exit %d", code)
	}
	report := fxNativeBytes(t, fxRead(t, filepath.Join(destination, "report.json")))
	if fxField(report, "status") != "interrupted" || !reflect.DeepEqual(fxField(report, "execution", "completed_batches"), []any{"en"}) ||
		fxField(report, "scoring", fxJev, "totals", "missing") != float64(24) {
		t.Fatalf("report = %v", report)
	}
	fxAbsent(t, filepath.Join(destination, "uk.record.json"))
	fxRead(t, filepath.Join(destination, "en.record.json"))
}

func fxAllOutput(t *testing.T, directory string) string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, entry := range entries {
		text.Write(fxRead(t, filepath.Join(directory, entry.Name())))
	}
	return text.String()
}

// Unchanged: credential values and raw collector errors are never retained.
func TestCredentialAndRawErrorValuesAreExcluded(t *testing.T) {
	t.Parallel()
	const canary = "SECRET_CANARY_DO_NOT_RETAIN"
	source := fxEvalSource(t, t.TempDir(), "")
	destination := filepath.Join(t.TempDir(), "error")
	run, _, _ := fxEvaluation(t, source, func(string) string { return canary })
	run.collect = func(context.Context, *object, batch) (map[string]any, error) { return nil, errors.New(canary) }
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(fxAllOutput(t, destination), canary) {
		t.Fatal("output retained a secret")
	}
	if fxField(fxNativeBytes(t, fxRead(t, filepath.Join(destination, "report.json"))), "status") != "error" {
		t.Fatal("report does not record the error")
	}
}

// Unchanged: unknown response members are not retained.
func TestUnknownProviderFieldsAreNotRetained(t *testing.T) {
	t.Parallel()
	source := fxEvalSource(t, t.TempDir(), "")
	destination := filepath.Join(t.TempDir(), "sanitized")
	run, _, stderr := fxEvaluation(t, source, fxEnv(fxProviders...))
	server := fxServe(t, func(name string, body []byte, writer http.ResponseWriter, _ *http.Request) {
		data, err := fxAnswer(name, body, fxAgree)
		if err != nil {
			t.Error(err)
		}
		_, _ = writer.Write(append([]byte(`{"internal_trace":"SECRET_CANARY","headers":{"Authorization":"SECRET_CANARY"},`), data[1:]...))
	})
	run.collect = server.sender.collect
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(fxAllOutput(t, destination), "SECRET_CANARY") {
		t.Fatal("unknown fields were retained")
	}
}

// Unchanged: oracle labels and rationales and credentials never enter payloads.
func TestOracleNeverEntersTransportPayloads(t *testing.T) {
	t.Parallel()
	source := fxEvalSource(t, t.TempDir(), "")
	var oracle struct {
		Labels map[string]struct {
			Rationale string `json:"rationale"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(fxRead(t, filepath.Join(source, "evals", "multilingual", "oracle.json")), &oracle); err != nil {
		t.Fatal(err)
	}
	run, _, stderr := fxEvaluation(t, source, fxEnv(fxProviders...))
	server := fxServe(t, fxHandler(t, nil, nil))
	run.collect = server.sender.collect
	run.Live, run.OutputDir = true, filepath.Join(t.TempDir(), "report")
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if server.total() != len(fxProviders)*len(fxLanguages) {
		t.Fatalf("%d requests", server.total())
	}
	for _, name := range fxProviders {
		for _, request := range server.sent(name) {
			for _, forbidden := range []string{`"expected"`, "rationale", "fake-"} {
				if strings.Contains(request.body, forbidden) {
					t.Fatalf("%s payload contains %s", name, forbidden)
				}
			}
			for _, label := range oracle.Labels {
				if strings.Contains(request.body, label.Rationale) {
					t.Fatalf("%s payload contains an oracle rationale", name)
				}
			}
		}
	}
}

// Unchanged: the offline run writes nothing into the source.
func TestOfflineRunNeverWrites(t *testing.T) {
	t.Parallel()
	source := fxEvalSource(t, t.TempDir(), "")
	list := func() []string {
		names := []string{}
		if err := filepath.WalkDir(source, func(path string, _ fs.DirEntry, err error) error {
			names = append(names, path)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return names
	}
	before := list()
	run, stdout, stderr := fxEvaluation(t, source, fxNoEnv(t))
	run.collect = fxNoCollect(t)
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	plan := fxNativeBytes(t, stdout.Bytes())
	if fxField(plan, "network_requests_made") != float64(0) || fxField(plan, "max_network_requests") != float64(9) || fxField(plan, "status") != "offline_validated" {
		t.Fatalf("plan = %s", stdout)
	}
	if after := list(); !slices.Equal(before, after) {
		t.Fatalf("offline run changed the source: %v", after)
	}
}

// Unchanged: an invalid corpus is refused offline.
func TestInvalidCorpusIsRejected(t *testing.T) {
	t.Parallel()
	for name, alter := range map[string]func(inputs, oracle map[string]any){
		"missing translation": func(inputs, _ map[string]any) {
			delete(fxField(inputs["cases"].([]any)[0], "variants").(map[string]any), "uk")
		},
		"blank evidence": func(inputs, _ map[string]any) {
			fxField(inputs["cases"].([]any)[0], "variants", "de").(map[string]any)["evidence"] = " "
		},
		"missing label": func(_, oracle map[string]any) { delete(oracle["labels"].(map[string]any), "c01") },
		"unknown label": func(_, oracle map[string]any) { fxField(oracle, "labels", "c01").(map[string]any)["expected"] = "no" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := fxEvalSource(t, t.TempDir(), "")
			corpus := filepath.Join(source, "evals", "multilingual")
			inputs := fxNativeBytes(t, fxRead(t, filepath.Join(corpus, "inputs.json"))).(map[string]any)
			oracle := fxNativeBytes(t, fxRead(t, filepath.Join(corpus, "oracle.json"))).(map[string]any)
			alter(inputs, oracle)
			for file, value := range map[string]any{"inputs.json": inputs, "oracle.json": oracle} {
				data, err := json.Marshal(value, json.Deterministic(true))
				if err != nil {
					t.Fatal(err)
				}
				fxWrite(t, filepath.Join(corpus, file), data)
			}
			run, _, _ := fxEvaluation(t, source, fxNoEnv(t))
			run.collect = fxNoCollect(t)
			if code := run.Run(t.Context()); code != 1 {
				t.Fatalf("exit %d", code)
			}
		})
	}
}

// Unchanged: bundled resources resolve from a source path with spaces, whatever the
// working directory. Not parallel: it changes the working directory.
func TestBundledResourcesWorkFromPathsWithSpaces(t *testing.T) {
	base := t.TempDir()
	source := fxEvalSource(t, filepath.Join(base, "managed source with spaces"), "")
	unrelated := filepath.Join(base, "unrelated project", ".git")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(unrelated))
	run, stdout, stderr := fxEvaluation(t, source, fxNoEnv(t))
	run.collect = fxNoCollect(t)
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if fxField(fxNativeBytes(t, stdout.Bytes()), "status") != "offline_validated" {
		t.Fatalf("plan = %s", stdout)
	}
}
