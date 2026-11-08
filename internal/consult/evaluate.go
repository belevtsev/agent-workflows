package consult

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
)

var (
	evalLanguages = []string{"en", "uk", "de"}
	evalChoices   = []string{"supported", "contradicted", "insufficient_evidence"}
	evalFamilies  = []string{"durability", "tenant_isolation", "cancellation", "release_identity"}
	evalCaseIDs   = []string{"c01", "c02", "c03", "c04", "c05", "c06", "c07", "c08", "c09", "c10", "c11", "c12"}
)

const (
	evalNetworkLimit = 9
	evalJudgments    = 36
)

// Evaluation runs the synthetic multilingual baseline. Offline corpus validation
// is the default: it reads no credentials, makes no requests and writes nothing.
type Evaluation struct {
	// Source is the checkout holding skills/task-orchestration.
	Source    string
	Live      bool
	OutputDir string
	Getenv    func(string) string
	Home      func() (string, error)
	// Executable identifies the runner in a live report's provenance.
	Executable func() (string, error)
	Stdout     io.Writer
	Stderr     io.Writer
	// Unset outside package tests, which keeps the endpoints fixed.
	collect collector
}

func (e Evaluation) skillRoot() string {
	return filepath.Join(e.Source, "skills", "task-orchestration")
}

func nonempty(value any) bool {
	content, ok := text(value)
	return ok && !blank(content)
}

func localizedField(value any) (*object, bool) {
	field, ok := mapping(value)
	return field, ok && field.exactly(evalLanguages...)
}

func validateCorpusLanguage(instructions, options any) error {
	content, ok := text(instructions)
	if !ok || blank(content) || !strings.Contains(content, "state.cases.{case_id}.evidence") || !strings.Contains(content, "state.cases.{case_id}.claim") {
		return errors.New("instructions must refer to the question's own evidence and claim")
	}
	choices, ok := mapping(options)
	if !ok || !choices.exactly(evalChoices...) || slices.ContainsFunc(choices.names, func(name string) bool { return !nonempty(choices.get(name)) }) {
		return errors.New("incomplete localized options")
	}
	return nil
}

func validateCorpusCase(value any, seen map[string]bool, families map[string]int) error {
	item, ok := mapping(value)
	if !ok || !item.exactly("id", "family", "variants") {
		return errors.New("invalid case fields")
	}
	id, ok := text(item.get("id"))
	if !ok || !slices.Contains(evalCaseIDs, id) || seen[id] {
		return errors.New("invalid or repeated case ID")
	}
	family, ok := text(item.get("family"))
	if !ok || !slices.Contains(evalFamilies, family) {
		return errors.New("invalid case family")
	}
	seen[id] = true
	families[family]++
	variants, ok := localizedField(item.get("variants"))
	if !ok {
		return errors.New("incomplete case translations")
	}
	for _, language := range variants.names {
		variant, ok := mapping(variants.get(language))
		if !ok || !variant.exactly("evidence", "claim") || !nonempty(variant.get("evidence")) || !nonempty(variant.get("claim")) {
			return errors.New("invalid case translation")
		}
	}
	return nil
}

func validateInputs(value any) (*object, error) {
	inputs, ok := mapping(value)
	if !ok || !inputs.exactly("version", "languages", "instructions", "options", "cases") {
		return nil, errors.New("invalid corpus fields")
	}
	if inputs.get("version") != integer("1") {
		return nil, errors.New("invalid corpus version")
	}
	if !equal(inputs.get("languages"), stringList(evalLanguages)) {
		return nil, errors.New("invalid corpus languages")
	}
	instructions, ok := localizedField(inputs.get("instructions"))
	if !ok {
		return nil, errors.New("incomplete localized corpus fields")
	}
	options, ok := localizedField(inputs.get("options"))
	if !ok {
		return nil, errors.New("incomplete localized corpus fields")
	}
	for _, language := range evalLanguages {
		if err := validateCorpusLanguage(instructions.get(language), options.get(language)); err != nil {
			return nil, err
		}
	}
	cases, ok := inputs.get("cases").([]any)
	if !ok || len(cases) != 12 {
		return nil, errors.New("corpus requires twelve cases")
	}
	seen, families := map[string]bool{}, map[string]int{}
	for _, item := range cases {
		if err := validateCorpusCase(item, seen, families); err != nil {
			return nil, err
		}
	}
	if len(seen) != len(evalCaseIDs) || slices.ContainsFunc(evalFamilies, func(family string) bool { return families[family] != 3 }) {
		return nil, errors.New("invalid case or family coverage")
	}
	return inputs, nil
}

func validateOracle(value any, inputs *object) (*object, error) {
	oracle, ok := mapping(value)
	if !ok || !oracle.exactly("version", "labels") || oracle.get("version") != integer("1") {
		return nil, errors.New("invalid oracle fields")
	}
	labels, ok := mapping(oracle.get("labels"))
	if !ok || !labels.exactly(evalCaseIDs...) {
		return nil, errors.New("incomplete oracle labels")
	}
	for _, id := range labels.names {
		label, ok := mapping(labels.get(id))
		if !ok || !label.exactly("expected", "rationale") || !nonempty(label.get("rationale")) {
			return nil, errors.New("invalid oracle label")
		}
		expected, ok := text(label.get("expected"))
		if !ok || !slices.Contains(evalChoices, expected) {
			return nil, errors.New("invalid oracle label")
		}
	}
	for _, family := range evalFamilies {
		found := map[string]bool{}
		for _, value := range inputs.get("cases").([]any) {
			item := value.(*object)
			if item.get("family") == family {
				found[labels.get(item.get("id").(string)).(*object).get("expected").(string)] = true
			}
		}
		if len(found) != len(evalChoices) {
			return nil, errors.New("oracle family labels must be balanced")
		}
	}
	return oracle, nil
}

// buildEvalRequest builds provider evidence from raw inputs only; no oracle
// enters this function.
func buildEvalRequest(inputs *object, language string) (*object, error) {
	if _, err := validateInputs(inputs); err != nil {
		return nil, err
	}
	if !slices.Contains(evalLanguages, language) {
		return nil, errors.New("unsupported corpus language")
	}
	cases := slices.Clone(inputs.get("cases").([]any))
	slices.SortFunc(cases, func(a, b any) int {
		return strings.Compare(a.(*object).get("id").(string), b.(*object).get("id").(string))
	})
	states, questions := newObject(), newObject()
	instructions := inputs.get("instructions").(*object).get(language).(string)
	options := inputs.get("options").(*object).get(language)
	for _, value := range cases {
		item := value.(*object)
		id := item.get("id").(string)
		states.set(id, item.get("variants").(*object).get(language))
		questions.set(id, newObject("type", "choice", "instructions", strings.ReplaceAll(instructions, "{case_id}", id), "criteria", options))
	}
	// Twelve cases per batch exceed the consultation authoring limit by design.
	return sharedContract(newObject("state", newObject("cases", states), "questions", questions))
}

// prepareEvalRequests prepares every adviser the policy enables, in fixed order.
func prepareEvalRequests(shared *object, selected policy) (*object, error) {
	prepared := newObject()
	for _, name := range providerOrder {
		model, enabled := selected.providers[name]
		if !enabled {
			continue
		}
		value, err := prepareProvider(name, shared, model)
		if err != nil {
			return nil, err
		}
		prepared.set(name, value)
	}
	return prepared, nil
}

// policyModels lists the policy's enabled advisers and their pinned models.
func policyModels(selected policy) *object {
	models := newObject()
	for _, name := range providerOrder {
		if model, enabled := selected.providers[name]; enabled {
			models.set(name, model)
		}
	}
	return models
}

// configuredAdvisers lists, in fixed order, the enabled advisers whose
// credential is set; only their credentials are returned.
func (e Evaluation) configuredAdvisers(selected policy) ([]string, map[string]string) {
	var names []string
	credentials := map[string]string{}
	for _, name := range providerOrder {
		if _, enabled := selected.providers[name]; !enabled {
			continue
		}
		if key := credential(e.Getenv, name); key != "" {
			names = append(names, name)
			credentials[name] = key
		}
	}
	return names, credentials
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// evalResources are the frozen bytes of one run; their hashes describe exactly
// what was loaded before any dispatch.
type evalResources struct {
	inputs, oracle *object
	policy         policy
	raw            map[string][]byte
}

var evalResourceOrder = []string{"inputs.json", "oracle.json", "model-policy.yaml"}

func (e Evaluation) load() (*evalResources, error) {
	skill := e.skillRoot()
	policyPath := filepath.Join(skill, "model-policy.yaml")
	raw := map[string][]byte{}
	for name, path := range map[string]string{
		"inputs.json":       filepath.Join(skill, "evals", "multilingual", "inputs.json"),
		"oracle.json":       filepath.Join(skill, "evals", "multilingual", "oracle.json"),
		"model-policy.yaml": policyPath,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw[name] = data
	}
	inputValue, err := parseJSON(raw["inputs.json"])
	if err != nil {
		return nil, errors.New("cannot read valid evaluation JSON")
	}
	inputs, err := validateInputs(inputValue)
	if err != nil {
		return nil, err
	}
	oracleValue, err := parseJSON(raw["oracle.json"])
	if err != nil {
		return nil, errors.New("cannot read valid evaluation JSON")
	}
	oracle, err := validateOracle(oracleValue, inputs)
	if err != nil {
		return nil, err
	}
	selected, err := loadPolicy(modelpolicy.Source{Path: policyPath})
	if err != nil {
		return nil, err
	}
	current, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(current, raw["model-policy.yaml"]) {
		return nil, errors.New("policy changed during validation")
	}
	for _, language := range evalLanguages {
		shared, err := buildEvalRequest(inputs, language)
		if err != nil {
			return nil, err
		}
		if _, err := prepareEvalRequests(shared, selected); err != nil {
			return nil, err
		}
	}
	return &evalResources{inputs: inputs, oracle: oracle, policy: selected, raw: raw}, nil
}

func ratio(numerator, denominator int) any {
	if denominator == 0 {
		return nil
	}
	return round6(float64(numerator) / float64(denominator))
}

type outcomeCounts struct{ total, correct, incorrect int }

func (c outcomeCounts) object() *object {
	valid := c.correct + c.incorrect
	return newObject("total", number(c.total), "correct", number(c.correct), "incorrect", number(c.incorrect),
		"missing", number(c.total-valid), "valid_answers", number(valid), "accuracy", ratio(c.correct, valid),
		"coverage", ratio(valid, c.total), "correct_over_total", ratio(c.correct, c.total))
}

// child walks retained record fields, treating absent ones as empty.
func child(value any, name string) *object {
	if item, ok := mapping(value); ok {
		if next, ok := mapping(item.get(name)); ok {
			return next
		}
	}
	return newObject()
}

func caseOutcome(stored *object, id, expected string) (*object, string) {
	answer := child(child(stored, "result"), "answers")
	item := child(answer, id)
	choice := ""
	reason := stored.get("status")
	if detail := child(stored, "error"); detail.has("category") {
		reason = detail.get("category")
	}
	if reason == nil {
		reason = "not_attempted"
	}
	switch stored.get("status") {
	case "success", "partial", "refusal":
		if item.get("type") == "choice" {
			if value, ok := text(item.get("choice")); ok && slices.Contains(evalChoices, value) {
				choice = value
			}
		} else if item.get("type") == "refusal" {
			reason = "refusal"
		}
	}
	if choice == "" {
		return newObject("id", id, "expected", expected, "choice", nil, "outcome", "missing", "missing_reason", reason), ""
	}
	state := "incorrect"
	if choice == expected {
		state = "correct"
	}
	return newObject("id", id, "expected", expected, "choice", choice, "outcome", state), choice
}

// notRun is the scoring entry of an adviser that was not configured for a run.
func notRun() *object { return newObject("status", "not_run", "reason", "not_configured") }

// scoreRecords scores synthetic labels without adjudicating a consultation.
// Every known adviser is listed; one outside configured is not_run, which is
// not a failure.
func scoreRecords(inputs, oracle *object, records map[string]*object, configured []string) (*object, error) {
	if _, err := validateInputs(inputs); err != nil {
		return nil, err
	}
	if _, err := validateOracle(oracle, inputs); err != nil {
		return nil, err
	}
	labels := oracle.get("labels").(*object)
	scored := newObject()
	for _, provider := range providerOrder {
		if !slices.Contains(configured, provider) {
			scored.set(provider, notRun())
			continue
		}
		languages := newObject()
		var totals outcomeCounts
		triplets := map[string][]string{}
		for _, language := range evalLanguages {
			stored := child(child(records[language], "providers"), provider)
			var counts outcomeCounts
			cases := make([]any, 0, len(evalCaseIDs))
			for _, id := range evalCaseIDs {
				expected := labels.get(id).(*object).get("expected").(string)
				outcome, choice := caseOutcome(stored, id, expected)
				counts.total++
				switch outcome.get("outcome") {
				case "correct":
					counts.correct++
				case "incorrect":
					counts.incorrect++
				}
				if choice != "" {
					triplets[id] = append(triplets[id], choice)
				}
				cases = append(cases, outcome)
			}
			languages.set(language, counts.object().with("cases", cases))
			totals.total += counts.total
			totals.correct += counts.correct
			totals.incorrect += counts.incorrect
		}
		complete, consistent := 0, 0
		for _, id := range evalCaseIDs {
			choices := triplets[id]
			if len(choices) != len(evalLanguages) {
				continue
			}
			complete++
			if choices[0] == choices[1] && choices[1] == choices[2] {
				consistent++
			}
		}
		scored.set(provider, newObject("languages", languages, "totals", totals.object(), "consistency", newObject(
			"possible_triplets", number(len(evalCaseIDs)), "complete_triplets", number(complete), "consistent_triplets", number(consistent),
			"incomplete_triplets", number(len(evalCaseIDs)-complete), "ratio", ratio(consistent, complete))))
	}
	return scored, nil
}

func inside(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// resolvePath follows symlinks through the closest existing ancestor, because a
// new name below a symlink can still land inside protected storage.
func resolvePath(target string) (string, error) {
	current, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			slices.Reverse(missing)
			return filepath.Join(append([]string{resolved}, missing...)...), nil
		}
		if !errors.Is(err, os.ErrNotExist) || current == filepath.Dir(current) {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = filepath.Dir(current)
	}
}

func expandHome(path string, home func() (string, error)) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	directory, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, strings.TrimPrefix(path, "~")), nil
}

func (e Evaluation) protectedRoots() ([]string, error) {
	skill, err := resolvePath(e.skillRoot())
	if err != nil {
		return nil, err
	}
	home, err := e.Home()
	if err != nil {
		return nil, err
	}
	roots := []string{skill}
	// A source run also protects the default and XDG manager stores. An installed
	// copy additionally discovers its custom immutable release root.
	stores := []string{filepath.Join(home, ".local", "state", "agent-workflows", "releases")}
	if state := e.Getenv("XDG_STATE_HOME"); state != "" {
		stores = append(stores, filepath.Join(state, "agent-workflows", "releases"))
	}
	for _, store := range stores {
		resolved, err := resolvePath(store)
		if err != nil {
			return nil, err
		}
		roots = append(roots, resolved)
	}
	for ancestor := filepath.Dir(skill); ancestor != filepath.Dir(ancestor); ancestor = filepath.Dir(ancestor) {
		if filepath.Base(ancestor) == "releases" {
			roots = append(roots, ancestor)
		}
	}
	return roots, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (e Evaluation) outputPath(path string) (string, error) {
	requested, err := expandHome(path, e.Home)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(requested); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("output directory must not already exist")
	}
	resolved, err := resolvePath(requested)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(resolved)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", errors.New("output parent directory must already exist")
	}
	roots, err := e.protectedRoots()
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		if inside(resolved, root) {
			return "", errors.New("output must be outside managed resources and releases")
		}
	}
	for ancestor := parent; ; ancestor = filepath.Dir(ancestor) {
		if exists(filepath.Join(ancestor, ".git")) {
			return "", errors.New("output must be outside source repositories")
		}
		if exists(filepath.Join(ancestor, "skills-manifest.json")) {
			return "", errors.New("output must be outside managed snapshots")
		}
		if ancestor == filepath.Dir(ancestor) {
			return resolved, nil
		}
	}
}

// writePrivate publishes a complete file exclusively, retaining earlier evidence
// on errors.
func writePrivate(directory, name string, data []byte) error {
	temporary := filepath.Join(directory, "."+name+".partial")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return os.Link(temporary, filepath.Join(directory, name))
}

func reportBytes(value any) ([]byte, error) {
	data, err := reportFormat.marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func utcNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z") }

func (e Evaluation) provenance(resources *evalResources) (*object, error) {
	artifacts := newObject()
	for _, name := range evalResourceOrder {
		artifacts.set(name, newObject("sha256", sha256Hex(resources.raw[name])))
	}
	executable, err := e.Executable()
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	artifacts.set("runner", newObject("sha256", sha256Hex(data)))
	return newObject("artifacts", artifacts, "runtime", newObject("implementation", "go", "version", runtime.Version(),
		"executable", resolved, "platform", runtime.GOOS+"/"+runtime.GOARCH), "requested_models", policyModels(resources.policy)), nil
}

func percent(value any) string {
	share, ok := value.(float64)
	if !ok {
		return "unknown"
	}
	return strconv.FormatFloat(share*100, 'f', 1, 64) + "%"
}

func (o *object) count(name string) string { return string(o.get(name).(integer)) }

func summaryText(report *object) string {
	scoring := report.get("scoring").(*object)
	lines := []string{"# Synthetic multilingual evaluation", "", "Status: " + report.get("status").(string) + ".", "",
		"These labeled synthetic results measure this corpus only. They do not select an adviser,",
		"prove production quality, or resolve real workflow decisions. Consultation decisions remain pending.",
		"", "| Adviser | Correct | Incorrect | Missing | Accuracy (valid answers) | Coverage | Correct / total |",
		"|---|---:|---:|---:|---:|---:|---:|"}
	var run []string
	for _, provider := range providerOrder {
		if scoring.get(provider).(*object).get("status") == "not_run" {
			lines = append(lines, "| "+provider+" | not run (not configured) | | | | | |")
		} else {
			run = append(run, provider)
		}
	}
	for _, provider := range run {
		item := scoring.get(provider).(*object).get("totals").(*object)
		lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |", provider, item.count("correct"), item.count("incorrect"), item.count("missing"),
			percent(item.get("accuracy")), percent(item.get("coverage")), percent(item.get("correct_over_total"))))
	}
	lines = append(lines, "", "| Adviser | Language | Correct | Incorrect | Missing | Accuracy | Coverage | Correct / total |", "|---|---|---:|---:|---:|---:|---:|---:|")
	for _, provider := range run {
		for _, language := range evalLanguages {
			item := scoring.get(provider).(*object).get("languages").(*object).get(language).(*object)
			lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s |", provider, language, item.count("correct"), item.count("incorrect"), item.count("missing"),
				percent(item.get("accuracy")), percent(item.get("coverage")), percent(item.get("correct_over_total"))))
		}
	}
	lines = append(lines, "", "| Adviser | Consistent / complete triplets | Incomplete triplets | Consistency |", "|---|---:|---:|---:|")
	for _, provider := range run {
		item := scoring.get(provider).(*object).get("consistency").(*object)
		lines = append(lines, fmt.Sprintf("| %s | %s / %s | %s | %s |", provider, item.count("consistent_triplets"), item.count("complete_triplets"), item.count("incomplete_triplets"), percent(item.get("ratio"))))
	}
	lines = append(lines, "", "Case outcomes, requested/returned model identities, whole-batch timing,",
		"provider timing and token usage are retained in report.json.",
		"Consistency is agreement across translations, not correctness. Missing answers are not scored as labels.")
	if len(run) < len(providerOrder) {
		lines = append(lines, "An adviser that is not configured (not enabled by the policy, or without a credential) is not run; that is not a failure.")
	}
	lines = append(lines, "Token usage is measured per provider batch; no per-case allocation is inferred.", "")
	return strings.Join(lines, "\n")
}

// batchSummary keeps a report's per-provider view to safe retained fields.
func batchSummary(record *object) *object {
	providers := newObject()
	stored := record.get("providers").(*object)
	for _, name := range stored.names {
		item := stored.get(name).(*object)
		summary := newObject()
		for _, field := range item.names {
			if slices.Contains([]string{"status", "requested_model", "resolved_model", "duration_seconds", "usage", "error"}, field) {
				summary.set(field, item.get(field))
			}
		}
		providers.set(name, summary)
	}
	return providers
}

// runBatches dispatches the sequential language batches to the configured
// advisers only; with none configured it makes no network call. Everything
// completed before a failure stays on disk and in the returned maps.
func (e Evaluation) runBatches(ctx context.Context, resources *evalResources, credentials map[string]string, directory string, metadata *object, records map[string]*object, batches *object, completed *[]any) error {
	for _, name := range evalResourceOrder {
		if err := writePrivate(directory, name, resources.raw[name]); err != nil {
			return err
		}
	}
	data, err := reportBytes(metadata)
	if err != nil {
		return err
	}
	if err := writePrivate(directory, "provenance.json", data); err != nil {
		return err
	}
	for _, language := range evalLanguages {
		if err := ctx.Err(); err != nil {
			return err
		}
		shared, err := buildEvalRequest(resources.inputs, language)
		if err != nil {
			return err
		}
		prepared, err := prepareEvalRequests(shared, resources.policy)
		if err != nil {
			return err
		}
		started := time.Now()
		raw, err := gather(ctx, e.collect, prepared, batch{credentials: credentials, deadline: resources.policy.deadline})
		if err != nil {
			return err
		}
		duration := seconds(started)
		record, err := makeRecord(shared, prepared, raw)
		if err != nil {
			return err
		}
		data, err := reportBytes(record)
		if err != nil {
			return err
		}
		fileName := language + ".record.json"
		if err := writePrivate(directory, fileName, data); err != nil {
			return err
		}
		records[language] = record
		*completed = append(*completed, language)
		batches.set(language, newObject("record_file", fileName, "duration_seconds", duration, "providers", batchSummary(record)))
	}
	return nil
}

func (e Evaluation) runLive(ctx context.Context, resources *evalResources) (int, error) {
	directory, err := e.outputPath(e.OutputDir)
	if err != nil {
		return 1, err
	}
	// Reserve private ownership before reading credentials or dispatching requests.
	if err := os.Mkdir(directory, 0o700); err != nil {
		return 1, err
	}
	records, batches := map[string]*object{}, newObject()
	startedAt, started := utcNow(), time.Now()
	metadata, err := e.provenance(resources)
	if err != nil {
		return 1, err
	}
	completed := []any{}
	state, code := "completed", 0
	configured, credentials := e.configuredAdvisers(resources.policy)
	// Filesystem errors and provider internals can contain credentials. Retain
	// completed safe batches, never raw error text.
	if err := e.runBatches(ctx, resources, credentials, directory, metadata, records, batches, &completed); err != nil {
		state, code = "error", 1
		if errors.Is(err, context.Canceled) {
			state, code = "interrupted", 130
		}
	}
	scoring, err := scoreRecords(resources.inputs, resources.oracle, records, configured)
	if err != nil {
		return 1, err
	}
	if state == "completed" {
		for _, provider := range configured {
			if scoring.get(provider).(*object).get("totals").(*object).get("missing") != integer("0") {
				state, code = "incomplete", 2
			}
		}
	}
	report := newObject("version", number(1), "kind", "synthetic_multilingual_evaluation", "status", state, "started_at", startedAt, "finished_at", utcNow(),
		"duration_seconds", seconds(started), "provenance", metadata, "execution", newObject(
			"languages", stringList(evalLanguages), "cases_per_language", number(len(evalCaseIDs)), "judgments_per_provider", number(evalJudgments),
			"max_network_requests", number(evalNetworkLimit), "deadline_seconds_per_batch", number(int(resources.policy.deadline.Seconds())),
			"automatic_retries", false, "completed_batches", completed), "batches", batches, "scoring", scoring)
	data, err := reportBytes(report)
	if err == nil {
		err = writePrivate(directory, "report.json", data)
	}
	if err == nil {
		err = writePrivate(directory, "summary.md", []byte(summaryText(report)))
	}
	if err != nil {
		fmt.Fprintln(e.Stderr, "Evaluation report could not be written; existing evidence is preserved.")
		if code == 130 {
			return 130, nil
		}
		return 1, nil
	}
	fmt.Fprintln(e.Stdout, "Synthetic evaluation "+state+"; private report: "+filepath.Join(directory, "report.json"))
	return code, nil
}

func stringList(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func (e Evaluation) offlinePlan(resources *evalResources) ([]byte, error) {
	hashes := newObject()
	for _, name := range evalResourceOrder {
		hashes.set(name, sha256Hex(resources.raw[name]))
	}
	return offlinePlanFormat.marshal(newObject("status", "offline_validated", "languages", stringList(evalLanguages), "cases_per_language", number(len(evalCaseIDs)),
		"judgments_per_provider", number(evalJudgments), "max_network_requests", number(evalNetworkLimit), "network_requests_made", number(0),
		"deadline_seconds_per_batch", number(int(resources.policy.deadline.Seconds())), "requested_models", policyModels(resources.policy), "fixture_sha256", hashes))
}

// Run validates the corpus, then runs the hosted baseline only when Live is set.
// It returns the process status: 0 complete, 2 incomplete coverage, 1 invalid
// inputs or output failures, 130 interrupted. Details are withheld throughout.
func (e Evaluation) Run(ctx context.Context) int {
	refused := func() int {
		fmt.Fprintln(e.Stderr, "Evaluation validation or output preflight failed; no exception details retained.")
		return 1
	}
	if e.Live != (e.OutputDir != "") {
		return refused()
	}
	resources, err := e.load()
	if err != nil {
		return refused()
	}
	if e.Live {
		code, err := e.runLive(ctx, resources)
		if err != nil {
			return refused()
		}
		return code
	}
	plan, err := e.offlinePlan(resources)
	if err != nil {
		return refused()
	}
	fmt.Fprintln(e.Stdout, string(plan))
	return 0
}
