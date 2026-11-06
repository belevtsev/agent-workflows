package consult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
)

// recordVersion is the only consultation record version this cw writes or reads.
const recordVersion = 4

const maxQuestions = 8

var errorCategories = []string{"timeout", "http_error", "network_error", "invalid_response", "worker_error"}

func pendingDecision() *object {
	return newObject("status", "pending", "questions", newObject())
}

// sharedRequest applies the common contract every participating adviser
// accepts, plus the structural authoring checks: at most eight Choice
// questions, a valid state.untrusted marking and a valid task adviser binding.
func sharedRequest(value any) (*object, error) {
	request, err := sharedContract(value)
	if err != nil {
		return nil, err
	}
	if questions := request.get("questions").(*object); questions.size() > maxQuestions {
		return nil, fmt.Errorf("request has %d questions; at most %d are allowed", questions.size(), maxQuestions)
	}
	return request, nil
}

// sharedContract is sharedRequest without the question count limit, which
// binds consultation requests but not the synthetic evaluation's fixed
// twelve-case batches.
func sharedContract(value any) (*object, error) {
	request, ok := mapping(value)
	if !ok || !request.exactly("state", "questions") {
		return nil, errors.New("shared request requires only state and questions")
	}
	switch request.get("state").(type) {
	case string, []any, *object:
	default:
		return nil, errors.New("invalid state type")
	}
	if _, err := prepareRequest(request, sharedJevModel); err != nil {
		return nil, err
	}
	questions := request.get("questions").(*object)
	for _, name := range questions.names {
		if codePoints(name) > 100 {
			return nil, errors.New("question ID exceeds the shared limit")
		}
	}
	for _, name := range questions.names {
		if questions.get(name).(*object).get("criteria").(*object).size() > 255 {
			return nil, errors.New("Choice exceeds the shared option limit")
		}
	}
	if err := checkUntrusted(request.get("state")); err != nil {
		return nil, err
	}
	if _, _, err := taskBinding(request.get("state")); err != nil {
		return nil, err
	}
	return request, nil
}

func fingerprint(value any) (string, error) {
	body, err := canonicalFormat.marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func unavailable(prepared *object, reason string, duration float64) *object {
	return newObject("request", prepared, "requested_model", prepared.get("model"), "status", "unavailable", "error", category(reason), "duration_seconds", duration)
}

// notConfigured is the raw record of a participating adviser that was not
// attempted because its credential is empty.
func notConfigured(prepared *object) *object {
	return newObject("request", prepared, "requested_model", prepared.get("model"), "status", "not_configured", "duration_seconds", 0.0)
}

// batch describes one bounded evidence collection across its advisers.
type batch struct {
	credentials map[string]string
	deadline    time.Duration
}

// collector gathers one raw provider record for each prepared adviser.
type collector func(context.Context, *object, batch) (map[string]any, error)

// consultWorker owns one provider attempt. A panic value can retain transport
// details, so only its category survives and the other advisers are unaffected.
func (t transport) consultWorker(ctx context.Context, provider string, prepared *object, credential string) (record *object) {
	defer func() {
		if recover() != nil {
			record = unavailable(prepared, "worker_error", 0.0)
		}
	}()
	if ctx.Err() != nil {
		return unavailable(prepared, "timeout", 0.0)
	}
	if provider == openAIProvider {
		record, _ = t.consultOpenAI(ctx, prepared, credential)
	} else {
		record, _ = t.consultPinned(ctx, provider, prepared, credential)
	}
	return record
}

// collect runs one attempt per configured adviser concurrently under a single
// deadline. That deadline bounds connecting, sending and reading the complete
// response. It returns at the deadline even if a provider has not yet stopped.
// An adviser without a credential is recorded as not_configured and never
// contacted; with none configured, collect makes no network call.
func (t transport) collect(ctx context.Context, prepared *object, options batch) (map[string]any, error) {
	records := map[string]any{}
	var active []string
	for _, name := range prepared.names {
		value := prepared.get(name).(*object)
		if options.credentials[name] == "" {
			records[name] = notConfigured(value)
		} else if name == openAIProvider && !requestSupported(value) {
			records[name] = unavailable(value, "unsupported_request", 0.0)
		} else {
			active = append(active, name)
		}
	}
	if len(active) == 0 {
		return records, nil
	}
	started := time.Now()
	bounded, cancel := context.WithTimeout(ctx, options.deadline)
	defer cancel()
	type result struct {
		name   string
		record *object
	}
	results := make(chan result, len(active))
	for _, name := range active {
		go func() {
			value := prepared.get(name).(*object)
			results <- result{name, t.consultWorker(bounded, name, value, options.credentials[name])}
		}()
	}
	for range active {
		select {
		case item := <-results:
			records[item.name] = item.record
		case <-bounded.Done():
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			elapsed := seconds(started)
			for _, name := range active {
				if _, finished := records[name]; !finished {
					records[name] = unavailable(prepared.get(name).(*object), "timeout", elapsed)
				}
			}
			return records, nil
		}
	}
	return records, nil
}

func validDuration(value any) bool {
	var duration float64
	switch item := value.(type) {
	case integer:
		duration = item.float()
	case float64:
		duration = item
	default:
		return false
	}
	return !math.IsInf(duration, 0) && !math.IsNaN(duration) && duration >= 0
}

// safeRecord keeps only the normalized fields of one provider's evidence.
func safeRecord(raw any, prepared *object, digest, provider string) (*object, error) {
	record, ok := mapping(raw)
	if !ok {
		return nil, errors.New("invalid provider record")
	}
	if !equal(record.get("requested_model"), prepared.get("model")) {
		return nil, errors.New("provider requested model differs")
	}
	if !equal(record.get("request"), prepared) {
		return nil, errors.New("provider evidence differs from shared request")
	}
	duration := record.get("duration_seconds")
	if !validDuration(duration) {
		return nil, errors.New("invalid provider duration")
	}
	status := record.get("status")
	result := newObject("status", status, "requested_model", prepared.get("model"), "request_sha256", digest, "duration_seconds", duration)
	isOpenAI := provider == openAIProvider
	switch {
	case status == "not_configured":
		if !equal(duration, integer("0")) {
			return nil, errors.New("invalid not_configured provider")
		}
	case status == "success" || isOpenAI && (status == "partial" || status == "refusal"):
		var validated *object
		var err error
		if isOpenAI {
			validated, err = validateOpenAIResult(record.get("result"), prepared)
		} else {
			validated, err = validateResponse(record.get("result"), prepared)
		}
		if err != nil {
			return nil, err
		}
		if isOpenAI && status != responseStatus(validated) {
			return nil, errors.New("provider status differs from answers")
		}
		if !equal(record.get("resolved_model"), validated.get("model")) {
			return nil, errors.New("provider resolved model differs")
		}
		result.set("resolved_model", validated.get("model"))
		result.set("result", validated)
		result.set("usage", validated.get("usage"))
	default:
		if status != "unavailable" {
			return nil, errors.New("invalid provider status")
		}
		detail, err := safeFailure(record.get("error"), isOpenAI)
		if err != nil {
			return nil, err
		}
		result.set("error", detail)
	}
	return result, nil
}

func safeFailure(value any, isOpenAI bool) (*object, error) {
	failure, ok := mapping(value)
	if !ok {
		return nil, errors.New("invalid provider failure")
	}
	name, ok := text(failure.get("category"))
	if !ok || !slices.Contains(errorCategories, name) && !(isOpenAI && name == "unsupported_request") {
		return nil, errors.New("invalid provider failure")
	}
	detail := category(name)
	if name == "http_error" {
		code, ok := failure.get("http_status").(integer)
		if !ok || !validHTTPStatus(code) {
			return nil, errors.New("invalid HTTP status")
		}
		detail.set("http_status", code)
	}
	return detail, nil
}

func validHTTPStatus(code integer) bool {
	return !code.negative() && len(code) == 3 && code >= "100" && code <= "599"
}

// attempted counts the participating advisers that were configured, and so
// received the request.
func attempted(providers *object) int {
	count := 0
	for _, name := range providers.names {
		if status, _ := providers.get(name).(*object).get("status").(string); AttemptedStatus(status) {
			count++
		}
	}
	return count
}

// comparison reports, per question, which attempted advisers chose which
// option and why every other participating adviser has no choice.
func comparison(shared, providers *object) *object {
	compared := newObject()
	tried := attempted(providers)
	questions := shared.get("questions").(*object)
	for _, id := range questions.names {
		choices, missing := newObject(), newObject()
		for _, name := range providers.names {
			record := providers.get(name).(*object)
			status := record.get("status")
			if status == "success" || status == "partial" || status == "refusal" {
				answer := record.get("result").(*object).get("answers").(*object).get(id).(*object)
				if answer.get("type") == "choice" {
					choices.set(name, answer.get("choice"))
				} else {
					missing.set(name, "refusal")
				}
			} else {
				missing.set(name, status)
			}
		}
		status := "unavailable"
		switch {
		case tried == 0:
			status = "no_advisers"
		case choices.size() == tried:
			status = "agreement"
			for _, name := range choices.names {
				if choices.get(name) != choices.get(choices.names[0]) {
					status = "disagreement"
				}
			}
		case choices.size() > 0:
			status = "partial"
		}
		compared.set(id, newObject("status", status, "choices", choices, "missing", missing))
	}
	return compared
}

// availability is the record status over the attempted advisers.
func availability(providers *object) string {
	tried, succeeded, answered := 0, 0, 0
	for _, name := range providers.names {
		status, _ := providers.get(name).(*object).get("status").(string)
		if !AttemptedStatus(status) {
			continue
		}
		tried++
		if status == "success" {
			succeeded++
		}
		if status == "success" || status == "partial" {
			answered++
		}
	}
	switch {
	case tried == 0:
		return "no_advisers_configured"
	case succeeded == tried:
		return "success"
	case answered > 0:
		return "partial"
	default:
		return "unavailable"
	}
}

// fixedOrder reports whether names are distinct known advisers in the record's
// fixed order.
func fixedOrder(names []string) bool {
	next := 0
	for _, name := range names {
		index := slices.Index(providerOrder, name)
		if index < next {
			return false
		}
		next = index + 1
	}
	return true
}

// makeRecord builds the retained version 4 consultation record. prepared holds
// every participating adviser, configured or not, and raw one provider record
// for each. A malformed provider record becomes that provider's
// invalid_response and never affects another.
func makeRecord(shared, prepared *object, raw map[string]any) (*object, error) {
	if !fixedOrder(prepared.names) {
		return nil, errors.New("invalid prepared providers")
	}
	digest, err := fingerprint(shared)
	if err != nil {
		return nil, err
	}
	providers := newObject()
	for _, name := range prepared.names {
		value := prepared.get(name).(*object)
		record, err := safeRecord(raw[name], value, digest, name)
		if err != nil {
			record, err = safeRecord(unavailable(value, "invalid_response", 0.0), value, digest, name)
			if err != nil {
				return nil, err
			}
		}
		providers.set(name, record)
	}
	return newObject("version", number(recordVersion), "status", availability(providers), "request", shared, "request_sha256", digest,
		"providers", providers, "comparison", comparison(shared, providers), "final_decision", pendingDecision()), nil
}

func textValue(value any) bool {
	content, ok := text(value)
	return ok && !blank(content)
}

// checkVersion accepts only the integer 4.
func checkVersion(value any) error {
	if value == integer("4") {
		return nil
	}
	shown := "missing"
	if value != nil {
		if data, err := wireFormat.marshal(value); err == nil {
			shown = string(data)
		}
	}
	return fmt.Errorf("consultation record version %s is not supported; this cw reads version %d", shown, recordVersion)
}

// verifyStored proves that retained provider evidence still normalizes to
// exactly what is stored, independent of the policy now in force, and that a
// task-bound request recorded exactly its bound advisers.
func verifyStored(record, shared *object, digest string) (*object, error) {
	stored, ok := mapping(record.get("providers"))
	if !ok {
		return nil, errors.New("invalid recorded providers")
	}
	for _, name := range stored.names {
		if !slices.Contains(providerOrder, name) {
			return nil, fmt.Errorf("record names unknown adviser %q", name)
		}
	}
	if !fixedOrder(stored.names) {
		return nil, errors.New("recorded providers are not in the fixed adviser order")
	}
	binding, bound, err := taskBinding(shared.get("state"))
	if err != nil {
		return nil, err
	}
	providers := newObject()
	for _, name := range stored.names {
		item, ok := mapping(stored.get(name))
		if !ok {
			return nil, fmt.Errorf("invalid stored provider %s", name)
		}
		model, ok := text(item.get("requested_model"))
		if !ok || !validModel(name, model) {
			return nil, fmt.Errorf("invalid stored model for provider %s", name)
		}
		if bound && binding[name] != model {
			return nil, fmt.Errorf("provider %s differs from the task's adviser binding", name)
		}
		if item.get("request_sha256") != digest {
			return nil, fmt.Errorf("provider %s evidence fingerprint differs", name)
		}
		prepared, err := prepareProvider(name, shared, model)
		if err != nil {
			return nil, err
		}
		safe, err := safeRecord(item.with("request", prepared), prepared, digest, name)
		if err != nil {
			return nil, err
		}
		if !equal(safe, item) {
			return nil, fmt.Errorf("stored provider %s contains inconsistent fields", name)
		}
		providers.set(name, safe)
	}
	if bound && len(binding) != providers.size() {
		return nil, errors.New("recorded providers differ from the task's adviser binding")
	}
	return providers, nil
}

// checkedRecord is a version 4 record whose request, provider evidence,
// comparison and availability have been revalidated.
type checkedRecord struct {
	record, shared, providers, compared *object
}

// checkRecord revalidates everything in a consultation record except its
// final decision.
func checkRecord(value any) (*checkedRecord, error) {
	record, ok := mapping(value)
	if !ok {
		return nil, errors.New("invalid consultation record")
	}
	if err := checkVersion(record.get("version")); err != nil {
		return nil, err
	}
	if !record.exactly("version", "status", "request", "request_sha256", "providers", "comparison", "final_decision") {
		return nil, errors.New("consultation record fields differ from version 4")
	}
	shared, err := sharedRequest(record.get("request"))
	if err != nil {
		return nil, err
	}
	digest, err := fingerprint(shared)
	if err != nil {
		return nil, err
	}
	if record.get("request_sha256") != digest {
		return nil, errors.New("shared evidence fingerprint differs")
	}
	providers, err := verifyStored(record, shared, digest)
	if err != nil {
		return nil, err
	}
	compared := comparison(shared, providers)
	if !equal(record.get("comparison"), compared) {
		return nil, errors.New("recorded comparison differs from provider answers")
	}
	if record.get("status") != availability(providers) {
		return nil, errors.New("recorded status differs from provider availability")
	}
	return &checkedRecord{record: record, shared: shared, providers: providers, compared: compared}, nil
}
