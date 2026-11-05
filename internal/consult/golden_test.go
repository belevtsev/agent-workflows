package consult

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// The testdata goldens were captured by running the former helpers end to end
// with only their transport replaced. They are the compatibility contract: a
// record, wire body or digest may differ only in duration_seconds.

var durations = regexp.MustCompile(`"duration_seconds": [0-9.e+-]+`)

func normalizeDurations(data []byte) []byte {
	return durations.ReplaceAll(data, []byte(`"duration_seconds": 0.0`))
}

func readTestdata(t *testing.T, path ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, path...)...))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeTestdata[T any](t *testing.T, path ...string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(readTestdata(t, path...), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func parsed(t *testing.T, data []byte) any {
	t.Helper()
	value, err := parseJSON(data)
	if err != nil {
		t.Fatalf("parse %s: %v", data, err)
	}
	return value
}

type scriptedResponse struct {
	Kind   string `json:"kind"`
	Status int    `json:"status"`
	Body   string `json:"body"`
	Repeat string `json:"repeat"`
	Count  int    `json:"count"`
	Pad    int    `json:"pad"`
}

func (r scriptedResponse) bytes() []byte {
	if r.Repeat != "" {
		return bytes.Repeat([]byte(r.Repeat), r.Count)
	}
	body := []byte(r.Body)
	if len(body) < r.Pad {
		body = append(body, bytes.Repeat([]byte(" "), r.Pad-len(body))...)
	}
	return body
}

type scenario struct {
	Policy      string                      `json:"policy"`
	Flags       []string                    `json:"flags"`
	Credentials []string                    `json:"credentials"`
	Responses   map[string]scriptedResponse `json:"responses"`
	Exit        int                         `json:"exit"`
	Stdout      string                      `json:"stdout"`
	Stderr      string                      `json:"stderr"`
}

type wireRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

var fakeKeys = map[string]string{"TYPESAFE_API_KEY": "fake-jev-key", "SUBQ_API_KEY": "fake-snap-key", "OPENAI_API_KEY": "fake-openai-key"}

// fixture serves one scripted adviser per provider, recording what arrived.
type fixture struct {
	mutex    sync.Mutex
	received map[string]wireRequest
	calls    map[string]int
	sender   transport
}

func newFixture(t *testing.T, responses map[string]scriptedResponse) *fixture {
	t.Helper()
	server := &fixture{received: map[string]wireRequest{}, calls: map[string]int{}}
	base := newTransport()
	server.sender = transport{client: base.client, endpoints: map[string]string{}}
	for _, name := range providerOrder {
		response := responses[name]
		handler := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body := new(bytes.Buffer)
			_, _ = body.ReadFrom(request.Body)
			server.mutex.Lock()
			server.calls[name]++
			server.received[name] = wireRequest{Method: request.Method, Headers: map[string]string{
				"Authorization": request.Header.Get("Authorization"), "Content-type": request.Header.Get("Content-Type"),
			}, Body: body.String()}
			server.mutex.Unlock()
			switch response.Kind {
			case "http_error":
				writer.WriteHeader(response.Status)
			case "network_error":
				connection, _, err := writer.(http.Hijacker).Hijack()
				if err == nil {
					connection.Close()
				}
			case "timeout":
				<-request.Context().Done()
			default:
				_, _ = writer.Write(response.bytes())
			}
		}))
		t.Cleanup(handler.Close)
		server.sender.endpoints[name] = handler.URL
	}
	return server
}

func (f *fixture) getenv(names []string) func(string) string {
	return func(name string) string {
		if slices.Contains(names, name) {
			return fakeKeys[name]
		}
		return ""
	}
}

func goldenFlags(o *Options, flags []string) {
	for _, flag := range flags {
		switch flag {
		case "--dry-run":
			o.DryRun = true
		case "--with-openai":
			o.UseOpenAI = new(true)
		case "--without-openai":
			o.UseOpenAI = new(false)
		}
	}
}

func hasTimeout(item scenario) bool {
	return slices.ContainsFunc([]string{jevProvider, snapProvider, openAIProvider}, func(name string) bool { return item.Responses[name].Kind == "timeout" })
}

func TestGoldenConsultationsMatchHelperRecords(t *testing.T) {
	names, err := filepath.Glob(filepath.Join("testdata", "golden", "*", "request.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no goldens: %v", err)
	}
	for _, path := range names {
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			item := decodeTestdata[scenario](t, "golden", name, "scenario.json")
			server := newFixture(t, item.Responses)
			output := filepath.Join(t.TempDir(), "record.json")
			var stdout, stderr bytes.Buffer
			options := Options{Request: path, Output: output, Policy: filepath.Join("testdata", "policy", item.Policy+".yaml"), Getenv: server.getenv(item.Credentials), Stdout: &stdout, Stderr: &stderr, collect: server.sender.collect}
			if hasTimeout(item) {
				options.deadline = 300 * time.Millisecond
			}
			goldenFlags(&options, item.Flags)
			code, err := Run(t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			if code != item.Exit || stdout.String() != item.Stdout || stderr.String() != item.Stderr {
				t.Fatalf("exit %d stdout %q stderr %q; want %d %q %q", code, stdout.String(), stderr.String(), item.Exit, item.Stdout, item.Stderr)
			}
			expected := filepath.Join("testdata", "golden", name, "record.json")
			want, err := os.ReadFile(expected)
			if os.IsNotExist(err) {
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("helper wrote no record, but one exists")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(normalizeDurations(got), want) {
				t.Fatalf("record differs from golden:\n--- got\n%s\n--- want\n%s", normalizeDurations(got), want)
			}
			info, err := os.Stat(output)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("record mode %v: %v", info.Mode(), err)
			}
			wire := decodeTestdata[map[string]wireRequest](t, "golden", name, "wire.json")
			checkWire(t, server, wire)
		})
	}
}

func checkWire(t *testing.T, server *fixture, wire map[string]wireRequest) {
	t.Helper()
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if len(server.received) != len(wire) {
		t.Fatalf("providers contacted = %d, helper contacted %d", len(server.received), len(wire))
	}
	for name, want := range wire {
		got := server.received[name]
		if got.Body != want.Body || got.Method != want.Method || got.Headers["Authorization"] != want.Headers["Authorization"] || got.Headers["Content-type"] != want.Headers["Content-type"] {
			t.Fatalf("%s request differs:\n%+v\n%+v", name, got, want)
		}
		if want.URL != fixedEndpoints[name] {
			t.Fatalf("%s endpoint changed: %s", name, fixedEndpoints[name])
		}
		if server.calls[name] != 1 {
			t.Fatalf("%s received %d attempts", name, server.calls[name])
		}
	}
}

func TestGoldenAdjudicationsMatchHelper(t *testing.T) {
	names, err := filepath.Glob(filepath.Join("testdata", "golden", "*", "consultation.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no goldens: %v", err)
	}
	for _, path := range names {
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			item := decodeTestdata[scenario](t, "golden", name, "scenario.json")
			output := filepath.Join(t.TempDir(), "final.json")
			var stdout, stderr bytes.Buffer
			refused := func(context.Context, *object, batch) (map[string]any, error) {
				t.Fatal("adjudication made a provider request")
				return nil, nil
			}
			code, err := Run(t.Context(), Options{Adjudicate: path, Decision: filepath.Join(filepath.Dir(path), "decision.json"), Output: output, Stdout: &stdout, Stderr: &stderr, collect: refused,
				Getenv: func(string) string { t.Fatal("adjudication read the environment"); return "" }})
			if err != nil {
				t.Fatal(err)
			}
			if code != item.Exit || stdout.String() != item.Stdout || stderr.String() != item.Stderr {
				t.Fatalf("exit %d stdout %q stderr %q; want %d %q %q", code, stdout.String(), stderr.String(), item.Exit, item.Stdout, item.Stderr)
			}
			want, err := os.ReadFile(filepath.Join(filepath.Dir(path), "final.json"))
			if os.IsNotExist(err) {
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("refused adjudication wrote output")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("final record differs:\n%s", got)
			}
		})
	}
}

func TestRequestValidationMatchesHelperMessages(t *testing.T) {
	type requestCase struct {
		Request string `json:"request"`
		Exit    int    `json:"exit"`
		Stdout  string `json:"stdout"`
		Stderr  string `json:"stderr"`
		Output  bool   `json:"output"`
	}
	for index, item := range decodeTestdata[[]requestCase](t, "request_errors.json") {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			root := t.TempDir()
			request, output := filepath.Join(root, "request.json"), filepath.Join(root, "output.json")
			if err := os.WriteFile(request, []byte(item.Request), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code, err := Run(t.Context(), Options{Request: request, Output: output, DryRun: true, Policy: filepath.Join("testdata", "policy", "required.yaml"), Getenv: func(string) string { return "" }, Stdout: &stdout, Stderr: &stderr})
			if err != nil {
				t.Fatal(err)
			}
			if code != item.Exit || stdout.String() != item.Stdout || stderr.String() != item.Stderr {
				t.Fatalf("request %s\nexit %d stdout %q stderr %q; want %d %q %q", item.Request, code, stdout.String(), stderr.String(), item.Exit, item.Stdout, item.Stderr)
			}
			if _, err := os.Stat(output); (err == nil) != item.Output {
				t.Fatalf("output exists mismatch: %v", err)
			}
		})
	}
}

func TestJSONCorpusMatchesHelperEncoding(t *testing.T) {
	type jsonCase struct {
		Input     string  `json:"input"`
		Compact   string  `json:"compact"`
		Sorted    string  `json:"sorted"`
		SHA256    string  `json:"sha256"`
		Indent    string  `json:"indent"`
		Unicode   *string `json:"unicode"`
		Invalid   bool    `json:"invalid"`
		Nonfinite bool    `json:"nonfinite"`
	}
	for index, item := range decodeTestdata[[]jsonCase](t, "json_cases.json") {
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
			if _, err := wireFormat.marshal(value); err == nil {
				t.Fatalf("%d: encoded nonfinite value", index)
			}
			continue
		}
		for name, check := range map[string]struct {
			format format
			want   string
		}{"compact": {wireFormat, item.Compact}, "sorted": {canonicalFormat, item.Sorted}, "indent": {recordFormat, item.Indent}} {
			got, err := check.format.marshal(value)
			if err != nil || string(got) != check.want {
				t.Fatalf("%d %s of %q:\n%s\n%s\n%v", index, name, item.Input, got, check.want, err)
			}
		}
		digest, err := fingerprint(value)
		if err != nil || digest != item.SHA256 {
			t.Fatalf("%d: digest %s want %s: %v", index, digest, item.SHA256, err)
		}
		got, err := reportFormat.marshal(value)
		if item.Unicode == nil {
			if err == nil {
				t.Fatalf("%d: wrote an unpaired surrogate as UTF-8", index)
			}
		} else if err != nil || string(got) != *item.Unicode {
			t.Fatalf("%d unicode of %q:\n%s\n%s\n%v", index, item.Input, got, *item.Unicode, err)
		}
	}
}

// mutation replaces or removes one field of a parsed base document, reproducing
// a case the helper evaluated.
type mutation struct {
	Path  []any   `json:"path"`
	Op    string  `json:"op"`
	Value *string `json:"value"`
	OK    any     `json:"ok"`
	Error *string `json:"error"`
	Crash *string `json:"crash"`
}

func (m mutation) String() string {
	value := ""
	if m.Value != nil {
		value = *m.Value
	}
	return fmt.Sprintf("%s %v %s", m.Op, m.Path, value)
}

func clone(value any) any {
	switch item := value.(type) {
	case []any:
		result := make([]any, len(item))
		for index := range item {
			result[index] = clone(item[index])
		}
		return result
	case *object:
		result := newObject()
		for _, name := range item.names {
			result.set(name, clone(item.get(name)))
		}
		return result
	default:
		return value
	}
}

func (m mutation) apply(t *testing.T, root any) any {
	t.Helper()
	root = clone(root)
	target := root
	step := func(part any) any {
		if name, ok := part.(string); ok {
			return target.(*object).get(name)
		}
		return target.([]any)[int(part.(float64))]
	}
	for _, part := range m.Path[:len(m.Path)-1] {
		target = step(part)
	}
	last := m.Path[len(m.Path)-1]
	if m.Op == "delete" {
		if name, ok := last.(string); ok {
			item := target.(*object)
			delete(item.values, name)
			item.names = slices.DeleteFunc(item.names, func(member string) bool { return member == name })
		} else {
			parent := root
			target = parent
			for _, part := range m.Path[:len(m.Path)-2] {
				target = step(part)
			}
			index := int(last.(float64))
			list := step(m.Path[len(m.Path)-2]).([]any)
			shorter := slices.Delete(slices.Clone(list), index, index+1)
			if name, ok := m.Path[len(m.Path)-2].(string); ok {
				target.(*object).set(name, shorter)
			} else {
				target.([]any)[int(m.Path[len(m.Path)-2].(float64))] = shorter
			}
		}
		return root
	}
	value := parsed(t, []byte(*m.Value))
	if name, ok := last.(string); ok {
		target.(*object).set(name, value)
	} else {
		target.([]any)[int(last.(float64))] = value
	}
	return root
}

func TestResponseValidationMatchesHelperMutations(t *testing.T) {
	data := parsed(t, readTestdata(t, "response_mutations.json")).(*object)
	raw := decodeTestdata[struct {
		Providers map[string]struct {
			Model string     `json:"model"`
			Cases []mutation `json:"cases"`
		} `json:"providers"`
	}](t, "response_mutations.json")
	shared := data.get("shared")
	for name, provider := range raw.Providers {
		base := data.get("providers").(*object).get(name).(*object).get("base")
		prepared, err := prepareRequest(shared, provider.Model)
		if err != nil {
			t.Fatal(err)
		}
		validate := validateResponse
		if name == openAIProvider {
			validate = validateOpenAIResponse
		}
		for _, item := range provider.Cases {
			result, err := validate(item.apply(t, base), prepared)
			if item.Error != nil {
				if err == nil || err.Error() != *item.Error {
					t.Fatalf("%s %s: error %v, want %q", name, item, err, *item.Error)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s %s: %v", name, item, err)
			}
			got, err := wireFormat.marshal(result)
			if err != nil || string(got) != item.OK {
				t.Fatalf("%s %s:\n%s\n%v\n%v", name, item, got, item.OK, err)
			}
		}
	}
}

func TestRecordBuildingMatchesHelperMutations(t *testing.T) {
	data := parsed(t, readTestdata(t, "record_mutations.json")).(*object)
	cases := decodeTestdata[struct {
		Cases []mutation `json:"cases"`
	}](t, "record_mutations.json").Cases
	shared, prepared, base := data.get("shared").(*object), data.get("prepared").(*object), data.get("raw")
	for _, item := range cases {
		mutated := item.apply(t, base).(*object)
		raw := map[string]any{}
		for _, name := range mutated.names {
			raw[name] = mutated.get(name)
		}
		record, err := makeRecord(shared, prepared, raw, 2)
		if err != nil {
			t.Fatalf("%s: %v", item, err)
		}
		body, err := wireFormat.marshal(record)
		if err != nil {
			t.Fatalf("%s: %v", item, err)
		}
		hash := sha256.Sum256(body)
		if got := hex.EncodeToString(hash[:])[:16]; got != item.OK {
			t.Fatalf("%s: record differs from helper:\n%s", item, body)
		}
	}
}

func TestAdjudicationMatchesHelperMutations(t *testing.T) {
	data := parsed(t, readTestdata(t, "adjudication_mutations.json")).(*object)
	cases := decodeTestdata[struct {
		Cases map[string][]mutation `json:"cases"`
	}](t, "adjudication_mutations.json").Cases
	record, decision := data.get("record"), data.get("decision")
	check := func(item mutation, final *object, err error) {
		t.Helper()
		if item.Error != nil {
			if err == nil || err.Error() != *item.Error {
				t.Fatalf("%s: error %v, want %q", item, err, *item.Error)
			}
			return
		}
		if item.Crash != nil {
			if err == nil {
				t.Fatalf("%s: helper crashed with %s, but it was accepted", item, *item.Crash)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s: %v", item, err)
		}
		if status := final.get("final_decision").(*object).get("status"); status != item.OK {
			t.Fatalf("%s: status %v, want %v", item, status, item.OK)
		}
	}
	for _, item := range cases["record"] {
		final, err := adjudicate(item.apply(t, record), decision)
		check(item, final, err)
	}
	for _, item := range cases["decision"] {
		final, err := adjudicate(record, item.apply(t, decision))
		check(item, final, err)
	}
}
