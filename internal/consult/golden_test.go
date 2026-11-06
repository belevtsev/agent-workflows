package consult

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Goldens under testdata/golden are authored from the design contract: version 3
// records whose provider shapes the contract leaves unchanged are relabelled
// version 4, and not_configured entries, record statuses and comparisons follow D3.
// A record may differ only in duration_seconds.

var fxDurations = regexp.MustCompile(`"duration_seconds": [0-9.e+-]+`)

func fxNormalizeDurations(data []byte) []byte {
	return fxDurations.ReplaceAll(data, []byte(`"duration_seconds": 0.0`))
}

func fxTestdata(t testing.TB, path ...string) []byte {
	t.Helper()
	return fxRead(t, filepath.Join(append([]string{"testdata"}, path...)...))
}

func fxDecode[T any](t testing.TB, path ...string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(fxTestdata(t, path...), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

type fxScripted struct {
	Kind   string `json:"kind"`
	Status int    `json:"status"`
	Body   string `json:"body"`
	Repeat string `json:"repeat"`
	Count  int    `json:"count"`
	Pad    int    `json:"pad"`
}

func (r fxScripted) bytes() []byte {
	if r.Repeat != "" {
		return bytes.Repeat([]byte(r.Repeat), r.Count)
	}
	body := []byte(r.Body)
	if len(body) < r.Pad {
		body = append(body, bytes.Repeat([]byte(" "), r.Pad-len(body))...)
	}
	return body
}

// fxScenario: stdout/stderr null means "not fixed by the contract"; *_contains
// then names a required fragment.
type fxScenario struct {
	Policy         string                `json:"policy"`
	Credentials    []string              `json:"credentials"`
	Responses      map[string]fxScripted `json:"responses"`
	Exit           int                   `json:"exit"`
	Stdout         *string               `json:"stdout"`
	Stderr         *string               `json:"stderr"`
	StdoutContains string                `json:"stdout_contains"`
	StderrContains string                `json:"stderr_contains"`
}

func (s fxScenario) check(t *testing.T, result fxResult) {
	t.Helper()
	if result.code != s.Exit || (s.Stdout != nil && result.stdout != *s.Stdout) || (s.Stderr != nil && result.stderr != *s.Stderr) ||
		!strings.Contains(result.stdout, s.StdoutContains) || !strings.Contains(result.stderr, s.StderrContains) {
		t.Fatalf("result %+v does not match scenario %+v", result, s)
	}
}

type fxWireGolden struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

var fxFakeKeys = map[string]string{"TYPESAFE_API_KEY": "fake-jev-key", "SUBQ_API_KEY": "fake-snap-key", "OPENAI_API_KEY": "fake-openai-key"}

func fxScriptedServer(t *testing.T, responses map[string]fxScripted) *fxServer {
	return fxServe(t, func(name string, _ []byte, writer http.ResponseWriter, request *http.Request) {
		response := responses[name]
		switch response.Kind {
		case "http_error":
			writer.WriteHeader(response.Status)
		case "network_error":
			if connection, _, err := writer.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		case "timeout":
			<-request.Context().Done()
		default:
			_, _ = writer.Write(response.bytes())
		}
	})
}

// D3 participation, statuses, comparisons and exit codes with unchanged wire formats.
func TestGoldenConsultations(t *testing.T) {
	t.Parallel()
	names, err := filepath.Glob(filepath.Join("testdata", "golden", "*", "request.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no goldens: %v", err)
	}
	for _, path := range names {
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			item := fxDecode[fxScenario](t, "golden", name, "scenario.json")
			server := fxScriptedServer(t, item.Responses)
			output := filepath.Join(t.TempDir(), "record.json")
			options := Options{Request: path, Output: output, Policy: fxPolicy(item.Policy), collect: server.sender.collect,
				Getenv: func(variable string) string {
					if slices.Contains(item.Credentials, variable) {
						return fxFakeKeys[variable]
					}
					return ""
				}}
			for _, response := range item.Responses {
				if response.Kind == "timeout" {
					options.deadline = 300 * time.Millisecond
				}
			}
			item.check(t, fxRun(t, options))
			want := fxTestdata(t, "golden", name, "record.json")
			got := fxRead(t, output)
			if !bytes.Equal(fxNormalizeDurations(got), want) {
				t.Fatalf("record differs from golden:\n--- got\n%s\n--- want\n%s", fxNormalizeDurations(got), want)
			}
			fxPrivate(t, output)
			wire := fxDecode[map[string]fxWireGolden](t, "golden", name, "wire.json")
			for _, provider := range fxProviders {
				sent := server.sent(provider)
				want, contacted := wire[provider]
				if !contacted {
					if len(sent) != 0 {
						t.Fatalf("%s was contacted %d times", provider, len(sent))
					}
					continue
				}
				if len(sent) != 1 {
					t.Fatalf("%s received %d attempts", provider, len(sent))
				}
				if got := sent[0]; got.body != want.Body || got.method != want.Method || got.authorization != want.Headers["Authorization"] || got.contentType != want.Headers["Content-type"] {
					t.Fatalf("%s request differs:\n%+v\n%+v", provider, got, want)
				}
				if newTransport().endpoints[provider] != want.URL {
					t.Fatalf("%s endpoint = %s", provider, newTransport().endpoints[provider])
				}
			}
		})
	}
}

// D4 through Run --adjudicate; final records compared without regard to member order.
func TestGoldenAdjudications(t *testing.T) {
	t.Parallel()
	names, err := filepath.Glob(filepath.Join("testdata", "golden", "*", "consultation.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no goldens: %v", err)
	}
	for _, path := range names {
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			item := fxDecode[fxScenario](t, "golden", name, "scenario.json")
			output := filepath.Join(t.TempDir(), "final.json")
			item.check(t, fxRun(t, Options{Adjudicate: path, Decision: filepath.Join(filepath.Dir(path), "decision.json"), Output: output,
				Getenv: fxNoEnv(t), collect: fxNoCollect(t)}))
			expected := filepath.Join(filepath.Dir(path), "final.json")
			if _, err := os.Stat(expected); os.IsNotExist(err) {
				fxAbsent(t, output)
				return
			}
			got := fxRead(t, output)
			if fxCanonical(t, fxParse(t, got)) != fxCanonical(t, fxParse(t, fxRead(t, expected))) {
				t.Fatalf("final record differs:\n%s", got)
			}
			if formatted, err := recordFormat.marshal(fxParse(t, got)); err != nil || !bytes.Equal(bytes.TrimSpace(formatted), bytes.TrimSpace(got)) {
				t.Fatalf("final record is not in record format: %v", err)
			}
			fxPrivate(t, output)
		})
	}
}

// Unchanged request validation messages (exit 1, no output) under a version 3 policy.
func TestRequestValidationMatchesHelperMessages(t *testing.T) {
	t.Parallel()
	type requestCase struct {
		Request string `json:"request"`
		Exit    int    `json:"exit"`
		Stdout  string `json:"stdout"`
		Stderr  string `json:"stderr"`
		Output  bool   `json:"output"`
	}
	for index, item := range fxDecode[[]requestCase](t, "request_errors.json") {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			request, output := filepath.Join(root, "request.json"), filepath.Join(root, "output.json")
			fxWrite(t, request, []byte(item.Request))
			result := fxRun(t, Options{Request: request, Output: output, DryRun: true, Policy: fxPolicy("all"), Getenv: fxEnv(), collect: fxNoCollect(t)})
			if result.code != item.Exit || result.stdout != item.Stdout || result.stderr != item.Stderr {
				t.Fatalf("request %s\nresult %+v; want %d %q %q", item.Request, result, item.Exit, item.Stdout, item.Stderr)
			}
			if _, err := os.Stat(output); (err == nil) != item.Output {
				t.Fatalf("output exists mismatch: %v", err)
			}
		})
	}
}

// Revision 2 fingerprint encoding: canonicalFormat is Python's sorted, compact,
// ensure_ascii JSON with numbers as written, and its SHA-256 is the fingerprint.
func TestJSONCorpusMatchesHelperEncoding(t *testing.T) {
	t.Parallel()
	type jsonCase struct {
		Input     string `json:"input"`
		Sorted    string `json:"sorted"`
		SHA256    string `json:"sha256"`
		Indent    string `json:"indent"`
		Invalid   bool   `json:"invalid"`
		Nonfinite bool   `json:"nonfinite"`
	}
	for index, item := range fxDecode[[]jsonCase](t, "json_cases.json") {
		value, err := parseJSON([]byte(item.Input))
		if item.Invalid {
			if err == nil {
				t.Fatalf("%d: accepted invalid JSON %q", index, item.Input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%d: rejected %q: %v", index, item.Input, err)
		}
		if item.Nonfinite {
			if _, err := canonicalFormat.marshal(value); err == nil {
				t.Fatalf("%d: encoded nonfinite value", index)
			}
			continue
		}
		sorted, err := canonicalFormat.marshal(value)
		if err != nil || string(sorted) != item.Sorted {
			t.Fatalf("%d sorted %q:\n%s\n%s\n%v", index, item.Input, sorted, item.Sorted, err)
		}
		if indent, err := recordFormat.marshal(value); err != nil || string(indent) != item.Indent {
			t.Fatalf("%d indent %q:\n%s\n%s\n%v", index, item.Input, indent, item.Indent, err)
		}
		if digest := fxFingerprint(t, value); digest != item.SHA256 {
			t.Fatalf("%d: digest %s want %s", index, digest, item.SHA256)
		}
	}
}

// fxMutation replaces or removes one member of a response, reproducing a case the
// former helper evaluated. OK holds the validated result; Error a helper message.
type fxMutation struct {
	Path  []any   `json:"path"`
	Op    string  `json:"op"`
	Value *string `json:"value"`
	OK    any     `json:"ok"`
	Error *string `json:"error"`
}

func (m fxMutation) String() string {
	value := ""
	if m.Value != nil {
		value = *m.Value
	}
	return fmt.Sprintf("%s %v %s", m.Op, m.Path, value)
}

const fxPlaceholder = "fx-mutation-placeholder"

// apply mutates a plain decoded copy of base and returns the response bytes, with
// the replacement value spliced in exactly as the helper wrote it.
func (m fxMutation) apply(t *testing.T, base jsontext.Value) []byte {
	t.Helper()
	var root any
	if err := json.Unmarshal(base, &root); err != nil {
		t.Fatal(err)
	}
	parent := root
	for _, part := range m.Path[:len(m.Path)-1] {
		switch key := part.(type) {
		case string:
			parent = parent.(map[string]any)[key]
		case float64:
			parent = parent.([]any)[int(key)]
		}
	}
	last := m.Path[len(m.Path)-1]
	switch key := last.(type) {
	case string:
		object := parent.(map[string]any)
		if m.Op == "delete" {
			delete(object, key)
		} else {
			object[key] = fxPlaceholder
		}
	case float64:
		list := parent.([]any)
		if m.Op == "delete" {
			shorter := slices.Delete(slices.Clone(list), int(key), int(key)+1)
			m.replaceList(t, root, shorter)
		} else {
			list[int(key)] = fxPlaceholder
		}
	}
	data, err := json.Marshal(root, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if m.Op != "delete" {
		data = bytes.Replace(data, []byte(`"`+fxPlaceholder+`"`), []byte(*m.Value), 1)
	}
	return data
}

// replaceList stores a shortened list at the mutation's parent path.
func (m fxMutation) replaceList(t *testing.T, root any, list []any) {
	t.Helper()
	path := m.Path[:len(m.Path)-1]
	container := root
	for _, part := range path[:len(path)-1] {
		switch key := part.(type) {
		case string:
			container = container.(map[string]any)[key]
		case float64:
			container = container.([]any)[int(key)]
		}
	}
	switch key := path[len(path)-1].(type) {
	case string:
		container.(map[string]any)[key] = list
	case float64:
		container.([]any)[int(key)] = list
	}
}

// Unchanged response validation (D3 "wire formats unchanged"): every helper
// mutation is sent over the transport; rejected responses become unavailable
// with invalid_response, accepted ones keep the helper's validated result.
func TestResponseValidationMatchesHelperMutations(t *testing.T) {
	t.Parallel()
	var corpus struct {
		Shared    jsontext.Value `json:"shared"`
		Providers map[string]struct {
			Model string         `json:"model"`
			Base  jsontext.Value `json:"base"`
			Cases []fxMutation   `json:"cases"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(fxTestdata(t, "response_mutations.json"), &corpus); err != nil {
		t.Fatal(err)
	}
	shared, err := sharedRequest(fxParse(t, corpus.Shared))
	if err != nil {
		t.Fatal(err)
	}
	var mutex sync.Mutex
	var body []byte
	server := fxServe(t, func(_ string, _ []byte, writer http.ResponseWriter, _ *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		_, _ = writer.Write(body)
	})
	checked := 0
	for name, provider := range corpus.Providers {
		prepared, err := prepareProvider(name, shared, provider.Model)
		if err != nil {
			t.Fatal(err)
		}
		participants := newObject(name, prepared)
		for _, item := range provider.Cases {
			if item.Error == nil && item.Value != nil && strings.Contains(*item.Value, "e400") {
				continue // a nonfinite number is now rejected while reading the response body
			}
			mutex.Lock()
			body = item.apply(t, provider.Base)
			mutex.Unlock()
			raw, err := server.sender.collect(t.Context(), participants, batch{credentials: map[string]string{name: "fake"}, deadline: 5 * time.Second})
			if err != nil {
				t.Fatalf("%s %s: %v", name, item, err)
			}
			record, err := makeRecord(shared, participants, raw)
			if err != nil {
				t.Fatalf("%s %s: %v", name, item, err)
			}
			entry := fxProvider(fxRoundTrip(t, record), name)
			if item.Error != nil {
				if entry.get("status") != "unavailable" || !fxSame(t, entry.get("error"), `{"category":"invalid_response"}`) {
					t.Fatalf("%s %s (helper: %s): %s", name, item, *item.Error, fxCanonical(t, entry))
				}
			} else {
				ok, _ := item.OK.(string)
				if status := entry.get("status"); status == "unavailable" || fxCanonical(t, entry.get("result")) != fxCanonical(t, fxParse(t, []byte(ok))) {
					t.Fatalf("%s %s:\n%s\nwant result %s", name, item, fxCanonical(t, entry), ok)
				}
			}
			checked++
		}
	}
	if checked < 800 {
		t.Fatalf("only %d mutations checked", checked)
	}
}
