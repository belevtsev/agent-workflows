package cli

import (
	json "encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Transcripts for cw task usage, written by hand in the format
// internal/usagelog's fixture tests use (claude_test.go): one assistant line
// per request and, when cost is not negative, a cost-state line whose model
// totals equal the requests' sums, so the import reconciles and reports both.

type usageRequest struct{ uncached, write, read, output int64 }

// writeUsageTranscript writes session s1 with requests to path, replacing an
// earlier version as a growing session would.
func writeUsageTranscript(t *testing.T, path string, cost float64, requests ...usageRequest) {
	t.Helper()
	var lines []string
	var sum usageRequest
	for i, r := range requests {
		lines = append(lines, fmt.Sprintf(`{"type":"assistant","sessionId":"s1","requestId":"r%d","message":{"id":"m%d","model":"m-opus","usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":0}}}}`,
			i, i, r.uncached, r.write, r.read, r.output, r.write))
		sum = usageRequest{sum.uncached + r.uncached, sum.write + r.write, sum.read + r.read, sum.output + r.output}
	}
	if cost >= 0 {
		lines = append(lines, fmt.Sprintf(`{"type":"cost-state","sessionId":"s1","totalCostUSD":%v,"hasUnknownModelCost":false,"modelUsage":{"m-opus":{"inputTokens":%d,"outputTokens":%d,"cacheReadInputTokens":%d,"cacheCreationInputTokens":%d}}}`,
			cost, sum.uncached, sum.output, sum.read, sum.write))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sampleUsage runs cw task usage on transcript and decodes its summary.
func sampleUsage(t *testing.T, h *taskBundleHarness, transcript string, extra ...string) taskUsageSummary {
	t.Helper()
	h.out.Reset()
	args := append([]string{"task", "usage", "--task", h.record.Directory, "--claude-transcript", transcript}, extra...)
	if err := Execute(t.Context(), args, h.config); err != nil {
		t.Fatalf("cw task usage: %v", err)
	}
	var summary taskUsageSummary
	if err := json.Unmarshal(h.out.Bytes(), &summary); err != nil {
		t.Fatalf("summary %q: %v", h.out.String(), err)
	}
	return summary
}

func loadUsageRecord(t *testing.T, h *taskBundleHarness) taskflow.Record {
	t.Helper()
	record, err := taskflow.Load(h.record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

var firstRequest = usageRequest{uncached: 10, write: 100, read: 200, output: 5}

// Oracle: the task's usage is the second sample minus the first. Between them
// the session added two requests, (20, 0, 300, 15) and (5, 50, 350, 30), and
// its recorded cost rose from 0.40 to 1.40 USD: uncached 25, cache writes 50,
// cache reads 650, output 45, and 1.00 USD. A third sample that moves usage
// by 0.10 USD, under 5% of lite's 15 USD (0.75), is not stored; one that
// moves it by 0.80 is.
func TestTaskUsageIsTheSessionDeltaAndSkipsSmallMoves(t *testing.T) {
	h := newTaskBundleHarness(t)
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	writeUsageTranscript(t, transcript, 0.40, firstRequest)
	first := sampleUsage(t, h, transcript)
	if !first.Stored || first.Usage.USD != nil || first.Usage.UnknownReason == "" {
		t.Fatalf("first sample: stored %v, usage %+v; want stored with usage unknown", first.Stored, first.Usage)
	}
	if first.Sample.Tokens == nil || *first.Sample.Tokens != (taskflow.UsageTokens{UncachedInput: 10, CacheWrite: 100, CacheRead: 200, Output: 5}) ||
		first.Sample.USD == nil || *first.Sample.USD != 0.40 || first.Sample.DollarSource != taskflow.DollarsRecorded {
		t.Fatalf("first sample %+v, want tokens (10, 100, 200, 5) and recorded 0.40 USD", first.Sample)
	}

	requests := []usageRequest{firstRequest, {20, 0, 300, 15}, {5, 50, 350, 30}}
	writeUsageTranscript(t, transcript, 1.40, requests...)
	second := sampleUsage(t, h, transcript)
	wantTokens := taskflow.UsageTokens{UncachedInput: 25, CacheWrite: 50, CacheRead: 650, Output: 45}
	if !second.Stored || second.Usage.USD == nil || math.Abs(*second.Usage.USD-1.00) > 1e-9 || second.Usage.Tokens == nil || *second.Usage.Tokens != wantTokens || second.Usage.UnknownReason != "" {
		t.Fatalf("second sample: stored %v, usage %+v; want stored, 1.00 USD and %+v", second.Stored, second.Usage, wantTokens)
	}
	revision := loadUsageRecord(t, h).Revision

	requests = append(requests, usageRequest{1, 0, 10, 1})
	writeUsageTranscript(t, transcript, 1.50, requests...)
	if third := sampleUsage(t, h, transcript); third.Stored || math.Abs(*third.Usage.USD-1.00) > 1e-9 {
		t.Fatalf("third sample: stored %v, usage %+v; want not stored, usage still 1.00 USD", third.Stored, third.Usage)
	}
	if got := loadUsageRecord(t, h); got.Revision != revision || len(got.Usage) != 2 {
		t.Fatalf("after a small move the record has revision %d and %d samples, want %d and 2", got.Revision, len(got.Usage), revision)
	}

	writeUsageTranscript(t, transcript, 2.20, requests...)
	if fourth := sampleUsage(t, h, transcript); !fourth.Stored || math.Abs(*fourth.Usage.USD-1.80) > 1e-9 {
		t.Fatalf("fourth sample: stored %v, usage %+v; want stored, 1.80 USD", fourth.Stored, fourth.Usage)
	}
}

// Oracle: a subagent transcript that cannot be read (a directory named like
// one, as usagelog's own incomplete case) makes the import incomplete. The
// sample keeps nil tokens and dollars with the reason, and since it turns the
// task's known usage unknown it is stored; the task's usage is then nil with a
// reason, never 0.
func TestTaskUsageOfAnIncompleteTranscriptIsUnknownNeverZero(t *testing.T) {
	h := newTaskBundleHarness(t)
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	writeUsageTranscript(t, transcript, 0.40, firstRequest)
	sampleUsage(t, h, transcript)
	writeUsageTranscript(t, transcript, 1.40, firstRequest, usageRequest{20, 0, 300, 15})
	if known := sampleUsage(t, h, transcript); known.Usage.USD == nil {
		t.Fatalf("precondition: usage %+v, want known", known.Usage)
	}
	if err := os.MkdirAll(filepath.Join(filepath.Dir(transcript), "s1", "subagents", "agent-a2.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	incomplete := sampleUsage(t, h, transcript)
	if incomplete.Sample.Tokens != nil || incomplete.Sample.USD != nil || !strings.Contains(incomplete.Sample.UnknownReason, "incomplete") {
		t.Fatalf("incomplete sample %+v, want nil tokens and dollars with the reason", incomplete.Sample)
	}
	if !incomplete.Stored || incomplete.Usage.USD != nil || incomplete.Usage.Tokens != nil || !strings.Contains(incomplete.Usage.UnknownReason, "incomplete") {
		t.Fatalf("after the incomplete sample: stored %v, usage %+v; want stored and unknown with the reason", incomplete.Stored, incomplete.Usage)
	}
	stored := loadUsageRecord(t, h).Usage
	if last := stored[len(stored)-1]; last.Tokens != nil || last.USD != nil || last.UnknownReason == "" {
		t.Fatalf("stored sample %+v, want nil tokens and dollars with a reason", last)
	}
}

// Oracle: the first two samples have no recorded cost, so their dollars come
// from the supplied price table (10, 1, 0.5 and 0.2 USD per million uncached,
// cache write, cache read and output tokens), and usage is their difference;
// the third has a recorded cost. Within one session dollars from two sources
// are not subtracted: usage turns unknown, so the third is stored, and the
// reason names both sources.
func TestTaskUsageAcrossDollarSourcesIsUnknown(t *testing.T) {
	h := newTaskBundleHarness(t)
	scratch := t.TempDir()
	transcript, prices := filepath.Join(scratch, "s1.jsonl"), filepath.Join(scratch, "prices.json")
	if err := os.WriteFile(prices, []byte(`{"currency":"USD","evidence":"test rates","models":{"m-opus":{"input_per_million":10,"cache_write_5m_per_million":1,"cache_write_1h_per_million":1,"cache_read_per_million":0.5,"output_per_million":0.2}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeUsageTranscript(t, transcript, -1, firstRequest)
	first := sampleUsage(t, h, transcript, "--prices", prices)
	// 10*10 + 100*1 + 200*0.5 + 5*0.2 = 301 USD per million tokens.
	if first.Sample.USD == nil || math.Abs(*first.Sample.USD-301e-6) > 1e-12 || first.Sample.DollarSource != taskflow.DollarsPriceTable {
		t.Fatalf("first sample %+v, want 0.000301 USD from the price table", first.Sample)
	}
	writeUsageTranscript(t, transcript, -1, firstRequest, usageRequest{20, 0, 300, 15})
	// 20*10 + 300*0.5 + 15*0.2 = 353 USD per million tokens more.
	if second := sampleUsage(t, h, transcript, "--prices", prices); second.Usage.USD == nil || math.Abs(*second.Usage.USD-353e-6) > 1e-12 {
		t.Fatalf("second sample usage %+v, want 0.000353 USD", second.Usage)
	}
	writeUsageTranscript(t, transcript, 1.40, firstRequest, usageRequest{20, 0, 300, 15})
	third := sampleUsage(t, h, transcript, "--prices", prices)
	if !third.Stored || third.Sample.DollarSource != taskflow.DollarsRecorded || third.Usage.USD != nil || !strings.Contains(third.Usage.UnknownReason, "different sources (price_table, then recorded)") {
		t.Fatalf("third sample: stored %v, %+v, usage %+v; want stored with recorded dollars and usage unknown across sources", third.Stored, third.Sample, third.Usage)
	}
}
