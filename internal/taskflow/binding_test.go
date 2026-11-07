package taskflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/belevtsev/agent-workflows/internal/consult"
	"github.com/belevtsev/agent-workflows/internal/modelpolicy"
)

// Consultation fixtures (D3, D4, Revision 2). Records are hand-built version-4
// JSON in the member order cw consult writes, fingerprinted with the contract
// rule, and checked with consult.ValidateRecord before a test relies on them
// being valid. No fixture depends on taskflow's own consultation types.

// fixtureProviderOrder is the fixed D3 provider order of a record.
var fixtureProviderOrder = []string{"jev", "subq_snap", "openai_decisions"}

// fixtureModels holds the D1 pinned model of each known provider.
var fixtureModels = map[string]string{"jev": "jev-1.14.0", "subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}

// fixtureOptions are the named options of every fixture question.
var fixtureOptions = []string{"supported", "contradicted", "insufficient_evidence"}

// fixtureNoAdvisersLimitation is the exact D5 verify limitation for an empty binding.
const fixtureNoAdvisersLimitation = "no advisers were configured when the task started; decisions rest on source evidence and checks only"

// fixtureAssignRefusal is the exact D5 flow refusal for early worker dispatch.
const fixtureAssignRefusal = "adjudicate and attach the before checkpoint before dispatching workers"

// fixtureBinding mirrors the shipped D1 default: Snap and OpenAI enabled, Jev opt-in.
func fixtureBinding() map[string]string {
	return map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}
}

// fixtureSourceEvidence is a typed D4 source entry for the fixture repository's
// source.txt, using a sha256 revision.
func fixtureSourceEvidence() string {
	sum := sha256.Sum256([]byte("first line\nsecond line\n"))
	return "source:source.txt:1-2@sha256:" + hex.EncodeToString(sum[:])
}

// fixtureObject is an ordered JSON object.
type fixtureObject struct {
	keys   []string
	values map[string]any
}

func fixtureObj(pairs ...any) *fixtureObject {
	if len(pairs)%2 != 0 {
		panic("fixtureObj needs key/value pairs")
	}
	object := &fixtureObject{values: map[string]any{}}
	for index := 0; index < len(pairs); index += 2 {
		object.set(pairs[index].(string), pairs[index+1])
	}
	return object
}

func (o *fixtureObject) set(key string, value any) *fixtureObject {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
	return o
}

func (o *fixtureObject) object(t *testing.T, path ...string) *fixtureObject {
	t.Helper()
	current := o
	for _, key := range path {
		next, ok := current.values[key].(*fixtureObject)
		if !ok {
			t.Fatalf("fixture has no object member %q in path %q", key, path)
		}
		current = next
	}
	return current
}

func (o *fixtureObject) MarshalJSON() ([]byte, error) {
	buffer := []byte{'{'}
	for index, key := range o.keys {
		if index > 0 {
			buffer = append(buffer, ',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(o.values[key], json.Deterministic(true))
		if err != nil {
			return nil, err
		}
		buffer = append(append(append(buffer, name...), ':'), value...)
	}
	return append(buffer, '}'), nil
}

func fixtureMarshal(t *testing.T, value any) jsontext.Value {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fixtureFingerprint applies the Revision 2 rule: lowercase hex SHA-256 of the
// request encoded like Python json.dumps(sort_keys=True, separators=(',', ':'),
// ensure_ascii=True), with numbers kept as written.
func fixtureFingerprint(t *testing.T, request []byte) string {
	t.Helper()
	var canonical bytes.Buffer
	if err := fixtureCanonical(&canonical, request); err != nil {
		t.Fatalf("fixture fingerprint: %v", err)
	}
	sum := sha256.Sum256(canonical.Bytes())
	return hex.EncodeToString(sum[:])
}

func fixtureCanonical(out *bytes.Buffer, value []byte) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 {
		return errors.New("empty JSON value")
	}
	switch trimmed[0] {
	case '{':
		var members map[string]jsontext.Value
		if err := json.Unmarshal(trimmed, &members); err != nil {
			return err
		}
		out.WriteByte('{')
		for index, key := range slices.Sorted(maps.Keys(members)) {
			if index > 0 {
				out.WriteByte(',')
			}
			fixturePythonString(out, key)
			out.WriteByte(':')
			if err := fixtureCanonical(out, members[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case '[':
		var items []jsontext.Value
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return err
		}
		out.WriteByte('[')
		for index, item := range items {
			if index > 0 {
				out.WriteByte(',')
			}
			if err := fixtureCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case '"':
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		fixturePythonString(out, text)
	default:
		// Numbers, booleans and null are kept exactly as written.
		out.Write(trimmed)
	}
	return nil
}

func fixturePythonString(out *bytes.Buffer, text string) {
	out.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			switch {
			case r >= 0x20 && r < 0x7f:
				out.WriteRune(r)
			case r < 0x10000:
				fmt.Fprintf(out, `\u%04x`, r)
			default:
				high, low := utf16.EncodeRune(r)
				fmt.Fprintf(out, `\u%04x\u%04x`, high, low)
			}
		}
	}
	out.WriteByte('"')
}

// fixtureContextFields returns the task context as emitted JSON members.
func fixtureContextFields(t *testing.T, context Context) map[string]jsontext.Value {
	t.Helper()
	data, err := encode(context)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]jsontext.Value{}
	if err := json.Unmarshal(bytes.TrimSpace(data), &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

// fixtureAdvisersMember requires the D5 "advisers" object in emitted context JSON.
func fixtureAdvisersMember(t *testing.T, fields map[string]jsontext.Value) map[string]string {
	t.Helper()
	raw, ok := fields["advisers"]
	if !ok {
		t.Fatalf("D5: task context JSON has no \"advisers\" member: %q", slices.Sorted(maps.Keys(fields)))
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		t.Fatalf("D5: task context \"advisers\" must be a JSON object, got %s", raw)
	}
	advisers := map[string]string{}
	if err := json.Unmarshal(trimmed, &advisers); err != nil {
		t.Fatalf("D5: task context \"advisers\" is not provider -> model: %v", err)
	}
	return advisers
}

func fixtureContextAdvisers(t *testing.T, context Context) map[string]string {
	t.Helper()
	return fixtureAdvisersMember(t, fixtureContextFields(t, context))
}

// fixtureStoredMember reads one top-level member of a stored JSON file.
func fixtureStoredMember(t *testing.T, path, name string) (jsontext.Value, bool) {
	t.Helper()
	fields := map[string]jsontext.Value{}
	if err := json.Unmarshal(productionTaskRead(t, path), &fields); err != nil {
		t.Fatal(err)
	}
	value, ok := fields[name]
	return value, ok
}

// adviserOutcome describes one participating provider of a fixture record.
type adviserOutcome struct {
	Status     string            // success, partial, refusal, unavailable or not_configured
	Model      string            // requested model; empty uses the context binding's model
	Choices    map[string]string // question -> choice; a partial provider refuses the others
	Category   string            // unavailable category; empty is timeout
	HTTPStatus int               // http_error status; zero is 503
}

// decisionItem is one D4 decision item.
type decisionItem struct {
	Status         string
	Choice         string // empty encodes null
	Reason         string
	Evidence       []string
	Verification   []string
	Rejected       map[string]string
	Investigations map[string]string
	NotIndependent bool
}

func (item decisionItem) value() *fixtureObject {
	var choice any
	if item.Choice != "" {
		choice = item.Choice
	}
	evidence, verification := item.Evidence, item.Verification
	if evidence == nil {
		evidence = []string{}
	}
	if verification == nil {
		verification = []string{}
	}
	rejected, investigations := item.Rejected, item.Investigations
	if rejected == nil {
		rejected = map[string]string{}
	}
	if investigations == nil {
		investigations = map[string]string{}
	}
	return fixtureObj("status", item.Status, "choice", choice, "reason", item.Reason, "evidence", evidence, "verification", verification, "rejected_judgments", rejected, "investigations", investigations, "agreement_not_independent", item.NotIndependent)
}

// fixtureResolvedItem is a D4-valid resolved item with typed evidence only.
func fixtureResolvedItem() decisionItem {
	return decisionItem{
		Status:       "resolved",
		Choice:       "supported",
		Reason:       "The bound source lines establish the claim.",
		Evidence:     []string{fixtureSourceEvidence()},
		Verification: []string{"observation:Read both bound source lines at the recorded revision."},
	}
}

// fixtureUnresolvedItem is a D4-valid unresolved item.
func fixtureUnresolvedItem() decisionItem {
	return decisionItem{Status: "unresolved", Reason: "The bound source does not yet decide the claim."}
}

// checkpointDraft is the editable description of a fixture record.
type checkpointDraft struct {
	// Context is the task context JSON placed in state.task_context; edit
	// "advisers" to describe a record from a different set.
	Context map[string]jsontext.Value
	// State replaces the request state when set.
	State     *fixtureObject
	Questions []string
	// Outcomes are the participating providers; nil makes every provider of
	// Context's advisers unavailable (timeout).
	Outcomes map[string]adviserOutcome
	// Items are decision items by question; a missing item is resolved.
	Items   map[string]decisionItem
	Pending bool
	// Forge edits the finished record after fingerprinting.
	Forge func(t *testing.T, record *fixtureObject)
}

func fixtureQuestions(questions []string) *fixtureObject {
	object := fixtureObj()
	for _, question := range questions {
		object.set(question, fixtureObj(
			"type", "choice",
			"instructions", "Does the bound source support the "+question+" claim?",
			"criteria", fixtureObj(
				"supported", "The source establishes the claim.",
				"contradicted", "The source establishes an incompatible claim.",
				"insufficient_evidence", "A necessary fact is missing.",
			),
		))
	}
	return object
}

func fixtureUsage(provider string) *fixtureObject {
	if provider == "openai_decisions" {
		return fixtureObj("input_tokens", 23, "output_tokens", 5, "total_tokens", 28,
			"input_tokens_details", fixtureObj("cached_tokens", 1, "cache_write_tokens", 2),
			"output_tokens_details", fixtureObj("reasoning_tokens", 3))
	}
	return fixtureObj("input_tokens", 20, "output_tokens", 3)
}

func fixtureAnswer(provider, choice string) *fixtureObject {
	probabilities := fixtureObj()
	for _, option := range fixtureOptions {
		var probability any = 0
		switch {
		case provider == "openai_decisions" && option == choice:
			probability = 0.5
		case provider == "openai_decisions":
			probability = 0.25
		case option == choice:
			probability = 1
		}
		probabilities.set(option, probability)
	}
	var confidence any = 1
	if provider == "openai_decisions" {
		confidence = 0.7
	}
	return fixtureObj("type", "choice", "choice", choice, "probabilities", probabilities, "confidence", confidence)
}

func fixtureProviderRecord(provider string, outcome adviserOutcome, model, fingerprint string, questions []string) *fixtureObject {
	record := fixtureObj("status", outcome.Status, "requested_model", model, "request_sha256", fingerprint)
	switch outcome.Status {
	case "not_configured":
		record.set("duration_seconds", 0)
	case "unavailable":
		record.set("duration_seconds", jsontext.Value("0.0"))
		category := outcome.Category
		if category == "" {
			category = "timeout"
		}
		failure := fixtureObj("category", category)
		if category == "http_error" {
			status := outcome.HTTPStatus
			if status == 0 {
				status = 503
			}
			failure.set("http_status", status)
		}
		record.set("error", failure)
	default:
		record.set("duration_seconds", jsontext.Value("0.0"))
		record.set("resolved_model", model)
		answers := fixtureObj()
		for _, question := range questions {
			choice, ok := outcome.Choices[question]
			if outcome.Status == "refusal" || !ok {
				answers.set(question, fixtureObj("type", "refusal"))
				continue
			}
			answers.set(question, fixtureAnswer(provider, choice))
		}
		record.set("result", fixtureObj("model", model, "answers", answers, "usage", fixtureUsage(provider)))
		record.set("usage", fixtureUsage(provider))
	}
	return record
}

func fixtureOrdered(names []string) []string {
	ordered := []string{}
	for _, name := range fixtureProviderOrder {
		if slices.Contains(names, name) {
			ordered = append(ordered, name)
		}
	}
	for _, name := range slices.Sorted(slices.Values(names)) {
		if !slices.Contains(ordered, name) {
			ordered = append(ordered, name)
		}
	}
	return ordered
}

// buildCheckpoint returns version-4 record bytes whose request binds context.
// Comparison and record status follow the D3 rules for the given outcomes.
func buildCheckpoint(t *testing.T, context Context, mutate func(*checkpointDraft)) []byte {
	t.Helper()
	draft := checkpointDraft{Context: fixtureContextFields(t, context), Questions: []string{"claim"}, Items: map[string]decisionItem{}}
	fixtureAdvisersMember(t, draft.Context)
	if mutate != nil {
		mutate(&draft)
	}
	advisers := map[string]string{}
	if raw, ok := draft.Context["advisers"]; ok {
		if err := json.Unmarshal(raw, &advisers); err != nil {
			t.Fatal(err)
		}
	}
	if draft.Outcomes == nil {
		draft.Outcomes = map[string]adviserOutcome{}
		for provider := range advisers {
			draft.Outcomes[provider] = adviserOutcome{Status: "unavailable"}
		}
	}
	state := draft.State
	if state == nil {
		state = fixtureObj("task_context", draft.Context, "note", "дані 👩 & <source>")
	}
	request := fixtureObj("state", state, "questions", fixtureQuestions(draft.Questions))
	fingerprint := fixtureFingerprint(t, fixtureMarshal(t, request))

	order := fixtureOrdered(slices.Collect(maps.Keys(draft.Outcomes)))
	providers := fixtureObj()
	attempted, succeeded, answered := 0, 0, 0
	for _, provider := range order {
		outcome := draft.Outcomes[provider]
		model := outcome.Model
		if model == "" {
			model = advisers[provider]
		}
		if model == "" {
			model = fixtureModels[provider]
		}
		providers.set(provider, fixtureProviderRecord(provider, outcome, model, fingerprint, draft.Questions))
		if outcome.Status != "not_configured" {
			attempted++
		}
		switch outcome.Status {
		case "success":
			succeeded++
			answered++
		case "partial":
			answered++
		}
	}
	comparison := fixtureObj()
	for _, question := range draft.Questions {
		choices, missing := fixtureObj(), fixtureObj()
		distinct := map[string]bool{}
		for _, provider := range order {
			outcome := draft.Outcomes[provider]
			choice, ok := outcome.Choices[question]
			switch {
			case (outcome.Status == "success" || outcome.Status == "partial") && ok:
				choices.set(provider, choice)
				distinct[choice] = true
			case outcome.Status == "partial" || outcome.Status == "refusal":
				missing.set(provider, "refusal")
			default:
				missing.set(provider, outcome.Status)
			}
		}
		status := "partial"
		switch {
		case attempted == 0:
			status = "no_advisers"
		case len(choices.keys) == 0:
			status = "unavailable"
		case len(choices.keys) == attempted && len(distinct) == 1:
			status = "agreement"
		case len(choices.keys) == attempted:
			status = "disagreement"
		}
		comparison.set(question, fixtureObj("status", status, "choices", choices, "missing", missing))
	}
	status := "unavailable"
	switch {
	case attempted == 0:
		status = "no_advisers_configured"
	case succeeded == attempted:
		status = "success"
	case answered > 0:
		status = "partial"
	}
	decision := fixtureObj("status", "pending", "questions", fixtureObj())
	if !draft.Pending {
		questions := fixtureObj()
		decisionStatus := "resolved"
		for _, question := range draft.Questions {
			item, ok := draft.Items[question]
			if !ok {
				item = fixtureResolvedItem()
			}
			if item.Status != "resolved" {
				decisionStatus = "unresolved"
			}
			questions.set(question, item.value())
		}
		decision = fixtureObj("status", decisionStatus, "questions", questions)
	}
	record := fixtureObj("version", 4, "status", status, "request", request, "request_sha256", fingerprint, "providers", providers, "comparison", comparison, "final_decision", decision)
	if draft.Forge != nil {
		draft.Forge(t, record)
	}
	return fixtureMarshal(t, record)
}

func writeCheckpointFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "consultation.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeConsultation writes a record the test relies on being a valid v4 record.
func writeConsultation(t *testing.T, context Context, mutate func(*checkpointDraft)) string {
	t.Helper()
	data := buildCheckpoint(t, context, mutate)
	if _, err := consult.ValidateRecord(data); err != nil {
		t.Fatalf("fixture invariant: consult.ValidateRecord rejected a record built as valid v4 (D3/D4): %v", err)
	}
	return writeCheckpointFile(t, data)
}

// writeUncheckedConsultation writes a record and returns ValidateRecord's verdict.
func writeUncheckedConsultation(t *testing.T, context Context, mutate func(*checkpointDraft)) (string, error) {
	t.Helper()
	data := buildCheckpoint(t, context, mutate)
	_, err := consult.ValidateRecord(data)
	return writeCheckpointFile(t, data), err
}

func attachCheckpoint(t *testing.T, record Record, phase string, context Context, mutate func(*checkpointDraft)) {
	t.Helper()
	if _, err := AttachConsultation(t.Context(), record.Directory, phase, writeConsultation(t, context, mutate)); err != nil {
		t.Fatalf("attach %s checkpoint: %v", phase, err)
	}
}

func withOutcomes(outcomes map[string]adviserOutcome) func(*checkpointDraft) {
	return func(draft *checkpointDraft) { draft.Outcomes = outcomes }
}

func substantialPlan(t *testing.T) Plan {
	t.Helper()
	return helperPlan(t, "substantial", "pass")
}

// D5 binding at start and context: Start stores the binding, including an
// empty map, as the record's "advisers" object; Load returns it; both
// checkpoint contexts emit it; later revisions keep it unchanged.
func TestStartStoresAdviserBindingAndContextsEmitIt(t *testing.T) {
	if Version != 3 {
		t.Fatalf("D5: task record Version = %d, want 3", Version)
	}
	for name, binding := range map[string]map[string]string{
		"shipped_default": fixtureBinding(),
		"jev_opt_in":      {"jev": "jev-1.14.0", "subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"},
		"none_configured": {},
	} {
		t.Run(name, func(t *testing.T) {
			expected := maps.Clone(binding)
			record, ref := fixtureTaskBound(t, substantialPlan(t), binding)
			if record.Version != 3 || !maps.Equal(record.Advisers, expected) {
				t.Fatalf("Start record: version=%d advisers=%v, want 3 %v", record.Version, record.Advisers, expected)
			}
			assertStoredBinding := func(revision int) {
				t.Helper()
				raw, ok := fixtureStoredMember(t, filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", revision)), "advisers")
				if !ok {
					t.Fatalf("revision %d stores no \"advisers\" member", revision)
				}
				if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
					t.Fatalf("revision %d stores advisers as %s, want a JSON object", revision, raw)
				}
				stored := map[string]string{}
				if err := json.Unmarshal(raw, &stored); err != nil || !maps.Equal(stored, expected) {
					t.Fatalf("revision %d stored advisers %s, want %v: %v", revision, raw, expected, err)
				}
			}
			assertStoredBinding(1)
			loaded, err := Load(record.Directory)
			if err != nil || !maps.Equal(loaded.Advisers, expected) {
				t.Fatalf("loaded advisers %v, want %v: %v", loaded.Advisers, expected, err)
			}
			if got := fixtureContextAdvisers(t, record.Contexts["before"]); !maps.Equal(got, expected) {
				t.Fatalf("stored before context advisers %v, want %v", got, expected)
			}
			before, err := PrepareContext(t.Context(), record.Directory, "before")
			if err != nil {
				t.Fatal(err)
			}
			if got := fixtureContextAdvisers(t, before); !maps.Equal(got, expected) {
				t.Fatalf("before context advisers %v, want %v", got, expected)
			}
			attachCheckpoint(t, record, "before", before, nil)
			mustAssess(t, record, ref)
			final, err := PrepareContext(t.Context(), record.Directory, "final")
			if err != nil {
				t.Fatal(err)
			}
			if got := fixtureContextAdvisers(t, final); !maps.Equal(got, expected) {
				t.Fatalf("final context advisers %v, want %v", got, expected)
			}
			loaded, err = Load(record.Directory)
			if err != nil || !maps.Equal(loaded.Advisers, expected) {
				t.Fatalf("binding changed by later revisions: %v %v", loaded.Advisers, err)
			}
			assertStoredBinding(loaded.Revision)
		})
	}
}

// D5 attach: a record whose provider set differs from the binding is
// rejected, even when it is a valid v4 record for its own (forged) context.
// The binding, not later policy or credentials, decides the required set.
func TestAttachRequiresExactlyTheBoundProviderSet(t *testing.T) {
	for _, test := range []struct {
		name              string
		binding, recorded map[string]string
	}{
		{"extra_provider", map[string]string{"subq_snap": "subq-snap-preview"}, fixtureBinding()},
		{"missing_bound_provider", fixtureBinding(), map[string]string{"openai_decisions": "gpt-6-luna"}},
		{"replaced_provider", map[string]string{"subq_snap": "subq-snap-preview"}, map[string]string{"openai_decisions": "gpt-6-luna"}},
		{"bound_set_dropped_to_none", fixtureBinding(), map[string]string{}},
		{"empty_binding_with_provider", map[string]string{}, map[string]string{"openai_decisions": "gpt-6-luna"}},
		{"empty_binding_with_opt_in_jev", map[string]string{}, map[string]string{"jev": "jev-1.14.0"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record, _ := fixtureTaskBound(t, substantialPlan(t), test.binding)
			path := writeConsultation(t, record.Contexts["before"], func(draft *checkpointDraft) {
				draft.Context["advisers"] = fixtureMarshal(t, test.recorded)
			})
			before := productionTaskBytes(t, record.Directory)
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
				t.Fatalf("record for %v attached to a task bound to %v", test.recorded, test.binding)
			}
			if !reflect.DeepEqual(productionTaskBytes(t, record.Directory), before) {
				t.Fatal("rejected attach changed task files")
			}
			// The exact bound set still attaches, so the refusal was the set.
			attachCheckpoint(t, record, "before", record.Contexts["before"], nil)
		})
	}
}

// D5 attach: a bound provider recorded as not_configured is rejected and named.
func TestAttachRejectsBoundProviderNotConfigured(t *testing.T) {
	notConfigured := adviserOutcome{Status: "not_configured"}
	for _, test := range []struct {
		name     string
		binding  map[string]string
		outcomes map[string]adviserOutcome
		named    []string
	}{
		{"one_of_two", fixtureBinding(), map[string]adviserOutcome{"subq_snap": notConfigured, "openai_decisions": {Status: "unavailable", Category: "network_error"}}, []string{"subq_snap"}},
		{"all_bound", fixtureBinding(), map[string]adviserOutcome{"subq_snap": notConfigured, "openai_decisions": notConfigured}, []string{"subq_snap", "openai_decisions"}},
		{"opt_in_jev", map[string]string{"jev": "jev-1.14.0"}, map[string]adviserOutcome{"jev": notConfigured}, []string{"jev"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record, _ := fixtureTaskBound(t, substantialPlan(t), test.binding)
			path := writeConsultation(t, record.Contexts["before"], withOutcomes(test.outcomes))
			_, err := AttachConsultation(t.Context(), record.Directory, "before", path)
			if err == nil {
				t.Fatal("not_configured bound provider accepted at attach")
			}
			if !slices.ContainsFunc(test.named, func(provider string) bool { return strings.Contains(err.Error(), provider) }) {
				t.Fatalf("rejection does not name the not_configured provider %v: %v", test.named, err)
			}
			if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused")); err == nil {
				t.Fatal("rejected checkpoint unlocked worker dispatch")
			}
		})
	}
}

// D5 attach: unavailable, refusal, partial and success are outcomes, so each
// completes the bound set and unlocks dispatch once resolved.
func TestAttachAcceptsAttemptedOutcomesForBoundProviders(t *testing.T) {
	for _, test := range []struct {
		name      string
		questions []string
		outcomes  map[string]adviserOutcome
	}{
		{"unavailable_and_refusal", nil, map[string]adviserOutcome{"subq_snap": {Status: "unavailable", Category: "http_error", HTTPStatus: 503}, "openai_decisions": {Status: "refusal"}}},
		{"success_and_partial", []string{"claim", "other"}, map[string]adviserOutcome{
			"subq_snap":        {Status: "success", Choices: map[string]string{"claim": "supported", "other": "supported"}},
			"openai_decisions": {Status: "partial", Choices: map[string]string{"claim": "supported"}},
		}},
		{"network_and_unsupported_request", nil, map[string]adviserOutcome{"subq_snap": {Status: "unavailable", Category: "network_error"}, "openai_decisions": {Status: "unavailable", Category: "unsupported_request"}}},
		{"invalid_response_and_worker_error", nil, map[string]adviserOutcome{"subq_snap": {Status: "unavailable", Category: "invalid_response"}, "openai_decisions": {Status: "unavailable", Category: "worker_error"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record, _ := fixtureTask(t, substantialPlan(t))
			attachCheckpoint(t, record, "before", record.Contexts["before"], func(draft *checkpointDraft) {
				if test.questions != nil {
					draft.Questions = test.questions
				}
				draft.Outcomes = test.outcomes
			})
			stored, err := Load(record.Directory)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := stored.Consultations["before"]; !ok {
				t.Fatal("accepted checkpoint was not recorded")
			}
			mustAssignWorker(t, stored, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
		})
	}
}

// D5 attach: each requested_model must equal the bound model.
func TestAttachRejectsRequestedModelDifferentFromBinding(t *testing.T) {
	binding := map[string]string{"jev": "jev-1.14.0"}
	t.Run("record_for_other_pin", func(t *testing.T) {
		record, _ := fixtureTaskBound(t, substantialPlan(t), binding)
		path := writeConsultation(t, record.Contexts["before"], func(draft *checkpointDraft) {
			draft.Context["advisers"] = fixtureMarshal(t, map[string]string{"jev": "jev-1.15.0"})
		})
		if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
			t.Fatal("record requesting jev-1.15.0 attached to a task bound to jev-1.14.0")
		}
		attachCheckpoint(t, record, "before", record.Contexts["before"], nil)
	})
	t.Run("record_model_differs_from_its_context", func(t *testing.T) {
		record, _ := fixtureTaskBound(t, substantialPlan(t), binding)
		path, _ := writeUncheckedConsultation(t, record.Contexts["before"], withOutcomes(map[string]adviserOutcome{"jev": {Status: "unavailable", Model: "jev-1.15.0"}}))
		if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
			t.Fatal("record with a different requested_model attached")
		}
	})
	t.Run("unsupported_model", func(t *testing.T) {
		record, _ := fixtureTask(t, substantialPlan(t))
		path, _ := writeUncheckedConsultation(t, record.Contexts["before"], withOutcomes(map[string]adviserOutcome{"subq_snap": {Status: "unavailable"}, "openai_decisions": {Status: "unavailable", Model: "gpt-unknown"}}))
		if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
			t.Fatal("record with an unsupported requested_model attached")
		}
	})
}

// writeFixturePolicy copies the shipped version-3 policy and its question
// patterns into a temp dir, keeping their relative layout, and changes only
// consultation.providers (D1: the enabled providers and their pins).
func writeFixturePolicy(t *testing.T, providers map[string]string) string {
	t.Helper()
	shipped := filepath.Join("..", "..", "skills", "task-orchestration")
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "references"), 0700); err != nil {
		t.Fatal(err)
	}
	patterns := productionTaskRead(t, filepath.Join(shipped, "references", "question-patterns.json"))
	if err := os.WriteFile(filepath.Join(directory, "references", "question-patterns.json"), patterns, 0600); err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(productionTaskRead(t, filepath.Join(shipped, "model-policy.yaml"))), "\n")
	var policy strings.Builder
	section, replaced := "", 0
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if line != "" && line[0] != ' ' && line[0] != '#' && line[0] != '\n' {
			section = strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
		}
		if section != "consultation" || !strings.HasPrefix(line, "  providers:") {
			policy.WriteString(line)
			continue
		}
		replaced++
		// Drop the shipped provider entries, then write the fixture's own.
		for index+1 < len(lines) && strings.HasPrefix(lines[index+1], "    ") {
			index++
		}
		if len(providers) == 0 {
			policy.WriteString("  providers: {}\n")
			continue
		}
		policy.WriteString("  providers:\n")
		for _, provider := range slices.Sorted(maps.Keys(providers)) {
			fmt.Fprintf(&policy, "    %s: %s\n", provider, providers[provider])
		}
	}
	if replaced != 1 {
		t.Fatalf("shipped policy has %d consultation.providers entries, want 1", replaced)
	}
	path := filepath.Join(directory, "model-policy.yaml")
	if err := os.WriteFile(path, []byte(policy.String()), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runFixtureConsult runs consult.Run offline (no credential is ever present)
// for a request bound to context and returns the written record.
func runFixtureConsult(t *testing.T, context Context, policy string) []byte {
	t.Helper()
	directory := t.TempDir()
	request := filepath.Join(directory, "request.json")
	output := filepath.Join(directory, "record.json")
	body := fixtureObj("state", fixtureObj("task_context", fixtureContextFields(t, context)), "questions", fixtureQuestions([]string{"claim"}))
	if err := os.WriteFile(request, fixtureMarshal(t, body), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code, err := consult.Run(t.Context(), consult.Options{
		Request: request,
		Policy:  modelpolicy.Source{Path: policy},
		Output:  output,
		Getenv:  func(string) string { return "" },
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	data, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatalf("consult.Run fixture (minimal D1 policy) wrote no record: code=%d err=%v stderr=%q: %v", code, err, stderr.String(), readErr)
	}
	t.Logf("consult.Run exit code %d, err %v", code, err)
	return data
}

// fixtureCheckFingerprint cross-checks the fixture fingerprint rule against a
// record written by consult.Run, so hand-built records hash the same way.
func fixtureCheckFingerprint(t *testing.T, data []byte) {
	t.Helper()
	fields := map[string]jsontext.Value{}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	var recorded string
	if err := json.Unmarshal(fields["request_sha256"], &recorded); err != nil {
		t.Fatal(err)
	}
	if computed := fixtureFingerprint(t, fields["request"]); computed != recorded {
		t.Fatalf("fixture fingerprint %s disagrees with consult.Run request_sha256 %s", computed, recorded)
	}
}

// fixtureParseObject reads top-level members in their written order.
func fixtureParseObject(t *testing.T, data []byte) *fixtureObject {
	t.Helper()
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.ReadToken(); err != nil || token.Kind() != '{' {
		t.Fatalf("record is not a JSON object: %v", err)
	}
	object := fixtureObj()
	for decoder.PeekKind() != '}' {
		token, err := decoder.ReadToken()
		if err != nil {
			t.Fatal(err)
		}
		// A token is voided by the next decoder call; copy the name first.
		name := token.String()
		value, err := decoder.ReadValue()
		if err != nil {
			t.Fatal(err)
		}
		object.set(name, jsontext.Value(slices.Clone(value)))
	}
	return object
}

// D5 "later change has no effect" through the real consult.Run: the policy now
// enables only openai_decisions and no credential is present, yet the record
// covers exactly the bound set (subq_snap, not_configured). Attach rejects it
// and names the provider; dispatch stays locked; the binding is unchanged.
func TestConsultRunUsesTaskBindingAfterPolicyAndCredentialChanges(t *testing.T) {
	binding := map[string]string{"subq_snap": "subq-snap-preview"}
	record, _ := fixtureTaskBound(t, substantialPlan(t), binding)
	policy := writeFixturePolicy(t, map[string]string{"openai_decisions": "gpt-6-luna"})
	data := runFixtureConsult(t, record.Contexts["before"], policy)
	parsed, err := consult.ValidateRecord(data)
	if err != nil {
		t.Fatalf("consult.Run record is not a valid v4 record: %v", err)
	}
	if got := slices.Sorted(maps.Keys(parsed.Providers)); !slices.Equal(got, []string{"subq_snap"}) {
		t.Fatalf("participation followed the policy, not the task binding: %v", got)
	}
	if outcome := parsed.Providers["subq_snap"]; outcome.Status != "not_configured" || outcome.RequestedModel != "subq-snap-preview" {
		t.Fatalf("bound provider without credential: %+v", outcome)
	}
	if parsed.Status != "no_advisers_configured" {
		t.Fatalf("record status %q, want no_advisers_configured", parsed.Status)
	}
	fixtureCheckFingerprint(t, data)
	before := productionTaskBytes(t, record.Directory)
	_, err = AttachConsultation(t.Context(), record.Directory, "before", writeCheckpointFile(t, data))
	if err == nil || !strings.Contains(err.Error(), "subq_snap") {
		t.Fatalf("not_configured bound provider not rejected by name: %v", err)
	}
	if !reflect.DeepEqual(productionTaskBytes(t, record.Directory), before) {
		t.Fatal("rejected attach changed task files")
	}
	if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused")); err == nil || !strings.Contains(err.Error(), fixtureAssignRefusal) {
		t.Fatalf("dispatch not locked without a resolved before checkpoint: %v", err)
	}
	stored, err := Load(record.Directory)
	if err != nil || !maps.Equal(stored.Advisers, binding) {
		t.Fatalf("binding changed: %v %v", stored.Advisers, err)
	}
}

// D3/D5 with an empty binding through the real consult.Run: the policy now
// enables two providers, yet the record is no_advisers_configured with no
// providers. Pending, it attaches but keeps dispatch locked; with a resolved
// typed decision it unlocks dispatch.
func TestConsultRunNoAdvisersRecordGatesDispatchUntilResolved(t *testing.T) {
	policy := writeFixturePolicy(t, fixtureBinding())
	for _, resolved := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "resolved"}[resolved], func(t *testing.T) {
			record, _ := fixtureTaskBound(t, substantialPlan(t), map[string]string{})
			data := runFixtureConsult(t, record.Contexts["before"], policy)
			parsed, err := consult.ValidateRecord(data)
			if err != nil {
				t.Fatalf("consult.Run record is not a valid v4 record: %v", err)
			}
			if parsed.Status != "no_advisers_configured" || len(parsed.Providers) != 0 || parsed.DecisionStatus != "pending" {
				t.Fatalf("empty binding record: status=%q providers=%v decision=%q", parsed.Status, parsed.Providers, parsed.DecisionStatus)
			}
			fixtureCheckFingerprint(t, data)
			if resolved {
				object := fixtureParseObject(t, data)
				object.set("final_decision", fixtureObj("status", "resolved", "questions", fixtureObj("claim", fixtureResolvedItem().value())))
				data = fixtureMarshal(t, object)
				if parsed, err := consult.ValidateRecord(data); err != nil || parsed.DecisionStatus != "resolved" {
					t.Fatalf("fixture invariant: resolved no-advisers record invalid: %+v %v", parsed, err)
				}
			}
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", writeCheckpointFile(t, data)); err != nil {
				t.Fatalf("no_advisers_configured record rejected for an empty binding: %v", err)
			}
			_, err = AssignWorker(t.Context(), record.Directory, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
			if resolved && err != nil {
				t.Fatalf("resolved no-advisers checkpoint did not unlock dispatch: %v", err)
			}
			if !resolved && (err == nil || !strings.Contains(err.Error(), fixtureAssignRefusal)) {
				t.Fatalf("pending checkpoint unlocked dispatch: %v", err)
			}
		})
	}
}
