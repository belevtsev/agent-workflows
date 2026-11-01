package consult

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const oracleCanary = "ORACLE_CANARY_DO_NOT_SEND"

func corpusInputs() *object {
	instructions, options := newObject(), newObject()
	for _, language := range evalLanguages {
		instructions.set(language, language+": state.cases.{case_id}.evidence; state.cases.{case_id}.claim")
		choices := newObject()
		for _, choice := range evalChoices {
			choices.set(choice, language+" "+choice)
		}
		options.set(language, choices)
	}
	cases := []any{}
	for index, id := range evalCaseIDs {
		variants := newObject()
		for _, language := range evalLanguages {
			variants.set(language, newObject("evidence", language+" observation "+id, "claim", language+" assertion "+id))
		}
		cases = append(cases, newObject("id", id, "family", evalFamilies[index/3], "variants", variants))
	}
	return newObject("version", integer("1"), "languages", stringList(evalLanguages), "instructions", instructions, "options", options, "cases", cases)
}

func corpusOracle() *object {
	labels := newObject()
	for index, id := range evalCaseIDs {
		labels.set(id, newObject("expected", evalChoices[index%3], "rationale", oracleCanary+" "+id))
	}
	return newObject("version", integer("1"), "labels", labels)
}

func expectedChoices() map[string]string {
	result := map[string]string{}
	for index, id := range evalCaseIDs {
		result[id] = evalChoices[index%3]
	}
	return result
}

func copyFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, data)
}

func writeFile(t *testing.T, target string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// corpusSource writes the synthetic corpus beside the frozen policy.
func corpusSource(t *testing.T, root string, inputs, oracle *object) string {
	t.Helper()
	skill := filepath.Join(root, "skills", "task-orchestration")
	for name, value := range map[string]*object{"inputs.json": inputs, "oracle.json": oracle} {
		data, err := reportBytes(value)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(skill, "evals", "multilingual", name), data)
	}
	copyFile(t, policyFile("required"), filepath.Join(skill, "model-policy.yaml"))
	copyFile(t, filepath.Join("testdata", "policy", "references", "jev-questions.json"), filepath.Join(skill, "references", "jev-questions.json"))
	return root
}

func evaluation(t *testing.T, source string) (Evaluation, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	return Evaluation{Source: source, Stdout: &stdout, Stderr: &stderr, Getenv: noCredentials, Home: func() (string, error) { return home, nil }, Executable: os.Executable}, &stdout, &stderr
}

func evalPrepared(t *testing.T, language string) (*object, *object) {
	t.Helper()
	shared, err := buildEvalRequest(corpusInputs(), language)
	if err != nil {
		t.Fatal(err)
	}
	return shared, triple(t, shared)
}

func evalSuccess(prepared *object, choices map[string]string, refusals ...string) *object {
	record := successRecord(prepared, "supported")
	result := record.get("result").(*object)
	result.get("usage").(*object).set("input_tokens", integer("24"))
	result.get("usage").(*object).set("output_tokens", integer("2"))
	answers := result.get("answers").(*object)
	questions := preparedQuestions(prepared)
	for _, id := range questions.names {
		if choice := choices[id]; choice != "" {
			answers.set(id, answerFor(questions.get(id).(*object), choice))
		}
	}
	if prepared.get("model") == openAIModel {
		usage := result.get("usage").(*object)
		usage.set("total_tokens", integer("26"))
		usage.set("input_tokens_details", newObject("cached_tokens", integer("0"), "cache_write_tokens", integer("0")))
		usage.set("output_tokens_details", newObject("reasoning_tokens", integer("0")))
	}
	for _, id := range refusals {
		answers.set(id, newObject("type", "refusal"))
		record.set("status", "partial")
	}
	return record
}

func successfulCollect(t *testing.T) collector {
	return func(_ context.Context, prepared *object, options batch) (map[string]any, error) {
		if options.deadline != 30*time.Second || !slices.Equal(options.required, providerOrder) {
			t.Fatalf("incorrect required provider set or deadline: %+v", options)
		}
		raw := map[string]any{}
		for _, name := range prepared.names {
			raw[name] = evalSuccess(prepared.get(name).(*object), nil)
		}
		return raw, nil
	}
}

func TestEvaluationOfflineMatchesHelperPlan(t *testing.T) {
	run, stdout, stderr := evaluation(t, filepath.Join("testdata", "evaluation", "source"))
	run.Getenv = func(string) string { t.Fatal("offline validation read the environment"); return "" }
	run.collect = func(context.Context, *object, batch) (map[string]any, error) {
		t.Fatal("offline validation dispatched a request")
		return nil, nil
	}
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if want := readTestdata(t, "evaluation", "offline.json"); !bytes.Equal(stdout.Bytes(), want) {
		t.Fatalf("offline plan differs:\n%s\n%s", stdout, want)
	}
}

func remove(item *object, name string) {
	delete(item.values, name)
	item.names = slices.DeleteFunc(item.names, func(member string) bool { return member == name })
}

// Reports differ from the helper's only in timing and runner identity.
func stableReport(t *testing.T, data []byte) []byte {
	t.Helper()
	report := parsed(t, data).(*object)
	for _, name := range []string{"started_at", "finished_at", "duration_seconds"} {
		remove(report, name)
	}
	provenance := report.get("provenance").(*object)
	remove(provenance, "runtime")
	remove(provenance.get("artifacts").(*object), "runner")
	batches := report.get("batches").(*object)
	for _, language := range batches.names {
		remove(batches.get(language).(*object), "duration_seconds")
	}
	result, err := reportBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEvaluationLiveMatchesHelperGoldens(t *testing.T) {
	run, stdout, stderr := evaluation(t, filepath.Join("testdata", "evaluation", "source"))
	scripted := parsed(t, readTestdata(t, "evaluation", "raw_records.json")).(*object)
	calls := 0
	run.collect = func(_ context.Context, prepared *object, options batch) (map[string]any, error) {
		if options.deadline != 30*time.Second {
			t.Fatalf("deadline = %v", options.deadline)
		}
		language := evalLanguages[calls]
		calls++
		raw := map[string]any{}
		for _, name := range prepared.names {
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
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(run.OutputDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, name := range []string{"summary.md", "en.record.json", "uk.record.json", "de.record.json"} {
		if !bytes.Equal(read(name), readTestdata(t, "evaluation", name)) {
			t.Fatalf("%s differs from helper output:\n%s", name, read(name))
		}
	}
	if got, want := stableReport(t, read("report.json")), readTestdata(t, "evaluation", "report.json"); !bytes.Equal(got, want) {
		t.Fatalf("report differs:\n%s\n--- want\n%s", got, want)
	}
	entries, err := os.ReadDir(run.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []any
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !equal(names, parsed(t, readTestdata(t, "evaluation", "files.json"))) {
		t.Fatalf("published files = %v", names)
	}
}

func TestEvaluationNumericsMatchHelper(t *testing.T) {
	items := parsed(t, readTestdata(t, "evaluation", "numeric.json")).([]any)
	for _, value := range items {
		item := value.(*object)
		input := floatOf(item.get("value"))
		if got := round6(input); got != floatOf(item.get("ratio")) {
			t.Fatalf("round6(%v) = %v", input, got)
		}
		if got := percent(input); got != item.get("percent") {
			t.Fatalf("percent(%v) = %s, want %v", input, got, item.get("percent"))
		}
	}
}

func TestCompleteFixtureAndLocalizedQuestionReferences(t *testing.T) {
	inputs, oracle := corpusInputs(), corpusOracle()
	if _, err := validateInputs(inputs); err != nil {
		t.Fatal(err)
	}
	if _, err := validateOracle(oracle, inputs); err != nil {
		t.Fatal(err)
	}
	for _, language := range evalLanguages {
		request, err := buildEvalRequest(inputs, language)
		if err != nil {
			t.Fatal(err)
		}
		questions := request.get("questions").(*object)
		if questions.size() != 12 {
			t.Fatalf("%d questions", questions.size())
		}
		states := request.get("state").(*object).get("cases").(*object)
		for index, id := range questions.names {
			question := questions.get(id).(*object)
			instructions := question.get("instructions").(string)
			if !strings.Contains(instructions, "state.cases."+id+".evidence") || !strings.Contains(instructions, "state.cases."+id+".claim") {
				t.Fatalf("instructions = %s", instructions)
			}
			variant := inputs.get("cases").([]any)[index].(*object).get("variants").(*object).get(language)
			if !equal(question.get("criteria"), inputs.get("options").(*object).get(language)) || !equal(states.get(id), variant) || states.get(id).(*object).has("family") {
				t.Fatalf("question %s = %s", id, encoded(t, question))
			}
		}
	}
}

func TestMissingTranslationEmptyTextAndIncorrectScopeAreRejected(t *testing.T) {
	cases := func(value *object) []any { return value.get("cases").([]any) }
	variants := func(value *object, index int) *object {
		return cases(value)[index].(*object).get("variants").(*object)
	}
	for index, alter := range []func(*object){
		func(v *object) { remove(variants(v, 0), "uk") },
		func(v *object) { variants(v, 0).get("de").(*object).set("evidence", " ") },
		func(v *object) { remove(v.get("options").(*object).get("uk").(*object), "supported") },
		func(v *object) { v.get("instructions").(*object).set("en", "Read all evidence") },
		func(v *object) { cases(v)[1].(*object).set("id", "c01") },
		func(v *object) { cases(v)[0].(*object).set("family", "cancellation") },
	} {
		invalid := corpusInputs()
		alter(invalid)
		if _, err := validateInputs(invalid); err == nil {
			t.Fatalf("alteration %d accepted", index)
		}
	}
}

func TestSeparateOracleRequiresAllBalancedLabels(t *testing.T) {
	inputs := corpusInputs()
	for index, alter := range []func(*object){
		func(l *object) { remove(l, "c01") },
		func(l *object) { l.get("c01").(*object).set("expected", "no") },
		func(l *object) { l.get("c01").(*object).set("expected", "contradicted") },
	} {
		oracle := corpusOracle()
		alter(oracle.get("labels").(*object))
		if _, err := validateOracle(oracle, inputs); err == nil {
			t.Fatalf("alteration %d accepted", index)
		}
	}
}

func TestOracleCanaryNeverEntersActualTransportPayloads(t *testing.T) {
	shared, prepared := evalPrepared(t, "en")
	var sent [][]byte
	sender := serve(t, func(name string, writer http.ResponseWriter, request *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(request.Body)
		sent = append(sent, body.Bytes())
		_, _ = writer.Write(wireAnswers(t, prepared.get(name).(*object)))
	})
	for _, name := range providerOrder {
		value := prepared.get(name).(*object)
		var record *object
		var code int
		if name == openAIProvider {
			record, code = sender.consultOpenAI(t.Context(), value, "credential-canary")
		} else {
			record, code = sender.consultPinned(t.Context(), name, value, "credential-canary")
		}
		if code != 0 || record.get("status") != "success" {
			t.Fatalf("%s = %d %s", name, code, encoded(t, record))
		}
	}
	rendered := string(bytes.Join(sent, []byte("\n")))
	if len(sent) != 3 {
		t.Fatalf("%d requests", len(sent))
	}
	for _, forbidden := range []string{oracleCanary, "rationale", `"expected"`, "credential-canary"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("payload contains %s", forbidden)
		}
	}
	for _, name := range []string{jevProvider, snapProvider} {
		value := prepared.get(name).(*object)
		if !equal(value.get("state"), shared.get("state")) || !equal(value.get("questions"), shared.get("questions")) {
			t.Fatalf("%s evidence changed", name)
		}
	}
}

func score(t *testing.T, records map[string]*object) *object {
	t.Helper()
	scored, err := scoreRecords(corpusInputs(), corpusOracle(), records)
	if err != nil {
		t.Fatal(err)
	}
	return scored
}

func languageScore(scored *object, provider, language string) *object {
	return scored.get(provider).(*object).get("languages").(*object).get(language).(*object)
}

func TestScoringWrongMissingAndValidInsufficientEvidence(t *testing.T) {
	shared, prepared := evalPrepared(t, "de")
	exact := expectedChoices()
	wrong := expectedChoices()
	wrong["c01"] = "contradicted"
	raw := map[string]any{
		jevProvider:    evalSuccess(prepared.get(jevProvider).(*object), wrong),
		snapProvider:   unavailable(prepared.get(snapProvider).(*object), "timeout", 30.0),
		openAIProvider: evalSuccess(prepared.get(openAIProvider).(*object), exact, "c01"),
	}
	record := recordFrom(t, shared, prepared, raw, 3)
	scored := score(t, map[string]*object{"de": record})
	jev := languageScore(scored, jevProvider, "de")
	if jev.get("correct") != integer("11") || jev.get("incorrect") != integer("1") || jev.get("missing") != integer("0") || !equal(jev.get("accuracy"), round6(11.0/12)) || !equal(jev.get("coverage"), 1.0) {
		t.Fatalf("jev = %s", encoded(t, jev))
	}
	third := jev.get("cases").([]any)[2].(*object)
	if third.get("choice") != "insufficient_evidence" || third.get("outcome") != "correct" {
		t.Fatalf("case = %s", encoded(t, third))
	}
	snap := languageScore(scored, snapProvider, "de")
	if snap.get("missing") != integer("12") || snap.get("accuracy") != nil || !equal(snap.get("coverage"), 0.0) {
		t.Fatalf("snap = %s", encoded(t, snap))
	}
	openai := languageScore(scored, openAIProvider, "de")
	if openai.get("correct") != integer("11") || openai.get("incorrect") != integer("0") || openai.get("missing") != integer("1") || openai.get("cases").([]any)[0].(*object).get("missing_reason") != "refusal" {
		t.Fatalf("openai = %s", encoded(t, openai))
	}
	wantStatus(t, record, "pending")
}

func TestConsistencyCountsOnlyCompleteTripletsAndIsNotAccuracy(t *testing.T) {
	records := map[string]*object{}
	for _, language := range evalLanguages {
		shared, prepared := evalPrepared(t, language)
		raw := map[string]any{}
		for _, name := range prepared.names {
			if language != "de" {
				raw[name] = evalSuccess(prepared.get(name).(*object), nil)
			} else if name == openAIProvider {
				raw[name] = evalSuccess(prepared.get(name).(*object), map[string]string{"c01": "contradicted"}, "c02")
			} else {
				raw[name] = evalSuccess(prepared.get(name).(*object), map[string]string{"c01": "contradicted"})
			}
		}
		records[language] = recordFrom(t, shared, prepared, raw, 3)
	}
	scored := score(t, records)
	consistency := func(provider, field string) any {
		return scored.get(provider).(*object).get("consistency").(*object).get(field)
	}
	if consistency(jevProvider, "consistent_triplets") != integer("11") || consistency(jevProvider, "complete_triplets") != integer("12") ||
		consistency(openAIProvider, "complete_triplets") != integer("11") || consistency(openAIProvider, "consistent_triplets") != integer("10") {
		t.Fatalf("consistency = %s", encoded(t, scored))
	}
	if equal(consistency(jevProvider, "ratio"), scored.get(jevProvider).(*object).get("totals").(*object).get("accuracy")) {
		t.Fatal("consistency matched accuracy")
	}
	empty := score(t, map[string]*object{})
	if empty.get(jevProvider).(*object).get("consistency").(*object).get("ratio") != nil {
		t.Fatal("empty consistency must stay unknown")
	}
	if (outcomeCounts{}).object().get("correct_over_total") != nil {
		t.Fatal("zero denominator must stay unknown")
	}
}

func TestModelMismatchIsMissingCoverage(t *testing.T) {
	shared, prepared := evalPrepared(t, "en")
	raw := map[string]any{}
	for _, name := range prepared.names {
		raw[name] = evalSuccess(prepared.get(name).(*object), nil)
	}
	raw[jevProvider].(*object).get("result").(*object).set("model", "jev-1.13.0")
	record := recordFrom(t, shared, prepared, raw, 3)
	if provider(record, jevProvider).get("error").(*object).get("category") != "invalid_response" {
		t.Fatalf("record = %s", encoded(t, record))
	}
	if got := languageScore(score(t, map[string]*object{"en": record}), jevProvider, "en").get("missing"); got != integer("12") {
		t.Fatalf("missing = %v", got)
	}
}

func TestOfflineRunNeverWrites(t *testing.T) {
	source := corpusSource(t, t.TempDir(), corpusInputs(), corpusOracle())
	before, err := filepath.Glob(filepath.Join(source, "skills", "task-orchestration", "*"))
	if err != nil {
		t.Fatal(err)
	}
	run, stdout, stderr := evaluation(t, source)
	run.Getenv = func(string) string { t.Fatal("credential lookup"); return "" }
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	plan := parsed(t, stdout.Bytes()).(*object)
	if plan.get("network_requests_made") != integer("0") || plan.get("max_network_requests") != integer("9") {
		t.Fatalf("plan = %s", stdout)
	}
	after, err := filepath.Glob(filepath.Join(source, "skills", "task-orchestration", "*"))
	if err != nil || !slices.Equal(before, after) {
		t.Fatalf("offline run changed resources: %v", err)
	}
}

func TestLiveFlagAndOutputAreRequiredTogether(t *testing.T) {
	for _, test := range []struct {
		live   bool
		output string
	}{{true, ""}, {false, "/unused"}} {
		run, _, stderr := evaluation(t, t.TempDir())
		run.Live, run.OutputDir = test.live, test.output
		if code := run.Run(t.Context()); code != 1 || stderr.String() != "Evaluation validation or output preflight failed; no exception details retained.\n" {
			t.Fatalf("code %d stderr %q", code, stderr)
		}
	}
}

func TestLiveCompletedWithWrongAnswersIsSuccessAndHasPrivateReports(t *testing.T) {
	source := corpusSource(t, t.TempDir(), corpusInputs(), corpusOracle())
	base, err := os.MkdirTemp("", "multilingual eval ")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	destination := filepath.Join(base, "baseline with spaces")
	run, _, stderr := evaluation(t, source)
	calls := 0
	collect := successfulCollect(t)
	run.collect = func(ctx context.Context, prepared *object, options batch) (map[string]any, error) {
		calls++
		return collect(ctx, prepared, options)
	}
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if calls != 3 {
		t.Fatalf("%d batches", calls)
	}
	data, err := os.ReadFile(filepath.Join(destination, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	report := parsed(t, data).(*object)
	execution := report.get("execution").(*object)
	jev := report.get("scoring").(*object).get(jevProvider).(*object)
	inputsDigest := sha256Hex(readFile(t, filepath.Join(destination, "oracle.json")))
	if report.get("status") != "completed" || jev.get("totals").(*object).get("incorrect") != integer("24") || execution.get("max_network_requests") != integer("9") ||
		!equal(execution.get("completed_batches"), stringList(evalLanguages)) ||
		report.get("provenance").(*object).get("artifacts").(*object).get("oracle.json").(*object).get("sha256") != inputsDigest ||
		report.get("batches").(*object).get("en").(*object).get("providers").(*object).get(jevProvider).(*object).get("usage").(*object).get("input_tokens") != integer("24") {
		t.Fatalf("report = %s", data)
	}
	if strings.Contains(string(data), "per_case_tokens") {
		t.Fatal("per-case tokens were inferred")
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode: %v %v", info, err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode: %v %v", entry.Name(), info, err)
		}
	}
	summary := string(readFile(t, filepath.Join(destination, "summary.md")))
	for _, provider := range providerOrder {
		for _, language := range evalLanguages {
			if !strings.Contains(summary, "| "+provider+" | "+language+" |") {
				t.Fatalf("summary lacks %s %s", provider, language)
			}
		}
	}
	if !strings.Contains(summary, "synthetic") || !strings.Contains(summary, "Consistent / complete triplets") {
		t.Fatal("summary lacks qualifications")
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExistingOutputAndFilesAreNeverOverwritten(t *testing.T) {
	source := corpusSource(t, t.TempDir(), corpusInputs(), corpusOracle())
	destination := filepath.Join(t.TempDir(), "existing")
	sentinel := filepath.Join(destination, "report.json")
	writeFile(t, sentinel, []byte("original"))
	run, _, _ := evaluation(t, source)
	run.Live, run.OutputDir = true, destination
	run.collect = func(context.Context, *object, batch) (map[string]any, error) {
		t.Fatal("request dispatched for an existing directory")
		return nil, nil
	}
	if code := run.Run(t.Context()); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if err := writePrivate(destination, "report.json", []byte("replacement")); err == nil {
		t.Fatal("existing evidence was replaced")
	}
	if string(readFile(t, sentinel)) != "original" {
		t.Fatal("sentinel changed")
	}
}

func TestSourceAndSnapshotExclusionsFollowSymlinks(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source tree")
	writeFile(t, filepath.Join(source, ".git"), []byte("gitdir: /other/place"))
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(base, "unrelated snapshot")
	writeFile(t, filepath.Join(snapshot, "skills-manifest.json"), []byte("{}"))
	snapshotAlias := filepath.Join(base, "snapshot alias")
	if err := os.Symlink(snapshot, snapshotAlias); err != nil {
		t.Fatal(err)
	}
	checkout := corpusSource(t, filepath.Join(base, "checkout"), corpusInputs(), corpusOracle())
	run, _, _ := evaluation(t, checkout)
	for _, target := range []string{filepath.Join(source, "output"), filepath.Join(alias, "output"), filepath.Join(snapshot, "output"), filepath.Join(snapshotAlias, "output"), filepath.Join(run.skillRoot(), "output")} {
		if _, err := run.outputPath(target); err == nil {
			t.Fatalf("%s accepted", target)
		}
	}
	allowed, err := run.outputPath(filepath.Join(base, "separate output"))
	want, resolveErr := resolvePath(filepath.Join(base, "separate output"))
	if err != nil || resolveErr != nil || allowed != want {
		t.Fatalf("allowed = %s %v %v", allowed, err, resolveErr)
	}
}

func TestDefaultXDGAndCurrentSnapshotReleaseRootsAreProtected(t *testing.T) {
	base := t.TempDir()
	defaultRoot := filepath.Join(base, ".local", "state", "agent-workflows", "releases")
	custom := filepath.Join(base, "custom state", "agent-workflows", "releases")
	source := filepath.Join(base, "other state", "releases", "revision")
	current := filepath.Join(source, "skills", "task-orchestration")
	for _, directory := range []string{defaultRoot, custom, current} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	run, _, _ := evaluation(t, source)
	run.Home = func() (string, error) { return base, nil }
	run.Getenv = func(name string) string {
		if name == "XDG_STATE_HOME" {
			return filepath.Join(base, "custom state")
		}
		return ""
	}
	for _, target := range []string{filepath.Join(defaultRoot, "output"), filepath.Join(custom, "output"), filepath.Join(source, "..", "output")} {
		if _, err := run.outputPath(target); err == nil {
			t.Fatalf("%s accepted", target)
		}
	}
}

func TestInterruptRetainsCompletedBatchAndMarksRemainingMissing(t *testing.T) {
	source := corpusSource(t, t.TempDir(), corpusInputs(), corpusOracle())
	destination := filepath.Join(t.TempDir(), "interrupted")
	run, _, _ := evaluation(t, source)
	collect := successfulCollect(t)
	calls := 0
	run.collect = func(ctx context.Context, prepared *object, options batch) (map[string]any, error) {
		calls++
		if calls == 2 {
			return nil, context.Canceled
		}
		return collect(ctx, prepared, options)
	}
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 130 {
		t.Fatalf("exit %d", code)
	}
	report := parsed(t, readFile(t, filepath.Join(destination, "report.json"))).(*object)
	if report.get("status") != "interrupted" || !equal(report.get("execution").(*object).get("completed_batches"), []any{"en"}) ||
		report.get("scoring").(*object).get(jevProvider).(*object).get("totals").(*object).get("missing") != integer("24") {
		t.Fatalf("report = %s", encoded(t, report))
	}
	if _, err := os.Stat(filepath.Join(destination, "uk.record.json")); !os.IsNotExist(err) {
		t.Fatal("interrupted batch left a record")
	}
	readFile(t, filepath.Join(destination, "en.record.json"))
}

func TestMissingCredentialsPreserveMissingCoverage(t *testing.T) {
	source := corpusSource(t, t.TempDir(), corpusInputs(), corpusOracle())
	destination := filepath.Join(t.TempDir(), "no credentials")
	run, _, _ := evaluation(t, source)
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 2 {
		t.Fatalf("exit %d", code)
	}
	report := parsed(t, readFile(t, filepath.Join(destination, "report.json"))).(*object)
	for _, name := range providerOrder {
		totals := report.get("scoring").(*object).get(name).(*object).get("totals").(*object)
		if totals.get("missing") != integer("36") || totals.get("accuracy") != nil {
			t.Fatalf("%s = %s", name, encoded(t, totals))
		}
		if got := encoded(t, report.get("batches").(*object).get("en").(*object).get("providers").(*object).get(name).(*object).get("error")); got != `{"category":"missing_credential"}` {
			t.Fatalf("%s error = %s", name, got)
		}
	}
}

func TestActualCollectorDeadlineIsBoundedWithoutRetries(t *testing.T) {
	shared, prepared := evalPrepared(t, "en")
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	sender := serve(t, func(_ string, _ http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	})
	started := time.Now()
	raw, err := sender.collect(t.Context(), prepared, batch{credentials: allCredentials(prepared), deadline: 150 * time.Millisecond, required: providerOrder})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("collection outlived its budget")
	}
	if calls.Load() != 3 {
		t.Fatalf("%d attempts for three providers", calls.Load())
	}
	record := recordFrom(t, shared, prepared, raw, 3)
	for _, name := range providerOrder {
		if provider(record, name).get("status") != "unavailable" || provider(record, name).get("error").(*object).get("category") != "timeout" {
			t.Fatalf("%s = %s", name, encoded(t, provider(record, name)))
		}
	}
}

func allOutput(t *testing.T, directory string) string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, entry := range entries {
		text.Write(readFile(t, filepath.Join(directory, entry.Name())))
	}
	return text.String()
}

func TestCredentialAndRawErrorValuesAreExcluded(t *testing.T) {
	const canary = "SECRET_CANARY_DO_NOT_RETAIN"
	source := corpusSource(t, t.TempDir(), corpusInputs(), corpusOracle())
	destination := filepath.Join(t.TempDir(), "error")
	run, _, _ := evaluation(t, source)
	run.Getenv = func(string) string { return canary }
	run.collect = func(context.Context, *object, batch) (map[string]any, error) { return nil, errors.New(canary) }
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 1 {
		t.Fatalf("exit %d", code)
	}
	output := allOutput(t, destination)
	if strings.Contains(output, canary) {
		t.Fatal("output retained a secret")
	}
	if parsed(t, readFile(t, filepath.Join(destination, "report.json"))).(*object).get("status") != "error" {
		t.Fatal("report does not record the error")
	}
}

func TestUnknownProviderFieldsAreNotRetained(t *testing.T) {
	source := corpusSource(t, t.TempDir(), corpusInputs(), corpusOracle())
	destination := filepath.Join(t.TempDir(), "sanitized")
	run, _, _ := evaluation(t, source)
	collect := successfulCollect(t)
	run.collect = func(ctx context.Context, prepared *object, options batch) (map[string]any, error) {
		raw, err := collect(ctx, prepared, options)
		for _, value := range raw {
			item := value.(*object)
			item.set("headers", newObject("Authorization", "SECRET_CANARY"))
			item.set("internal_trace", "SECRET_CANARY")
			item.get("result").(*object).set("diagnostic", "SECRET_CANARY")
		}
		return raw, err
	}
	run.Live, run.OutputDir = true, destination
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(allOutput(t, destination), "SECRET_CANARY") {
		t.Fatal("unknown fields were retained")
	}
}

func TestBundledResourcesWorkFromPathsWithSpaces(t *testing.T) {
	base := t.TempDir()
	source := corpusSource(t, filepath.Join(base, "managed source with spaces"), corpusInputs(), corpusOracle())
	unrelated := filepath.Join(base, "unrelated project", ".git")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(unrelated))
	run, stdout, stderr := evaluation(t, source)
	if code := run.Run(t.Context()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if parsed(t, stdout.Bytes()).(*object).get("status") != "offline_validated" {
		t.Fatalf("plan = %s", stdout)
	}
}

func TestEvaluationRefusesPolicyWithoutAllThreeAdvisers(t *testing.T) {
	source := corpusSource(t, t.TempDir(), corpusInputs(), corpusOracle())
	copyFile(t, policyFile("pair"), filepath.Join(source, "skills", "task-orchestration", "model-policy.yaml"))
	run, _, stderr := evaluation(t, source)
	if code := run.Run(t.Context()); code != 1 || !strings.Contains(stderr.String(), "no exception details retained") {
		t.Fatal(fmt.Sprint(code, stderr))
	}
}
