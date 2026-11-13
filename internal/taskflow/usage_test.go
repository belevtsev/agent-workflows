package taskflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

func usageTestSample(usd float64) taskflow.UsageSample {
	return sessionUsageSample("s1", usd)
}

func sessionUsageSample(session string, usd float64) taskflow.UsageSample {
	source := &taskflow.DollarSource{Kind: taskflow.PriceTableBuiltIn, Date: "2026-11-13", SHA256: strings.Repeat("ab", 32)}
	return taskflow.UsageSample{Source: taskflow.UsageSourceClaude, SessionID: session, Tokens: &taskflow.UsageTokens{Output: 1}, USD: &usd, USDEstimated: true, DollarSource: source}
}

// Oracle: each sample is judged against its own session under lite's 15 USD
// budget (5% is 0.75 USD). Session A reads 0, 3, 8 and 12 USD, every step at
// least 0.75, so all four are stored though session B, with one sample, keeps
// the task's usage unknown between them; B's second sample (0.50) is stored
// because it gives B's usage. Usage is A's 12 plus B's 0.50. Rows with a
// session that reads lower than its previous stored sample (1, 13, then 4)
// store the drop and leave usage unknown, never 3 USD.
func TestUsageSamplesAreJudgedPerSession(t *testing.T) {
	for _, c := range []struct {
		name    string
		samples []taskflow.UsageSample
		want    *float64
	}{
		{"growth while another session is unknown", []taskflow.UsageSample{sessionUsageSample("A", 0), sessionUsageSample("A", 3), sessionUsageSample("B", 0),
			sessionUsageSample("A", 8), sessionUsageSample("A", 12), sessionUsageSample("B", 0.5)}, new(12.5)},
		{"a drop below the previous sample", []taskflow.UsageSample{sessionUsageSample("A", 1), sessionUsageSample("A", 13), sessionUsageSample("A", 4)}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			repo, store := spRepo(t, map[string]string{"a.txt": "a\n"})
			record, err := taskflow.StartWithPacks(ctx, repo, store, spPlan("go", "version"), nil, taskflow.SelectionDefault, nil)
			if err != nil {
				t.Fatal(err)
			}
			var result taskflow.UsageResult
			for i, sample := range c.samples {
				if result, err = (taskflow.Env{}).RecordUsage(ctx, record.Directory, sample); err != nil || !result.Stored {
					t.Fatalf("sample %d (%s at %v): stored %v, %v; want stored", i, sample.SessionID, *sample.USD, result.Stored, err)
				}
			}
			usage := taskflow.UsageOf(result.Record)
			switch {
			case c.want == nil && (usage.USD != nil || usage.UnknownReason == ""):
				t.Fatalf("usage %+v, want unknown with a reason", usage)
			case c.want != nil && (usage.USD == nil || *usage.USD != *c.want):
				t.Fatalf("usage %+v, want %v USD", usage, *c.want)
			}
		})
	}
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
	first := filepath.Join(record.Directory, "record-000001.json")
	withoutPlanProfile(t, first)
	rewriteRevision(t, first, "profile", nil)
	if _, err := (taskflow.Env{}).RecordUsage(ctx, record.Directory, usageTestSample(1)); !errors.Is(err, taskflow.ErrInvalidInput) {
		t.Fatalf("RecordUsage on a record without a profile = %v, want ErrInvalidInput", err)
	}
	if loaded, err := taskflow.Load(record.Directory); err != nil || loaded.Revision != 1 {
		t.Fatalf("after the refusal: revision %d, %v; want revision 1", loaded.Revision, err)
	}
}
