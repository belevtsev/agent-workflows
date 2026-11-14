package consult

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Transport behavior the design keeps unchanged ("Wire formats per provider are
// unchanged"), exercised through newTransport, transport.collect and makeRecord.

func fxPrepared(t *testing.T, request string, names ...string) (*object, *object) {
	t.Helper()
	shared, err := sharedRequest(fxParse(t, []byte(request)))
	if err != nil {
		t.Fatal(err)
	}
	prepared := newObject()
	for _, name := range fxProviders {
		if fxMembers(names...)[name] != "" {
			value, err := prepareProvider(name, shared, fxPins[name])
			if err != nil {
				t.Fatal(err)
			}
			prepared.set(name, value)
		}
	}
	return shared, prepared
}

func fxCredentials(names ...string) map[string]string {
	credentials := map[string]string{}
	for _, name := range names {
		credentials[name] = "fake-" + name
	}
	return credentials
}

func fxCollected(t *testing.T, sender transport, shared, prepared *object, credentials map[string]string, deadline time.Duration) *object {
	t.Helper()
	raw, err := sender.collect(t.Context(), prepared, batch{credentials: credentials, deadline: deadline})
	if err != nil {
		t.Fatal(err)
	}
	record, err := makeRecord(shared, prepared, raw)
	if err != nil {
		t.Fatal(err)
	}
	return fxRoundTrip(t, record)
}

// Local fixtures use plain HTTP, so this is the only test that sees ALPN. The
// advisers negotiate h2 whenever a client offers it.
func TestTransportNeverOffersHTTP2OverTLS(t *testing.T) {
	t.Parallel()
	shared, prepared := fxPrepared(t, fxSample, fxJev)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.ProtoMajor != 1 {
			t.Errorf("request used %s", request.Proto)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		data, err := fxAnswer(fxJev, body, fxAgree)
		if err != nil {
			t.Error(err)
		}
		_, _ = writer.Write(data)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	selected := newTransport()
	base := selected.client.Transport.(*http.Transport)
	config := &tls.Config{}
	if base.TLSClientConfig != nil {
		config = base.TLSClientConfig.Clone()
	}
	config.RootCAs = roots
	base.TLSClientConfig = config
	selected.endpoints = map[string]string{fxJev: server.URL}
	record := fxCollected(t, selected, shared, prepared, fxCredentials(fxJev), 4*time.Second)
	if fxProvider(record, fxJev).get("status") != "success" {
		t.Fatalf("exchange over TLS = %s", fxCanonical(t, fxProvider(record, fxJev)))
	}
}

func TestTransportShapeAndFixedEndpoints(t *testing.T) {
	t.Parallel()
	selected := newTransport()
	base := selected.client.Transport.(*http.Transport)
	if base.Protocols.HTTP2() || !base.Protocols.HTTP1() || !base.DisableKeepAlives || !base.DisableCompression || base.TLSHandshakeTimeout != 0 {
		t.Fatalf("transport = %+v", base)
	}
	for name, url := range map[string]string{fxJev: "https://api.typesafe.ai/v1/systemone", fxSnap: "https://api.subq.ai/v1/systemone", fxOpenAI: "https://api.openai.com/v1/decisions"} {
		if selected.endpoints[name] != url {
			t.Fatalf("%s endpoint = %s", name, selected.endpoints[name])
		}
	}
}

// One POST per provider, overlapping, each with only its own credential, HTTP/1.1,
// closed connection and identity encoding; credentials never reach the record.
func TestRequestsOverlapAndKeepCredentialsSeparate(t *testing.T) {
	t.Parallel()
	shared, prepared := fxPrepared(t, fxSample, fxProviders...)
	var arrived sync.WaitGroup
	arrived.Add(len(fxProviders))
	server := fxServe(t, func(name string, body []byte, writer http.ResponseWriter, _ *http.Request) {
		// Every handler waits for the others, so only overlapping requests finish.
		arrived.Done()
		arrived.Wait()
		fxHandler(t, nil, nil)(name, body, writer, nil)
	})
	record := fxCollected(t, server.sender, shared, prepared, fxCredentials(fxProviders...), 4*time.Second)
	if record.get("status") != "success" {
		t.Fatalf("record = %s", fxCanonical(t, record.get("providers")))
	}
	for _, name := range fxProviders {
		sent := server.sent(name)
		if len(sent) != 1 {
			t.Fatalf("%s received %d requests", name, len(sent))
		}
		request := sent[0]
		if request.method != http.MethodPost || request.authorization != "Bearer fake-"+name || request.contentType != "application/json" ||
			request.protoMajor != 1 || request.protoMinor != 1 || !request.close || request.acceptEncoding != "identity" {
			t.Fatalf("%s request = %+v", name, request)
		}
	}
	if strings.Contains(fxCanonical(t, record), "fake-") {
		t.Fatal("record retained a credential")
	}
}

// Decisions receives the state as sorted compact JSON text and the questions with
// verbatim instructions and options sorted by value, independent of key order.
func TestDecisionsRequestMapping(t *testing.T) {
	t.Parallel()
	question := `{"type":"choice","instructions":"  Inspect the source.\nUse the actual evidence.  ","criteria":{"supported":"S.","contradicted":"C.","insufficient_evidence":"I."}}`
	bodies := []string{}
	for _, state := range []string{`{"z":[true,null,1],"a":"quoted \"text\""}`, `{"a":"quoted \"text\"","z":[true,null,1]}`} {
		shared, prepared := fxPrepared(t, `{"state":`+state+`,"questions":{"claim":`+question+`}}`, fxOpenAI)
		server := fxServe(t, fxHandler(t, nil, nil))
		fxCollected(t, server.sender, shared, prepared, fxCredentials(fxOpenAI), 4*time.Second)
		sent := server.sent(fxOpenAI)
		if len(sent) != 1 {
			t.Fatalf("%d requests", len(sent))
		}
		bodies = append(bodies, sent[0].body)
	}
	want := `{"model":"gpt-6-luna","input":"{\"a\":\"quoted \\\"text\\\"\",\"z\":[true,null,1]}","questions":[{"type":"choice","name":"claim",` +
		`"instructions":"  Inspect the source.\nUse the actual evidence.  ","choices":[{"value":"contradicted","description":"C."},` +
		`{"value":"insufficient_evidence","description":"I."},{"value":"supported","description":"S."}]}]}`
	for _, body := range bodies {
		if !fxSame(t, fxParse(t, []byte(body)), want) {
			t.Fatalf("body = %s\nwant %s", body, want)
		}
	}
	if bodies[0] != bodies[1] {
		t.Fatalf("key order changed the request:\n%s\n%s", bodies[0], bodies[1])
	}
}

// D3: a stalled provider becomes unavailable/timeout within the batch deadline,
// keeps the others' results, and its abandoned request really ends.
func TestStalledProvidersAreBoundedByTheBatchDeadline(t *testing.T) {
	t.Parallel()
	const deadline = 300 * time.Millisecond
	for name, stall := range map[string]struct {
		provider string
		partial  bool
	}{"hang": {fxSnap, false}, "partial frame": {fxSnap, true}, "hanging Decisions": {fxOpenAI, false}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shared, prepared := fxPrepared(t, fxSample, fxProviders...)
			release := make(chan struct{})
			defer close(release)
			var stopped sync.WaitGroup
			stopped.Add(1)
			server := fxServe(t, func(name string, body []byte, writer http.ResponseWriter, request *http.Request) {
				if name != stall.provider {
					fxHandler(t, nil, nil)(name, body, writer, request)
					return
				}
				defer stopped.Done()
				if stall.partial {
					_, _ = writer.Write([]byte(`{"model":"partial`))
					writer.(http.Flusher).Flush()
				}
				select {
				case <-request.Context().Done():
				case <-release:
				}
			})
			started := time.Now()
			record := fxCollected(t, server.sender, shared, prepared, fxCredentials(fxProviders...), deadline)
			if elapsed := time.Since(started); elapsed > deadline+500*time.Millisecond {
				t.Fatalf("batch took %v with a %v budget", elapsed, deadline)
			}
			stalled := fxProvider(record, stall.provider)
			if stalled.get("status") != "unavailable" || !fxSame(t, stalled.get("error"), `{"category":"timeout"}`) {
				t.Fatalf("stalled provider = %s", fxCanonical(t, stalled))
			}
			for _, other := range fxProviders {
				if other != stall.provider && fxProvider(record, other).get("status") != "success" {
					t.Fatalf("%s lost its result: %s", other, fxCanonical(t, fxProvider(record, other)))
				}
			}
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

type fxPanickingTransport struct {
	http.RoundTripper
	host string
}

func (p fxPanickingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host == p.host {
		panic(fxSecret)
	}
	return p.RoundTripper.RoundTrip(request)
}

// D3 unavailable category worker_error; the other provider keeps its answer; no secret retained.
func TestWorkerFailurePreservesOtherProvider(t *testing.T) {
	t.Parallel()
	shared, prepared := fxPrepared(t, fxSample, fxJev, fxSnap)
	server := fxServe(t, fxHandler(t, nil, nil))
	server.sender.client = &http.Client{Transport: fxPanickingTransport{server.sender.client.Transport, strings.TrimPrefix(server.sender.endpoints[fxSnap], "http://")}}
	record := fxCollected(t, server.sender, shared, prepared, fxCredentials(fxJev, fxSnap), 4*time.Second)
	if fxProvider(record, fxJev).get("status") != "success" || !fxSame(t, fxProvider(record, fxSnap).get("error"), `{"category":"worker_error"}`) ||
		strings.Contains(fxCanonical(t, record), fxSecret) {
		t.Fatalf("record = %s", fxCanonical(t, record.get("providers")))
	}
}

// D3: an HTTP failure is one attempt, recorded as http_error with its status and
// without the raw error body; a redirect is not followed.
func TestHTTPFailureIsOneSanitizedAttempt(t *testing.T) {
	t.Parallel()
	shared, prepared := fxPrepared(t, fxSample, fxProviders...)
	server := fxServe(t, func(_ string, _ []byte, writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, fxSecret, http.StatusTooManyRequests)
	})
	record := fxCollected(t, server.sender, shared, prepared, fxCredentials(fxProviders...), 4*time.Second)
	for _, name := range fxProviders {
		if server.count(name) != 1 || !fxSame(t, fxProvider(record, name).get("error"), `{"category":"http_error","http_status":429}`) {
			t.Fatalf("%s after %d attempts = %s", name, server.count(name), fxCanonical(t, fxProvider(record, name)))
		}
	}
	if record.get("status") != "unavailable" || strings.Contains(fxCanonical(t, record), fxSecret) {
		t.Fatalf("record = %s", fxCanonical(t, record))
	}

	var moved atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { moved.Add(1) }))
	defer destination.Close()
	redirect := fxServe(t, func(_ string, _ []byte, writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusFound)
	})
	shared, prepared = fxPrepared(t, fxSample, fxOpenAI)
	record = fxCollected(t, redirect.sender, shared, prepared, fxCredentials(fxOpenAI), 4*time.Second)
	if moved.Load() != 0 || !fxSame(t, fxProvider(record, fxOpenAI).get("error"), `{"category":"http_error","http_status":302}`) {
		t.Fatalf("redirect = %d %s", moved.Load(), fxCanonical(t, fxProvider(record, fxOpenAI)))
	}
}

// D3: a malformed answer is invalid_response for that provider only.
func TestMalformedAnswerIsUnavailableWithoutDiscardingOther(t *testing.T) {
	t.Parallel()
	answer := func(model, body string) string {
		return `{"model":"` + model + `","answers":{"claim":` + body + `},"usage":{"input_tokens":20,"output_tokens":3}}`
	}
	valid := `{"type":"choice","choice":"supported","probabilities":{"supported":1,"contradicted":0,"insufficient_evidence":0},"confidence":1}`
	for name, response := range map[string]string{
		"model":        answer("subq-snap-other", valid),
		"type":         answer(fxPins[fxSnap], strings.Replace(valid, `"choice","choice"`, `"noul","choice"`, 1)),
		"distribution": answer(fxPins[fxSnap], strings.Replace(valid, `"supported":1,`, `"supported":0.7,`, 1)),
		"not maximum":  answer(fxPins[fxSnap], strings.Replace(valid, `"choice":"supported"`, `"choice":"contradicted"`, 1)),
		"nan":          answer(fxPins[fxSnap], strings.Replace(valid, `"confidence":1`, `"confidence":NaN`, 1)),
		"no answers":   `{"model":"subq-snap-preview","answers":{},"usage":{"input_tokens":20,"output_tokens":3}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shared, prepared := fxPrepared(t, fxSample, fxJev, fxSnap)
			server := fxServe(t, func(provider string, body []byte, writer http.ResponseWriter, request *http.Request) {
				if provider == fxSnap {
					_, _ = writer.Write([]byte(response))
					return
				}
				fxHandler(t, nil, nil)(provider, body, writer, request)
			})
			record := fxCollected(t, server.sender, shared, prepared, fxCredentials(fxJev, fxSnap), 4*time.Second)
			if fxProvider(record, fxJev).get("status") != "success" || !fxSame(t, fxProvider(record, fxSnap).get("error"), `{"category":"invalid_response"}`) ||
				record.get("status") != "partial" {
				t.Fatalf("record = %s", fxCanonical(t, record.get("providers")))
			}
		})
	}
}

// Unchanged: a pinned adviser's distribution must sum to 1 within the tolerance.
func TestDistributionSumIsBoundedByTheTolerance(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		share string
		valid bool
	}{{"0.4", true}, {"0.4000005", true}, {"0.3999995", true}, {"0.400002", false}, {"0.399998", false}} {
		t.Run(test.share, func(t *testing.T) {
			t.Parallel()
			shared, prepared := fxPrepared(t, fxSample, fxJev)
			server := fxServe(t, func(_ string, _ []byte, writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(`{"model":"jev-1.14.0","answers":{"claim":{"type":"choice","choice":"supported","probabilities":{"supported":0.6,"contradicted":` +
					test.share + `,"insufficient_evidence":0.0},"confidence":0.6}},"usage":{"input_tokens":20,"output_tokens":3}}`))
			})
			record := fxCollected(t, server.sender, shared, prepared, fxCredentials(fxJev), 4*time.Second)
			if got := fxProvider(record, fxJev).get("status") == "success"; got != test.valid {
				t.Fatalf("share %s: %s", test.share, fxCanonical(t, fxProvider(record, fxJev)))
			}
		})
	}
}

// Unchanged: large evidence and responses complete within the batch.
func TestLargeEvidenceCompletes(t *testing.T) {
	t.Parallel()
	request := `{"state":"` + strings.Repeat("x", 300000) + `",` + fxQuestions + `}`
	shared, prepared := fxPrepared(t, request, fxJev, fxSnap)
	server := fxServe(t, fxHandler(t, nil, nil))
	if record := fxCollected(t, server.sender, shared, prepared, fxCredentials(fxJev, fxSnap), 4*time.Second); record.get("status") != "success" {
		t.Fatalf("record = %s", fxCanonical(t, record.get("providers")))
	}
}
