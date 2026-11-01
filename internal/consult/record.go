package consult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"slices"
	"time"
)

var pairProviders = []string{jevProvider, snapProvider}

var errorCategories = []string{"missing_credential", "deadline_unavailable", "timeout", "http_error", "network_error", "invalid_response", "worker_error"}

var disabledReasons = []string{"missing_credential", "excluded"}

func pendingDecision() *object {
	return newObject("status", "pending", "questions", newObject())
}

// sharedRequest applies the common contract every participating adviser accepts.
func sharedRequest(value any) (*object, error) {
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

func disabled(prepared *object, reason string) *object {
	return newObject("request", prepared, "requested_model", prepared.get("model"), "status", "disabled", "reason", reason, "duration_seconds", 0.0)
}

// batch describes one bounded evidence collection across its advisers.
type batch struct {
	credentials map[string]string
	deadline    time.Duration
	disabled    map[string]string
	// required lists the providers that cannot be disabled; nil means all.
	required []string
}

type collector func(context.Context, *object, batch) (map[string]any, error)

func (b batch) check(prepared *object) error {
	required := b.required
	if required == nil {
		required = prepared.names
	}
	for _, name := range required {
		if !prepared.has(name) {
			return errors.New("required provider cannot be disabled")
		}
	}
	for name := range b.disabled {
		if !prepared.has(name) || slices.Contains(required, name) {
			return errors.New("required provider cannot be disabled")
		}
	}
	return nil
}

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

// collect runs one attempt per active adviser concurrently under a single
// deadline. That deadline bounds connecting, sending and reading the complete
// response. It returns at the deadline even if a provider has not yet stopped.
func (t transport) collect(ctx context.Context, prepared *object, options batch) (map[string]any, error) {
	if err := options.check(prepared); err != nil {
		return nil, err
	}
	records := map[string]any{}
	var active []string
	for _, name := range prepared.names {
		value := prepared.get(name).(*object)
		if reason, excluded := options.disabled[name]; excluded {
			records[name] = disabled(value, reason)
		} else if options.credentials[name] == "" {
			records[name] = unavailable(value, "missing_credential", 0.0)
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
func safeRecord(raw any, prepared *object, digest, provider string, allowDisabled bool) (*object, error) {
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
	case status == "disabled":
		reason, ok := text(record.get("reason"))
		if !allowDisabled || !isOpenAI || !ok || !slices.Contains(disabledReasons, reason) || !equal(duration, integer("0")) {
			return nil, errors.New("invalid disabled provider")
		}
		result.set("reason", reason)
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

func comparison(shared, providers *object) *object {
	compared := newObject()
	enabled := 0
	for _, name := range providers.names {
		if providers.get(name).(*object).get("status") != "disabled" {
			enabled++
		}
	}
	extended := providers.has(openAIProvider)
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
		if choices.size() > 0 {
			status = "partial"
		}
		if choices.size() > 0 && choices.size() == enabled {
			status = "agreement"
			for _, name := range choices.names {
				if choices.get(name) != choices.get(choices.names[0]) {
					status = "disagreement"
				}
			}
		}
		item := newObject("status", status, "choices", choices)
		if extended {
			item.set("missing", missing)
		}
		compared.set(id, item)
	}
	return compared
}

func availability(providers *object) string {
	enabled, succeeded, answered := 0, 0, 0
	for _, name := range providers.names {
		status := providers.get(name).(*object).get("status")
		if status == "disabled" {
			continue
		}
		enabled++
		if status == "success" {
			succeeded++
		}
		if status == "success" || status == "partial" {
			answered++
		}
	}
	switch {
	case enabled > 0 && succeeded == enabled:
		return "success"
	case answered > 0:
		return "partial"
	default:
		return "unavailable"
	}
}

func expectedProviders(version int) []string {
	if version == 1 {
		return pairProviders
	}
	return providerOrder
}

// makeRecord builds the retained consultation record. A malformed provider
// record becomes that provider's invalid_response and never affects another.
func makeRecord(shared, prepared *object, raw map[string]any, version int) (*object, error) {
	if version == 0 {
		version = 1
		if prepared.has(openAIProvider) {
			version = 3
		}
	}
	if version < 1 || version > 3 {
		return nil, errors.New("invalid consultation record version")
	}
	if !prepared.exactly(expectedProviders(version)...) {
		return nil, errors.New("invalid prepared providers")
	}
	digest, err := fingerprint(shared)
	if err != nil {
		return nil, err
	}
	providers := newObject()
	for _, name := range prepared.names {
		value := prepared.get(name).(*object)
		record, err := safeRecord(raw[name], value, digest, name, version == 2)
		if err != nil {
			record, err = safeRecord(unavailable(value, "invalid_response", 0.0), value, digest, name, false)
			if err != nil {
				return nil, err
			}
		}
		providers.set(name, record)
	}
	return newObject("version", number(version), "status", availability(providers), "request", shared, "request_sha256", digest,
		"providers", providers, "comparison", comparison(shared, providers), "final_decision", pendingDecision()), nil
}

func textValue(value any) bool {
	content, ok := text(value)
	return ok && !blank(content)
}

func textList(value any) bool {
	items, ok := value.([]any)
	return ok && !slices.ContainsFunc(items, func(item any) bool { return !textValue(item) })
}

func recordVersion(value any) (int, bool) {
	switch value {
	case integer("1"):
		return 1, true
	case integer("2"):
		return 2, true
	case integer("3"):
		return 3, true
	}
	return 0, false
}

// verifyStored proves that retained provider evidence still normalizes to
// exactly what is stored, independent of the policy now in force.
func verifyStored(record, shared *object, digest string, version int) (*object, error) {
	stored, ok := mapping(record.get("providers"))
	if !ok || !stored.exactly(expectedProviders(version)...) {
		return nil, errors.New("invalid recorded providers")
	}
	providers := newObject()
	for _, name := range stored.names {
		item, ok := mapping(stored.get(name))
		if !ok {
			return nil, errors.New("invalid stored provider")
		}
		model, ok := text(item.get("requested_model"))
		valid := ok && model == snapModel
		if name == jevProvider {
			valid = ok && pinnedJev.MatchString(model)
		} else if name == openAIProvider {
			valid = ok && model == openAIModel
		}
		if !valid {
			return nil, errors.New("invalid stored provider model")
		}
		if item.get("request_sha256") != digest {
			return nil, errors.New("provider evidence fingerprint differs")
		}
		prepared, err := prepareProvider(name, shared, model)
		if err != nil {
			return nil, err
		}
		safe, err := safeRecord(item.with("request", prepared), prepared, digest, name, version == 2)
		if err != nil {
			return nil, err
		}
		if !equal(safe, item) {
			return nil, errors.New("stored provider contains inconsistent fields")
		}
		providers.set(name, safe)
	}
	return providers, nil
}

func checkDecisionItem(value any, id string, shared, compared *object) (bool, error) {
	item, ok := mapping(value)
	if !ok || !item.exactly("status", "choice", "reason", "evidence", "verification", "rejected_judgments") {
		return false, errors.New("invalid decision item")
	}
	status := item.get("status")
	if status != "resolved" && status != "unresolved" {
		return false, errors.New("invalid decision status")
	}
	if !textValue(item.get("reason")) {
		return false, errors.New("decision needs a reason")
	}
	if !textList(item.get("evidence")) || !textList(item.get("verification")) {
		return false, errors.New("invalid decision references")
	}
	rejected, ok := mapping(item.get("rejected_judgments"))
	if !ok || slices.ContainsFunc(rejected.names, func(name string) bool { return !textValue(rejected.get(name)) }) {
		return false, errors.New("invalid rejected judgment reasons")
	}
	if status != "resolved" {
		if item.get("choice") != nil || rejected.size() > 0 {
			return false, errors.New("unresolved decision must not choose or reject")
		}
		return false, nil
	}
	criteria := shared.get("questions").(*object).get(id).(*object).get("criteria").(*object)
	choice, ok := text(item.get("choice"))
	if !ok || !criteria.has(choice) {
		return false, errors.New("decision choice is not an allowed option")
	}
	if choice == "insufficient_evidence" {
		return false, errors.New("insufficient evidence remains unresolved")
	}
	if len(item.get("evidence").([]any)) == 0 || len(item.get("verification").([]any)) == 0 {
		return false, errors.New("resolved decision needs evidence and verification")
	}
	choices := compared.get(id).(*object).get("choices").(*object)
	var expected []string
	for _, name := range choices.names {
		if choices.get(name) != choice {
			expected = append(expected, name)
		}
	}
	if !rejected.exactly(expected...) {
		return false, errors.New("decision must explain each rejected provider judgment")
	}
	return true, nil
}

// adjudicate validates the coordinator's recorded disposition without hosted requests,
// the current policy, or voting. The record must first prove itself unchanged.
func adjudicate(recordValue, decisionValue any) (*object, error) {
	record, ok := mapping(recordValue)
	if !ok || !record.exactly("version", "status", "request", "request_sha256", "providers", "comparison", "final_decision") {
		return nil, errors.New("invalid consultation record")
	}
	version, ok := recordVersion(record.get("version"))
	if !ok {
		return nil, errors.New("invalid consultation record")
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
	providers, err := verifyStored(record, shared, digest, version)
	if err != nil {
		return nil, err
	}
	compared := comparison(shared, providers)
	if !equal(record.get("comparison"), compared) || record.get("status") != availability(providers) {
		return nil, errors.New("recorded comparison differs from provider answers")
	}
	if !equal(record.get("final_decision"), pendingDecision()) {
		return nil, errors.New("consultation already has a decision")
	}
	decision, ok := mapping(decisionValue)
	if !ok || !decision.exactly("questions") {
		return nil, errors.New("invalid decision fields")
	}
	questions, ok := mapping(decision.get("questions"))
	if !ok || !questions.sameNames(shared.get("questions").(*object)) {
		return nil, errors.New("decision must cover every question")
	}
	status := "resolved"
	for _, id := range questions.names {
		resolved, err := checkDecisionItem(questions.get(id), id, shared, compared)
		if err != nil {
			return nil, err
		}
		if !resolved {
			status = "unresolved"
		}
	}
	return record.with("final_decision", newObject("status", status, "questions", questions)), nil
}
