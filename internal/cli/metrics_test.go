package cli

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/outcomes"
	"github.com/belevtsev/agent-workflows/internal/workflow"
)

func metricsConfig(t *testing.T, root string) (Config, *bytes.Buffer) {
	t.Helper()
	out := new(bytes.Buffer)
	forbidden := func() (string, error) { t.Fatal("metrics resolved manager/install environment"); return "", nil }
	return Config{
		Out: out, Err: new(bytes.Buffer), WorkingDir: func() (string, error) { return root, nil },
		HomeDir: forbidden, Executable: forbidden,
		Getenv:     func(string) string { t.Fatal("metrics queried credentials or configuration"); return "" },
		NewService: func(workflow.Options) Service { t.Fatal("metrics created a manager service"); return nil },
	}, out
}

func metricsFixture(t *testing.T, root, file string) []byte {
	t.Helper()
	data := []byte(`{"schema_version":1,"observations":[
		{"observation_id":"baseline-1","case_id":"case-1","task_id":"execution-baseline-1","project":{"kind":"go","identity":"local-alias"},"variant":"baseline","workload":"command-duration","source":{"revision":"actual-head","fingerprint":"sha256:task-source"},"environment":"runtime-a","evidence":["local:observer-baseline"],"scope":"real_project_diagnostic","status":"succeeded","duration_ms":2},
		{"observation_id":"candidate-1","case_id":"case-1","task_id":"execution-candidate-1","project":{"kind":"go","identity":"local-alias"},"variant":"candidate","workload":"command-duration","source":{"revision":"actual-head","fingerprint":"sha256:task-source"},"environment":"runtime-a","evidence":["local:observer-candidate"],"scope":"real_project_diagnostic","status":"succeeded","duration_ms":1}
	]}`)
	if err := os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMetricsSchemaIsRegisteredAndHasNoEnvironmentDependencies(t *testing.T) {
	config, out := metricsConfig(t, "/unused")
	config.WorkingDir = config.HomeDir
	if err := Execute(t.Context(), []string{"metrics", "schema"}, config); err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Title    string              `json:"title"`
		Examples []outcomes.Document `json:"examples"`
	}
	if err := json.Unmarshal(out.Bytes(), &schema); err != nil || schema.Title == "" || len(schema.Examples) != 1 {
		t.Fatalf("not standalone versioned schema JSON: %s, %v", out, err)
	}
	if schema.Examples[0].SchemaVersion != outcomes.SchemaVersion {
		t.Fatal("schema/example version mismatch")
	}
}

func TestMetricsSummarizeIsReadOnlyAndUsesWorkingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "measurements with spaces")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := "pilot observations.json"
	before := metricsFixture(t, root, file)
	state := filepath.Join(t.TempDir(), "must-not-be-created")
	config, out := metricsConfig(t, root)
	if err := Execute(t.Context(), []string{"--state-dir", state, "--source", "/unread/source", "--apply", "metrics", "summarize", file}, config); err != nil {
		t.Fatal(err)
	}
	var report outcomes.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal("stdout is not one standalone JSON report", err)
	}
	if report.Status != "ok" || report.Counts.Succeeded != 2 || report.Comparisons.ComparablePairs != 1 {
		t.Fatalf("incorrect CLI observation report: %+v", report)
	}
	for _, summary := range report.Comparisons.Summaries {
		if summary.Scope != "real_project_diagnostic" || summary.Metrics["input_tokens"].Delta != nil || summary.Metrics["input_tokens"].KnownPairs != 0 {
			t.Fatal("CLI inferred productivity or usage")
		}
	}
	after, err := os.ReadFile(filepath.Join(root, file))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("metrics modified its input", err)
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("metrics created manager state", err)
	}
}

func TestMetricsExplicitRootDoesNotResolveCWD(t *testing.T) {
	root := t.TempDir()
	metricsFixture(t, root, "observations.json")
	config, out := metricsConfig(t, "/unused")
	config.WorkingDir = config.HomeDir
	if err := Execute(t.Context(), []string{"metrics", "summarize", "--input-root", root, filepath.Join(root, "observations.json")}, config); err != nil {
		t.Fatal(err)
	}
	var report outcomes.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Counts.Accepted != 2 {
		t.Fatal("explicit root did not isolate inputs", err)
	}
}

func TestMetricsInvalidSamplesEmitJSONAndNonzeroExit(t *testing.T) {
	root := t.TempDir()
	data := metricsFixture(t, root, "bad.json")
	data = bytes.Replace(data, []byte(`"duration_ms":2`), []byte(`"duration_ms":-2`), 1)
	if err := os.WriteFile(filepath.Join(root, "bad.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	config, out := metricsConfig(t, root)
	err := Execute(t.Context(), []string{"metrics", "summarize", "bad.json"}, config)
	if !errors.Is(err, outcomes.ErrInvalidInput) || ExitCode(err) != 1 {
		t.Fatal("invalid samples exited successfully", err)
	}
	var report outcomes.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Status != "invalid_input" || report.Counts.Invalid != 1 || report.Counts.Accepted != 1 {
		t.Fatal("partial JSON report unavailable", err)
	}
}

func TestMetricsCancellationAndUsageErrors(t *testing.T) {
	config, out := metricsConfig(t, "/unused")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Execute(ctx, []string{"metrics", "summarize", "--input-root", "/unused", "missing.json"}, config)
	if !errors.Is(err, context.Canceled) || ExitCode(err) != 130 {
		t.Fatal("metrics lost cancellation exit code", err)
	}
	var report outcomes.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Status != "cancelled" {
		t.Fatal("cancellation report missing", err)
	}
	for _, args := range [][]string{{"metrics", "summarize"}, {"metrics", "schema", "extra"}, {"metrics", "unknown"}} {
		config, _ := metricsConfig(t, "/unused")
		if err := Execute(t.Context(), args, config); ExitCode(err) != 2 {
			t.Fatalf("metrics usage error %v got %v", args, err)
		}
	}
}
