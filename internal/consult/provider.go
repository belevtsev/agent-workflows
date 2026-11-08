package consult

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
)

const (
	jevEndpoint      = "https://api.typesafe.ai/v1/systemone"
	snapEndpoint     = "https://api.subq.ai/v1/systemone"
	openAIEndpoint   = "https://api.openai.com/v1/decisions"
	sharedJevModel   = "jev-1.14.0"
	maxResponseBytes = 1024 * 1024
	sumTolerance     = 1e-6
	maxOpenAIItems   = 200
	maxOpenAIText    = 1048576
	maxOpenAIInput   = 10485760
)

const (
	jevProvider    = "jev"
	snapProvider   = "subq_snap"
	openAIProvider = "openai_decisions"
)

// providerOrder is the record's adviser order, the known provider IDs of the
// policy pin rules. Only these fixed services and these credential variables
// can ever receive evidence or be read.
var providerOrder = modelpolicy.Providers()

// The single pinned models come from the policy pin rules in internal/modelpolicy.
var (
	snapModel, _   = modelpolicy.PinnedModel(snapProvider)
	openAIModel, _ = modelpolicy.PinnedModel(openAIProvider)
)

var credentialNames = map[string]string{
	jevProvider:    "TYPESAFE_API_KEY",
	snapProvider:   "SUBQ_API_KEY",
	openAIProvider: "OPENAI_API_KEY",
}

var fixedEndpoints = map[string]string{
	jevProvider:    jevEndpoint,
	snapProvider:   snapEndpoint,
	openAIProvider: openAIEndpoint,
}

// transport makes one bounded attempt per provider. Its endpoints are fixed
// outside package tests; no flag, policy or environment value can redirect them.
type transport struct {
	client    *http.Client
	endpoints map[string]string
}

func newTransport() transport {
	// One connection per attempt leaves net/http nothing to replay, and these are
	// the HTTP/1.1, uncompressed, close-delimited exchanges the advisers receive.
	// This is a new Transport because a clone of the default one keeps offering h2
	// over TLS, which an HTTP/1 client then cannot read. The batch deadline is the
	// only bound, covering every phase of the exchange.
	base := &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true, DisableCompression: true, Protocols: new(http.Protocols)}
	base.Protocols.SetHTTP1(true)
	return transport{endpoints: fixedEndpoints, client: &http.Client{
		Transport: base,
		// A redirect is unavailable advice; never send credentials to a new URL.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func category(name string) *object { return newObject("category", name) }

func transportFailure(ctx context.Context, err error) *object {
	var network net.Error
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return category("timeout")
	}
	return category("network_error")
}

// exchange posts one payload and returns its strictly decoded response, or a
// sanitized failure. Transport errors can name request details and are dropped.
func (t transport) exchange(ctx context.Context, provider, credential string, payload []byte) (any, *object) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoints[provider], bytes.NewReader(payload))
	if err != nil {
		return nil, category("network_error")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Accept-Encoding", "identity")
	response, err := t.client.Do(request)
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		detail := category("http_error")
		if response.StatusCode >= 100 && response.StatusCode <= 599 {
			detail.set("http_status", number(response.StatusCode))
		}
		return nil, detail
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	// A truncated body is a malformed response, not a failed connection.
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, transportFailure(ctx, err)
	}
	if len(body) > maxResponseBytes {
		return nil, category("invalid_response")
	}
	raw, err := parseJSON(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")))
	if err != nil {
		return nil, category("invalid_response")
	}
	return raw, nil
}

func seconds(started time.Time) float64 { return round6(time.Since(started).Seconds()) }

func round6(value float64) float64 {
	result, _ := strconv.ParseFloat(strconv.FormatFloat(value, 'f', 6, 64), 64)
	return result
}

func jsonValue(value any) error {
	switch item := value.(type) {
	case float64:
		if math.IsInf(item, 0) || math.IsNaN(item) {
			return errors.New("nonfinite JSON number")
		}
	case []any:
		for _, entry := range item {
			if err := jsonValue(entry); err != nil {
				return err
			}
		}
	case *object:
		for _, name := range item.names {
			if err := jsonValue(item.get(name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func description(value any) error {
	valid := false
	switch item := value.(type) {
	case string:
		valid = !blank(item)
	case []any:
		valid = len(item) > 0
	case *object:
		valid = item.size() > 0
	}
	if !valid {
		return errors.New("empty or invalid description")
	}
	return jsonValue(value)
}

func validateChoice(value any) error {
	question, ok := mapping(value)
	if !ok {
		return errors.New("question must be a mapping")
	}
	if !question.exactly("type", "instructions", "criteria") {
		return errors.New("invalid Choice fields")
	}
	if question.get("type") != "choice" {
		return errors.New("only Choice questions are supported")
	}
	if err := description(question.get("instructions")); err != nil {
		return err
	}
	criteria, ok := mapping(question.get("criteria"))
	if !ok || criteria.size() < 2 {
		return errors.New("Choice needs at least two options")
	}
	for _, name := range criteria.names {
		if blank(name) {
			return errors.New("invalid option ID")
		}
	}
	for _, name := range criteria.names {
		if err := description(criteria.get(name)); err != nil {
			return err
		}
	}
	return nil
}

// prepareRequest validates all evidence before anything is serialized or sent.
func prepareRequest(value any, model string) (*object, error) {
	request, ok := mapping(value)
	if !ok || !request.exactly("state", "questions") && !request.exactly("state", "questions", "model") {
		return nil, errors.New("invalid request fields")
	}
	if request.has("model") && request.get("model") != model {
		return nil, errors.New("request conflicts with pinned model")
	}
	if err := jsonValue(request.get("state")); err != nil {
		return nil, err
	}
	questions, ok := mapping(request.get("questions"))
	if !ok || questions.size() == 0 {
		return nil, errors.New("questions must be a nonempty mapping")
	}
	for _, name := range questions.names {
		if blank(name) {
			return nil, errors.New("invalid question ID")
		}
	}
	for _, name := range questions.names {
		if err := validateChoice(questions.get(name)); err != nil {
			return nil, err
		}
	}
	return newObject("model", model, "state", request.get("state"), "questions", questions), nil
}

func prepareOpenAI(shared any, model string) (*object, error) {
	if model != openAIModel {
		return nil, errors.New("invalid OpenAI Decisions model")
	}
	return prepareRequest(shared, model)
}

func prepareProvider(name string, shared any, model string) (*object, error) {
	if name == openAIProvider {
		return prepareOpenAI(shared, model)
	}
	return prepareRequest(shared, model)
}

func preparedQuestions(prepared *object) *object {
	questions, _ := prepared.get("questions").(*object)
	return questions
}

func probability(value any) error {
	valid := false
	switch item := value.(type) {
	case integer:
		valid = item == "0" || item == "1"
	case float64:
		valid = item >= 0 && item <= 1
	}
	if !valid {
		return errors.New("invalid probability or confidence")
	}
	return nil
}

// sumsToOne adds exactly, so a long distribution cannot pass on rounding alone.
func sumsToOne(values []any) bool {
	sum := new(big.Rat)
	for _, value := range values {
		exact, _ := rational(value)
		sum.Add(sum, exact)
	}
	total, _ := sum.Float64()
	return math.Abs(total-1) <= sumTolerance
}

func floatOf(value any) float64 {
	switch item := value.(type) {
	case integer:
		return item.float()
	case float64:
		return item
	}
	return math.NaN()
}

func tokenCount(value any) (integer, bool) {
	tokens, ok := value.(integer)
	return tokens, ok && !tokens.negative()
}

func validateResponse(value any, prepared *object) (*object, error) {
	response, ok := mapping(value)
	if !ok {
		return nil, errors.New("response must be a mapping")
	}
	if response.get("model") != prepared.get("model") {
		return nil, errors.New("resolved model differs from pinned model")
	}
	questions := preparedQuestions(prepared)
	answers, ok := mapping(response.get("answers"))
	if !ok || !answers.sameNames(questions) {
		return nil, errors.New("invalid answer IDs")
	}
	validated := newObject()
	for _, id := range questions.names {
		answer, ok := mapping(answers.get(id))
		if !ok || answer.get("type") != "choice" {
			return nil, errors.New("invalid answer type")
		}
		options := questions.get(id).(*object).get("criteria").(*object)
		choice, ok := text(answer.get("choice"))
		if !ok || !options.has(choice) {
			return nil, errors.New("invalid chosen option")
		}
		probabilities, ok := mapping(answer.get("probabilities"))
		if !ok || !probabilities.sameNames(options) {
			return nil, errors.New("invalid probability option IDs")
		}
		values := make([]any, 0, probabilities.size())
		highest := math.Inf(-1)
		for _, name := range probabilities.names {
			item := probabilities.get(name)
			if err := probability(item); err != nil {
				return nil, err
			}
			values = append(values, item)
			highest = max(highest, floatOf(item))
		}
		if !sumsToOne(values) {
			return nil, errors.New("invalid probability distribution")
		}
		if floatOf(probabilities.get(choice)) != highest {
			return nil, errors.New("chosen option is not a maximum")
		}
		confidence := answer.get("confidence")
		if err := probability(confidence); err != nil {
			return nil, err
		}
		validated.set(id, newObject("type", "choice", "choice", choice, "probabilities", probabilities, "confidence", confidence))
	}
	usage, ok := mapping(response.get("usage"))
	if !ok {
		return nil, errors.New("invalid response usage")
	}
	safeUsage := newObject()
	for _, field := range []string{"input_tokens", "output_tokens"} {
		tokens, ok := tokenCount(usage.get(field))
		if !ok {
			return nil, errors.New("invalid token usage")
		}
		safeUsage.set(field, tokens)
	}
	// Unknown response fields have no role in a consultation and are never retained.
	return newObject("model", response.get("model"), "answers", validated, "usage", safeUsage), nil
}

func requestSupported(prepared *object) bool {
	questions := preparedQuestions(prepared)
	if questions.size() < 1 || questions.size() > maxOpenAIItems {
		return false
	}
	input, err := canonicalFormat.marshal(prepared.get("state"))
	if err != nil || len(input) > maxOpenAIInput {
		return false
	}
	bounded := func(value any) bool {
		content, ok := text(value)
		return ok && codePoints(content) <= maxOpenAIText
	}
	for _, name := range questions.names {
		question := questions.get(name).(*object)
		if !bounded(question.get("instructions")) {
			return false
		}
		criteria := question.get("criteria").(*object)
		for _, option := range criteria.names {
			if !bounded(criteria.get(option)) {
				return false
			}
		}
	}
	return true
}

// openAIRequestBody maps canonical shared state to deterministic input text;
// the endpoint's encoding must not change the facts or a question's meaning.
func openAIRequestBody(prepared *object) (*object, error) {
	if prepared.get("model") != openAIModel {
		return nil, errors.New("invalid OpenAI Decisions model")
	}
	if !requestSupported(prepared) {
		return nil, errors.New("unsupported OpenAI Decisions request")
	}
	input, err := canonicalFormat.marshal(prepared.get("state"))
	if err != nil {
		return nil, err
	}
	questions := preparedQuestions(prepared)
	items := make([]any, 0, questions.size())
	for _, name := range sortedNames(questions) {
		question := questions.get(name).(*object)
		criteria := question.get("criteria").(*object)
		choices := make([]any, 0, criteria.size())
		for _, option := range sortedNames(criteria) {
			choices = append(choices, newObject("value", option, "description", criteria.get(option)))
		}
		items = append(items, newObject("type", "choice", "name", name, "instructions", question.get("instructions"), "choices", choices))
	}
	return newObject("model", openAIModel, "input", string(input), "questions", items), nil
}

func sortedNames(value *object) []string {
	names := slices.Clone(value.names)
	slices.Sort(names)
	return names
}

func validateOpenAIUsage(value any) (*object, error) {
	usage, ok := mapping(value)
	if !ok {
		return nil, errors.New("invalid response usage")
	}
	result := newObject()
	for _, field := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		tokens, ok := tokenCount(usage.get(field))
		if !ok {
			return nil, errors.New("invalid token usage")
		}
		result.set(field, tokens)
	}
	for _, group := range []struct {
		field string
		names []string
	}{{"input_tokens_details", []string{"cached_tokens", "cache_write_tokens"}}, {"output_tokens_details", []string{"reasoning_tokens"}}} {
		detail, ok := mapping(usage.get(group.field))
		if !ok {
			return nil, errors.New("invalid token usage details")
		}
		tokens := newObject()
		for _, name := range group.names {
			count, ok := tokenCount(detail.get(name))
			if !ok {
				return nil, errors.New("invalid token usage")
			}
			tokens.set(name, count)
		}
		result.set(group.field, tokens)
	}
	return result, nil
}

func validateOpenAIResponse(value any, prepared *object) (*object, error) {
	response, ok := mapping(value)
	if !ok {
		return nil, errors.New("response must be a mapping")
	}
	// Only this model is documented for Decisions. A future snapshot ID needs a
	// documented mapping before it can enter an independently recorded judgment.
	if response.get("model") != openAIModel {
		return nil, errors.New("resolved model differs from supported Decisions model")
	}
	questions := preparedQuestions(prepared)
	answers, ok := response.get("answers").([]any)
	if !ok || len(answers) != questions.size() {
		return nil, errors.New("invalid answer IDs")
	}
	validated := newObject()
	for _, entry := range answers {
		answer, ok := mapping(entry)
		if !ok {
			return nil, errors.New("invalid answer type")
		}
		name, ok := text(answer.get("name"))
		if !ok || !questions.has(name) || validated.has(name) {
			return nil, errors.New("invalid answer IDs")
		}
		if answer.get("type") == "refusal" {
			validated.set(name, newObject("type", "refusal"))
			continue
		}
		if answer.get("type") != "choice" {
			return nil, errors.New("invalid answer type")
		}
		item, err := validateOpenAIChoice(answer, questions.get(name).(*object).get("criteria").(*object))
		if err != nil {
			return nil, err
		}
		validated.set(name, item)
	}
	usage, err := validateOpenAIUsage(response.get("usage"))
	if err != nil {
		return nil, err
	}
	return newObject("model", openAIModel, "answers", validated, "usage", usage), nil
}

func validateOpenAIChoice(answer, options *object) (*object, error) {
	choice, ok := text(answer.get("choice"))
	if !ok || !options.has(choice) {
		return nil, errors.New("invalid chosen option")
	}
	probabilities, ok := answer.get("probabilities").([]any)
	if !ok || len(probabilities) != options.size() {
		return nil, errors.New("invalid probability option IDs")
	}
	distribution := newObject()
	values := make([]any, 0, len(probabilities))
	for _, entry := range probabilities {
		item, ok := mapping(entry)
		if !ok {
			return nil, errors.New("invalid probability option IDs")
		}
		option, ok := text(item.get("value"))
		if !ok || !options.has(option) || distribution.has(option) {
			return nil, errors.New("invalid probability option IDs")
		}
		if err := probability(item.get("probability")); err != nil {
			return nil, err
		}
		distribution.set(option, item.get("probability"))
		values = append(values, item.get("probability"))
	}
	if !sumsToOne(values) {
		return nil, errors.New("invalid probability distribution")
	}
	confidence := answer.get("confidence")
	if err := probability(confidence); err != nil {
		return nil, err
	}
	// Decisions does not document a choice-is-argmax invariant.
	return newObject("type", "choice", "choice", choice, "probabilities", distribution, "confidence", confidence), nil
}

// validateOpenAIResult revalidates a retained normalized result during offline
// adjudication by restoring its wire shape.
func validateOpenAIResult(value any, prepared *object) (*object, error) {
	result, ok := mapping(value)
	if !ok {
		return nil, errors.New("response must be a mapping")
	}
	answers, ok := mapping(result.get("answers"))
	if !ok || !answers.sameNames(preparedQuestions(prepared)) {
		return nil, errors.New("invalid answer IDs")
	}
	wire := make([]any, 0, answers.size())
	for _, name := range answers.names {
		answer, ok := mapping(answers.get(name))
		if !ok {
			return nil, errors.New("invalid answer type")
		}
		item := answer.with("name", name)
		if item.get("type") == "choice" {
			probabilities, ok := mapping(item.get("probabilities"))
			if !ok {
				return nil, errors.New("invalid probability option IDs")
			}
			list := make([]any, 0, probabilities.size())
			for _, option := range probabilities.names {
				list = append(list, newObject("value", option, "probability", probabilities.get(option)))
			}
			item.set("probabilities", list)
		}
		wire = append(wire, item)
	}
	return validateOpenAIResponse(result.with("answers", wire), prepared)
}

func responseStatus(result *object) string {
	answers := result.get("answers").(*object)
	choices := 0
	for _, name := range answers.names {
		if answers.get(name).(*object).get("type") == "choice" {
			choices++
		}
	}
	switch choices {
	case answers.size():
		return "success"
	case 0:
		return "refusal"
	default:
		return "partial"
	}
}

func finishUnavailable(record, detail *object, started time.Time) (*object, int) {
	record.set("status", "unavailable")
	record.set("error", detail)
	record.set("duration_seconds", seconds(started))
	return record, 2
}

func skipped(record *object, name string) (*object, int) {
	record.set("status", "unavailable")
	record.set("error", category(name))
	record.set("duration_seconds", 0.0)
	return record, 2
}

// consultPinned sends the prepared request to Jev or Snap, which share a wire
// contract. Only the explicitly supplied credential reaches the fixed endpoint;
// collect never calls it without one.
func (t transport) consultPinned(ctx context.Context, provider string, prepared *object, credential string) (*object, int) {
	started := time.Now()
	record := newObject("request", prepared, "requested_model", prepared.get("model"))
	payload, err := wireFormat.marshal(prepared)
	if err != nil {
		return finishUnavailable(record, category("invalid_response"), started)
	}
	raw, detail := t.exchange(ctx, provider, credential, payload)
	if detail != nil {
		return finishUnavailable(record, detail, started)
	}
	result, err := validateResponse(raw, prepared)
	if err != nil {
		return finishUnavailable(record, category("invalid_response"), started)
	}
	record.set("status", "success")
	record.set("resolved_model", result.get("model"))
	record.set("result", result)
	record.set("usage", result.get("usage"))
	record.set("duration_seconds", seconds(started))
	return record, 0
}

func (t transport) consultOpenAI(ctx context.Context, prepared *object, credential string) (*object, int) {
	started := time.Now()
	record := newObject("request", prepared, "requested_model", prepared.get("model"))
	if !requestSupported(prepared) {
		return skipped(record, "unsupported_request")
	}
	body, err := openAIRequestBody(prepared)
	if err != nil {
		return finishUnavailable(record, category("invalid_response"), started)
	}
	payload, err := wireFormat.marshal(body)
	if err != nil {
		return finishUnavailable(record, category("invalid_response"), started)
	}
	raw, detail := t.exchange(ctx, openAIProvider, credential, payload)
	if detail != nil {
		return finishUnavailable(record, detail, started)
	}
	result, err := validateOpenAIResponse(raw, prepared)
	if err != nil {
		return finishUnavailable(record, category("invalid_response"), started)
	}
	status := responseStatus(result)
	record.set("status", status)
	record.set("resolved_model", result.get("model"))
	record.set("result", result)
	record.set("usage", result.get("usage"))
	record.set("duration_seconds", seconds(started))
	if status != "success" {
		return record, 2
	}
	return record, 0
}
