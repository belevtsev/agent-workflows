package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

// These commands own their prerequisite checks, service readiness and cleanup.
// They exercise an HTTP application and its persisted format, using no paid
// hosts and no Kitsu-owned service lifecycle or unconditional success command.
func TestTaskVerificationApplicationCommand(t *testing.T) {
	if os.Getenv("KITSU_TEST_APPLICATION_COMMAND") != "1" {
		return
	}
	if data, err := os.ReadFile("prerequisite.ready"); err != nil || string(data) != "ready\n" {
		t.Fatal("application prerequisite unavailable")
	}
	data, err := os.ReadFile("app/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Defect string `json:"defect"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if os.Args[len(os.Args)-1] == "full" {
		applicationSavedFormat(t)
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(request.Value) == "" && config.Defect != "accept-empty" {
			http.Error(w, "value required", http.StatusBadRequest)
			return
		}
		version := 1
		if config.Defect == "response-version" {
			version = 2
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"version": version, "value": request.Value})
	}))
	t.Cleanup(server.Close)
	for _, value := range []string{"", " ", "\t"} {
		response, err := server.Client().Post(server.URL, "application/json", strings.NewReader(fmt.Sprintf("{\"value\":%q}", value)))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("empty value accepted: HTTP %d", response.StatusCode)
		}
		fmt.Printf("HTTP rejects empty value %q with 400\n", value)
	}
	for i := range 24 {
		value := fmt.Sprintf("record-%02d-λ", i)
		response, err := server.Client().Post(server.URL, "application/json", strings.NewReader(fmt.Sprintf("{\"value\":%q}", value)))
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Version int    `json:"version"`
			Value   string `json:"value"`
		}
		err = json.NewDecoder(response.Body).Decode(&saved)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != http.StatusCreated || saved.Version != 1 || saved.Value != value {
			t.Fatalf("response violates v1 saved-format contract: status=%d body=%+v err=%v", response.StatusCode, saved, err)
		}
		fmt.Printf("HTTP creates %s with compatible version 1\n", value)
	}
}

func applicationSavedFormat(t *testing.T) {
	t.Helper()
	f, err := os.Open("data/saved.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var saved struct {
		Version int    `json:"version"`
		ID      string `json:"id"`
		Value   string `json:"value"`
	}
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != 1 || saved.ID != "existing-record" || saved.Value != "preserved" {
		t.Fatalf("saved format incompatible: %+v", saved)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("saved format has trailing content: %v", err)
	}
	fmt.Println("saved version 1 preserves existing record identity and value")
}

func applicationFixture(t *testing.T, defect string) *kitsutest.Repo {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Setenv("KITSU_TEST_APPLICATION_COMMAND", "1")
	r.Setenv("GORACE", "atexit_sleep_ms=0")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := []string{filepath.ToSlash(program), "-test.run=^TestTaskVerificationApplicationCommand$", "-test.count=1", "-test.timeout=15s", "--"}
	declaration := "version=1\n[[check]]\nid=\"http-contract\"\nrun=" + decl.QuoteList(append(slices.Clone(command), "api")) + "\npaths=[\"app/**\",\"prerequisite.ready\"]\nwhen=[\"app/**\"]\nexpect=\"reject empty values and preserve HTTP response version 1\"\nverify=true\n[[check]]\nid=\"saved-format\"\nrun=" + decl.QuoteList(append(slices.Clone(command), "full")) + "\npaths=[\"app/**\",\"data/**\",\"prerequisite.ready\"]\n"
	r.Write("kitsu.toml", declaration)
	r.Write("app/config.json", `{"defect":"","revision":0}`)
	r.Write("prerequisite.ready", "ready\n")
	r.Write("data/saved.json", `{"version":1,"id":"existing-record","value":"preserved"}`)
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "application contract baseline")
	r.Write("app/config.json", fmt.Sprintf("{\"defect\":%q,\"revision\":1}", defect))
	return r
}

func applicationReport(t *testing.T, result kitsutest.Result, code int) taskVerificationReport {
	t.Helper()
	if result.Code != code {
		t.Fatalf("exit=%d want=%d: %s", result.Code, code, result.Output())
	}
	var report taskVerificationReport
	if err := json.Unmarshal([]byte(result.Stdout), &report); err != nil {
		t.Fatalf("report: %v: %s", err, result.Output())
	}
	if result.Stderr != "" {
		t.Fatalf("unexpected diagnostics: %s", result.Stderr)
	}
	return report
}

func applicationRecords(t *testing.T, r *kitsutest.Repo) []store.Record {
	t.Helper()
	records, damaged, err := store.Open(filepath.Dir(r.StoreDir())).AllRecords()
	if err != nil || len(damaged) != 0 {
		t.Fatalf("records unreadable: %v, %+v", err, damaged)
	}
	return records
}

func applicationVerify(r *kitsutest.Repo, extra ...string) kitsutest.Result {
	args := []string{"task", "verify", "application", "--workflow", "small", "--json"}
	return r.Kitsu(append(args, extra...)...)
}

func TestTaskVerificationApplicationRequiresBase(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	result := applicationVerify(r)
	if result.Code == 0 || !strings.Contains(result.Output(), "first verification requires --from") || len(applicationRecords(t, r)) != 0 || r.Exists("openspec/changes/.verification/application") {
		t.Fatalf("first run inferred a base or ran commands: %+v", result)
	}
}

func TestTaskVerificationApplicationCalibration(t *testing.T) {
	for _, tc := range []struct {
		name, defect, prerequisite, saved string
		failed                            bool
		runs                              int
	}{
		{name: "correct control", runs: 2},
		{name: "empty input defect", defect: "accept-empty", failed: true, runs: 1},
		{name: "response compatibility defect", defect: "response-version", failed: true, runs: 1},
		{name: "missing prerequisite", prerequisite: "missing", failed: true, runs: 1},
		{name: "full suite saved format defect", saved: `{"version":2,"id":"existing-record","value":"preserved"}`, failed: true, runs: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := applicationFixture(t, tc.defect)
			if tc.prerequisite == "missing" {
				r.Remove("prerequisite.ready")
			}
			if tc.saved != "" {
				r.Write("data/saved.json", tc.saved)
			}
			code := 0
			if tc.failed {
				code = ExitNotVerified
			}
			report := applicationReport(t, applicationVerify(r, "--from", "HEAD"), code)
			records := applicationRecords(t, r)
			if report.Executed != tc.runs || len(records) != tc.runs || report.ChecksVerified == tc.failed || !slices.Equal(report.Verification.Required, []string{"http-contract"}) {
				t.Fatalf("wrong calibration facts: report=%+v records=%+v", report, records)
			}
			if tc.failed && tc.runs == 1 && slices.ContainsFunc(records, func(r store.Record) bool { return r.ID == "saved-format" }) {
				t.Fatal("targeted failure ran full suite")
			}
			if tc.failed && tc.runs == 1 && report.Verification.Ready {
				t.Fatal("failed application contract became ready")
			}
			if tc.saved != "" && (report.ChecksVerified || report.Checks[1].State != store.Failed) {
				t.Fatalf("full suite failure became green: %+v", report)
			}
			if tc.failed {
				want := "empty value accepted"
				switch {
				case tc.defect == "response-version":
					want = "response violates v1 saved-format contract"
				case tc.prerequisite != "":
					want = "application prerequisite unavailable"
				case tc.saved != "":
					want = "saved format incompatible"
				}
				failed := 0
				for _, record := range records {
					if record.Outcome == store.Failed {
						failed++
						if !strings.Contains(r.Read(".git/kitsu/"+record.Log), want) {
							t.Fatalf("record did not capture expected application defect %q", want)
						}
					}
				}
				if failed != 1 {
					t.Fatalf("expected one observed application failure, got %d", failed)
				}
			}
			if tc.failed {
				r.Write("app/config.json", `{"defect":"","revision":2}`)
				r.Write("prerequisite.ready", "ready\n")
				r.Write("data/saved.json", `{"version":1,"id":"existing-record","value":"preserved"}`)
				report = applicationReport(t, applicationVerify(r), 0)
				correctedRuns := 2
				if tc.saved != "" {
					correctedRuns = 3
				} // A changed prior local observation forces the final required gate.
				if !report.Verification.Ready || !report.ChecksVerified || report.Executed != correctedRuns || len(applicationRecords(t, r)) != tc.runs+correctedRuns {
					t.Fatalf("corrected control did not pass: %+v", report)
				}
			}
			history, err := verification.Load(r.Dir, "application")
			if err != nil {
				t.Fatal(err)
			}
			before := len(applicationRecords(t, r))
			repeatedResult := applicationVerify(r)
			repeated := applicationReport(t, repeatedResult, 0)
			after, err := verification.Load(r.Dir, "application")
			if err != nil || history.ID != after.ID || repeated.Executed != 0 || repeated.Reused != 2 || len(applicationRecords(t, r)) != before || !repeated.Verification.Ready {
				t.Fatalf("unchanged success reran/rewrote history: %+v %v", repeated, err)
			}
			t.Logf("injected_failure=%t corrected_or_original_control=true recorded_runs=%d reused_checks=%d visible_output_bytes=%d", tc.failed, before, repeated.Reused, len(repeatedResult.Stdout))
		})
	}
}

func TestTaskVerificationApplicationRejectsEditedGateLogs(t *testing.T) {
	for _, id := range []string{"http-contract", "saved-format"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			r := applicationFixture(t, "")
			result := r.Kitsu("task", "verify", "application", "--workflow", "small", "--from", "HEAD", "--compact")
			var page contextpack.Page
			if result.Code != 0 || json.Unmarshal([]byte(result.Stdout), &page) != nil {
				t.Fatalf("initial verification failed: %s", result.Output())
			}
			for _, record := range applicationRecords(t, r) {
				if record.ID == id {
					r.Write(".git/kitsu/"+record.Log, "edited gate evidence\n")
				}
			}
			expanded := r.Kitsu("context", "show", page.ID, "--item", id)
			if expanded.Code == 0 || !strings.Contains(expanded.Output(), "stale") {
				t.Fatalf("changed check log expanded under stale digest: %s", expanded.Output())
			}
			fresh := applicationReport(t, applicationVerify(r), 0)
			for _, check := range fresh.Checks {
				data := r.Read(".git/kitsu/logs/" + check.RunID + ".log")
				if check.LogSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(data))) {
					t.Fatalf("fresh JSON report retained stale log digest: %+v", check)
				}
			}
			human := r.Kitsu("task", "verify", "application", "--workflow", "small")
			if human.Code != 0 || !strings.Contains(human.Stdout, "verification passed") || !strings.Contains(human.Stdout, "Recorded run attempts: 0") {
				t.Fatalf("human view did not reobserve: %s", human.Output())
			}
		})
	}
}

func TestTaskVerificationApplicationCompactEquivalence(t *testing.T) {
	t.Parallel()
	r := applicationFixture(t, "")
	firstResult := applicationVerify(r, "--from", "HEAD")
	first := applicationReport(t, firstResult, 0)
	plainResult := applicationVerify(r)
	plain := applicationReport(t, plainResult, 0)
	compactResult := r.Kitsu("task", "verify", "application", "--workflow", "small", "--compact", "--max-bytes", "4096")
	if compactResult.Code != 0 || len(compactResult.Stdout) > 4096 {
		t.Fatalf("compact failed or oversized: %s", compactResult.Output())
	}
	var page contextpack.Page
	if err := json.Unmarshal([]byte(compactResult.Stdout), &page); err != nil {
		t.Fatal(err)
	}
	var compact taskVerificationReport
	if err := json.Unmarshal(page.Metadata, &compact); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plain, compact) || !compact.Verification.Ready || !compact.ChecksVerified || compact.Executed != 0 || len(applicationRecords(t, r)) != 2 {
		t.Fatalf("JSON/compact facts differ: JSON=%+v compact=%+v", plain, compact)
	}
	show := r.Kitsu("task", "show", "application", "--json")
	var view struct {
		Verification *taskVerificationView `json:"verification"`
	}
	if show.Code != 0 || json.Unmarshal([]byte(show.Stdout), &view) != nil || !reflect.DeepEqual(view.Verification, compact.Verification) {
		t.Fatalf("public task view disagrees: %s", show.Output())
	}
	var recovered strings.Builder
	cursor := ""
	for range 30 {
		args := []string{"context", "show", page.ID, "--item", "http-contract", "--max-bytes", "4096"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		result := r.Kitsu(args...)
		var expanded contextpack.Page
		if result.Code != 0 || json.Unmarshal([]byte(result.Stdout), &expanded) != nil || len(expanded.Items) != 1 {
			t.Fatalf("context log expansion failed: %s", result.Output())
		}
		for _, line := range expanded.Items[0].Lines {
			recovered.WriteString(line.Text)
		}
		cursor = expanded.Items[0].Next
		if cursor == "" {
			break
		}
	}
	var log string
	for _, record := range applicationRecords(t, r) {
		if record.ID == "http-contract" {
			log = r.Read(".git/kitsu/" + record.Log)
		}
	}
	if cursor != "" || recovered.String() != log || !strings.Contains(log, "HTTP rejects empty value") || !strings.Contains(log, "HTTP creates record-23-λ") {
		t.Fatal("expanded log lost application assertion evidence")
	}
	t.Logf("successful_control_runs=%d required_checks=%d reused_checks=%d json_output_bytes=%d compact_output_bytes=%d initial_output_bytes=%d", len(applicationRecords(t, r)), len(first.Verification.Required), plain.Reused, len(plainResult.Stdout), len(compactResult.Stdout), len(firstResult.Stdout))
}
