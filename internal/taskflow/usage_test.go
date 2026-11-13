package taskflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

func usageTestSample(usd float64) taskflow.UsageSample {
	return taskflow.UsageSample{Source: taskflow.UsageSourceClaude, SessionID: "s1", Tokens: &taskflow.UsageTokens{Output: 1}, USD: &usd, DollarSource: taskflow.DollarsRecorded}
}

// Oracle: stored usage samples are append-only. Load refuses a revision that
// rewrites an earlier sample with exactly "stored usage samples changed"; the
// control row re-encodes the samples unchanged and must load.
func TestLaterRevisionThatRewritesAUsageSampleFailsToLoad(t *testing.T) {
	for _, rewrite := range []bool{false, true} {
		ctx := context.Background()
		repo, store := spRepo(t, map[string]string{"a.txt": "a\n"})
		record, err := taskflow.StartWithPacks(ctx, repo, store, spPlan("go", "version"), nil, taskflow.SelectionDefault, nil)
		if err != nil {
			t.Fatal(err)
		}
		var result taskflow.UsageResult
		for _, usd := range []float64{1, 5} {
			if result, err = (taskflow.Env{}).RecordUsage(ctx, record.Directory, usageTestSample(usd)); err != nil || !result.Stored {
				t.Fatalf("precondition: sample %v stored %v: %v", usd, result.Stored, err)
			}
		}
		samples := result.Record.Usage
		if rewrite {
			lower := 0.5
			samples[0].USD = &lower
		}
		value, _ := json.Marshal(samples)
		rewriteRevision(t, filepath.Join(record.Directory, "record-000003.json"), "usage", value)
		_, err = taskflow.Load(record.Directory)
		switch {
		case !rewrite && err != nil:
			t.Fatalf("control: re-encoding the samples unchanged gives %v", err)
		case rewrite && (err == nil || err.Error() != "stored usage samples changed"):
			t.Fatalf("Load after rewriting the first sample = %v, want exactly %q", err, "stored usage samples changed")
		}
	}
}

// Oracle: a record started before profiles has no budget, so its usage is
// refused as invalid input and no revision is written.
func TestUsageOfATaskWithoutAProfileIsRefused(t *testing.T) {
	ctx := context.Background()
	repo, store := spRepo(t, map[string]string{"a.txt": "a\n"})
	record, err := taskflow.StartWithPacks(ctx, repo, store, spPlan("go", "version"), nil, taskflow.SelectionDefault, nil)
	if err != nil {
		t.Fatal(err)
	}
	rewriteRevision(t, filepath.Join(record.Directory, "record-000001.json"), "profile", nil)
	if _, err := (taskflow.Env{}).RecordUsage(ctx, record.Directory, usageTestSample(1)); !errors.Is(err, taskflow.ErrInvalidInput) {
		t.Fatalf("RecordUsage on a record without a profile = %v, want ErrInvalidInput", err)
	}
	if loaded, err := taskflow.Load(record.Directory); err != nil || loaded.Revision != 1 {
		t.Fatalf("after the refusal: revision %d, %v; want revision 1", loaded.Revision, err)
	}
}
