package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

func adviserTokenFixture(t *testing.T, root, task, workflow, outcome string) *advisers.Consultation {
	t.Helper()
	cfg := &advisers.Config{
		Advisers: map[string]advisers.Definition{
			"wise": {Model: "requested-test-model", Command: []string{"unused-command", "{model}"}, Timeout: time.Minute},
		},
		Access: map[string][]string{"default": {"wise"}},
	}
	if _, err := advisers.Bind(root, task, workflow, cfg, false); err != nil {
		t.Fatal(err)
	}
	pending, err := advisers.Begin(root, advisers.Consultation{
		Task: task, Role: "lookup", Stage: "lookup", Adviser: "wise",
		Request: advisers.Request{Version: 1, Kind: "advice", Question: "Which approach?"}, Prompt: "An explicit question.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != "pending" {
		finished := *pending
		finished.Execution = advisers.Execution{
			Outcome: outcome, ActualModel: "unknown", StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC(),
		}
		if outcome == "success" {
			finished.Execution.Answer = "An adviser answer."
		}
		if err := advisers.Finish(root, finished); err != nil {
			t.Fatal(err)
		}
	}
	return pending
}

func adviserTokenJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var out, diagnostic bytes.Buffer
	cfg := Config{In: strings.NewReader(""), Out: &out, Err: &diagnostic}
	if err := runTokens(context.Background(), cfg, args); err != nil {
		t.Fatalf("tokens %v: %v\n%s", args, err, diagnostic.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("tokens JSON: %v\n%s", err, out.String())
	}
	return result
}

func TestAdviserTokenActivityLoadsSmallFullAndArchivedWithoutDoubleCounting(t *testing.T) {
	root := t.TempDir()
	full := adviserTokenFixture(t, root, "full-task", "full", "success")
	adviserTokenFixture(t, root, "small-task", "small", "failed")
	adviserTokenFixture(t, root, "archived-task", "full", "pending")
	archive := filepath.Join(root, "openspec", "changes", "archive")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "openspec", "changes", "archived-task"), filepath.Join(archive, "archived-task")); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(filepath.Join(archive, "full-task"), os.DirFS(filepath.Join(root, "openspec", "changes", "full-task"))); err != nil {
		t.Fatal(err)
	}
	items, err := loadAdviserTokenActivity(root)
	if err != nil || len(items) != 3 {
		t.Fatalf("adviser activity = %+v, %v", items, err)
	}
	if items[0].Task != "archived-task" || items[0].Outcome != "pending" || items[1].ID != full.ID || items[2].Outcome != "failed" {
		t.Fatalf("adviser attempts lost their original attribution: %+v", items)
	}
	for _, item := range items {
		if item.RequestedModel != "requested-test-model" {
			t.Fatalf("requested model was inferred from runtime output: %+v", item)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "kitsu-advisers.toml")); !os.IsNotExist(err) {
		t.Fatal("fixture unexpectedly has a configuration file")
	}
	for range 2 {
		again, err := loadAdviserTokenActivity(root)
		if err != nil || !reflect.DeepEqual(items, again) {
			t.Fatalf("reading activity changed or duplicated records: %+v, %v", again, err)
		}
	}
}

func TestAdviserTokenActivitySurvivesSmallPromotion(t *testing.T) {
	root := t.TempDir()
	pending := adviserTokenFixture(t, root, "promoted-task", "small", "pending")
	before, err := loadAdviserTokenActivity(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &advisers.Config{
		Advisers: map[string]advisers.Definition{
			"wise": {Model: "requested-test-model", Command: []string{"unused-command", "{model}"}, Timeout: time.Minute},
		},
		Access: map[string][]string{"default": {"wise"}},
	}
	if _, err := advisers.Bind(root, "promoted-task", "full", cfg, true); err != nil {
		t.Fatal(err)
	}
	after, err := loadAdviserTokenActivity(root)
	if err != nil || len(after) != 1 || after[0].ID != pending.ID || !reflect.DeepEqual(before, after) {
		t.Fatalf("promotion duplicated or lost adviser activity: %+v, %v", after, err)
	}
}

func TestAdviserTokenReportsKeepMeasuredCountsAndModels(t *testing.T) {
	report := tokens.Build([]tokens.Start{
		{Session: "s", Agent: tokens.SessionAgent, Known: true, Models: map[string]tokens.Counts{"host-model": {Input: 2}}},
		{Session: "s", Agent: "role", Task: "host-task", Stage: "code", Round: 1, Known: true,
			Models: map[string]tokens.Counts{"host-model": {Input: 10, Output: 5}}},
	}, nil)
	activity := attachAdviserTokenActivity(report, []adviserTokenItem{
		{ID: "one", Task: "host-task", Adviser: "wise", RequestedModel: "adviser-model", Outcome: "success"},
		{ID: "two", Task: "advice-only", Adviser: "wise", RequestedModel: "adviser-model", Outcome: "pending"},
	})
	task, _, _, exists := report.Find("host-task")
	if !exists || task.Complete || task.Total != 17 || task.Starts != 1 || len(task.Roles) != 1 || task.Roles[0].Models[0] != "host-model" {
		t.Fatalf("adviser usage changed measured host tokens: %+v", task)
	}
	adviceOnly, _, _, exists := report.Find("advice-only")
	if !exists || adviceOnly.Complete || adviceOnly.Starts != 0 || activity.all.Calls != 2 || activity.all.Complete || activity.all.Usage != "unknown" {
		t.Fatalf("advice-only activity was omitted or guessed: %+v, %+v", adviceOnly, activity)
	}
	data, err := json.Marshal(adviserGlobalTokenJSON(report, activity))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	usage := document["advisers"].(map[string]any)
	if usage["calls"] != float64(2) || usage["usage"] != "unknown" || usage["complete"] != false {
		t.Fatalf("global usage contract = %s", data)
	}
}

func TestAdviserTokensLeaveUnconfiguredJSONShapeUnchanged(t *testing.T) {
	report := tokens.Build(nil, nil)
	want, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	activity := attachAdviserTokenActivity(report, nil)
	got, err := json.Marshal(adviserGlobalTokenJSON(report, activity))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("unconfigured token JSON changed: %s, %v; want %s", got, err, want)
	}
}

func TestAdviserTokensAdviceOnlyCLIReportsUnknownUsage(t *testing.T) {
	_, root := guardRepo(t, "")
	t.Chdir(root)
	adviserTokenFixture(t, root, "advice-only", "small", "success")
	task := adviserTokenJSON(t, "--task", "advice-only", "--json")
	usage := task["advisers"].(map[string]any)
	if task["complete"] != false || usage["calls"] != float64(1) || usage["usage"] != "unknown" {
		t.Fatalf("task adviser usage = %+v", task)
	}
	global := adviserTokenJSON(t, "--json")
	if global["advisers"].(map[string]any)["calls"] != float64(1) || len(global["tasks"].([]any)) != 1 {
		t.Fatalf("global adviser usage = %+v", global)
	}
	var output bytes.Buffer
	if err := runTokens(context.Background(), Config{Out: &output, Err: &output}, []string{"--task", "advice-only"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "token usage unknown") || strings.Contains(output.String(), "0 tokens") {
		t.Fatalf("advice-only tokens were represented as zero: %s", output.String())
	}
}

func TestAdviserTokensRejectDamagedRecordsInsteadOfOmittingCalls(t *testing.T) {
	root := t.TempDir()
	pending := adviserTokenFixture(t, root, "damaged-task", "small", "pending")
	history := filepath.Join(root, "openspec", "changes", ".advice", "damaged-task")
	file := filepath.Join(history, "exchanges", pending.ID, "request.json")
	if err := os.WriteFile(file, []byte(`{"version":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAdviserTokenActivity(root); err == nil {
		t.Fatal("a damaged adviser call silently disappeared from the report")
	}
	if err := os.Remove(filepath.Join(history, "binding.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAdviserTokenActivity(root); err == nil {
		t.Fatal("an orphaned adviser history silently disappeared from the report")
	}
}

type adviserNoticeMustNotRead struct{}

func (adviserNoticeMustNotRead) Read([]byte) (int, error) {
	panic("an adviser hook must not read or attribute a host notice")
}

func TestAdviserTokenRecordHookSkipsRecursiveHostNotices(t *testing.T) {
	t.Setenv("KITSU_ADVISER_CALL", "1")
	if err := runTokensRecord(context.Background(), Config{In: adviserNoticeMustNotRead{}}, []string{"--invalid"}); err != nil {
		t.Fatalf("recursive adviser hook should do nothing: %v", err)
	}
}
