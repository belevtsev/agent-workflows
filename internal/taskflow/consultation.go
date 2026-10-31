package taskflow

import (
	"bytes"
	"context"
	json1 "encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"unicode/utf16"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
)

type Question struct {
	Type         string                    `json:"type"`
	Instructions jsontext.Value            `json:"instructions"`
	Criteria     map[string]jsontext.Value `json:"criteria"`
}
type SharedRequest struct {
	State     jsontext.Value      `json:"state"`
	Questions map[string]Question `json:"questions"`
}
type ProviderError struct {
	Category   string `json:"category"`
	HTTPStatus *int   `json:"http_status,omitempty"`
}
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
}
type ProviderResult struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   jsontext.Value    `json:"usage"`
}
type Provider struct {
	Status          string          `json:"status"`
	RequestedModel  string          `json:"requested_model"`
	ResolvedModel   string          `json:"resolved_model,omitempty"`
	RequestSHA256   string          `json:"request_sha256"`
	DurationSeconds *float64        `json:"duration_seconds"`
	Result          *ProviderResult `json:"result,omitempty"`
	Usage           jsontext.Value  `json:"usage,omitempty"`
	Error           *ProviderError  `json:"error,omitempty"`
}
type Comparison struct {
	Status  string            `json:"status"`
	Choices map[string]string `json:"choices"`
	Missing map[string]string `json:"missing"`
}
type QuestionDecision struct {
	Status            string            `json:"status"`
	Choice            *string           `json:"choice"`
	Reason            string            `json:"reason"`
	Evidence          []string          `json:"evidence"`
	Verification      []string          `json:"verification"`
	RejectedJudgments map[string]string `json:"rejected_judgments"`
}
type FinalDecision struct {
	Status    string                      `json:"status"`
	Questions map[string]QuestionDecision `json:"questions"`
}

// Consultation is the current helper's version-three raw/adjudicated format.
// Earlier pair-only or optional-provider formats cannot satisfy a new task.
type Consultation struct {
	Version       int                   `json:"version"`
	Status        string                `json:"status"`
	Request       jsontext.Value        `json:"request"`
	RequestSHA256 string                `json:"request_sha256"`
	Providers     map[string]Provider   `json:"providers"`
	Comparison    map[string]Comparison `json:"comparison"`
	FinalDecision FinalDecision         `json:"final_decision"`
}

var adviserIDs = []string{"jev", "subq_snap", "openai_decisions"}
var jevModel = regexp.MustCompile(`^jev-[0-9]+\.[0-9]+\.[0-9]+$`)

func sameKeys[V any](values map[string]V, keys []string) bool {
	if len(values) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}
func fieldKeys(data []byte, keys ...string) error {
	var fields map[string]jsontext.Value
	if err := decodeBytes(data, &fields); err != nil {
		return err
	}
	if !sameKeys(fields, keys) {
		return errors.New("consultation fields differ from the helper contract")
	}
	return nil
}
func validDescription(data jsontext.Value) bool {
	if len(data) == 0 {
		return false
	}
	switch data.Kind() {
	case '"':
		var text string
		return decodeBytes(data, &text) == nil && nonblank(text)
	case '{':
		var values map[string]jsontext.Value
		return decodeBytes(data, &values) == nil && len(values) > 0
	case '[':
		var values []jsontext.Value
		return decodeBytes(data, &values) == nil && len(values) > 0
	default:
		return false
	}
}

// canonicalJSON is the form request_sha256 covers: sorted keys, no insignificant
// whitespace, and every character outside printable ASCII \u-escaped. Records
// written by cw consult already hold normalized numbers, so their spelling is
// preserved here rather than reparsed.
func canonicalJSON(data []byte) ([]byte, error) {
	decoder := json1.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	var write func(any) error
	write = func(value any) error {
		switch item := value.(type) {
		case nil:
			output.WriteString("null")
		case bool:
			output.WriteString(strconv.FormatBool(item))
		case json1.Number:
			output.WriteString(string(item))
		case string:
			output.WriteByte('"')
			for _, r := range item {
				switch r {
				case '"', '\\':
					output.WriteByte('\\')
					output.WriteRune(r)
				case '\b':
					output.WriteString(`\b`)
				case '\f':
					output.WriteString(`\f`)
				case '\n':
					output.WriteString(`\n`)
				case '\r':
					output.WriteString(`\r`)
				case '\t':
					output.WriteString(`\t`)
				default:
					if r < 0x20 || r >= 0x7f {
						if r > 0xffff {
							a, b := utf16.EncodeRune(r)
							fmt.Fprintf(&output, `\u%04x\u%04x`, a, b)
						} else {
							fmt.Fprintf(&output, `\u%04x`, r)
						}
					} else {
						output.WriteRune(r)
					}
				}
			}
			output.WriteByte('"')
		case []any:
			output.WriteByte('[')
			for i, entry := range item {
				if i > 0 {
					output.WriteByte(',')
				}
				if err := write(entry); err != nil {
					return err
				}
			}
			output.WriteByte(']')
		case map[string]any:
			keys := make([]string, 0, len(item))
			for key := range item {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			output.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					output.WriteByte(',')
				}
				write(key)
				output.WriteByte(':')
				if err := write(item[key]); err != nil {
					return err
				}
			}
			output.WriteByte('}')
		default:
			return errors.New("unsupported canonical JSON value")
		}
		return nil
	}
	if err := write(value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func requestContext(state jsontext.Value, expected Context) error {
	var fields map[string]jsontext.Value
	if err := decodeBytes(state, &fields); err != nil {
		return errors.New("shared state must include a structured task_context or task_contexts")
	}
	single, one := fields["task_context"]
	multiple, many := fields["task_contexts"]
	if one == many {
		return errors.New("shared state needs exactly one task_context or task_contexts field")
	}
	var contexts []Context
	var rawContexts []jsontext.Value
	if one {
		var item Context
		if err := decodeBytes(single, &item); err != nil {
			return err
		}
		contexts = []Context{item}
		rawContexts = []jsontext.Value{single}
	} else if err := decodeBytes(multiple, &contexts); err != nil {
		return err
	} else if err := decodeBytes(multiple, &rawContexts); err != nil {
		return err
	}
	if len(contexts) == 0 || len(contexts) > 256 {
		return errors.New("task contexts must be a bounded nonempty array")
	}
	seen := map[string]bool{}
	matches := 0
	for i, item := range contexts {
		if err := fieldKeys(rawContexts[i], "task_id", "phase", "snapshot", "evidence_sha256"); err != nil {
			return errors.New("shared task context must contain every exact emitted field")
		}
		var rawContext map[string]jsontext.Value
		if err := decodeBytes(rawContexts[i], &rawContext); err != nil {
			return err
		}
		if err := fieldKeys(rawContext["snapshot"], "root", "selected_path", "head", "branch", "fingerprint", "dirty", "file_count"); err != nil {
			return errors.New("shared task context snapshot must contain every exact emitted field")
		}
		key := item.TaskID + ":" + item.Phase
		if !nonblank(item.TaskID) || !phaseValid(item.Phase) || item.Snapshot.Fingerprint == "" || seen[key] || item.Phase == "before" && item.EvidenceSHA256 != "" || item.Phase == "final" && !validSHA256(item.EvidenceSHA256) {
			return errors.New("invalid or duplicate task context")
		}
		seen[key] = true
		if item == expected {
			matches++
		}
	}
	if matches != 1 {
		return errors.New("consultation task, phase or complete source snapshot differs")
	}
	return nil
}

func validateUsage(raw jsontext.Value, openai bool) error {
	var fields map[string]jsontext.Value
	if err := decodeBytes(raw, &fields); err != nil {
		return err
	}
	keys := []string{"input_tokens", "output_tokens"}
	if openai {
		keys = append(keys, "total_tokens", "input_tokens_details", "output_tokens_details")
	}
	if !sameKeys(fields, keys) {
		return errors.New("invalid provider token usage fields")
	}
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		if raw, ok := fields[key]; ok {
			var number int64
			if err := decodeBytes(raw, &number); err != nil || number < 0 {
				return errors.New("invalid provider token usage")
			}
		}
	}
	if openai {
		for key, details := range map[string][]string{"input_tokens_details": {"cached_tokens", "cache_write_tokens"}, "output_tokens_details": {"reasoning_tokens"}} {
			var values map[string]int64
			if err := decodeBytes(fields[key], &values); err != nil || !sameKeys(values, details) {
				return errors.New("invalid provider token usage details")
			}
			for _, number := range values {
				if number < 0 {
					return errors.New("invalid provider token usage details")
				}
			}
		}
	}
	return nil
}

func validateProvider(name string, provider Provider, request SharedRequest, requestHash string, raw jsontext.Value) error {
	openai := name == "openai_decisions"
	modelOK := provider.RequestedModel == "subq-snap-preview"
	if name == "jev" {
		modelOK = jevModel.MatchString(provider.RequestedModel)
	}
	if openai {
		modelOK = provider.RequestedModel == "gpt-6-luna"
	}
	if !modelOK || provider.RequestSHA256 != requestHash || provider.DurationSeconds == nil || math.IsNaN(*provider.DurationSeconds) || math.IsInf(*provider.DurationSeconds, 0) || *provider.DurationSeconds < 0 {
		return errors.New("provider model, evidence fingerprint or duration is invalid")
	}
	base := []string{"status", "requested_model", "request_sha256", "duration_seconds"}
	// The helper only retains normalized fields. Re-encoding also catches empty
	// extra refusal/result fields that a typed zero value would otherwise hide.
	normalized, err := encode(provider)
	if err != nil {
		return err
	}
	var originalValue, normalizedValue any
	if decodeBytes(raw, &originalValue) != nil || decodeBytes(normalized, &normalizedValue) != nil || !reflect.DeepEqual(originalValue, normalizedValue) {
		return errors.New("stored provider differs from the helper's normalized fields")
	}
	if provider.Status == "unavailable" {
		if err := fieldKeys(raw, append(base, "error")...); err != nil {
			return err
		}
		if provider.Error == nil || !slices.Contains([]string{"missing_credential", "deadline_unavailable", "timeout", "http_error", "network_error", "invalid_response", "worker_error", "unsupported_request"}, provider.Error.Category) || (provider.Error.Category == "unsupported_request" && !openai) {
			return errors.New("invalid unavailable-provider category")
		}
		if provider.Error.Category == "http_error" {
			if provider.Error.HTTPStatus == nil || *provider.Error.HTTPStatus < 100 || *provider.Error.HTTPStatus > 599 {
				return errors.New("invalid provider HTTP status")
			}
		} else if provider.Error.HTTPStatus != nil {
			return errors.New("unexpected provider HTTP status")
		}
		return nil
	}
	if provider.Status != "success" && !(openai && (provider.Status == "partial" || provider.Status == "refusal")) {
		return errors.New("all three advisers are required; disabled or invalid provider status")
	}
	if err := fieldKeys(raw, append(base, "resolved_model", "result", "usage")...); err != nil {
		return err
	}
	if provider.Result == nil || provider.ResolvedModel != provider.RequestedModel || provider.Result.Model != provider.RequestedModel || !sameKeys(provider.Result.Answers, mapKeys(request.Questions)) {
		return errors.New("invalid provider resolved model or answers")
	}
	if err := validateUsage(provider.Usage, openai); err != nil {
		return err
	}
	if err := validateUsage(provider.Result.Usage, openai); err != nil {
		return err
	}
	a, _ := canonicalJSON(provider.Usage)
	b, _ := canonicalJSON(provider.Result.Usage)
	if !bytes.Equal(a, b) {
		return errors.New("provider usage differs from retained result")
	}
	choices := 0
	for id, answer := range provider.Result.Answers {
		if openai && answer.Type == "refusal" {
			if answer.Choice != "" || len(answer.Probabilities) > 0 || answer.Confidence != nil {
				return errors.New("refusal contains a fabricated choice")
			}
			continue
		}
		question := request.Questions[id]
		if answer.Type != "choice" || answer.Confidence == nil || *answer.Confidence < 0 || *answer.Confidence > 1 || math.IsNaN(*answer.Confidence) || !sameKeys(answer.Probabilities, mapKeys(question.Criteria)) {
			return errors.New("invalid Choice answer")
		}
		chosen, ok := answer.Probabilities[answer.Choice]
		if !ok {
			return errors.New("provider selected an unknown choice")
		}
		sum, maximum := 0.0, 0.0
		for _, probability := range answer.Probabilities {
			if probability < 0 || probability > 1 || math.IsNaN(probability) || math.IsInf(probability, 0) {
				return errors.New("invalid Choice probability")
			}
			sum += probability
			maximum = max(maximum, probability)
		}
		if math.Abs(sum-1) > 1e-6 || (!openai && chosen != maximum) {
			return errors.New("invalid Choice distribution or maximum")
		}
		choices++
	}
	expected := "success"
	if choices == 0 {
		expected = "refusal"
	} else if choices < len(request.Questions) {
		expected = "partial"
	}
	if provider.Status != expected {
		return errors.New("provider availability differs from its answers")
	}
	return nil
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
func textList(values []string) bool {
	for _, value := range values {
		if !nonblank(value) {
			return false
		}
	}
	return values != nil
}

// ValidateConsultation revalidates the helper's retained Choice-only provider,
// comparison and adjudication contract. Availability remains independent for
// each provider; recorded source fallback permits honest unavailable coverage.
func ValidateConsultation(data []byte, expected Context) (Consultation, error) {
	var record Consultation
	if err := decodeBytes(data, &record); err != nil {
		return record, err
	}
	if err := fieldKeys(data, "version", "status", "request", "request_sha256", "providers", "comparison", "final_decision"); err != nil {
		return record, err
	}
	if record.Version != 3 || !sameKeys(record.Providers, adviserIDs) {
		return record, errors.New("substantial task needs helper version 3 and all three required advisers")
	}
	var request SharedRequest
	if err := decodeBytes(record.Request, &request); err != nil {
		return record, err
	}
	if err := fieldKeys(record.Request, "state", "questions"); err != nil {
		return record, err
	}
	if len(request.Questions) == 0 || len(request.Questions) > 1024 {
		return record, errors.New("consultation needs a bounded nonempty Choice question map")
	}
	for id, question := range request.Questions {
		if !nonblank(id) || len([]rune(id)) > 100 || question.Type != "choice" || !validDescription(question.Instructions) || len(question.Criteria) < 2 || len(question.Criteria) > 255 {
			return record, errors.New("invalid shared Choice question")
		}
		for option, description := range question.Criteria {
			if !nonblank(option) || !validDescription(description) {
				return record, errors.New("invalid shared Choice option")
			}
		}
	}
	canonical, err := canonicalJSON(record.Request)
	if err != nil || digest(canonical) != record.RequestSHA256 {
		return record, errors.New("shared consultation request fingerprint differs")
	}
	if err := requestContext(request.State, expected); err != nil {
		return record, err
	}
	var raw map[string]jsontext.Value
	decodeBytes(data, &raw)
	if err := fieldKeys(raw["final_decision"], "status", "questions"); err != nil {
		return record, err
	}
	var rawProviders map[string]jsontext.Value
	decodeBytes(raw["providers"], &rawProviders)
	success, available := 0, 0
	for _, name := range adviserIDs {
		provider := record.Providers[name]
		if err := validateProvider(name, provider, request, record.RequestSHA256, rawProviders[name]); err != nil {
			return record, fmt.Errorf("%s: %w", name, err)
		}
		if provider.Status == "success" {
			success++
		}
		if provider.Status == "success" || provider.Status == "partial" {
			available++
		}
	}
	availability := "unavailable"
	if success == 3 {
		availability = "success"
	} else if available > 0 {
		availability = "partial"
	}
	if record.Status != availability || !sameKeys(record.Comparison, mapKeys(request.Questions)) {
		return record, errors.New("consultation availability or comparison questions differ")
	}
	for id := range request.Questions {
		choices, missing := map[string]string{}, map[string]string{}
		for _, name := range adviserIDs {
			provider := record.Providers[name]
			if provider.Result != nil {
				answer := provider.Result.Answers[id]
				if answer.Type == "choice" {
					choices[name] = answer.Choice
				} else {
					missing[name] = "refusal"
				}
			} else {
				missing[name] = provider.Status
			}
		}
		status := "unavailable"
		if len(choices) > 0 {
			status = "partial"
		}
		if len(choices) == 3 {
			status = "agreement"
			first := ""
			for _, choice := range choices {
				if first == "" {
					first = choice
				}
				if choice != first {
					status = "disagreement"
				}
			}
		}
		compared := record.Comparison[id]
		if compared.Choices == nil || compared.Missing == nil || compared.Status != status || !stringMapsEqual(compared.Choices, choices) || !stringMapsEqual(compared.Missing, missing) {
			return record, errors.New("comparison differs from independent provider answers")
		}
	}
	if record.FinalDecision.Status == "pending" {
		if record.FinalDecision.Questions == nil || len(record.FinalDecision.Questions) != 0 {
			return record, errors.New("pending decision contains adjudication")
		}
		return record, nil
	}
	if !sameKeys(record.FinalDecision.Questions, mapKeys(request.Questions)) {
		return record, errors.New("Codex adjudication must cover every question")
	}
	resolved := true
	for id, item := range record.FinalDecision.Questions {
		if !nonblank(item.Reason) || !textList(item.Evidence) || !textList(item.Verification) || item.RejectedJudgments == nil {
			return record, errors.New("invalid Coordinator decision references or reason")
		}
		if item.Status == "unresolved" {
			resolved = false
			if item.Choice != nil || len(item.RejectedJudgments) != 0 {
				return record, errors.New("unresolved decision cannot select a judgment")
			}
			continue
		}
		if item.Status != "resolved" || item.Choice == nil || *item.Choice == "insufficient_evidence" || len(item.Evidence) == 0 || len(item.Verification) == 0 {
			return record, errors.New("resolved Codex judgment needs a choice, sources and verification")
		}
		if _, ok := request.Questions[id].Criteria[*item.Choice]; !ok {
			return record, errors.New("Codex selected an unknown choice")
		}
		expectedRejections := []string{}
		for name, choice := range record.Comparison[id].Choices {
			if choice != *item.Choice {
				expectedRejections = append(expectedRejections, name)
			}
		}
		if !sameKeys(item.RejectedJudgments, expectedRejections) {
			return record, errors.New("Codex must explain every differing provider judgment")
		}
		for _, reason := range item.RejectedJudgments {
			if !nonblank(reason) {
				return record, errors.New("empty provider rejection reason")
			}
		}
	}
	expectedStatus := "unresolved"
	if resolved {
		expectedStatus = "resolved"
	}
	if record.FinalDecision.Status != expectedStatus {
		return record, errors.New("Codex final status differs from its questions")
	}
	return record, nil
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			return false
		}
	}
	return true
}

func AttachConsultation(ctx context.Context, path, phase, inputPath string) (Record, error) {
	if !phaseValid(phase) {
		return Record{}, errors.New("phase must be before or final")
	}
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return Record{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Record{}, err
	}
	if !info.Mode().IsRegular() {
		return Record{}, errors.New("consultation input must be a regular file, not a symlink")
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Record{}, err
	}
	file, err := os.Open(abs)
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Record{}, errors.New("consultation input changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxInputBytes+1))
	if err != nil {
		return Record{}, err
	}
	if len(raw) > MaxInputBytes {
		return Record{}, errors.New("consultation exceeds the 8 MiB input limit")
	}
	return update(path, func(root *os.Root, record *Record) error {
		for other, attachment := range record.Consultations {
			if other != phase {
				otherInfo, statErr := os.Stat(attachment.SourcePath)
				if attachment.SourcePath == abs || statErr == nil && os.SameFile(info, otherInfo) {
					return errors.New("before and final consultation must use distinct original artifact files")
				}
			}
		}
		current, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		expected, ok := record.Contexts[phase]
		if !ok || expected.TaskID != record.ID || expected.Phase != phase {
			return errors.New("prepare the matching task phase context first")
		}
		if !equalSnapshot(expected.Snapshot, current) {
			return errors.New("consultation context is stale against current source")
		}
		if phase == "before" && !equalSnapshot(expected.Snapshot, record.Baseline) {
			return errors.New("before consultation differs from task baseline")
		}
		if phase == "final" {
			if problems := localEvidenceProblems(root, *record, current); len(problems) > 0 {
				return fmt.Errorf("final consultation requires completed evidence: %v", problems)
			}
			binding, err := evidenceBinding(*record)
			if err != nil {
				return err
			}
			if expected.EvidenceSHA256 != binding {
				return errors.New("final consultation context is stale against completed task evidence")
			}
		}
		consultation, err := ValidateConsultation(raw, expected)
		if err != nil {
			return err
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		name := "consultation-" + phase + "-" + id + ".json"
		if err := writeNew(root, name, raw); err != nil {
			return err
		}
		statuses := map[string]string{}
		for name, value := range consultation.Providers {
			statuses[name] = value.Status
		}
		record.Consultations[phase] = Attachment{Phase: phase, SourcePath: abs, File: name, SHA256: digest(raw), Context: expected, DecisionStatus: consultation.FinalDecision.Status, ProviderStatuses: statuses}
		record.Verification = nil
		return nil
	})
}

func readConsultation(root *os.Root, record Record, phase string, snapshot projectctx.Snapshot) (Consultation, error) {
	attachment, ok := record.Consultations[phase]
	if !ok {
		return Consultation{}, errors.New("required phase artifact is missing")
	}
	expected := Context{TaskID: record.ID, Phase: phase, Snapshot: snapshot}
	if phase == "final" {
		var err error
		expected.EvidenceSHA256, err = evidenceBinding(record)
		if err != nil {
			return Consultation{}, err
		}
	}
	if attachment.Phase != phase || attachment.Context != expected {
		return Consultation{}, errors.New("phase artifact is bound to stale or mismatched source or completed task evidence")
	}
	data, err := privateRead(root, attachment.File, MaxInputBytes)
	if err != nil {
		return Consultation{}, err
	}
	if digest(data) != attachment.SHA256 {
		return Consultation{}, errors.New("consultation artifact hash differs")
	}
	consultation, err := ValidateConsultation(data, expected)
	if err != nil {
		return consultation, err
	}
	if consultation.FinalDecision.Status != "resolved" || attachment.DecisionStatus != "resolved" {
		return consultation, errors.New("material Codex consultation decision remains pending or unresolved")
	}
	if !sameKeys(attachment.ProviderStatuses, adviserIDs) {
		return consultation, errors.New("consultation availability metadata omits required providers")
	}
	for name, provider := range consultation.Providers {
		if attachment.ProviderStatuses[name] != provider.Status {
			return consultation, errors.New("consultation availability metadata differs")
		}
	}
	return consultation, nil
}
