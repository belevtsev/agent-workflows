package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

func TestAdviserProviderCatalogIsOfflineAndNeedsNoRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	for _, args := range [][]string{{"advisers", "providers"}, {"advisers", "providers", "--json"}} {
		output.Reset()
		if err := Execute(t.Context(), args, Config{Out: &output, Err: &output}); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"typesafe", "subq", "systemone", "jev-1.14.0", "subq-snap-1.2", "boolean"} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("missing %s: %s", want, output.String())
			}
		}
	}
}

type nativeCLIFixture struct {
	t             *testing.T
	root          string
	client        *http.Client
	configuration string
	calls         atomic.Int32
	mutate        atomic.Bool
	status        atomic.Int32
}

func newNativeCLIFixture(t *testing.T) *nativeCLIFixture {
	t.Helper()
	_, root := guardRepo(t, "")
	t.Chdir(root)
	t.Setenv("KITSU_TEST_NATIVE_KEY", "private-test-native-api-key")
	f := &nativeCLIFixture{t: t, root: root}
	f.status.Store(200)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if f.mutate.Load() {
			if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("changed during call"), 0o600); err != nil {
				t.Error(err)
			}
		}
		if f.status.Load() != 200 {
			w.WriteHeader(int(f.status.Load()))
			_, _ = io.WriteString(w, "private-test-native-api-key")
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if !strings.Contains(string(data), "explicit source") || strings.Contains(string(data), "private-test-native-api-key") {
			t.Errorf("incorrect provider input: %s", data)
		}
		_, _ = io.WriteString(w, `{"model":"resolved-snap","answers":{"result":{"type":"noul","noul":0.25}},"usage":{"input_tokens":23,"output_tokens":0}}`)
	}))
	t.Cleanup(server.Close)
	f.client = server.Client()
	f.configuration = fmt.Sprintf("version = 2\n[advisers.snap]\nprovider = 'subq'\nbase_url = %q\napi_key_env = 'KITSU_TEST_NATIVE_KEY'\n[access]\ncoordinator = ['snap']\n[[checkpoints]]\nid = 'review-advice'\nstage = 'review'\nrole = 'coordinator'\nadvisers = ['snap']\n", server.URL+"/v1")
	f.write(advisers.FileName, f.configuration)
	f.write("source.txt", "explicit source")
	f.run("", true, "advisers", "bind", "--task", "native-task", "--workflow", "small")
	if f.calls.Load() != 0 {
		t.Fatal("binding contacted provider")
	}
	return f
}

func (f *nativeCLIFixture) write(name, data string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, name), []byte(data), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *nativeCLIFixture) run(input string, success bool, args ...string) string {
	f.t.Helper()
	var output bytes.Buffer
	err := Execute(context.Background(), args, Config{In: strings.NewReader(input), Out: &output, Err: &output,
		NativeExecute: func(ctx context.Context, call *advisers.NativeCall) advisers.Execution {
			return call.Execute(ctx, f.client)
		},
	})
	if (err == nil) != success {
		f.t.Fatalf("%v: %v; %s", args, err, output.String())
	}
	return output.String()
}

func (f *nativeCLIFixture) consult(success bool) advisers.Consultation {
	f.t.Helper()
	out := f.run(`{"version":2,"kind":"boolean","question":"Ready?","context":["source.txt"]}`, success,
		"consult", "--task", "native-task", "--role", "coordinator", "--stage", "review", "--checkpoint", "review-advice", "--adviser", "snap", "--request", "-", "--json")
	var c advisers.Consultation
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		f.t.Fatalf("consultation: %s, %v", out, err)
	}
	return c
}

func TestNativeConsultationWorkflowAndOfflineHistory(t *testing.T) {
	f := newNativeCLIFixture(t)
	for _, args := range [][]string{{"advisers", "list", "--json"}, {"advisers", "list", "--task", "native-task", "--json"}} {
		if out := f.run("", true, args...); !strings.Contains(out, `"boolean"`) || !strings.Contains(out, `"provider": "subq"`) {
			t.Fatalf("missing capabilities: %s", out)
		}
	}
	c := f.consult(true)
	if c.Version != 2 || c.Execution.Version != 2 || c.Execution.Result == nil || *c.Execution.Result.ProbabilityTrue != 0.25 || c.Execution.ActualModel != "resolved-snap" || f.calls.Load() != 1 {
		t.Fatalf("consultation: %+v", c)
	}
	f.run("", false, "advisers", "check", "--task", "native-task")
	f.run("", true, "advisers", "resolve", "--task", "native-task", c.ID, "--disposition", "defer", "--reason", "Needs independent assessment")
	f.run("", true, "advisers", "check", "--task", "native-task")
	out := f.run("", true, "tokens", "--task", "native-task", "--json")
	var report struct {
		Advisers adviserTokenUsage `json:"advisers"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Advisers.Complete || report.Advisers.InputTokens != 23 || report.Advisers.OutputTokens != 0 {
		t.Fatalf("usage: %s", out)
	}
	// Offline validation needs neither configuration nor credentials.
	if err := os.Remove(filepath.Join(f.root, advisers.FileName)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KITSU_TEST_NATIVE_KEY", "")
	f.run("", true, "advisers", "list", "--task", "native-task", "--json")
	status, err := advisers.Evaluate(f.root, "native-task", c.Content, "", "")
	if err != nil || !status.Ready {
		t.Fatalf("offline evaluation: %+v, %v", status, err)
	}
	f.run("", true, "change", "check", "native-task")
	// Removing repository policy changes its code content; restore it for
	// the public freshness check, still without credentials.
	f.write(advisers.FileName, f.configuration)
	f.run("", true, "advisers", "check", "--task", "native-task")
	f.write("source.txt", "modified context")
	f.run("", false, "advisers", "check", "--task", "native-task")
	if f.calls.Load() != 1 {
		t.Fatal("offline commands contacted provider")
	}
}

func TestNativePreflightRejectsBeforeRecording(t *testing.T) {
	f := newNativeCLIFixture(t)
	args := []string{"consult", "--task", "native-task", "--role", "coordinator", "--stage", "code", "--adviser", "snap", "--request", "-"}
	f.run(`{"version":1,"kind":"advice","question":"Why?"}`, false, args...)
	t.Setenv("KITSU_TEST_NATIVE_KEY", "")
	f.run(`{"version":2,"kind":"boolean","question":"Ready?"}`, false, args...)
	t.Setenv("KITSU_TEST_NATIVE_KEY", "private-test-native-api-key")
	f.write(advisers.FileName, strings.Replace(f.configuration, "provider = 'subq'", "provider = 'subq'\nmodel = 'new-model'", 1))
	f.run(`{"version":2,"kind":"boolean","question":"Ready?"}`, false, args...)
	items, err := advisers.ReadConsultations(f.root, "native-task")
	if err != nil || len(items) != 0 || f.calls.Load() != 0 {
		t.Fatalf("preflight created activity: %+v, %v", items, err)
	}
	f.write(advisers.FileName, f.configuration)
	f.consult(true)
}

func TestNativeFailedAndStaleCallsKeepHistory(t *testing.T) {
	f := newNativeCLIFixture(t)
	f.status.Store(429)
	failed := f.consult(false)
	if failed.Execution.Outcome != "failed" || strings.Contains(failed.Execution.Detail, "private-test-native-api-key") {
		t.Fatalf("failure: %+v", failed)
	}
	f.status.Store(200)
	f.mutate.Store(true)
	stale := f.consult(false)
	if stale.Execution.Outcome != "failed" || stale.Execution.Result == nil || !stale.Execution.UsageKnown {
		t.Fatalf("stale response lost usage or answer: %+v", stale)
	}
	f.run("", true, "advisers", "resolve", "--task", "native-task", stale.ID, "--disposition", "defer", "--reason", "Content changed")
	f.run("", false, "advisers", "check", "--task", "native-task")
	if f.calls.Load() != 2 {
		t.Fatal("automatic retry occurred")
	}
}

func TestNativeUsageMixedWithCLIStaysSeparateFromHost(t *testing.T) {
	report := tokens.Build([]tokens.Start{{Session: "s", Agent: "developer", Task: "mixed", Known: true, Models: map[string]tokens.Counts{"host": {Input: 100}}}}, nil)
	activity := attachAdviserTokenActivity(report, []adviserTokenItem{
		{Task: "mixed", Adviser: "snap", Tokens: &advisers.Usage{InputTokens: 23}},
		{Task: "mixed", Adviser: "cli"},
	})
	if activity.all.Usage != "partial" || activity.all.Complete || activity.all.InputTokens != 23 || activity.all.MeasuredCalls != 1 {
		t.Fatalf("usage: %+v", activity.all)
	}
	task, _, _, ok := report.Find("mixed")
	if !ok || task.Total != 100 || task.Complete {
		t.Fatalf("host totals changed: %+v", task)
	}
}
