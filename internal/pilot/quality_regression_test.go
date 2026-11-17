package pilot

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var (
	charsetV2Input  = flag.String("pilot-charset-v2-input", "", "new offline charset probe input with baseline, reference and optional candidate source directories")
	charsetV2Output = flag.String("pilot-charset-v2-output", "", "new private charset v2 evidence directory; never rewrites frozen grades")
)

type charsetV2Case struct {
	ID       string   `json:"id"`
	Header   string   `json:"header"`
	Allowed  []string `json:"allowed"`
	Body     string   `json:"body"`
	Expected int      `json:"expected"`
	Family   string   `json:"family"`
}

// v2 adds both independently confirmed regression families to the original
// quoted-charset task. Literal ordinary charset policy remains authoritative;
// extended MIME parameters and malformed unrelated parameters cannot replace it.
func charsetV2Cases() []charsetV2Case {
	return []charsetV2Case{
		{"ordinary_allowed", "text/plain; charset=utf-8", []string{"UTF-8"}, "body", 204, "control"},
		{"ordinary_disallowed", "text/plain; charset=latin-1", []string{"UTF-8", ""}, "body", 415, "control"},
		{"quoted_allowed", `text/plain; charset="UTF-8"`, []string{"utf-8"}, "body", 204, "original_task"},
		{"quoted_disallowed", `text/plain; charset="latin-1"`, []string{"utf-8", ""}, "body", 415, "original_task"},
		{"bodyless_bypass", "text/plain; charset=latin-1", []string{"utf-8"}, "", 204, "control"},
		{"absent_charset", "text/plain", []string{"utf-8", ""}, "body", 204, "control"},
		{"extended_star_overrides_disallowed_charset", "text/plain; charset=latin-1; charset*=UTF-8''utf-8", []string{"utf-8"}, "body", 415, "extended_override"},
		{"extended_continuations_override_disallowed_charset", "text/plain; charset=latin-1; charset*0=utf-; charset*1=8", []string{"utf-8"}, "body", 415, "extended_override"},
		{"extended_star_overrides_allowed_charset", "text/plain; charset=utf-8; charset*=UTF-8''latin-1", []string{"utf-8"}, "body", 204, "extended_override"},
		{"legacy_unquoted_stray_quote_utf8", "text/plain; note=foo\"bar; charset=utf-8", []string{"UTF-8"}, "body", 204, "unquoted_stray_quote"},
		{"legacy_unquoted_stray_quote_disallowed_latin1", "text/plain; note=foo\"bar; charset=latin-1", []string{"UTF-8", ""}, "body", 415, "unquoted_stray_quote"},
	}
}

type charsetV2Row struct {
	ID     string `json:"id"`
	Status int    `json:"status"`
	Calls  int    `json:"calls"`
	Body   string `json:"body"`
}

type charsetV2Observed struct {
	Version int            `json:"version"`
	Go      string         `json:"go_version"`
	Rows    []charsetV2Row `json:"rows"`
}

func charsetV2Failures(observed charsetV2Observed) ([]string, error) {
	cases := charsetV2Cases()
	if observed.Version != 2 || observed.Go == "" || len(observed.Rows) != len(cases) {
		return nil, errors.New("charset v2 requires exact version, Go runtime, and every executed behavior")
	}
	rows := map[string]charsetV2Row{}
	for _, row := range observed.Rows {
		if _, duplicate := rows[row.ID]; duplicate {
			return nil, errors.New("charset v2 duplicated behavior")
		}
		rows[row.ID] = row
	}
	failures := []string{}
	for _, c := range cases {
		row, found := rows[c.ID]
		if !found {
			return nil, fmt.Errorf("charset v2 behavior did not execute: %s", c.ID)
		}
		calls, body := 0, ""
		if c.Expected == 204 {
			calls, body = 1, c.Body
		}
		if row.Status != c.Expected || row.Calls != calls || row.Body != body {
			failures = append(failures, c.ID)
		}
	}
	return failures, nil
}

func TestCharsetRegressionV2RequiresBothFamiliesAndExecutedEvidence(t *testing.T) {
	passed := charsetV2Observed{Version: 2, Go: runtime.Version()}
	for _, c := range charsetV2Cases() {
		row := charsetV2Row{ID: c.ID, Status: c.Expected}
		if c.Expected == 204 {
			row.Calls, row.Body = 1, c.Body
		}
		passed.Rows = append(passed.Rows, row)
	}
	if failures, err := charsetV2Failures(passed); err != nil || len(failures) != 0 {
		t.Fatal("complete matching probe was rejected", failures, err)
	}
	for i, c := range charsetV2Cases() {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			observed := passed
			observed.Rows = append([]charsetV2Row(nil), passed.Rows...)
			observed.Rows[i].Status = 619
			failures, err := charsetV2Failures(observed)
			if err != nil || len(failures) != 1 || failures[0] != c.ID {
				t.Fatal("a failed behavior escaped the versioned contract", failures, err)
			}
			observed.Rows[i] = passed.Rows[i]
			observed.Rows[i].Calls++
			if failures, err := charsetV2Failures(observed); err != nil || len(failures) != 1 {
				t.Fatal("status alone was accepted without the expected handler invocation", failures, err)
			}
			observed.Rows[i] = passed.Rows[i]
			observed.Rows[i].Body += "changed"
			if failures, err := charsetV2Failures(observed); err != nil || len(failures) != 1 {
				t.Fatal("status alone was accepted without preserving the request body", failures, err)
			}
			observed.Rows[i] = observed.Rows[(i+1)%len(observed.Rows)]
			if _, err := charsetV2Failures(observed); err == nil {
				t.Fatal("duplicate/missing behavior was accepted")
			}
		})
	}
}

// This optional entry launches only a local Go HTTP probe in fresh copies of
// supplied source trees. A failing reference is explicitly rejected as ground
// truth. Results are diagnostic evidence, never retroactive solver grades.
func TestPilotCharsetRegressionV2(t *testing.T) {
	if *charsetV2Input == "" && *charsetV2Output == "" {
		t.Skip("explicit offline source directories and new private output required; no models invoked")
	}
	var input struct {
		Baseline   string `json:"baseline"`
		Reference  string `json:"reference"`
		GoBinary   string `json:"go_binary"`
		Candidates []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"candidates"`
	}
	if err := readJSON(*charsetV2Input, &input); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(*charsetV2Output) || !filepath.IsAbs(input.Baseline) || !filepath.IsAbs(input.Reference) || input.Baseline == input.Reference {
		t.Fatal("probe requires separate absolute source directories and new absolute output")
	}
	if input.GoBinary == "" {
		var err error
		input.GoBinary, err = exec.LookPath("go")
		if err != nil {
			t.Fatal(err)
		}
	}
	goBinary := asset{Path: input.GoBinary}
	if err := sealAsset(&goBinary, "."); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(*charsetV2Output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(*charsetV2Output, "contract-v2.json"), charsetV2Cases(), true); err != nil {
		t.Fatal(err)
	}
	probe := asset{Path: filepath.Join(*charsetV2Output, "probe-v2.go.txt")}
	if err := os.WriteFile(probe.Path, []byte(charsetV2Probe), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sealAsset(&probe, "."); err != nil {
		t.Fatal(err)
	}
	type result struct {
		ID       string     `json:"id"`
		Source   string     `json:"source"`
		SHA256   string     `json:"copied_source_sha256"`
		Failures []string   `json:"failed_behaviors"`
		Run      invocation `json:"execution"`
	}
	results := []result{}
	ids := map[string]bool{}
	run := func(id, source string) {
		t.Helper()
		if !slug.MatchString(id) || ids[id] || !filepath.IsAbs(source) {
			t.Fatal("invalid or duplicate probe source", id)
		}
		ids[id] = true
		dir := filepath.Join(*charsetV2Output, id)
		work := filepath.Join(dir, "work")
		if err := copyCandidateFiles(source, work); err != nil {
			t.Fatal(err)
		}
		digest, err := snapshotHash(t.Context(), work)
		if err != nil {
			t.Fatal(err)
		}
		probeDir := filepath.Join(work, "_charset_v2_probe")
		if err := os.Mkdir(probeDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(probeDir, "main.go"), []byte(charsetV2Probe), 0o600); err != nil {
			t.Fatal(err)
		}
		casesPath := filepath.Join(probeDir, "cases.json")
		if err := writeJSON(casesPath, charsetV2Cases(), true); err != nil {
			t.Fatal(err)
		}
		home, cache, temporary := filepath.Join(dir, "home"), filepath.Join(*charsetV2Output, "go-cache"), filepath.Join(dir, "tmp")
		for _, path := range []string{home, cache, temporary} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		goBin := goBinary.Path
		invoked, err := invoke(t.Context(), work, filepath.Join(dir, "probe"), "", []string{goBin, "run", "./_charset_v2_probe", casesPath},
			[]string{"PATH=" + filepath.Dir(goBin) + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + home, "GOCACHE=" + cache, "TMPDIR=" + temporary,
				"GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOENV=off", "GOFLAGS=-mod=readonly -p=2", "CGO_ENABLED=0", "GOMAXPROCS=2"}, 120)
		if err != nil || invoked.ExitCode != 0 || invoked.Stopped || invoked.Cleanup != "" {
			t.Fatalf("offline probe failed: %v; see %s", err, invoked.Stderr.Path)
		}
		var observed charsetV2Observed
		if err := readJSON(invoked.Stdout.Path, &observed); err != nil {
			t.Fatal(err)
		}
		failures, err := charsetV2Failures(observed)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, result{ID: id, Source: source, SHA256: digest, Failures: failures, Run: invoked})
	}
	run("baseline", input.Baseline)
	run("reference", input.Reference)
	for _, candidate := range input.Candidates {
		run(candidate.ID, candidate.Path)
	}
	eligible := len(results[1].Failures) == 0
	contractHash, err := hashFile(filepath.Join(*charsetV2Output, "contract-v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(*charsetV2Output, "quality-v2.json"), map[string]any{
		"version": 2, "scope": "offline diagnostic probe; frozen acceptance, scores, manifests and prior evidence unchanged; no solver quality claim",
		"contract_sha256": contractHash, "probe": probe, "go_binary": goBinary, "results": results, "ground_truth_eligible": eligible,
		"reference_disposition": map[bool]string{true: "passes v2 contract", false: "rejected as v2 ground truth"}[eligible],
	}, true); err != nil {
		t.Fatal(err)
	}
	t.Logf("v2 reference eligible=%t; failed=%v; receipt=%s", eligible, results[1].Failures, filepath.Join(*charsetV2Output, "quality-v2.json"))
}

const charsetV2Probe = `package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	var cases []struct {
		ID, Header, Body string
		Allowed []string
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		panic(err)
	}
	type row struct {
		ID string
		Status, Calls int
		Body string
	}
	rows := []row{}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "http://example.invalid/", strings.NewReader(c.Body))
		r.Header.Set("Content-Type", c.Header)
		w := httptest.NewRecorder()
		result := row{ID: c.ID}
		handler := middleware.ContentCharset(append([]string(nil), c.Allowed...)...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result.Calls++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				panic(err)
			}
			result.Body = string(body)
			w.WriteHeader(http.StatusNoContent)
		}))
		handler.ServeHTTP(w, r)
		result.Status = w.Code
		rows = append(rows, result)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"version": 2, "go_version": runtime.Version(), "rows": rows}); err != nil {
		panic(err)
	}
}
`
