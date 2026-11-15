package advisers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const nativeTestKey = "native-test-credential-do-not-record"

func nativeTestDefinition(t *testing.T, provider, base string) Definition {
	t.Helper()
	t.Setenv("KITSU_NATIVE_TEST_KEY", nativeTestKey)
	return Definition{Provider: provider, Model: "test-model", BaseURL: base, APIKeyEnv: "KITSU_NATIVE_TEST_KEY", Timeout: time.Second}
}

func TestNativeProvidersAndRequestMapping(t *testing.T) {
	cases := []struct {
		name     string
		request  string
		wireType string
		criteria string
		answer   string
	}{
		{"choice", `{"version":1,"kind":"decision","question":"Which?","choices":[{"id":"OptionA","text":"Reuse"},{"id":"b","text":"Rewrite"}]}`, "choice", `{"OptionA":"Reuse","b":"Rewrite"}`, `{"type":"choice","choice":"OptionA","confidence":0,"probabilities":{"OptionA":1,"b":0}}`},
		{"score", `{"version":2,"kind":"score","question":"How complete?","levels":["Missing","Partial","Complete"]}`, "score", `["Missing","Partial","Complete"]`, `{"type":"score","score":1.25,"confidence":0.8,"legend":{"0":"Missing","1":"Partial","2":"Complete"},"probabilities":{"0":0,"1":0.75,"2":0.25}}`},
		{"boolean", `{"version":2,"kind":"boolean","question":"Ready?","criteria":{"true":"All tests pass","false":"Tests fail"}}`, "noul", `{"true":"All tests pass","false":"Tests fail"}`, `{"type":"noul","noul":0}`},
	}
	for _, provider := range []string{"typesafe", "subq", "systemone"} {
		t.Run(provider, func(t *testing.T) {
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					r, err := ParseRequest(strings.NewReader(test.request))
					if err != nil {
						t.Fatal(err)
					}
					calls := atomic.Int32{}
					body := `{"model":"reported-model","answers":{"result":` + test.answer + `},"usage":{"input_tokens":42,"output_tokens":0}}`
					server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						calls.Add(1)
						if req.Method != "POST" || req.URL.Path != "/v1/systemone" || req.Header.Get("Authorization") != "Bearer "+nativeTestKey || req.Header.Get("Content-Type") != "application/json" {
							t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
						}
						assertNativeBody(t, req.Body, test.wireType, test.criteria)
						_, _ = io.WriteString(w, body)
					}))
					defer server.Close()
					call, err := PrepareNative(nativeTestDefinition(t, provider, server.URL+"/v1"), r, "explicit context only")
					if err != nil {
						t.Fatal(err)
					}
					got := call.Execute(t.Context(), server.Client())
					if got.Outcome != "success" || got.ActualModel != "reported-model" || got.Answer != body || !got.UsageKnown || got.Usage.InputTokens != 42 || got.Usage.OutputTokens != 0 || calls.Load() != 1 {
						t.Fatalf("result: %+v; calls: %d", got, calls.Load())
					}
					data, err := json.Marshal(got)
					if err != nil || strings.Contains(string(data), nativeTestKey) {
						t.Fatalf("unsafe result: %s, %v", data, err)
					}
					if test.name == "boolean" && (got.Result.ProbabilityTrue == nil || *got.Result.ProbabilityTrue != 0) {
						t.Fatal("lost zero probability")
					}
					if test.name == "choice" && (got.Result.Confidence == nil || *got.Result.Confidence != 0) {
						t.Fatal("lost zero confidence")
					}
					if test.name == "score" && (got.Result.Score == nil || *got.Result.Score != 1.25) {
						t.Fatal("lost fractional score")
					}
				})
			}
		})
	}
}

func assertNativeBody(t *testing.T, body io.Reader, wireType, criteria string) {
	t.Helper()
	var got struct {
		Model     string `json:"model"`
		State     string `json:"state"`
		Questions map[string]struct {
			Type         string `json:"type"`
			Instructions string `json:"instructions"`
			Criteria     any    `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.NewDecoder(body).Decode(&got); err != nil {
		t.Error(err)
		return
	}
	var expected any
	if err := json.Unmarshal([]byte(criteria), &expected); err != nil {
		t.Error(err)
		return
	}
	q := got.Questions["result"]
	if got.Model != "test-model" || got.State != "explicit context only" || len(got.Questions) != 1 || q.Type != wireType || q.Instructions == "" || !reflect.DeepEqual(q.Criteria, expected) {
		t.Errorf("unexpected body: %+v", got)
	}
}

func TestNativeFailuresAreBoundedAndDoNotRetry(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body, outcome string
	}{
		{"bad request", 400, nativeTestKey, "failed"}, {"credentials", 401, nativeTestKey, "failed"},
		{"credits", 402, nativeTestKey, "failed"}, {"access", 403, nativeTestKey, "failed"},
		{"validation", 422, nativeTestKey, "failed"}, {"rate limit", 429, nativeTestKey, "failed"},
		{"server", 503, nativeTestKey, "failed"}, {"malformed", 200, "{", "failed"},
		{"empty", 200, "", "failed"}, {"invalid utf8", 200, "\xff", "failed"},
		{"overflow", 200, strings.Repeat("x", MaxAnswer+1), "overflow"},
		{"echoed key", 200, `{"model":"` + nativeTestKey + `","answers":{"result":{"type":"noul","noul":1}}}`, "failed"},
		{"escaped echoed key", 200, `{"model":"\u006eative-test-credential-do-not-record","answers":{"result":{"type":"noul","noul":1}}}`, "failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			call, err := PrepareNative(nativeTestDefinition(t, "typesafe", server.URL), Request{Version: 2, Kind: "boolean", Question: "Ready?"}, "Context")
			if err != nil {
				t.Fatal(err)
			}
			got := call.Execute(t.Context(), server.Client())
			if got.Outcome != test.outcome || got.Answer != "" || got.Result != nil || got.UsageKnown || calls.Load() != 1 {
				t.Fatalf("unexpected failure: %+v", got)
			}
			data, err := json.Marshal(got)
			if err != nil || strings.Contains(string(data), nativeTestKey) {
				t.Fatal("credentials appeared in execution")
			}
		})
	}
}

func TestNativeRefusesRedirects(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, status) }))
			defer server.Close()
			call, err := PrepareNative(nativeTestDefinition(t, "subq", server.URL), Request{Version: 2, Kind: "boolean", Question: "Ready?"}, "Context")
			if err != nil {
				t.Fatal(err)
			}
			got := call.Execute(t.Context(), server.Client())
			if got.Outcome != "failed" || !strings.Contains(got.Detail, "redirect refused") || destinationCalls.Load() != 0 {
				t.Fatalf("redirect followed: %+v", got)
			}
		})
	}
}

func TestNativeCancellationAndTimeout(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Send headers so cancellation also exercises a blocked body read.
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			call, err := PrepareNative(nativeTestDefinition(t, "subq", server.URL), Request{Version: 2, Kind: "boolean", Question: "Ready?"}, "Context")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelled {
				go func() {
					select {
					case <-started:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			got := call.Execute(ctx, server.Client())
			want := "timed_out"
			if cancelled {
				want = "cancelled"
			}
			if got.Outcome != want || got.Answer != "" {
				t.Fatalf("outcome: %+v, want %s", got, want)
			}
		})
	}
}
