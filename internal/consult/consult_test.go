package consult

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/modelpolicy"
)

// Tests in this package are written from the design contract (D1, D3, D4 and
// "Revision 2 additions") and the approved test seams only. Test-only
// identifiers carry an fx prefix so they cannot collide with package code.

const (
	fxJev    = "jev"
	fxSnap   = "subq_snap"
	fxOpenAI = "openai_decisions"

	fxClaimQuestion = `{"type":"choice","instructions":"Does the evidence support the claim?","criteria":{` +
		`"supported":"The evidence establishes the claim.","contradicted":"The evidence establishes an incompatible claim.",` +
		`"insufficient_evidence":"A necessary fact is missing."}}`
	fxQuestions     = `"questions":{"claim":` + fxClaimQuestion + `}`
	fxEvidenceState = `"evidence":"The service acknowledges work only after storing it.","claim":"Acknowledgement follows storage."`
	fxSample        = `{"state":{` + fxEvidenceState + `},` + fxQuestions + `}`
	// Python json.dumps(request, sort_keys=True, separators=(',', ':'), ensure_ascii=True), SHA-256.
	fxSampleDigest = "9cbde4a76e716b876eca243a8b43bdad60bae597a6d65ebbddc488ed003d95cd"
	fxSecret       = "fake-secret-must-not-leak"
	fxReason       = "The inspected source commits the write before it sends the acknowledgement."
	fxFailure      = "Decision consultation validation failed: "
	fxAdviserText  = "evidence and verification must reference source, checks or observations, not adviser output"
	fxNoBinding    = "task context has no adviser binding; regenerate it with this cw's 'cw task context'"
)

var (
	// D3: fixed provider order.
	fxProviders = []string{fxJev, fxSnap, fxOpenAI}
	// D1: pinned models.
	fxPins = map[string]string{fxJev: "jev-1.14.0", fxSnap: "subq-snap-preview", fxOpenAI: "gpt-6-luna"}
	// D3: credential variables.
	fxVariables    = map[string]string{fxJev: "TYPESAFE_API_KEY", fxSnap: "SUBQ_API_KEY", fxOpenAI: "OPENAI_API_KEY"}
	fxEvidence     = []string{"source:internal/store/store.go:40-52@3f2a9c1d", "observation:the acknowledgement is sent after the commit returns"}
	fxVerification = []string{"check:go-test-store@run-42"}
)

// ---- JSON helpers built on the approved value.go seams ----

func fxParse(t testing.TB, data []byte) any {
	t.Helper()
	value, err := parseJSON(data)
	if err != nil {
		t.Fatalf("parse %s: %v", data, err)
	}
	return value
}

func fxObject(t testing.TB, text string) *object {
	t.Helper()
	value, ok := fxParse(t, []byte(text)).(*object)
	if !ok {
		t.Fatalf("not an object: %s", text)
	}
	return value
}

func fxCanonical(t testing.TB, value any) string {
	t.Helper()
	data, err := canonicalFormat.marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func fxFingerprint(t testing.TB, value any) string {
	t.Helper()
	sum := sha256.Sum256([]byte(fxCanonical(t, value)))
	return hex.EncodeToString(sum[:])
}

func fxNativeText(t testing.TB, text string) any {
	t.Helper()
	var result any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("decode %s: %v", text, err)
	}
	return result
}

// fxNative converts a parsed value to plain Go values for order-insensitive comparison.
func fxNative(t testing.TB, value any) any {
	t.Helper()
	return fxNativeText(t, fxCanonical(t, value))
}

// fxSame reports whether value equals the JSON text, ignoring member order.
func fxSame(t testing.TB, value any, want string) bool {
	t.Helper()
	return reflect.DeepEqual(fxNative(t, value), fxNativeText(t, want))
}

func fxRoundTrip(t testing.TB, value any) *object {
	t.Helper()
	data, err := recordFormat.marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return fxParse(t, data).(*object)
}

// fxKeys lists an object's member names in their serialized order.
func fxKeys(t testing.TB, value any) []string {
	t.Helper()
	data, err := recordFormat.marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowInvalidUTF8(true))
	if token, err := decoder.ReadToken(); err != nil || token.Kind() != '{' {
		t.Fatalf("not an object: %s", data)
	}
	keys := []string{}
	for decoder.PeekKind() != '}' {
		token, err := decoder.ReadToken()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, token.String())
		if err := decoder.SkipValue(); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func fxStrings(values ...string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func fxNotes(pairs ...string) *object {
	result := newObject()
	for index := 0; index+1 < len(pairs); index += 2 {
		result.set(pairs[index], pairs[index+1])
	}
	return result
}

func fxMembers(names ...string) map[string]string {
	result := map[string]string{}
	for _, name := range names {
		result[name] = fxPins[name]
	}
	return result
}

func fxProvider(record *object, name string) *object {
	return record.get("providers").(*object).get(name).(*object)
}

func fxComparison(record *object, id string) *object {
	return record.get("comparison").(*object).get(id).(*object)
}

func fxDecisionStatus(record *object) any {
	return record.get("final_decision").(*object).get("status")
}

// ---- Scripted advisers behind httptest servers (transport.endpoints) ----

type fxWire struct {
	method, authorization, contentType, acceptEncoding, body string
	protoMajor, protoMinor                                   int
	close                                                    bool
}

type fxServer struct {
	mutex    sync.Mutex
	calls    map[string]int
	requests map[string][]fxWire
	sender   transport
}

// fxServe starts one server per provider. The request body is drained first,
// because net/http only reports a client disconnect through the request
// context once that body has been consumed.
func fxServe(t *testing.T, handle func(name string, body []byte, writer http.ResponseWriter, request *http.Request)) *fxServer {
	t.Helper()
	server := &fxServer{calls: map[string]int{}, requests: map[string][]fxWire{}}
	server.sender = newTransport()
	server.sender.endpoints = map[string]string{}
	for _, name := range fxProviders {
		handler := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
			}
			server.mutex.Lock()
			server.calls[name]++
			server.requests[name] = append(server.requests[name], fxWire{
				method: request.Method, authorization: request.Header.Get("Authorization"), contentType: request.Header.Get("Content-Type"),
				acceptEncoding: request.Header.Get("Accept-Encoding"), body: string(body),
				protoMajor: request.ProtoMajor, protoMinor: request.ProtoMinor, close: request.Close,
			})
			server.mutex.Unlock()
			handle(name, body, writer, request)
		}))
		t.Cleanup(handler.Close)
		server.sender.endpoints[name] = handler.URL
	}
	return server
}

func (s *fxServer) count(name string) int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.calls[name]
}

func (s *fxServer) total() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	total := 0
	for _, calls := range s.calls {
		total += calls
	}
	return total
}

func (s *fxServer) sent(name string) []fxWire {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return slices.Clone(s.requests[name])
}

type fxVerdict struct {
	choice     string
	confidence float64
	refuse     bool
}

type fxJudge func(provider, question string) fxVerdict

func fxAgree(string, string) fxVerdict { return fxVerdict{choice: "supported", confidence: 1} }

// fxBy answers per provider and agrees ("supported", confidence 1) otherwise.
func fxBy(verdicts map[string]fxVerdict) fxJudge {
	return func(provider, _ string) fxVerdict {
		if verdict, ok := verdicts[provider]; ok {
			return verdict
		}
		return fxAgree(provider, "")
	}
}

func fxShares(options []string, verdict fxVerdict) (map[string]float64, error) {
	if !slices.Contains(options, verdict.choice) {
		return nil, fmt.Errorf("choice %q is not one of %v", verdict.choice, options)
	}
	rest := 0.0
	if len(options) > 1 {
		rest = (1 - verdict.confidence) / float64(len(options)-1)
	}
	shares := map[string]float64{}
	for _, option := range options {
		shares[option] = rest
	}
	shares[verdict.choice] = verdict.confidence
	return shares, nil
}

// fxAnswer builds a valid wire response for a captured provider request
// (formats per the existing wire goldens: mapped questions for the pinned
// advisers, a question list for Decisions).
func fxAnswer(provider string, body []byte, decide fxJudge) ([]byte, error) {
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	model, _ := request["model"].(string)
	switch questions := request["questions"].(type) {
	case map[string]any:
		answers := map[string]any{}
		for id, value := range questions {
			question, _ := value.(map[string]any)
			criteria, _ := question["criteria"].(map[string]any)
			verdict := decide(provider, id)
			if verdict.refuse {
				return nil, fmt.Errorf("%s cannot refuse", provider)
			}
			shares, err := fxShares(slices.Sorted(maps.Keys(criteria)), verdict)
			if err != nil {
				return nil, err
			}
			answers[id] = map[string]any{"type": "choice", "choice": verdict.choice, "probabilities": shares, "confidence": verdict.confidence}
		}
		return json.Marshal(map[string]any{"model": model, "answers": answers, "usage": map[string]any{"input_tokens": 20, "output_tokens": 3}}, json.Deterministic(true))
	case []any:
		answers := []any{}
		for _, value := range questions {
			question, _ := value.(map[string]any)
			id, _ := question["name"].(string)
			verdict := decide(provider, id)
			if verdict.refuse {
				answers = append(answers, map[string]any{"name": id, "type": "refusal"})
				continue
			}
			options := []string{}
			choices, _ := question["choices"].([]any)
			for _, choice := range choices {
				entry, _ := choice.(map[string]any)
				option, _ := entry["value"].(string)
				options = append(options, option)
			}
			shares, err := fxShares(options, verdict)
			if err != nil {
				return nil, err
			}
			list := []any{}
			for _, option := range options {
				list = append(list, map[string]any{"value": option, "probability": shares[option]})
			}
			answers = append(answers, map[string]any{"name": id, "type": "choice", "choice": verdict.choice, "confidence": verdict.confidence, "probabilities": list})
		}
		usage := map[string]any{"input_tokens": 23, "output_tokens": 5, "total_tokens": 28,
			"input_tokens_details": map[string]any{"cached_tokens": 1, "cache_write_tokens": 2}, "output_tokens_details": map[string]any{"reasoning_tokens": 3}}
		return json.Marshal(map[string]any{"model": model, "answers": answers, "usage": usage}, json.Deterministic(true))
	}
	return nil, fmt.Errorf("unrecognised request body %s", body)
}

// fxHandler answers with decide unless failures names a failure kind for the provider.
func fxHandler(t *testing.T, decide fxJudge, failures map[string]string) func(string, []byte, http.ResponseWriter, *http.Request) {
	if decide == nil {
		decide = fxAgree
	}
	return func(name string, body []byte, writer http.ResponseWriter, request *http.Request) {
		switch failures[name] {
		case "http_error":
			writer.WriteHeader(http.StatusServiceUnavailable)
		case "network_error":
			if connection, _, err := writer.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		case "invalid_response":
			_, _ = writer.Write([]byte(`{"model":"not-the-pinned-model","answers":{}}`))
		case "timeout":
			<-request.Context().Done()
		default:
			data, err := fxAnswer(name, body, decide)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = writer.Write(data)
		}
	}
}

type fxCase struct {
	request    string
	members    map[string]string // participation set: provider -> model
	configured []string          // participating providers with a credential
	decide     fxJudge
	failures   map[string]string
}

// fxConsult produces a version 4 record through sharedRequest, prepareProvider,
// transport.collect against scripted servers, and makeRecord, read back from JSON.
func fxConsult(t *testing.T, c fxCase) (*object, *fxServer) {
	t.Helper()
	text := c.request
	if text == "" {
		text = fxSample
	}
	shared, err := sharedRequest(fxParse(t, []byte(text)))
	if err != nil {
		t.Fatal(err)
	}
	prepared := newObject()
	for _, name := range fxProviders {
		if model, ok := c.members[name]; ok {
			value, err := prepareProvider(name, shared, model)
			if err != nil {
				t.Fatal(err)
			}
			prepared.set(name, value)
		}
	}
	server := fxServe(t, fxHandler(t, c.decide, c.failures))
	credentials := map[string]string{}
	for _, name := range c.configured {
		credentials[name] = "fake-" + name
	}
	deadline := 5 * time.Second
	if slices.Contains(slices.Collect(maps.Values(c.failures)), "timeout") {
		deadline = 300 * time.Millisecond
	}
	raw, err := server.sender.collect(t.Context(), prepared, batch{credentials: credentials, deadline: deadline})
	if err != nil {
		t.Fatal(err)
	}
	record, err := makeRecord(shared, prepared, raw)
	if err != nil {
		t.Fatal(err)
	}
	return fxRoundTrip(t, record), server
}

// ---- Decisions (D4) ----

func fxItem() *object {
	return newObject("status", "resolved", "choice", "supported", "reason", fxReason,
		"evidence", fxStrings(fxEvidence...), "verification", fxStrings(fxVerification...),
		"rejected_judgments", newObject(), "investigations", newObject(), "agreement_not_independent", false)
}

func fxUnresolvedItem() *object {
	return newObject("status", "unresolved", "choice", nil, "reason", "Durability of the stored write is not established.",
		"evidence", []any{}, "verification", []any{}, "rejected_judgments", newObject(), "investigations", newObject(),
		"agreement_not_independent", false)
}

func fxClaim(item *object) *object { return newObject("questions", newObject("claim", item)) }

func fxAdjudicated(t *testing.T, record, decision any) *object {
	t.Helper()
	final, err := adjudicate(record, decision)
	if err != nil {
		t.Fatalf("adjudicate: %v\ndecision %s", err, fxCanonical(t, decision))
	}
	return final
}

func fxRefused(t *testing.T, record, decision any, fragments ...string) {
	t.Helper()
	_, err := adjudicate(record, decision)
	if err == nil {
		t.Fatalf("accepted decision %s", fxCanonical(t, decision))
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error %q does not mention %q", err, fragment)
		}
	}
}

// ---- Run helpers ----

type fxResult struct {
	code           int
	stdout, stderr string
}

func fxRun(t *testing.T, options Options) fxResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	options.Stdout, options.Stderr = &stdout, &stderr
	code, err := Run(t.Context(), options)
	if err != nil {
		t.Fatalf("Run: %v (stderr %q)", err, stderr.String())
	}
	return fxResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func fxPolicyPath(name string) string { return filepath.Join("testdata", "policy", name+".yaml") }

// fxPolicy selects one testdata policy file.
func fxPolicy(name string) modelpolicy.Source { return modelpolicy.Source{Path: fxPolicyPath(name)} }

// fxEnv is a credential environment holding a fake value for each named provider.
func fxEnv(providers ...string) func(string) string {
	values := map[string]string{}
	for _, name := range providers {
		values[fxVariables[name]] = "fake-" + fxVariables[name]
	}
	return func(name string) string { return values[name] }
}

func fxNoEnv(t *testing.T) func(string) string {
	return func(name string) string {
		t.Errorf("unexpected credential lookup %s", name)
		return ""
	}
}

func fxNoCollect(t *testing.T) collector {
	return func(context.Context, *object, batch) (map[string]any, error) {
		t.Error("unexpected provider collection")
		return nil, errors.New("unexpected provider collection")
	}
}

func fxWrite(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func fxSave(t testing.TB, path string, value any) {
	t.Helper()
	data, err := recordFormat.marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	fxWrite(t, path, data)
}

func fxRead(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fxAbsent(t testing.TB, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or is unreadable: %v", path, err)
	}
}

func fxPrivate(t testing.TB, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode: %v %v", path, info, err)
	}
}

// fxFiles writes a request and returns its path and a fresh output path.
func fxFiles(t *testing.T, request string) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "request.json")
	fxWrite(t, path, []byte(request))
	return path, filepath.Join(root, "record.json")
}

// fxBound is the sample request carrying a task_context adviser binding.
func fxBound(binding string) string {
	return `{"state":{"task_context":{"task_id":"task-1","phase":"before","advisers":` + binding + `},` + fxEvidenceState + `},` + fxQuestions + `}`
}

// fxAdjudicateFiles runs `--adjudicate` on files and returns the result and output path.
func fxAdjudicateFiles(t *testing.T, record, decision any) (fxResult, string) {
	t.Helper()
	root := t.TempDir()
	advice, choice, final := filepath.Join(root, "consultation.json"), filepath.Join(root, "decision.json"), filepath.Join(root, "final.json")
	fxSave(t, advice, record)
	fxSave(t, choice, decision)
	result := fxRun(t, Options{Adjudicate: advice, Decision: choice, Output: final, Getenv: fxNoEnv(t), collect: fxNoCollect(t)})
	return result, final
}

// ---- Record checks (D3) ----

// D3 record: version 4, fixed member order, fingerprints per Revision 2.
func TestRecordIsVersionFourWithFixedOrderAndFingerprint(t *testing.T) {
	t.Parallel()
	record, _ := fxConsult(t, fxCase{members: fxMembers(fxProviders...), configured: fxProviders})
	if got := fxCanonical(t, record.get("version")); got != "4" {
		t.Fatalf("version = %s", got)
	}
	if got := fxKeys(t, record); !slices.Equal(got, []string{"version", "status", "request", "request_sha256", "providers", "comparison", "final_decision"}) {
		t.Fatalf("record members = %v", got)
	}
	if got := fxKeys(t, record.get("providers")); !slices.Equal(got, fxProviders) {
		t.Fatalf("provider order = %v", got)
	}
	if record.get("request_sha256") != fxSampleDigest || fxFingerprint(t, record.get("request")) != fxSampleDigest {
		t.Fatalf("fingerprint = %v", record.get("request_sha256"))
	}
	for _, name := range fxProviders {
		if fxProvider(record, name).get("request_sha256") != fxSampleDigest || fxProvider(record, name).get("requested_model") != fxPins[name] {
			t.Fatalf("%s = %s", name, fxCanonical(t, fxProvider(record, name)))
		}
	}
	if record.get("status") != "success" || !fxSame(t, record.get("final_decision"), `{"status":"pending","questions":{}}`) {
		t.Fatalf("record = %s", fxCanonical(t, record))
	}
	if fxKeys(t, fxComparison(record, "claim"))[0] != "status" {
		t.Fatalf("comparison = %s", fxCanonical(t, fxComparison(record, "claim")))
	}
}

// D3 record status and comparison status, including no_advisers.
func TestRecordAndComparisonStatuses(t *testing.T) {
	t.Parallel()
	all := fxMembers(fxProviders...)
	for _, test := range []struct {
		name       string
		c          fxCase
		status     string
		comparison string
	}{
		{"agreement", fxCase{members: all, configured: fxProviders}, "success",
			`{"status":"agreement","choices":{"jev":"supported","subq_snap":"supported","openai_decisions":"supported"},"missing":{}}`},
		{"disagreement", fxCase{members: all, configured: fxProviders, decide: fxBy(map[string]fxVerdict{fxSnap: {choice: "contradicted", confidence: 1}, fxOpenAI: {choice: "insufficient_evidence", confidence: 0.7}})}, "success",
			`{"status":"disagreement","choices":{"jev":"supported","subq_snap":"contradicted","openai_decisions":"insufficient_evidence"},"missing":{}}`},
		{"one unavailable", fxCase{members: all, configured: fxProviders, failures: map[string]string{fxJev: "http_error"}}, "partial",
			`{"status":"partial","choices":{"subq_snap":"supported","openai_decisions":"supported"},"missing":{"jev":"unavailable"}}`},
		{"refusal", fxCase{members: all, configured: fxProviders, decide: fxBy(map[string]fxVerdict{fxOpenAI: {refuse: true}})}, "partial",
			`{"status":"partial","choices":{"jev":"supported","subq_snap":"supported"},"missing":{"openai_decisions":"refusal"}}`},
		{"all unavailable", fxCase{members: all, configured: fxProviders, failures: map[string]string{fxJev: "http_error", fxSnap: "network_error", fxOpenAI: "invalid_response"}}, "unavailable",
			`{"status":"unavailable","choices":{},"missing":{"jev":"unavailable","subq_snap":"unavailable","openai_decisions":"unavailable"}}`},
		{"not configured is not attempted", fxCase{members: all, configured: []string{fxSnap, fxOpenAI}}, "success",
			`{"status":"agreement","choices":{"subq_snap":"supported","openai_decisions":"supported"},"missing":{"jev":"not_configured"}}`},
		{"not configured and unavailable", fxCase{members: all, configured: []string{fxSnap, fxOpenAI}, failures: map[string]string{fxSnap: "http_error"}}, "partial",
			`{"status":"partial","choices":{"openai_decisions":"supported"},"missing":{"jev":"not_configured","subq_snap":"unavailable"}}`},
		{"none configured", fxCase{members: all}, "no_advisers_configured",
			`{"status":"no_advisers","choices":{},"missing":{"jev":"not_configured","subq_snap":"not_configured","openai_decisions":"not_configured"}}`},
		{"empty participation", fxCase{members: map[string]string{}, configured: fxProviders}, "no_advisers_configured",
			`{"status":"no_advisers","choices":{},"missing":{}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			record, server := fxConsult(t, test.c)
			if record.get("status") != test.status {
				t.Fatalf("status = %v, want %s", record.get("status"), test.status)
			}
			if !fxSame(t, fxComparison(record, "claim"), test.comparison) {
				t.Fatalf("comparison = %s, want %s", fxCanonical(t, fxComparison(record, "claim")), test.comparison)
			}
			for _, name := range fxProviders {
				if !slices.Contains(test.c.configured, name) && server.count(name) != 0 {
					t.Fatalf("%s was contacted without a credential", name)
				}
			}
			if fxDecisionStatus(record) != "pending" {
				t.Fatalf("final decision = %v", fxDecisionStatus(record))
			}
		})
	}
}

// D3 not_configured entry: only status, requested_model, request_sha256 and a zero
// duration, no network call, and no removed missing_credential/disabled statuses.
func TestNotConfiguredEntryHasOnlyItsFields(t *testing.T) {
	t.Parallel()
	record, server := fxConsult(t, fxCase{members: fxMembers(fxProviders...), configured: []string{fxSnap}})
	for _, name := range []string{fxJev, fxOpenAI} {
		entry := fxProvider(record, name)
		keys := fxKeys(t, entry)
		slices.Sort(keys)
		if !slices.Equal(keys, []string{"duration_seconds", "request_sha256", "requested_model", "status"}) {
			t.Fatalf("%s members = %v", name, keys)
		}
		if entry.get("status") != "not_configured" || entry.get("requested_model") != fxPins[name] || entry.get("request_sha256") != fxSampleDigest ||
			!reflect.DeepEqual(fxNative(t, entry.get("duration_seconds")), float64(0)) {
			t.Fatalf("%s = %s", name, fxCanonical(t, entry))
		}
		if server.count(name) != 0 {
			t.Fatalf("%s received %d requests", name, server.count(name))
		}
	}
	if strings.Contains(fxCanonical(t, record), "missing_credential") || strings.Contains(fxCanonical(t, record), "disabled") {
		t.Fatalf("record uses a removed status: %s", fxCanonical(t, record))
	}
}

// D3: any record version other than 4 fails fast in --adjudicate and writes nothing.
func TestOlderRecordVersionsFailFast(t *testing.T) {
	t.Parallel()
	record, _ := fxConsult(t, fxCase{members: fxMembers(fxProviders...), configured: fxProviders})
	for _, version := range []string{"1", "2", "3", "5"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			old := fxRoundTrip(t, record)
			old.set("version", integer(version))
			message := "consultation record version " + version + " is not supported; this cw reads version 4"
			result, final := fxAdjudicateFiles(t, old, fxClaim(fxItem()))
			if result.code != 1 || result.stdout != "" || !strings.Contains(result.stderr, message) {
				t.Fatalf("result = %+v", result)
			}
			fxAbsent(t, final)
			fxRefused(t, old, fxClaim(fxItem()), message)
			data, err := recordFormat.marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateRecord(data); err == nil || !strings.Contains(err.Error(), message) {
				t.Fatalf("ValidateRecord error = %v", err)
			}
		})
	}
}

// D3 record integrity: recomputed fingerprints, statuses and comparisons; removed statuses rejected.
func TestTamperedRecordsAreRefused(t *testing.T) {
	t.Parallel()
	base, _ := fxConsult(t, fxCase{members: fxMembers(fxProviders...), configured: []string{fxSnap, fxOpenAI}})
	fxAdjudicated(t, base, fxClaim(fxItem()))
	for name, mutate := range map[string]func(*object){
		"request evidence":        func(r *object) { r.get("request").(*object).set("state", "changed evidence") },
		"record fingerprint":      func(r *object) { r.set("request_sha256", strings.Repeat("0", 64)) },
		"provider fingerprint":    func(r *object) { fxProvider(r, fxSnap).set("request_sha256", strings.Repeat("0", 64)) },
		"comparison status":       func(r *object) { fxComparison(r, "claim").set("status", "disagreement") },
		"comparison missing":      func(r *object) { fxComparison(r, "claim").set("missing", newObject(fxJev, "unavailable")) },
		"comparison choice":       func(r *object) { fxComparison(r, "claim").get("choices").(*object).set(fxSnap, "contradicted") },
		"record status":           func(r *object) { r.set("status", "partial") },
		"no advisers status":      func(r *object) { r.set("status", "no_advisers_configured") },
		"not configured error":    func(r *object) { fxProvider(r, fxJev).set("error", newObject("category", "missing_credential")) },
		"not configured duration": func(r *object) { fxProvider(r, fxJev).set("duration_seconds", 1.5) },
		"not configured model":    func(r *object) { fxProvider(r, fxJev).set("requested_model", "jev-latest") },
		"removed missing_credential": func(r *object) {
			fxProvider(r, fxJev).set("status", "unavailable")
			fxProvider(r, fxJev).set("error", newObject("category", "missing_credential"))
		},
		"removed disabled": func(r *object) {
			r.get("providers").(*object).set(fxJev, newObject("status", "disabled", "requested_model", fxPins[fxJev], "request_sha256", fxSampleDigest, "duration_seconds", 0.0, "reason", "excluded"))
		},
		"unknown provider": func(r *object) { r.get("providers").(*object).set("codex", fxProvider(r, fxSnap)) },
		"requested model":  func(r *object) { fxProvider(r, fxSnap).set("requested_model", "subq-snap-other") },
		"raw error":        func(r *object) { fxProvider(r, fxSnap).set("raw_error", fxSecret) },
		"answer changed": func(r *object) {
			fxProvider(r, fxSnap).get("result").(*object).get("answers").(*object).get("claim").(*object).set("choice", "contradicted")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			record := fxRoundTrip(t, base)
			mutate(record)
			fxRefused(t, record, fxClaim(fxItem()))
			data, err := recordFormat.marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateRecord(data); err == nil {
				t.Fatal("ValidateRecord accepted a tampered record")
			}
		})
	}
}

// fxRebind replaces the request's task_context binding and recomputes the record and
// every provider fingerprint, so the stored advice stays internally consistent.
func fxRebind(t *testing.T, record *object, binding *object) *object {
	t.Helper()
	changed := fxRoundTrip(t, record)
	request := changed.get("request").(*object)
	request.get("state").(*object).get("task_context").(*object).set("advisers", binding)
	digest := fxFingerprint(t, request)
	changed.set("request_sha256", digest)
	providers := changed.get("providers").(*object)
	for _, name := range fxKeys(t, providers) {
		providers.get(name).(*object).set("request_sha256", digest)
	}
	return changed
}

// Revision 4 R2 (D3): a task-bound record's providers must be exactly the advisers,
// with their models, bound in its own request.state.task_context. ValidateRecord
// and --adjudicate refuse a mismatch (exit 1, nothing written) even when every
// fingerprint is recomputed consistently.
func TestTaskBoundRecordMustMatchItsBinding(t *testing.T) {
	t.Parallel()
	base, server := fxConsult(t, fxCase{request: fxBound(`{"jev":"jev-1.14.0","subq_snap":"subq-snap-preview"}`),
		members: fxMembers(fxJev, fxSnap), configured: []string{fxJev, fxSnap}})
	if got := fxKeys(t, base.get("providers")); !slices.Equal(got, []string{fxJev, fxSnap}) || server.count(fxOpenAI) != 0 {
		t.Fatalf("base providers = %v", got)
	}
	validate := func(t *testing.T, record *object) error {
		t.Helper()
		data, err := recordFormat.marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ValidateRecord(data)
		return err
	}
	// Control: rebinding to the same advisers with recomputed fingerprints stays valid,
	// so the cases below can only fail on the binding rule.
	same := fxRebind(t, base, newObject(fxJev, "jev-1.14.0", fxSnap, "subq-snap-preview"))
	if err := validate(t, same); err != nil {
		t.Fatalf("consistent rebinding refused: %v", err)
	}
	if result, final := fxAdjudicateFiles(t, same, fxClaim(fxItem())); result.code != 0 {
		t.Fatalf("consistent rebinding adjudication = %+v", result)
	} else {
		fxPrivate(t, final)
	}
	for name, binding := range map[string]*object{
		"missing bound provider": newObject(fxJev, "jev-1.14.0", fxSnap, "subq-snap-preview", fxOpenAI, "gpt-6-luna"),
		"extra provider":         newObject(fxJev, "jev-1.14.0"),
		"replaced provider":      newObject(fxJev, "jev-1.14.0", fxOpenAI, "gpt-6-luna"),
		"different model":        newObject(fxJev, "jev-1.15.0", fxSnap, "subq-snap-preview"),
		"empty binding":          newObject(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			record := fxRebind(t, base, binding)
			if record.get("request_sha256") != fxFingerprint(t, record.get("request")) || record.get("request_sha256") == base.get("request_sha256") {
				t.Fatal("fingerprints were not recomputed")
			}
			if err := validate(t, record); err == nil {
				t.Fatal("ValidateRecord accepted providers that differ from the binding")
			}
			fxRefused(t, record, fxClaim(fxItem()))
			result, final := fxAdjudicateFiles(t, record, fxClaim(fxItem()))
			if result.code != 1 || result.stdout != "" || !strings.HasPrefix(result.stderr, fxFailure) {
				t.Fatalf("adjudicate = %+v", result)
			}
			fxAbsent(t, final)
		})
	}
}

// Unchanged validation: a malformed stored duration is refused before any output.
func TestMalformedDurationIsRefusedWithoutOutput(t *testing.T) {
	t.Parallel()
	record, _ := fxConsult(t, fxCase{members: fxMembers(fxProviders...), configured: fxProviders})
	fxProvider(record, fxJev).set("duration_seconds", integer("1"+strings.Repeat("0", 1000)))
	result, final := fxAdjudicateFiles(t, record, fxClaim(fxItem()))
	if result.code != 1 || result.stderr != fxFailure+"invalid provider duration\n" {
		t.Fatalf("result = %+v", result)
	}
	fxAbsent(t, final)
}

// Exit code 1 for invalid input: Options.Check flag combinations (--with/--without-openai removed).
func TestFlagCombinations(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		options Options
		valid   bool
	}{
		{"request", Options{Request: "r", Output: "o"}, true},
		{"request dry run", Options{Request: "r", Output: "o", DryRun: true}, true},
		{"adjudicate", Options{Adjudicate: "r", Decision: "d", Output: "o"}, true},
		{"neither", Options{Output: "o"}, false},
		{"both", Options{Request: "r", Adjudicate: "a", Decision: "d", Output: "o"}, false},
		{"no output", Options{Request: "r"}, false},
		{"decision alone", Options{Request: "r", Decision: "d", Output: "o"}, false},
		{"adjudicate alone", Options{Adjudicate: "a", Output: "o"}, false},
		{"adjudicate dry run", Options{Adjudicate: "a", Decision: "d", Output: "o", DryRun: true}, false},
	} {
		if err := test.options.Check(); (err == nil) != test.valid {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

// Unchanged: a cancelled run returns the cancellation and leaves no record content.
func TestCancellationStopsTheRunWithoutARecord(t *testing.T) {
	t.Parallel()
	request, output := fxFiles(t, fxSample)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := fxServe(t, func(_ string, _ []byte, _ http.ResponseWriter, request *http.Request) {
		cancel()
		<-request.Context().Done()
	})
	code, err := Run(ctx, Options{Request: request, Output: output, Policy: fxPolicy("all"), Getenv: fxEnv(fxProviders...),
		Stdout: new(bytes.Buffer), Stderr: new(bytes.Buffer), collect: server.sender.collect})
	if !errors.Is(err, context.Canceled) || code == 0 {
		t.Fatalf("code %d err %v", code, err)
	}
	if data, err := os.ReadFile(output); err == nil && len(data) != 0 {
		t.Fatalf("cancelled run wrote %q", data)
	}
}

// Unchanged JSON reader bound: deep nesting is refused instead of exhausting the stack.
func TestDeepNestingIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := parseJSON([]byte(strings.Repeat("[", 32) + strings.Repeat("]", 32))); err != nil {
		t.Fatalf("shallow nesting refused: %v", err)
	}
	if _, err := parseJSON([]byte(strings.Repeat("[", 1000000))); err == nil {
		t.Fatal("unbounded depth accepted")
	}
}

// ---- Public API (D3 exports) ----

// D3 ConfiguredAdvisers: policy providers whose credential is present.
func TestConfiguredAdvisers(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		policy string
		env    func(string) string
		want   map[string]string
	}{
		{"empty policy", "none", fxEnv(fxProviders...), map[string]string{}},
		{"no credentials", "all", fxEnv(), map[string]string{}},
		{"partial credentials", "all", fxEnv(fxSnap), fxMembers(fxSnap)},
		{"two credentials", "all", fxEnv(fxJev, fxOpenAI), fxMembers(fxJev, fxOpenAI)},
		{"all credentials", "all", fxEnv(fxProviders...), fxMembers(fxProviders...)},
		{"credential for a disabled provider", "snap_openai", fxEnv(fxJev, fxSnap), fxMembers(fxSnap)},
		{"opt-in jev", "jev", fxEnv(fxProviders...), fxMembers(fxJev)},
		{"empty value is missing", "all", func(name string) string {
			if name == fxVariables[fxSnap] {
				return "fake"
			}
			return ""
		}, fxMembers(fxSnap)},
	} {
		got, err := ConfiguredAdvisers(fxPolicy(test.policy), test.env)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if !maps.Equal(got, test.want) {
			t.Fatalf("%s: got %v, want %v", test.name, got, test.want)
		}
	}
	for _, policy := range []string{"unknown_provider", "jev_alias", "version2"} {
		if _, err := ConfiguredAdvisers(fxPolicy(policy), fxEnv(fxProviders...)); err == nil {
			t.Fatalf("%s policy accepted", policy)
		}
	}
	if _, err := ConfiguredAdvisers(modelpolicy.Source{Path: filepath.Join(t.TempDir(), "missing.yaml")}, fxEnv(fxProviders...)); err == nil {
		t.Fatal("missing policy accepted")
	}
}

// D3 ValidateRecord: pending, resolved and unresolved records; tampering and older versions.
func TestValidateRecord(t *testing.T) {
	t.Parallel()
	request, output := fxFiles(t, fxSample)
	server := fxServe(t, fxHandler(t, nil, nil))
	if result := fxRun(t, Options{Request: request, Output: output, Policy: fxPolicy("all"), Getenv: fxEnv(fxSnap, fxOpenAI), collect: server.sender.collect}); result.code != 0 {
		t.Fatalf("consult = %+v", result)
	}
	data := fxRead(t, output)
	pending, err := ValidateRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	wantProviders := map[string]ProviderOutcome{
		fxJev:    {Status: "not_configured", RequestedModel: fxPins[fxJev]},
		fxSnap:   {Status: "success", RequestedModel: fxPins[fxSnap]},
		fxOpenAI: {Status: "success", RequestedModel: fxPins[fxOpenAI]},
	}
	if pending.Version != 4 || pending.Status != "success" || pending.DecisionStatus != "pending" || len(pending.Providers) != len(wantProviders) {
		t.Fatalf("pending = %+v", pending)
	}
	for name, outcome := range wantProviders {
		if got := pending.Providers[name]; got.Status != outcome.Status || got.RequestedModel != outcome.RequestedModel {
			t.Fatalf("%s outcome = %+v, want %+v", name, got, outcome)
		}
	}
	if got := fxCanonical(t, fxParse(t, []byte(pending.State))); got != fxCanonical(t, fxObject(t, fxSample).get("state")) {
		t.Fatalf("state = %s", got)
	}
	record := fxParse(t, data).(*object)

	resolved, final := fxAdjudicateFiles(t, record, fxClaim(fxItem()))
	if resolved.code != 0 {
		t.Fatalf("adjudicate = %+v", resolved)
	}
	finalData := fxRead(t, final)
	if got, err := ValidateRecord(finalData); err != nil || got.DecisionStatus != "resolved" || got.Status != "success" {
		t.Fatalf("resolved = %+v %v", got, err)
	}
	unresolved, open := fxAdjudicateFiles(t, record, fxClaim(fxUnresolvedItem()))
	if unresolved.code != 2 {
		t.Fatalf("adjudicate unresolved = %+v", unresolved)
	}
	if got, err := ValidateRecord(fxRead(t, open)); err != nil || got.DecisionStatus != "unresolved" {
		t.Fatalf("unresolved = %+v %v", got, err)
	}

	none, _ := fxConsult(t, fxCase{members: map[string]string{}})
	noneData, err := recordFormat.marshal(none)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ValidateRecord(noneData); err != nil || got.Status != "no_advisers_configured" || len(got.Providers) != 0 || got.DecisionStatus != "pending" {
		t.Fatalf("no advisers = %+v %v", got, err)
	}

	finalRecord := fxParse(t, finalData).(*object)
	claim := func(r *object) *object {
		return r.get("final_decision").(*object).get("questions").(*object).get("claim").(*object)
	}
	for name, test := range map[string]struct {
		base   *object
		mutate func(*object)
	}{
		"fingerprint":        {record, func(r *object) { r.get("request").(*object).set("state", "changed") }},
		"comparison":         {record, func(r *object) { fxComparison(r, "claim").set("status", "partial") }},
		"version 1":          {record, func(r *object) { r.set("version", integer("1")) }},
		"version 2":          {record, func(r *object) { r.set("version", integer("2")) }},
		"version 3":          {record, func(r *object) { r.set("version", integer("3")) }},
		"adviser evidence":   {finalRecord, func(r *object) { claim(r).set("evidence", fxStrings("observation:Snap chose supported")) }},
		"empty verification": {finalRecord, func(r *object) { claim(r).set("verification", []any{}) }},
		"final status":       {finalRecord, func(r *object) { r.get("final_decision").(*object).set("status", "unresolved") }},
		"resolved choice":    {finalRecord, func(r *object) { claim(r).set("choice", "insufficient_evidence") }},
	} {
		changed := fxRoundTrip(t, test.base)
		test.mutate(changed)
		data, err := recordFormat.marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateRecord(data); err == nil {
			t.Fatalf("%s: tampered record accepted", name)
		} else if strings.HasPrefix(name, "version ") && !strings.Contains(err.Error(), "consultation record version "+strings.TrimPrefix(name, "version ")+" is not supported; this cw reads version 4") {
			t.Fatalf("%s: error %v", name, err)
		}
	}
	if _, err := ValidateRecord([]byte("not json")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

// D3/D4 with a task-bound request: numbers and text that the canonical encoder
// must reproduce, a non-text question Decisions cannot take, and revalidation
// of the written and adjudicated records.
func TestGoWrittenRecordsRevalidate(t *testing.T) {
	t.Parallel()
	request := `{"state":{"task_context":{"task_id":"task-1","phase":"before","advisers":{"jev":"jev-1.14.0","subq_snap":"subq-snap-preview","openai_decisions":"gpt-6-luna"}},
		"numbers":[1.50,1E2,-0.0,-0,1e-7,1e22,1e16,1e15,0.0001,0.00001,123456789012345678901234567890],
		"text":"Привіт 😀 \" \\ \n \t \u0001 \u007f   é","z":{"b":1,"a":2,"é":3,"𝒳":4},"":null},
		"questions":{"zeta":{"type":"choice","instructions":{"nested":["Так",1.5]},"criteria":{"yes":"Так","no":["Nein"]}},
		"alpha":{"type":"choice","instructions":"Second","criteria":{"yes":"y","no":"n"}}}}`
	path, output := fxFiles(t, request)
	server := fxServe(t, fxHandler(t, func(string, string) fxVerdict { return fxVerdict{choice: "yes", confidence: 1} }, nil))
	result := fxRun(t, Options{Request: path, Output: output, Policy: fxPolicy("all"), Getenv: fxEnv(fxProviders...), collect: server.sender.collect})
	if result.code != 2 {
		t.Fatalf("run = %+v", result)
	}
	if server.count(fxOpenAI) != 0 {
		t.Fatal("non-text instructions reached Decisions")
	}
	data := fxRead(t, output)
	record, err := ValidateRecord(data)
	if err != nil {
		t.Fatalf("Go-written record refused: %v\n%s", err, data)
	}
	if record.Status != "partial" || record.Providers[fxOpenAI].Status != "unavailable" || record.Providers[fxJev].Status != "success" {
		t.Fatalf("record = %+v", record)
	}
	written := fxParse(t, data).(*object)
	if !fxSame(t, fxProvider(written, fxOpenAI).get("error"), `{"category":"unsupported_request"}`) {
		t.Fatalf("openai = %s", fxCanonical(t, fxProvider(written, fxOpenAI)))
	}
	if got := fxCanonical(t, fxParse(t, []byte(record.State))); got != fxCanonical(t, written.get("request").(*object).get("state")) {
		t.Fatalf("state = %s", got)
	}
	item := fxItem().with("choice", "yes")
	adjudicated, final := fxAdjudicateFiles(t, written, newObject("questions", newObject("zeta", item, "alpha", item)))
	if adjudicated.code != 0 || adjudicated.stdout != "Coordinator decision resolved\n" {
		t.Fatalf("adjudicate = %+v", adjudicated)
	}
	if got, err := ValidateRecord(fxRead(t, final)); err != nil || got.DecisionStatus != "resolved" {
		t.Fatalf("final = %+v %v", got, err)
	}
}
