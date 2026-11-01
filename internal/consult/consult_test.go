package consult

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
	"github.com/belevtsev/agent-workflows/internal/taskflow"
)

const (
	sampleText = `{"state":{"evidence":"The service acknowledges work only after storing it.","claim":"Acknowledgement follows storage."},
	"questions":{"claim":{"type":"choice","instructions":"Does the evidence support the claim?","criteria":{
	"supported":"The evidence establishes the claim.","contradicted":"The evidence establishes an incompatible claim.","insufficient_evidence":"A necessary fact is missing."}}}}`
	sampleDigest = "9cbde4a76e716b876eca243a8b43bdad60bae597a6d65ebbddc488ed003d95cd"
	secret       = "fake-secret-must-not-leak"
)

func sampleRequest(t *testing.T) *object {
	t.Helper()
	return parsed(t, []byte(sampleText)).(*object)
}

// twoQuestions adds a second independent question to the sample.
func twoQuestions(t *testing.T) *object {
	t.Helper()
	shared := sampleRequest(t)
	shared.get("questions").(*object).set("other", clone(shared.get("questions").(*object).get("claim")))
	return shared
}

func prepare(t *testing.T, shared any, models map[string]string) *object {
	t.Helper()
	prepared := newObject()
	for _, name := range providerOrder {
		if model, ok := models[name]; ok {
			value, err := prepareProvider(name, shared, model)
			if err != nil {
				t.Fatal(err)
			}
			prepared.set(name, value)
		}
	}
	return prepared
}

func pair(t *testing.T, shared any) *object {
	// Historical records must remain valid after the active model pin changes.
	return prepare(t, shared, map[string]string{jevProvider: "jev-1.13.0", snapProvider: snapModel})
}

func triple(t *testing.T, shared any) *object {
	return prepare(t, shared, map[string]string{jevProvider: sharedJevModel, snapProvider: snapModel, openAIProvider: openAIModel})
}

func answerFor(question *object, choice string) *object {
	criteria := question.get("criteria").(*object)
	probabilities := newObject()
	for _, option := range criteria.names {
		if option == choice {
			probabilities.set(option, 1.0)
		} else {
			probabilities.set(option, 0.0)
		}
	}
	return newObject("type", "choice", "choice", choice, "probabilities", probabilities, "confidence", 1.0)
}

func successRecord(prepared *object, choice string) *object {
	answers := newObject()
	questions := preparedQuestions(prepared)
	for _, id := range questions.names {
		answers.set(id, answerFor(questions.get(id).(*object), choice))
	}
	model := prepared.get("model")
	return newObject("request", prepared, "requested_model", model, "resolved_model", model, "status", "success", "duration_seconds", 0.01,
		"result", newObject("model", model, "answers", answers, "usage", newObject("input_tokens", integer("20"), "output_tokens", integer("0"))))
}

func openAIRecord(prepared *object, choice string, refusals ...string) *object {
	record := successRecord(prepared, choice)
	result := record.get("result").(*object)
	usage := result.get("usage").(*object)
	usage.set("total_tokens", integer("20"))
	usage.set("input_tokens_details", newObject("cached_tokens", integer("0"), "cache_write_tokens", integer("0")))
	usage.set("output_tokens_details", newObject("reasoning_tokens", integer("0")))
	for _, name := range refusals {
		result.get("answers").(*object).set(name, newObject("type", "refusal"))
	}
	record.set("status", responseStatus(result))
	return record
}

func rawSuccess(prepared *object, choices map[string]string) map[string]any {
	raw := map[string]any{}
	for _, name := range prepared.names {
		choice := choices[name]
		if choice == "" {
			choice = "supported"
		}
		value := prepared.get(name).(*object)
		if name == openAIProvider {
			raw[name] = openAIRecord(value, choice)
		} else {
			raw[name] = successRecord(value, choice)
		}
	}
	return raw
}

func recordFrom(t *testing.T, shared, prepared *object, raw map[string]any, version int) *object {
	t.Helper()
	record, err := makeRecord(shared, prepared, raw, version)
	if err != nil {
		t.Fatal(err)
	}
	// A retained record is always read back from its JSON form.
	data, err := recordFormat.marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return parsed(t, data).(*object)
}

func decisionItem(choice any, status string, rejected map[string]string) *object {
	reasons := newObject()
	for name, reason := range rejected {
		reasons.set(name, reason)
	}
	return newObject("status", status, "choice", choice, "reason", "Source and fixture establish storage before acknowledgement.",
		"evidence", []any{"fixture/service.go:12 at fixture-revision"}, "verification", []any{"source inspected; storage-before-ack test passed"}, "rejected_judgments", reasons)
}

func decision(item *object) *object {
	return newObject("questions", newObject("claim", item))
}

func resolved(rejected map[string]string) *object {
	return decision(decisionItem("supported", "resolved", rejected))
}

func wantStatus(t *testing.T, record *object, want string) {
	t.Helper()
	if got := record.get("final_decision").(*object).get("status"); got != want {
		t.Fatalf("final status = %v, want %s", got, want)
	}
}

func refused(t *testing.T, record, decisionValue any, message string) {
	t.Helper()
	if _, err := adjudicate(record, decisionValue); err == nil || message != "" && !strings.Contains(err.Error(), message) {
		t.Fatalf("adjudication error = %v, want %q", err, message)
	}
}

func provider(record *object, name string) *object {
	return record.get("providers").(*object).get(name).(*object)
}

func comparisonOf(record *object, id string) *object {
	return record.get("comparison").(*object).get(id).(*object)
}

func encoded(t *testing.T, value any) string {
	t.Helper()
	data, err := canonicalFormat.marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// serve starts one test server per adviser. handle receives the provider name.
// The body is drained first because net/http only reports a client disconnect
// through the request context once that body has been consumed.
func serve(t *testing.T, handle func(string, http.ResponseWriter, *http.Request)) transport {
	t.Helper()
	sender := newTransport()
	sender.endpoints = map[string]string{}
	for _, name := range providerOrder {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			handle(name, writer, request)
		}))
		t.Cleanup(server.Close)
		sender.endpoints[name] = server.URL
	}
	return sender
}

func wireAnswers(t *testing.T, prepared *object, refusals ...string) []byte {
	t.Helper()
	if prepared.get("model") != openAIModel {
		data, err := wireFormat.marshal(successRecord(prepared, "supported").get("result"))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	questions := preparedQuestions(prepared)
	answers := []any{}
	for _, id := range questions.names {
		if strings.Contains(strings.Join(refusals, " "), id) {
			answers = append(answers, newObject("name", id, "type", "refusal"))
			continue
		}
		list := []any{}
		criteria := questions.get(id).(*object).get("criteria").(*object)
		for _, option := range criteria.names {
			share := 0.0
			if option == "supported" {
				share = 1.0
			}
			list = append(list, newObject("value", option, "probability", share))
		}
		answers = append(answers, newObject("name", id, "type", "choice", "choice", "supported", "confidence", 0.7, "probabilities", list))
	}
	body := newObject("model", openAIModel, "answers", answers, "usage", newObject("input_tokens", integer("23"), "output_tokens", integer("0"), "total_tokens", integer("23"),
		"input_tokens_details", newObject("cached_tokens", integer("0"), "cache_write_tokens", integer("0")), "output_tokens_details", newObject("reasoning_tokens", integer("0"))))
	data, err := wireFormat.marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func allCredentials(prepared *object) map[string]string {
	credentials := map[string]string{}
	for _, name := range prepared.names {
		credentials[name] = "fake-" + name
	}
	return credentials
}

func TestSharedContractRejectsProviderSpecificInputs(t *testing.T) {
	for _, extra := range []string{"model", "images"} {
		if _, err := sharedRequest(sampleRequest(t).with(extra, []any{})); err == nil {
			t.Fatalf("accepted extra %s", extra)
		}
	}
	invalid := sampleRequest(t)
	invalid.get("questions").(*object).get("claim").(*object).set("type", "noul")
	if _, err := sharedRequest(invalid); err == nil {
		t.Fatal("accepted a non-Choice question")
	}
	long := sampleRequest(t)
	questions := long.get("questions").(*object)
	long.set("questions", newObject(strings.Repeat("x", 101), questions.get("claim")))
	if _, err := sharedRequest(long); err == nil {
		t.Fatal("accepted a question ID over the shared limit")
	}
}

func TestAgreementDisagreementAndIndependentQuestions(t *testing.T) {
	shared := twoQuestions(t)
	prepared := pair(t, shared)
	raw := rawSuccess(prepared, nil)
	other := raw[snapProvider].(*object).get("result").(*object).get("answers").(*object).get("other").(*object)
	other.set("choice", "contradicted")
	other.set("probabilities", newObject("supported", 0.1, "contradicted", 0.9, "insufficient_evidence", integer("0")))
	other.set("confidence", 0.7)
	record := recordFrom(t, shared, prepared, raw, 0)
	if comparisonOf(record, "claim").get("status") != "agreement" || comparisonOf(record, "other").get("status") != "disagreement" {
		t.Fatalf("comparison = %s", encoded(t, record.get("comparison")))
	}
	wantStatus(t, record, "pending")
	if strings.Contains(encoded(t, record), "winner") {
		t.Fatal("record names a winner")
	}
}

func TestPartialAndUnavailableRemainPending(t *testing.T) {
	shared := sampleRequest(t)
	prepared := pair(t, shared)
	for _, missing := range [][]string{{jevProvider}, {snapProvider}, {jevProvider, snapProvider}} {
		raw := rawSuccess(prepared, nil)
		for _, name := range missing {
			raw[name] = unavailable(prepared.get(name).(*object), "missing_credential", 0.0)
		}
		want := "partial"
		if len(missing) == 2 {
			want = "unavailable"
		}
		record := recordFrom(t, shared, prepared, raw, 0)
		if record.get("status") != want || comparisonOf(record, "claim").get("status") != want {
			t.Fatalf("missing %v: %s", missing, encoded(t, record))
		}
		wantStatus(t, record, "pending")
	}
	sender := serve(t, func(string, http.ResponseWriter, *http.Request) { t.Error("request without credentials") })
	records, err := sender.collect(t.Context(), prepared, batch{deadline: time.Minute})
	if err != nil || len(records) != 2 {
		t.Fatalf("collect = %v %v", records, err)
	}
}

func TestMalformedProviderIsUnavailableWithoutDiscardingOther(t *testing.T) {
	shared := sampleRequest(t)
	prepared := pair(t, shared)
	different, err := prepareRequest(shared.with("state", "different evidence"), snapModel)
	if err != nil {
		t.Fatal(err)
	}
	answer := func(record *object) *object {
		return record.get("result").(*object).get("answers").(*object).get("claim").(*object)
	}
	for name, mutate := range map[string]func(*object){
		"model":        func(r *object) { r.get("result").(*object).set("model", "different-model") },
		"type":         func(r *object) { answer(r).set("type", "noul") },
		"nan":          func(r *object) { answer(r).set("confidence", math.NaN()) },
		"distribution": func(r *object) { answer(r).get("probabilities").(*object).set("supported", 0.7) },
		"not maximum":  func(r *object) { answer(r).set("choice", "contradicted") },
		"no answers":   func(r *object) { r.get("result").(*object).set("answers", newObject()) },
		"evidence":     func(r *object) { r.set("request", different) },
	} {
		t.Run(name, func(t *testing.T) {
			raw := rawSuccess(prepared, nil)
			mutate(raw[snapProvider].(*object))
			record := recordFrom(t, shared, prepared, raw, 0)
			if provider(record, jevProvider).get("status") != "success" || provider(record, snapProvider).get("error").(*object).get("category") != "invalid_response" {
				t.Fatalf("record = %s", encoded(t, record.get("providers")))
			}
		})
	}
}

func TestRequestsOverlapAndKeepEndpointsSeparate(t *testing.T) {
	for _, build := range []func(*testing.T, any) *object{pair, triple} {
		shared := sampleRequest(t)
		prepared := build(t, shared)
		var arrived sync.WaitGroup
		arrived.Add(prepared.size())
		sender := serve(t, func(name string, writer http.ResponseWriter, request *http.Request) {
			// Every handler waits for the others, so only overlapping requests finish.
			arrived.Done()
			arrived.Wait()
			if request.Header.Get("Authorization") != "Bearer fake-"+name {
				t.Errorf("%s received another credential", name)
			}
			_, _ = writer.Write(wireAnswers(t, prepared.get(name).(*object)))
		})
		records, err := sender.collect(t.Context(), prepared, batch{credentials: allCredentials(prepared), deadline: 4 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if record := recordFrom(t, shared, prepared, records, 0); record.get("status") != "success" {
			t.Fatalf("record = %s", encoded(t, record.get("providers")))
		}
	}
}

func TestLargeEvidenceAndResponsesComplete(t *testing.T) {
	shared := sampleRequest(t).with("state", strings.Repeat("x", 300000))
	prepared := pair(t, shared)
	sender := serve(t, func(name string, writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(wireAnswers(t, prepared.get(name).(*object)))
	})
	records, err := sender.collect(t.Context(), prepared, batch{credentials: allCredentials(prepared), deadline: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if record := recordFrom(t, shared, prepared, records, 0); record.get("status") != "success" {
		t.Fatal("large evidence did not complete")
	}
}

func TestStalledProvidersAreBoundedByTheBatchDeadline(t *testing.T) {
	const deadline = 300 * time.Millisecond
	for name, stall := range map[string]struct {
		provider string
		partial  bool
	}{"hang": {snapProvider, false}, "partial frame": {snapProvider, true}, "hanging OpenAI": {openAIProvider, false}} {
		t.Run(name, func(t *testing.T) {
			shared := sampleRequest(t)
			prepared := triple(t, shared)
			release := make(chan struct{})
			defer close(release)
			var stopped sync.WaitGroup
			stopped.Add(1)
			sender := serve(t, func(name string, writer http.ResponseWriter, request *http.Request) {
				if name != stall.provider {
					_, _ = writer.Write(wireAnswers(t, prepared.get(name).(*object)))
					return
				}
				defer stopped.Done()
				if stall.partial {
					_, _ = writer.Write([]byte(`{"model":"partial`))
					writer.(http.Flusher).Flush()
				}
				// This handler ignores its own deadline, so the client must stop it.
				select {
				case <-request.Context().Done():
				case <-release:
				}
			})
			started := time.Now()
			records, err := sender.collect(t.Context(), prepared, batch{credentials: allCredentials(prepared), deadline: deadline})
			elapsed := time.Since(started)
			if err != nil {
				t.Fatal(err)
			}
			if elapsed > deadline+500*time.Millisecond {
				t.Fatalf("batch took %v with a %v budget", elapsed, deadline)
			}
			record := recordFrom(t, shared, prepared, records, 3)
			if provider(record, stall.provider).get("error").(*object).get("category") != "timeout" {
				t.Fatalf("stalled provider = %s", encoded(t, provider(record, stall.provider)))
			}
			for _, name := range providerOrder {
				if name != stall.provider && provider(record, name).get("status") != "success" {
					t.Fatalf("%s lost its result: %s", name, encoded(t, provider(record, name)))
				}
			}
			// The abandoned request must really end rather than leak past the run.
			finished := make(chan struct{})
			go func() { stopped.Wait(); close(finished) }()
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("stalled request was not cancelled")
			}
		})
	}
}

type panickingTransport struct {
	http.RoundTripper
	host string
}

func (p panickingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host == p.host {
		panic(secret)
	}
	return p.RoundTripper.RoundTrip(request)
}

func TestWorkerFailurePreservesOtherProvider(t *testing.T) {
	shared := sampleRequest(t)
	prepared := pair(t, shared)
	sender := serve(t, func(name string, writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(wireAnswers(t, prepared.get(name).(*object)))
	})
	sender.client = &http.Client{Transport: panickingTransport{sender.client.Transport, strings.TrimPrefix(sender.endpoints[snapProvider], "http://")}}
	records, err := sender.collect(t.Context(), prepared, batch{credentials: allCredentials(prepared), deadline: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	record := recordFrom(t, shared, prepared, records, 0)
	if provider(record, jevProvider).get("status") != "success" || provider(record, snapProvider).get("error").(*object).get("category") != "worker_error" || strings.Contains(encoded(t, record), "fake-secret") {
		t.Fatalf("record = %s", encoded(t, record.get("providers")))
	}
}

func TestHTTPFailureIsOneAttemptAndNeverExposesRawError(t *testing.T) {
	shared := sampleRequest(t)
	prepared := pair(t, shared)
	var calls atomic.Int32
	sender := serve(t, func(_ string, writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(writer, secret, http.StatusTooManyRequests)
	})
	records, err := sender.collect(t.Context(), prepared, batch{credentials: map[string]string{jevProvider: secret, snapProvider: secret}, deadline: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("%d attempts", calls.Load())
	}
	record := recordFrom(t, shared, prepared, records, 0)
	for _, name := range prepared.names {
		if provider(record, name).get("error").(*object).get("http_status") != integer("429") {
			t.Fatalf("%s = %s", name, encoded(t, provider(record, name)))
		}
	}
	if strings.Contains(encoded(t, record), "fake-secret") {
		t.Fatal("record retained a raw error")
	}
}

func TestFinalDecisionRequiresVerificationAndRejectionReasons(t *testing.T) {
	shared := sampleRequest(t)
	prepared := pair(t, shared)
	record := recordFrom(t, shared, prepared, rawSuccess(prepared, map[string]string{snapProvider: "contradicted"}), 0)
	rejected := map[string]string{snapProvider: "The inspected source explicitly stores before acknowledgement."}
	final, err := adjudicate(record, resolved(rejected))
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, final, "resolved")
	wantStatus(t, record, "pending")
	for field, value := range map[string]any{"evidence": []any{}, "verification": []any{}, "rejected_judgments": newObject(), "reason": ""} {
		refused(t, record, decision(decisionItem("supported", "resolved", rejected).with(field, value)), "")
	}
	for _, choice := range []string{"unknown", "insufficient_evidence"} {
		refused(t, record, decision(decisionItem(choice, "resolved", rejected)), "")
	}
	refused(t, record, newObject("questions", newObject()), "")
}

func TestAgreementAndOutagesDoNotReplaceSourceVerification(t *testing.T) {
	shared := sampleRequest(t)
	prepared := pair(t, shared)
	for _, missing := range [][]string{nil, {jevProvider}, {jevProvider, snapProvider}} {
		raw := rawSuccess(prepared, nil)
		for _, name := range missing {
			raw[name] = unavailable(prepared.get(name).(*object), "missing_credential", 0.0)
		}
		record := recordFrom(t, shared, prepared, raw, 0)
		refused(t, record, decision(decisionItem("supported", "resolved", nil).with("verification", []any{})), "")
		final, err := adjudicate(record, resolved(nil))
		if err != nil {
			t.Fatal(err)
		}
		wantStatus(t, final, "resolved")
	}
}

func TestUnresolvedAndTamperedRecords(t *testing.T) {
	shared := sampleRequest(t)
	prepared := pair(t, shared)
	fresh := func() *object { return recordFrom(t, shared, prepared, rawSuccess(prepared, nil), 0) }
	item := decisionItem(nil, "unresolved", nil).with("reason", "Storage durability is not established by the available source.").with("evidence", []any{}).with("verification", []any{})
	final, err := adjudicate(fresh(), decision(item))
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, final, "unresolved")
	for _, change := range []func(*object){
		func(r *object) { r.get("request").(*object).set("state", "changed evidence") },
		func(r *object) { provider(r, jevProvider).set("request_sha256", "different") },
		func(r *object) { comparisonOf(r, "claim").set("status", "disagreement") },
		func(r *object) { provider(r, jevProvider).set("raw_error", "fake-secret") },
	} {
		record := fresh()
		change(record)
		refused(t, record, decision(item), "")
	}
}

func tempRequest(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "request.json")
	if err := os.WriteFile(path, []byte(sampleText), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, filepath.Join(root, "record.json")
}

func noCredentials(string) string { return "" }

func policyFile(name string) string { return filepath.Join("testdata", "policy", name+".yaml") }

func TestDryRunReservesPrivateOutputWithoutNetworkAndNeverOverwrites(t *testing.T) {
	request, output := tempRequest(t)
	failing := func(context.Context, *object, batch) (map[string]any, error) {
		t.Fatal("dry run made a request")
		return nil, nil
	}
	var stdout, stderr bytes.Buffer
	options := Options{Request: request, Output: output, Policy: policyFile("pair"), DryRun: true, Stdout: &stdout, Stderr: &stderr, collect: failing,
		Getenv: func(name string) string { return "fake-key-" + name }}
	if code, err := Run(t.Context(), options); code != 0 || err != nil {
		t.Fatalf("dry run = %d %v", code, err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "fake-key") {
		t.Fatal("record retained a credential")
	}
	record := parsed(t, data).(*object)
	if record.get("status") != "dry_run" || record.get("version") != integer("1") || encoded(t, record.get("providers")) != `{"jev":{"requested_model":"jev-1.14.0"},"subq_snap":{"requested_model":"subq-snap-preview"}}` {
		t.Fatalf("record = %s", data)
	}
	options.DryRun = false
	stderr.Reset()
	if code, err := Run(t.Context(), options); code != 1 || err != nil {
		t.Fatalf("second run = %d %v", code, err)
	}
	if stderr.String() != "Decision consultation validation failed: cannot create a new output file\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if after, _ := os.ReadFile(output); !bytes.Equal(after, data) {
		t.Fatal("existing record was overwritten")
	}
}

func TestMissingKeysThenOfflineAdjudicationFromFiles(t *testing.T) {
	request, output := tempRequest(t)
	sender := serve(t, func(string, http.ResponseWriter, *http.Request) { t.Error("request without credentials") })
	var stdout, stderr bytes.Buffer
	if code, err := Run(t.Context(), Options{Request: request, Output: output, Policy: policyFile("required"), Getenv: noCredentials, Stdout: &stdout, Stderr: &stderr, collect: sender.collect}); code != 2 || err != nil {
		t.Fatalf("run = %d %v", code, err)
	}
	if stdout.String() != "Decision consultation unavailable; Coordinator decision pending\n" || stderr.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
	decisionPath, final := filepath.Join(filepath.Dir(output), "decision.json"), filepath.Join(filepath.Dir(output), "final.json")
	data, err := recordFormat.marshal(resolved(nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisionPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code, err := Run(t.Context(), Options{Adjudicate: output, Decision: decisionPath, Output: final, Stdout: &stdout, Stderr: &stderr}); code != 0 || err != nil {
		t.Fatalf("adjudicate = %d %v %s", code, err, stderr.String())
	}
	result, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, parsed(t, result).(*object), "resolved")
	if info, err := os.Stat(final); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("final mode: %v %v", info, err)
	}
}

func TestMalformedDurationIsRefusedWithoutOutput(t *testing.T) {
	shared := sampleRequest(t)
	prepared := pair(t, shared)
	record := recordFrom(t, shared, prepared, rawSuccess(prepared, nil), 0)
	provider(record, jevProvider).set("duration_seconds", integer("1"+strings.Repeat("0", 1000)))
	root := t.TempDir()
	write := func(name string, value any) string {
		data, err := recordFormat.marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	var stderr bytes.Buffer
	final := filepath.Join(root, "final.json")
	code, err := Run(t.Context(), Options{Adjudicate: write("advice.json", record), Decision: write("decision.json", resolved(nil)), Output: final, Stdout: new(bytes.Buffer), Stderr: &stderr})
	if code != 1 || err != nil || stderr.String() != "Decision consultation validation failed: invalid provider duration\n" {
		t.Fatalf("code %d err %v stderr %q", code, err, stderr.String())
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatal("refused adjudication wrote output")
	}
}

func TestThreeWayComparisonKeepsJudgmentsIndependent(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	record := recordFrom(t, shared, prepared, rawSuccess(prepared, map[string]string{snapProvider: "contradicted", openAIProvider: "insufficient_evidence"}), 2)
	if record.get("version") != integer("2") || record.get("status") != "success" {
		t.Fatalf("record = %s", encoded(t, record))
	}
	if got := encoded(t, comparisonOf(record, "claim")); got != `{"choices":{"jev":"supported","openai_decisions":"insufficient_evidence","subq_snap":"contradicted"},"missing":{},"status":"disagreement"}` {
		t.Fatalf("comparison = %s", got)
	}
	rejected := map[string]string{snapProvider: "Inspected behavior contradicts this judgment.", openAIProvider: "The source supplies the missing evidence."}
	final, err := adjudicate(record, resolved(rejected))
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, final, "resolved")
	for name := range rejected {
		incomplete := map[string]string{snapProvider: rejected[snapProvider], openAIProvider: rejected[openAIProvider]}
		delete(incomplete, name)
		refused(t, record, resolved(incomplete), "")
	}
}

func TestMixedRefusalPreservesOtherChoiceAndNeedsOnlyDifferingReasons(t *testing.T) {
	for _, version := range []int{2, 3} {
		shared := twoQuestions(t)
		prepared := triple(t, shared)
		raw := rawSuccess(prepared, nil)
		raw[openAIProvider] = openAIRecord(prepared.get(openAIProvider).(*object), "contradicted", "claim")
		record := recordFrom(t, shared, prepared, raw, version)
		if provider(record, openAIProvider).get("status") != "partial" || encoded(t, comparisonOf(record, "claim").get("missing")) != `{"openai_decisions":"refusal"}` ||
			comparisonOf(record, "other").get("choices").(*object).get(openAIProvider) != "contradicted" || comparisonOf(record, "claim").get("status") != "partial" {
			t.Fatalf("record = %s", encoded(t, record))
		}
		item := decisionItem("supported", "resolved", nil)
		both := newObject("questions", newObject("claim", item, "other", item))
		refused(t, record, both, "each rejected provider judgment")
		reasons := map[string]string{openAIProvider: "Source establishes the ordering."}
		valid := newObject("questions", newObject("claim", item, "other", decisionItem("supported", "resolved", reasons)))
		final, err := adjudicate(record, valid)
		if err != nil {
			t.Fatal(err)
		}
		wantStatus(t, final, "resolved")
		refused(t, record, newObject("questions", newObject("claim", decisionItem("supported", "resolved", map[string]string{openAIProvider: "A refusal is not a judgment."}), "other", item)), "")
		raw[openAIProvider] = openAIRecord(prepared.get(openAIProvider).(*object), "supported", "claim", "other")
		record = recordFrom(t, shared, prepared, raw, version)
		if provider(record, openAIProvider).get("status") != "refusal" || record.get("status") != "partial" {
			t.Fatalf("all refused = %s", encoded(t, record))
		}
		if _, err := adjudicate(record, newObject("questions", newObject("claim", item, "other", item))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRefusalsAndOutagesAreDistinctMissingAdvice(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	raw := rawSuccess(prepared, nil)
	raw[openAIProvider] = openAIRecord(prepared.get(openAIProvider).(*object), "supported", "claim")
	record := recordFrom(t, shared, prepared, raw, 2)
	if provider(record, openAIProvider).get("status") != "refusal" || record.get("status") != "partial" {
		t.Fatalf("record = %s", encoded(t, record))
	}
	final, err := adjudicate(record, resolved(nil))
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, final, "resolved")
	for _, name := range []string{jevProvider, snapProvider} {
		raw[name] = unavailable(prepared.get(name).(*object), "timeout", 0.0)
	}
	record = recordFrom(t, shared, prepared, raw, 2)
	if got := encoded(t, comparisonOf(record, "claim")); record.get("status") != "unavailable" || got != `{"choices":{},"missing":{"jev":"unavailable","openai_decisions":"refusal","subq_snap":"unavailable"},"status":"unavailable"}` {
		t.Fatalf("comparison = %s", got)
	}
}

func TestDisabledOptionalPreservesPairButForcedMissingKeyIsPartial(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	optional := prepared.get(openAIProvider).(*object)
	for _, reason := range disabledReasons {
		raw := rawSuccess(prepared, nil)
		raw[openAIProvider] = disabled(optional, reason)
		record := recordFrom(t, shared, prepared, raw, 2)
		if record.get("status") != "success" || comparisonOf(record, "claim").get("status") != "agreement" || encoded(t, comparisonOf(record, "claim").get("missing")) != `{"openai_decisions":"disabled"}` {
			t.Fatalf("record = %s", encoded(t, record))
		}
		final, err := adjudicate(record, resolved(nil))
		if err != nil {
			t.Fatal(err)
		}
		wantStatus(t, final, "resolved")
	}
	raw := rawSuccess(prepared, nil)
	raw[openAIProvider] = unavailable(optional, "missing_credential", 0.0)
	record := recordFrom(t, shared, prepared, raw, 2)
	if record.get("status") != "partial" || encoded(t, comparisonOf(record, "claim").get("missing")) != `{"openai_decisions":"unavailable"}` {
		t.Fatalf("record = %s", encoded(t, record))
	}
}

func TestVersionedProviderSetsAndStoredMetadataAreStrict(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	original := recordFrom(t, shared, prepared, rawSuccess(prepared, nil), 2)
	openai := func(r *object) *object { return provider(r, openAIProvider) }
	drop := func(r *object, name string) {
		providers := newObject()
		for _, member := range r.get("providers").(*object).names {
			if member != name {
				providers.set(member, provider(r, member))
			}
		}
		r.set("providers", providers)
	}
	mutations := []func(*object){
		func(r *object) { r.set("version", integer("1")) },
		func(r *object) { drop(r, openAIProvider) },
		func(r *object) { r.get("providers").(*object).set("other", openai(r)) },
		func(r *object) { openai(r).set("requested_model", "gpt-6-astra") },
		func(r *object) { openai(r).set("resolved_model", "gpt-6-astra") },
		func(r *object) { openai(r).set("usage", newObject("input_tokens", integer("7"))) },
		func(r *object) {
			openai(r).get("result").(*object).get("usage").(*object).set("input_tokens", integer("-1"))
		},
		func(r *object) { openai(r).set("request_sha256", "wrong") },
		func(r *object) { comparisonOf(r, "claim").get("missing").(*object).set(openAIProvider, "refusal") },
		func(r *object) {
			openai(r).get("result").(*object).get("answers").(*object).get("claim").(*object).set("type", "refusal")
		},
		func(r *object) { openai(r).set("status", "partial") },
		func(r *object) { openai(r).set("raw_error", "fake-secret") },
	}
	for index, mutate := range mutations {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			record := clone(original).(*object)
			mutate(record)
			refused(t, record, resolved(nil), "")
		})
	}
	raw := rawSuccess(prepared, nil)
	raw[openAIProvider] = disabled(prepared.get(openAIProvider).(*object), "excluded")
	excluded := recordFrom(t, shared, prepared, raw, 2)
	for index, mutate := range []func(*object){
		func(r *object) { openai(r).set("reason", []any{}) },
		func(r *object) { openai(r).set("duration_seconds", integer("1")) },
		func(r *object) { openai(r).set("result", newObject("answers", newObject())) },
		func(r *object) {
			provider(r, jevProvider).set("status", "disabled")
			provider(r, jevProvider).set("reason", "excluded")
		},
	} {
		t.Run(fmt.Sprintf("disabled %d", index), func(t *testing.T) {
			record := clone(excluded).(*object)
			mutate(record)
			refused(t, record, resolved(nil), "")
		})
	}
}

func TestNewTripleRecordsAreVersionThreeAndSelfContained(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	record := recordFrom(t, shared, prepared, rawSuccess(prepared, nil), 0)
	if record.get("version") != integer("3") || !record.get("providers").(*object).exactly(providerOrder...) || record.get("status") != "success" {
		t.Fatalf("record = %s", encoded(t, record))
	}
	if got := encoded(t, comparisonOf(record, "claim")); got != `{"choices":{"jev":"supported","openai_decisions":"supported","subq_snap":"supported"},"missing":{},"status":"agreement"}` {
		t.Fatalf("comparison = %s", got)
	}
	for _, name := range providerOrder {
		if provider(record, name).get("request_sha256") != record.get("request_sha256") || record.get("request_sha256") != sampleDigest {
			t.Fatalf("%s fingerprint differs", name)
		}
	}
	final, err := adjudicate(record, resolved(nil))
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, final, "resolved")
}

func TestMissingRequiredOpenAIKeyPreservesPairWithoutDisabling(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	sender := serve(t, func(name string, writer http.ResponseWriter, _ *http.Request) {
		if name == openAIProvider {
			t.Error("OpenAI request without a credential")
		}
		_, _ = writer.Write(wireAnswers(t, prepared.get(name).(*object)))
	})
	records, err := sender.collect(t.Context(), prepared, batch{credentials: map[string]string{jevProvider: "fake", snapProvider: "fake"}, deadline: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	record := recordFrom(t, shared, prepared, records, 0)
	if record.get("version") != integer("3") || record.get("status") != "partial" || encoded(t, provider(record, openAIProvider).get("error")) != `{"category":"missing_credential"}` ||
		encoded(t, comparisonOf(record, "claim").get("missing")) != `{"openai_decisions":"unavailable"}` {
		t.Fatalf("record = %s", encoded(t, record))
	}
	final, err := adjudicate(record, resolved(nil))
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, final, "resolved")
}

func TestDisabledAndMissingKeyDoNotDispatchOptionalProvider(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	sender := serve(t, func(name string, writer http.ResponseWriter, _ *http.Request) {
		if name == openAIProvider {
			t.Error("disabled provider received a request")
		}
		_, _ = writer.Write(wireAnswers(t, prepared.get(name).(*object)))
	})
	for _, test := range []struct {
		disabled map[string]string
		key      string
		want     string
	}{{map[string]string{openAIProvider: "missing_credential"}, "", "disabled"}, {map[string]string{openAIProvider: "excluded"}, "fake-openai", "disabled"}, {nil, "", "unavailable"}} {
		credentials := map[string]string{jevProvider: "fake", snapProvider: "fake", openAIProvider: test.key}
		records, err := sender.collect(t.Context(), prepared, batch{credentials: credentials, deadline: 4 * time.Second, disabled: test.disabled, required: pairProviders})
		if err != nil {
			t.Fatal(err)
		}
		if records[openAIProvider].(*object).get("status") != test.want || records[jevProvider].(*object).get("status") != "success" || records[snapProvider].(*object).get("status") != "success" {
			t.Fatalf("records = %v", records)
		}
	}
}

func TestVersionThreeDisabledProviderIsNeverAccepted(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	for _, name := range providerOrder {
		record := recordFrom(t, shared, prepared, rawSuccess(prepared, nil), 3)
		digest := record.get("request_sha256")
		record.get("providers").(*object).set(name, newObject("status", "disabled", "requested_model", prepared.get(name).(*object).get("model"), "request_sha256", digest, "duration_seconds", 0.0, "reason", "excluded"))
		record.set("status", availability(record.get("providers").(*object)))
		record.set("comparison", comparison(shared, record.get("providers").(*object)))
		refused(t, record, resolved(nil), "")
		sender := serve(t, func(string, http.ResponseWriter, *http.Request) {})
		if _, err := sender.collect(t.Context(), prepared, batch{deadline: time.Minute, disabled: map[string]string{name: "excluded"}}); err == nil || err.Error() != "required provider cannot be disabled" {
			t.Fatalf("collect error = %v", err)
		}
	}
	raw := rawSuccess(prepared, nil)
	raw[openAIProvider] = disabled(prepared.get(openAIProvider).(*object), "excluded")
	record := recordFrom(t, shared, prepared, raw, 3)
	if encoded(t, provider(record, openAIProvider).get("error")) != `{"category":"invalid_response"}` || record.get("status") != "partial" {
		t.Fatalf("record = %s", encoded(t, record))
	}
}

func TestVersionThreeCannotDropProviderOrChangeVersion(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	raw := rawSuccess(prepared, nil)
	original := recordFrom(t, shared, prepared, raw, 3)
	version4 := clone(original).(*object)
	version4.set("version", integer("4"))
	refused(t, version4, resolved(nil), "")
	if _, err := makeRecord(shared, pair(t, shared), raw, 3); err == nil {
		t.Fatal("version 3 accepted a provider pair")
	}
	if _, err := makeRecord(shared, prepared, raw, 1); err == nil {
		t.Fatal("version 1 accepted three providers")
	}
}

func TestUnknownOrWrongVersionFramesAreRefused(t *testing.T) {
	data := readTestdata(t, "golden", "adjudicate_historical_v2", "consultation.json")
	record := parsed(t, data).(*object)
	final, err := adjudicate(record, resolved(nil))
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, final, "resolved")
	refused(t, record.with("version", integer("3")), resolved(nil), "")
}

func TestWithoutOpenAIRejectsRequiredPolicyBeforeOutputCredentialsOrHTTP(t *testing.T) {
	root := t.TempDir()
	for _, dryRun := range []bool{false, true} {
		output := filepath.Join(root, "advice.json")
		var stderr bytes.Buffer
		code, err := Run(t.Context(), Options{Request: filepath.Join(root, "not-read.json"), Output: output, Policy: policyFile("required"), DryRun: dryRun, UseOpenAI: new(false), Stdout: new(bytes.Buffer), Stderr: &stderr,
			Getenv: func(string) string { t.Fatal("credential lookup"); return "" }})
		if code != 1 || err != nil || !strings.Contains(stderr.String(), "required OpenAI adviser cannot be disabled") {
			t.Fatalf("code %d err %v stderr %q", code, err, stderr.String())
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("rejected run reserved output")
		}
	}
}

func TestFlagCombinations(t *testing.T) {
	yes := new(true)
	for _, test := range []struct {
		name    string
		options Options
		valid   bool
	}{
		{"request", Options{Request: "r", Output: "o"}, true},
		{"adjudicate", Options{Adjudicate: "r", Decision: "d", Output: "o"}, true},
		{"neither", Options{Output: "o"}, false},
		{"both", Options{Request: "r", Adjudicate: "a", Decision: "d", Output: "o"}, false},
		{"no output", Options{Request: "r"}, false},
		{"decision alone", Options{Request: "r", Decision: "d", Output: "o"}, false},
		{"adjudicate alone", Options{Adjudicate: "a", Output: "o"}, false},
		{"adjudicate dry run", Options{Adjudicate: "a", Decision: "d", Output: "o", DryRun: true}, false},
		{"adjudicate OpenAI flag", Options{Adjudicate: "a", Decision: "d", Output: "o", UseOpenAI: yes}, false},
	} {
		if err := test.options.Check(); (err == nil) != test.valid {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

func TestMappingPreservesEvidenceAndVerbatimQuestions(t *testing.T) {
	shared := twoQuestions(t)
	shared.set("state", parsed(t, []byte(`{"z":[true,null,1],"a":"quoted \"text\""}`)))
	claim := shared.get("questions").(*object).get("claim").(*object)
	claim.set("instructions", "  Inspect the source.\nUse the actual evidence.  ")
	prepared, err := prepareOpenAI(shared, openAIModel)
	if err != nil {
		t.Fatal(err)
	}
	body, err := openAIRequestBody(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if !body.exactly("model", "input", "questions") || body.get("model") != openAIModel || body.get("input") != `{"a":"quoted \"text\"","z":[true,null,1]}` {
		t.Fatalf("body = %s", encoded(t, body))
	}
	first := body.get("questions").([]any)[0].(*object)
	if first.get("name") != "claim" || first.get("instructions") != claim.get("instructions") ||
		encoded(t, first.get("choices")) != `[{"description":"The evidence establishes an incompatible claim.","value":"contradicted"},{"description":"A necessary fact is missing.","value":"insufficient_evidence"},{"description":"The evidence establishes the claim.","value":"supported"}]` {
		t.Fatalf("question = %s", encoded(t, first))
	}
	reversed := newObject("a", `quoted "text"`, "z", []any{true, nil, integer("1")})
	other, err := prepareOpenAI(shared.with("state", newObject("z", reversed.get("z"), "a", reversed.get("a"))), openAIModel)
	if err != nil {
		t.Fatal(err)
	}
	second, err := openAIRequestBody(other)
	if err != nil || encoded(t, second) != encoded(t, body) {
		t.Fatalf("key order changed the request: %v", err)
	}
	text, err := prepareOpenAI(shared.with("state", "text"), openAIModel)
	if err != nil {
		t.Fatal(err)
	}
	if body, err := openAIRequestBody(text); err != nil || body.get("input") != `"text"` {
		t.Fatalf("text input = %v %v", body, err)
	}
}

func openAIFixture(t *testing.T, refusals ...string) (*object, any) {
	t.Helper()
	prepared := triple(t, twoQuestions(t)).get(openAIProvider).(*object)
	return prepared, parsed(t, wireAnswers(t, prepared, refusals...))
}

func TestFixedRequestedAndDocumentedReturnedModel(t *testing.T) {
	shared := twoQuestions(t)
	prepared, wire := openAIFixture(t)
	for _, model := range []string{"gpt-6-astra", "gpt-6-luna-unknown-snapshot", ""} {
		if _, err := prepareOpenAI(shared, model); err == nil {
			t.Fatalf("requested %q", model)
		}
		if _, err := validateOpenAIResponse(wire.(*object).with("model", model), prepared); err == nil {
			t.Fatalf("returned %q", model)
		}
	}
	result, err := validateOpenAIResponse(wire, prepared)
	if err != nil || result.get("model") != openAIModel || result.get("usage").(*object).get("input_tokens") != integer("23") {
		t.Fatalf("result = %v %v", result, err)
	}
}

func TestPartialRefusalsPreserveOtherAnswersAndRoundTrip(t *testing.T) {
	for _, test := range []struct {
		refusals []string
		want     string
	}{{nil, "success"}, {[]string{"claim"}, "partial"}, {[]string{"claim", "other"}, "refusal"}} {
		prepared, wire := openAIFixture(t, test.refusals...)
		result, err := validateOpenAIResponse(wire, prepared)
		if err != nil {
			t.Fatal(err)
		}
		if responseStatus(result) != test.want {
			t.Fatalf("status = %s", responseStatus(result))
		}
		answers := result.get("answers").(*object)
		for _, name := range test.refusals {
			if encoded(t, answers.get(name)) != `{"type":"refusal"}` {
				t.Fatalf("refusal = %s", encoded(t, answers.get(name)))
			}
		}
		if test.want == "partial" && answers.get("other").(*object).get("choice") != "supported" {
			t.Fatal("refusal discarded another answer")
		}
		again, err := validateOpenAIResult(result, prepared)
		if err != nil || !equal(again, result) {
			t.Fatalf("round trip = %v %v", again, err)
		}
	}
}

func TestChoiceDoesNotRequireUndocumentedArgmax(t *testing.T) {
	prepared, wire := openAIFixture(t)
	answer := wire.(*object).get("answers").([]any)[0].(*object)
	answer.set("probabilities", parsed(t, []byte(`[{"value":"supported","probability":0.3},{"value":"contradicted","probability":0.7},{"value":"insufficient_evidence","probability":0}]`)))
	result, err := validateOpenAIResponse(wire, prepared)
	if err != nil || result.get("answers").(*object).get("claim").(*object).get("choice") != "supported" {
		t.Fatalf("result = %v %v", result, err)
	}
}

func TestOpenAIRejectsMissingDuplicateExtraAndMistypedNames(t *testing.T) {
	first := func(value *object) *object { return value.get("answers").([]any)[0].(*object) }
	for index, mutate := range []func(*object){
		func(v *object) { v.set("answers", v.get("answers").([]any)[:1]) },
		func(v *object) { v.set("answers", append(v.get("answers").([]any), clone(first(v)))) },
		func(v *object) { first(v).set("name", "other") },
		func(v *object) { first(v).set("name", "unexpected") },
		func(v *object) { first(v).set("name", nil) },
		func(v *object) { first(v).set("name", []any{}) },
		func(v *object) { v.set("answers", newObject()) },
		func(v *object) { first(v).set("type", "predicate") },
		func(v *object) { first(v).set("choice", true) },
	} {
		prepared, wire := openAIFixture(t)
		mutate(wire.(*object))
		if _, err := validateOpenAIResponse(wire, prepared); err == nil {
			t.Fatalf("mutation %d accepted", index)
		}
	}
	prepared, wire := openAIFixture(t)
	answers := wire.(*object).get("answers").([]any)
	answers[0], answers[1] = answers[1], answers[0]
	result, err := validateOpenAIResponse(wire, prepared)
	if err != nil || !result.get("answers").(*object).exactly("claim", "other") {
		t.Fatalf("names must identify questions independently of order: %v", err)
	}
}

func TestOpenAIRejectsInvalidDistributionsAndConfidence(t *testing.T) {
	list := func(answer *object) []any { return answer.get("probabilities").([]any) }
	entry := func(answer *object) *object { return list(answer)[0].(*object) }
	infinity := parsed(t, []byte("1e400"))
	for index, mutate := range []func(*object){
		func(a *object) { a.set("probabilities", newObject("supported", integer("1"))) },
		func(a *object) { a.set("probabilities", list(a)[:2]) },
		func(a *object) { entry(a).set("value", true) },
		func(a *object) { entry(a).set("value", "unexpected") },
		func(a *object) { entry(a).set("value", list(a)[1].(*object).get("value")) },
		func(a *object) { entry(a).set("probability", 0.5) },
		func(a *object) { entry(a).set("probability", true) },
		func(a *object) { entry(a).set("probability", -0.1) },
		func(a *object) { entry(a).set("probability", 1.1) },
		func(a *object) { entry(a).set("probability", infinity) },
		func(a *object) { a.set("confidence", infinity) },
		func(a *object) { a.set("confidence", integer("1"+strings.Repeat("0", 1000))) },
		func(a *object) { a.set("confidence", false) },
	} {
		prepared, wire := openAIFixture(t)
		mutate(wire.(*object).get("answers").([]any)[0].(*object))
		if _, err := validateOpenAIResponse(wire, prepared); err == nil {
			t.Fatalf("mutation %d accepted", index)
		}
	}
}

func TestOpenAIRejectsMalformedUsageEvenForAllRefused(t *testing.T) {
	for index, mutate := range []func(*object){
		func(u *object) { u.set("input_tokens", nil) },
		func(u *object) { u.set("input_tokens", true) },
		func(u *object) { u.set("output_tokens", integer("-1")) },
		func(u *object) { u.set("total_tokens", 0.5) },
		func(u *object) { u.set("input_tokens_details", nil) },
		func(u *object) {
			u.get("input_tokens_details").(*object).set("cached_tokens", parsed(t, []byte("1e400")))
		},
		func(u *object) { u.get("output_tokens_details").(*object).set("reasoning_tokens", "0") },
	} {
		prepared, wire := openAIFixture(t, "claim", "other")
		mutate(wire.(*object).get("usage").(*object))
		if _, err := validateOpenAIResponse(wire, prepared); err == nil {
			t.Fatalf("mutation %d accepted", index)
		}
	}
}

func TestProviderLimitsDoNotRewriteNontextInstructions(t *testing.T) {
	sender := serve(t, func(string, http.ResponseWriter, *http.Request) { t.Error("unsupported request was sent") })
	question := func(shared *object) *object { return shared.get("questions").(*object).get("claim").(*object) }
	for name, mutate := range map[string]func(*object){
		"count": func(s *object) {
			questions := newObject()
			for index := range 201 {
				questions.set(fmt.Sprint(index), question(s))
			}
			s.set("questions", questions)
		},
		"instructions": func(s *object) { question(s).set("instructions", newObject("policy", "keep intact")) },
		"description":  func(s *object) { question(s).get("criteria").(*object).set("supported", []any{"keep intact"}) },
		"length":       func(s *object) { question(s).set("instructions", strings.Repeat("x", maxOpenAIText+1)) },
	} {
		t.Run(name, func(t *testing.T) {
			shared := sampleRequest(t)
			mutate(shared)
			prepared, err := prepareOpenAI(shared, openAIModel)
			if err != nil {
				t.Fatal(err)
			}
			if requestSupported(prepared) {
				t.Fatal("unsupported request accepted")
			}
			record, code := sender.consultOpenAI(t.Context(), prepared, "fake-openai")
			if code != 2 || encoded(t, record.get("error")) != `{"category":"unsupported_request"}` {
				t.Fatalf("record = %s", encoded(t, record))
			}
		})
	}
	shared := sampleRequest(t)
	questions := newObject()
	for index := range 200 {
		questions.set(fmt.Sprint(index), shared.get("questions").(*object).get("claim"))
	}
	prepared, err := prepareOpenAI(shared.with("questions", questions), openAIModel)
	if err != nil || !requestSupported(prepared) {
		t.Fatalf("200 questions must be supported: %v", err)
	}
}

func TestTransportSendsOnlyExplicitCredentialOverOneClosedHTTP11Connection(t *testing.T) {
	prepared, _ := openAIFixture(t)
	var seen []*http.Request
	var bodies []string
	sender := serve(t, func(_ string, writer http.ResponseWriter, request *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(request.Body)
		seen = append(seen, request)
		bodies = append(bodies, body.String())
		_, _ = writer.Write(wireAnswers(t, prepared, "claim"))
	})
	record, code := sender.consultOpenAI(t.Context(), prepared, "fake-openai")
	if len(seen) != 1 || record.get("status") != "partial" || code != 2 {
		t.Fatalf("%d requests, record %s, code %d", len(seen), encoded(t, record), code)
	}
	request := seen[0]
	expected, err := openAIRequestBody(prepared)
	if err != nil {
		t.Fatal(err)
	}
	want, err := wireFormat.marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != http.MethodPost || bodies[0] != string(want) || request.Header.Get("Authorization") != "Bearer fake-openai" || request.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %+v %s", request, bodies[0])
	}
	if request.ProtoMajor != 1 || request.ProtoMinor != 1 || !request.Close || request.Header.Get("Accept-Encoding") != "identity" {
		t.Fatalf("transport shape = %+v", request)
	}
	for _, fixed := range []struct{ name, url string }{{jevProvider, "https://api.typesafe.ai/v1/systemone"}, {snapProvider, "https://api.subq.ai/v1/systemone"}, {openAIProvider, "https://api.openai.com/v1/decisions"}} {
		if newTransport().endpoints[fixed.name] != fixed.url {
			t.Fatalf("%s endpoint = %s", fixed.name, newTransport().endpoints[fixed.name])
		}
	}
	if strings.Contains(encoded(t, record), "fake-openai") {
		t.Fatal("record retained a credential")
	}
}

func TestErrorsAreSingleAttemptSanitizedAndResponsesBounded(t *testing.T) {
	prepared, _ := openAIFixture(t)
	for _, test := range []struct {
		name, category string
		handle         func(http.ResponseWriter, *http.Request)
	}{
		{"http", "http_error", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, secret, http.StatusTooManyRequests) }},
		{"network", "network_error", func(w http.ResponseWriter, _ *http.Request) {
			connection, _, _ := w.(http.Hijacker).Hijack()
			connection.Close()
		}},
		{"oversized", "invalid_response", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(bytes.Repeat([]byte("x"), maxResponseBytes+1))
		}},
		{"duplicate", "invalid_response", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"model":"x","model":"y"}`)) }},
		{"truncated", "invalid_response", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte(`{"model":`))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			sender := serve(t, func(_ string, w http.ResponseWriter, r *http.Request) { calls.Add(1); test.handle(w, r) })
			record, code := sender.consultOpenAI(t.Context(), prepared, secret)
			if calls.Load() != 1 || code != 2 || record.get("error").(*object).get("category") != test.category || strings.Contains(encoded(t, record), "fake-secret") {
				t.Fatalf("calls %d code %d record %s", calls.Load(), code, encoded(t, record))
			}
		})
	}
}

func TestDistributionSumIsBoundedByTheTolerance(t *testing.T) {
	prepared := pair(t, sampleRequest(t)).get(jevProvider).(*object)
	for _, test := range []struct {
		share float64
		valid bool
	}{{0.4, true}, {0.4000005, true}, {0.3999995, true}, {0.400002, false}, {0.399998, false}} {
		result := successRecord(prepared, "supported").get("result").(*object)
		result.get("answers").(*object).get("claim").(*object).set("probabilities", newObject("supported", 0.6, "contradicted", test.share, "insufficient_evidence", 0.0))
		if _, err := validateResponse(result, prepared); (err == nil) != test.valid {
			t.Fatalf("share %v: %v", test.share, err)
		}
	}
}

func TestOpenAIStatusMustMatchItsAnswers(t *testing.T) {
	shared := sampleRequest(t)
	prepared := triple(t, shared)
	raw := rawSuccess(prepared, nil)
	raw[openAIProvider].(*object).set("status", "partial")
	record := recordFrom(t, shared, prepared, raw, 3)
	if encoded(t, provider(record, openAIProvider).get("error")) != `{"category":"invalid_response"}` {
		t.Fatalf("record = %s", encoded(t, provider(record, openAIProvider)))
	}
}

func TestTransportHasNoSeparatePhaseTimeoutOrSecondProtocol(t *testing.T) {
	base := newTransport().client.Transport.(*http.Transport)
	if base.Protocols.HTTP2() || !base.Protocols.HTTP1() || !base.DisableKeepAlives || !base.DisableCompression || base.TLSHandshakeTimeout != 0 {
		t.Fatalf("transport = %+v", base)
	}
}

func TestMissingCredentialNeverCalls(t *testing.T) {
	prepared, _ := openAIFixture(t)
	sender := serve(t, func(string, http.ResponseWriter, *http.Request) { t.Error("request without a credential") })
	if record, code := sender.consultOpenAI(t.Context(), prepared, ""); code != 2 || encoded(t, record.get("error")) != `{"category":"missing_credential"}` {
		t.Fatalf("record = %s", encoded(t, record))
	}
}

func TestFullResponseReadIsBoundedAndRedirectsAreRejected(t *testing.T) {
	prepared, _ := openAIFixture(t)
	slow := serve(t, func(_ string, writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
	})
	bounded, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	record, _ := slow.consultOpenAI(bounded, prepared, "fake")
	if time.Since(started) > time.Second || encoded(t, record.get("error")) != `{"category":"timeout"}` {
		t.Fatalf("record = %s", encoded(t, record))
	}
	var moved atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { moved.Add(1) }))
	defer destination.Close()
	redirect := serve(t, func(_ string, writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusFound)
	})
	record, _ = redirect.consultOpenAI(t.Context(), prepared, "fake")
	if moved.Load() != 0 || encoded(t, record.get("error")) != `{"category":"http_error","http_status":302}` {
		t.Fatalf("redirect = %d %s", moved.Load(), encoded(t, record))
	}
}

func TestCancellationStopsTheRunWithoutARecord(t *testing.T) {
	request, output := tempRequest(t)
	ctx, cancel := context.WithCancel(t.Context())
	sender := serve(t, func(_ string, _ http.ResponseWriter, request *http.Request) {
		cancel()
		<-request.Context().Done()
	})
	code, err := Run(ctx, Options{Request: request, Output: output, Policy: policyFile("required"), Stdout: new(bytes.Buffer), Stderr: new(bytes.Buffer), collect: sender.collect,
		Getenv: func(string) string { return "fake" }})
	if !errors.Is(err, context.Canceled) || code == 0 {
		t.Fatalf("code %d err %v", code, err)
	}
	if data, err := os.ReadFile(output); err != nil || len(data) != 0 {
		t.Fatalf("reserved output = %q %v", data, err)
	}
}

func TestGoWrittenRecordsPassTaskConsultationValidation(t *testing.T) {
	expected := taskflow.Context{TaskID: "task-1", Phase: "before", Snapshot: projectctx.Snapshot{Root: "/repo", SelectedPath: "/repo", Head: "abc123", Branch: "main", Fingerprint: "fingerprint", Dirty: true, FileCount: 3}}
	contextJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise every value the two independent canonical encoders must agree on.
	request := fmt.Sprintf(`{"state":{"task_context":%s,"numbers":[1.50,1E2,-0.0,-0,1e-7,1e22,1e16,1e15,0.0001,0.00001,123456789012345678901234567890],
		"text":"Привіт 😀 \" \\ \n \t \u0001 \u007f   é","z":{"b":1,"a":2,"é":3,"𝒳":4},"":null},
		"questions":{"zeta":{"type":"choice","instructions":{"nested":["Так",1.5]},"criteria":{"yes":"Так","no":["Nein"]}},
		"alpha":{"type":"choice","instructions":"Second","criteria":{"yes":"y","no":"n"}}}}`, contextJSON)
	root := t.TempDir()
	path, output := filepath.Join(root, "request.json"), filepath.Join(root, "record.json")
	if err := os.WriteFile(path, []byte(request), 0o600); err != nil {
		t.Fatal(err)
	}
	shared := parsed(t, []byte(request)).(*object)
	prepared := triple(t, shared)
	sender := serve(t, func(name string, writer http.ResponseWriter, _ *http.Request) {
		if name == openAIProvider {
			t.Error("non-text instructions cannot reach Decisions")
		}
		questions := preparedQuestions(prepared.get(name).(*object))
		answers := newObject()
		for _, id := range questions.names {
			answers.set(id, answerFor(questions.get(id).(*object), "yes"))
		}
		body := newObject("model", prepared.get(name).(*object).get("model"), "answers", answers, "usage", newObject("input_tokens", integer("1"), "output_tokens", integer("2")))
		data, err := wireFormat.marshal(body)
		if err != nil {
			t.Error(err)
		}
		_, _ = writer.Write(data)
	})
	code, err := Run(t.Context(), Options{Request: path, Output: output, Policy: policyFile("required"), Stdout: new(bytes.Buffer), Stderr: new(bytes.Buffer), collect: sender.collect, Getenv: func(string) string { return "fake" }})
	if err != nil || code != 2 {
		t.Fatalf("run = %d %v", code, err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	consultation, err := taskflow.ValidateConsultation(data, expected)
	if err != nil {
		t.Fatalf("task validation refused a Go-written record: %v\n%s", err, data)
	}
	if consultation.Status != "partial" || consultation.Providers["openai_decisions"].Error.Category != "unsupported_request" {
		t.Fatalf("consultation = %+v", consultation)
	}
	item := decisionItem("yes", "resolved", nil)
	decisionData, err := recordFormat.marshal(newObject("questions", newObject("zeta", item, "alpha", item)))
	if err != nil {
		t.Fatal(err)
	}
	decisionPath, finalPath := filepath.Join(root, "decision.json"), filepath.Join(root, "final.json")
	if err := os.WriteFile(decisionPath, decisionData, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, err := Run(t.Context(), Options{Adjudicate: output, Decision: decisionPath, Output: finalPath, Stdout: new(bytes.Buffer), Stderr: new(bytes.Buffer)}); code != 0 || err != nil {
		t.Fatalf("adjudicate = %d %v", code, err)
	}
	final, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := taskflow.ValidateConsultation(final, expected)
	if err != nil || validated.FinalDecision.Status != "resolved" {
		t.Fatalf("final = %+v %v", validated.FinalDecision, err)
	}
}

func TestDeepNestingIsRefusedBeforeItCanExhaustTheStack(t *testing.T) {
	deep := strings.Repeat("[", maxJSONDepth) + strings.Repeat("]", maxJSONDepth)
	if _, err := parseJSON([]byte(deep)); err != nil {
		t.Fatalf("bounded depth refused: %v", err)
	}
	tooDeep := strings.Repeat("[", 1000000)
	if _, err := parseJSON([]byte(tooDeep)); err == nil {
		t.Fatal("unbounded depth accepted")
	}
}
