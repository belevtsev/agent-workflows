package cli

import (
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
)

// Transcripts for cw task usage, written by hand in the shape Claude Code
// writes (internal/usagelog's claude_test.go): one assistant line per request
// of claude-opus-5-5, its cache writes split as 5m writes, and, when cost is
// not negative, a cost-state line, which Claude Code writes when a session
// exits. Dollars are estimates from the built-in table dated 2026-11-13,
// claude-opus-5-5 at 4 / 5 / 8 / 0.20 / 20 USD per million input, 5m write,
// 1h write, cache read and output tokens; the cost-state never changes them.

type usageRequest struct{ uncached, write, read, output int64 }

// opusUSD is the built-in estimate of requests, by hand from the table row.
func opusUSD(requests ...usageRequest) float64 {
	total := 0.0
	for _, r := range requests {
		total += (float64(r.uncached)*4 + float64(r.write)*5 + float64(r.read)*0.20 + float64(r.output)*20) / 1e6
	}
	return total
}

func usageLine(i int, model string, r usageRequest) string {
	return fmt.Sprintf(`{"type":"assistant","sessionId":"s1","requestId":"r%d","message":{"id":"m%d","model":%q,"usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":0},"speed":"standard","inference_geo":"global"}}}`,
		i, i, model, r.uncached, r.write, r.read, r.output, r.write)
}

// writeUsageTranscript writes session s1 with requests to path, replacing an
// earlier version as a growing session would.
func writeUsageTranscript(t *testing.T, path string, cost float64, requests ...usageRequest) {
	t.Helper()
	writeModelTranscript(t, path, "claude-opus-5-5", cost, requests...)
}

func writeModelTranscript(t *testing.T, path, model string, cost float64, requests ...usageRequest) {
	t.Helper()
	var lines []string
	for i, r := range requests {
		lines = append(lines, usageLine(i, model, r))
	}
	if cost >= 0 {
		lines = append(lines, fmt.Sprintf(`{"type":"cost-state","sessionId":"s1","totalCostUSD":%v,"hasUnknownModelCost":false,"modelUsage":{}}`, cost))
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

func usdNear(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-9 }

var firstRequest = usageRequest{uncached: 10, write: 100, read: 200, output: 5}

// Oracle: the task's usage is the second sample minus the first. Between them
// the session added (20, 0, 300, 15) and (5, 50, 350, 30): uncached 25, cache
// writes 50, cache reads 650, output 45, and opusUSD of the two, estimated
// from the built-in table. 1,000 more output tokens (0.02 USD, under 5% of
// lite's 15 USD) are not stored; 50,000 more (1.00 USD) are.
func TestTaskUsageIsTheSessionDeltaAndSkipsSmallMoves(t *testing.T) {
	h := newTaskBundleHarness(t)
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	writeUsageTranscript(t, transcript, -1, firstRequest)
	first := sampleUsage(t, h, transcript)
	if !first.Stored || first.Usage.USD != nil || first.Usage.UnknownReason == "" {
		t.Fatalf("first sample: stored %v, usage %+v; want stored with usage unknown", first.Stored, first.Usage)
	}
	if first.Sample.Tokens == nil || *first.Sample.Tokens != (taskflow.UsageTokens{UncachedInput: 10, CacheWrite: 100, CacheRead: 200, Output: 5}) || !usdNear(first.Sample.USD, opusUSD(firstRequest)) ||
		!first.Sample.USDEstimated || first.Sample.DollarSource == nil || first.Sample.DollarSource.Kind != taskflow.PriceTableBuiltIn || first.Sample.DollarSource.Date != "2026-11-13" {
		t.Fatalf("first sample %+v, want tokens (10, 100, 200, 5) and %v USD estimated from the built-in table of 2026-11-13", first.Sample, opusUSD(firstRequest))
	}

	added := []usageRequest{{20, 0, 300, 15}, {5, 50, 350, 30}}
	requests := append([]usageRequest{firstRequest}, added...)
	writeUsageTranscript(t, transcript, -1, requests...)
	second := sampleUsage(t, h, transcript)
	wantTokens := taskflow.UsageTokens{UncachedInput: 25, CacheWrite: 50, CacheRead: 650, Output: 45}
	if !second.Stored || !usdNear(second.Usage.USD, opusUSD(added...)) || !second.Usage.USDEstimated || second.Usage.Tokens == nil || *second.Usage.Tokens != wantTokens {
		t.Fatalf("second sample: stored %v, usage %+v; want stored, %v USD estimated and %+v", second.Stored, second.Usage, opusUSD(added...), wantTokens)
	}
	revision := loadUsageRecord(t, h).Revision

	requests = append(requests, usageRequest{output: 1000})
	writeUsageTranscript(t, transcript, -1, requests...)
	if third := sampleUsage(t, h, transcript); third.Stored || !usdNear(third.Usage.USD, opusUSD(added...)) {
		t.Fatalf("third sample: stored %v, usage %+v; want not stored, usage unchanged", third.Stored, third.Usage)
	}
	if got := loadUsageRecord(t, h); got.Revision != revision || len(got.Usage) != 2 {
		t.Fatalf("after a small move the record has revision %d and %d samples, want %d and 2", got.Revision, len(got.Usage), revision)
	}

	requests = append(requests, usageRequest{output: 50000})
	writeUsageTranscript(t, transcript, -1, requests...)
	if fourth := sampleUsage(t, h, transcript); !fourth.Stored || !usdNear(fourth.Usage.USD, opusUSD(requests[1:]...)) {
		t.Fatalf("fourth sample: stored %v, usage %+v; want stored, %v USD", fourth.Stored, fourth.Usage, opusUSD(requests[1:]...))
	}
}

// Oracle: a subagent transcript that cannot be read (a directory named like
// one, as usagelog's own incomplete case) makes the import incomplete. The
// sample keeps nil tokens and dollars with the reason, and since it turns the
// session's known dollars unknown it is stored; the task's usage is then nil
// with a reason, never 0.
func TestTaskUsageOfAnIncompleteTranscriptIsUnknownNeverZero(t *testing.T) {
	h := newTaskBundleHarness(t)
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	writeUsageTranscript(t, transcript, -1, firstRequest)
	sampleUsage(t, h, transcript)
	writeUsageTranscript(t, transcript, -1, firstRequest, usageRequest{20, 0, 300, 15})
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

// overridePrices replaces the built-in claude-opus-5-5 row with 10, 1, 1, 0.5
// and 0.2 USD per million input, 5m write, 1h write, cache read and output.
const overridePrices = `{"currency":"USD","date":"2026-01-01","evidence":"test rates","models":{"claude-opus-5-5":{"input_per_million":10,"cache_write_5m_per_million":1,"cache_write_1h_per_million":1,"cache_read_per_million":0.5,"output_per_million":0.2}}}`

// Oracle: --prices replaces the built-in table, so a sample priced by it
// takes the file's row: firstRequest and (20, 0, 300, 15) cost
// (10*10 + 100*1 + 200*0.5 + 5*0.2) + (20*10 + 300*0.5 + 15*0.2) = 654 USD per
// million tokens, and its dollar source is the file, its date and the SHA-256
// of its bytes. Within one session dollars from two tables are not
// subtracted, whether the built-in one and a file or two files with other
// bytes: usage turns unknown, the sample is stored, and the reason names both
// sources.
func TestTaskUsageAcrossDollarSourcesIsUnknown(t *testing.T) {
	digest := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	_, builtIn, err := usagelog.BuiltInPrices()
	if err != nil {
		t.Fatal(err)
	}
	otherPrices := strings.Replace(overridePrices, "test rates", "other evidence", 1)
	for _, c := range []struct {
		name, first, third, want string
	}{
		{"built-in then a file", "", overridePrices, "(built-in price table 2026-11-13 (sha256 " + digest(builtIn)[:12] + "), then price table file 2026-01-01 (sha256 " + digest([]byte(overridePrices))[:12] + "))"},
		{"two files", otherPrices, overridePrices, "(price table file 2026-01-01 (sha256 " + digest([]byte(otherPrices))[:12] + "), then price table file 2026-01-01 (sha256 " + digest([]byte(overridePrices))[:12] + "))"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newTaskBundleHarness(t)
			scratch := t.TempDir()
			transcript := filepath.Join(scratch, "s1.jsonl")
			flags := func(table string) []string {
				if table == "" {
					return nil
				}
				path := filepath.Join(scratch, digest([]byte(table))+".json")
				if err := os.WriteFile(path, []byte(table), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"--prices", path}
			}
			writeUsageTranscript(t, transcript, -1, firstRequest)
			sampleUsage(t, h, transcript, flags(c.first)...)
			writeUsageTranscript(t, transcript, -1, firstRequest, usageRequest{20, 0, 300, 15})
			if second := sampleUsage(t, h, transcript, flags(c.first)...); second.Usage.USD == nil {
				t.Fatalf("precondition: usage %+v, want known", second.Usage)
			}
			got := sampleUsage(t, h, transcript, flags(c.third)...)
			if source := got.Sample.DollarSource; !usdNear(got.Sample.USD, 654e-6) || source == nil || source.Kind != taskflow.PriceTableFile || source.Date != "2026-01-01" || source.SHA256 != digest([]byte(c.third)) {
				t.Fatalf("third sample %+v (source %+v), want 654e-6 USD from the file's row", got.Sample, got.Sample.DollarSource)
			}
			if !got.Stored || got.Usage.USD != nil || !strings.Contains(got.Usage.UnknownReason, "different sources "+c.want) {
				t.Fatalf("third sample: stored %v, usage %+v; want stored and usage unknown with %q", got.Stored, got.Usage, c.want)
			}
		})
	}
}

// Oracle: a session's samples estimate 1, then 13, then 4 USD (50,000,
// 650,000 and 200,000 output tokens at 20 USD per million), the last from a
// stale, truncated copy of the transcript that lacks its latest request. The
// drop against the sample before it, though 4 is above the first sample's 1,
// is stored and makes the usage unknown, never 3 USD.
func TestTaskUsageOfAStaleTranscriptCopyIsUnknown(t *testing.T) {
	h := newTaskBundleHarness(t)
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	requests := []usageRequest{{output: 50000}, {output: 150000}, {output: 450000}}
	writeUsageTranscript(t, transcript, -1, requests[0])
	sampleUsage(t, h, transcript)
	writeUsageTranscript(t, transcript, -1, requests...)
	if second := sampleUsage(t, h, transcript); !usdNear(second.Sample.USD, 13) || !usdNear(second.Usage.USD, 12) {
		t.Fatalf("precondition: sample %v USD, usage %+v; want 13 and 12 USD", second.Sample.USD, second.Usage)
	}
	writeUsageTranscript(t, transcript, -1, requests[:2]...)
	stale := sampleUsage(t, h, transcript)
	if !stale.Stored || !usdNear(stale.Sample.USD, 4) || stale.Usage.USD != nil || stale.Usage.Tokens != nil || !strings.Contains(stale.Usage.UnknownReason, "fell between samples") {
		t.Fatalf("stale copy: stored %v, sample %v USD, usage %+v; want stored, 4 USD, and usage unknown because the dollars fell", stale.Stored, stale.Sample.USD, stale.Usage)
	}
}

// Claude Code writes a cost-state line only when a session exits, and a
// resumed session logs more requests after it. Oracle: dollars are estimated
// from the requests both before any cost-state and after a stale one (here
// 0.9 USD, matching nothing), so usage is the added requests' opusUSD.
func TestTaskUsageOfALiveClaudeSessionIsEstimated(t *testing.T) {
	h := newTaskBundleHarness(t)
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	writeUsageTranscript(t, transcript, -1, firstRequest)
	if first := sampleUsage(t, h, transcript); !usdNear(first.Sample.USD, opusUSD(firstRequest)) {
		t.Fatalf("before any cost-state: sample %+v, want %v USD", first.Sample, opusUSD(firstRequest))
	}
	writeUsageTranscript(t, transcript, 0.9, firstRequest, usageRequest{20, 0, 300, 15})
	data, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	resumed := usageLine(9, "claude-opus-5-5", usageRequest{7, 0, 900, 40}) + "\n"
	if err := os.WriteFile(transcript, append(data, resumed...), 0o600); err != nil {
		t.Fatal(err)
	}
	want := opusUSD(usageRequest{20, 0, 300, 15}, usageRequest{7, 0, 900, 40})
	if last := sampleUsage(t, h, transcript); !usdNear(last.Sample.USD, opusUSD(firstRequest)+want) || !usdNear(last.Usage.USD, want) {
		t.Fatalf("after the cost-state and a resume: sample %v, usage %+v; want usage %v USD", last.Sample.USD, last.Usage, want)
	}
}

// Oracle: a model the table does not list leaves the dollars unknown with the
// reason, while the tokens stay known; and while a session's dollars are
// unknown its samples are judged by tokens: 40 more of 650 (6%) is stored,
// 1 more is not.
func TestTaskUsageOfAnUnknownModelStoresTokenGrowth(t *testing.T) {
	h := newTaskBundleHarness(t)
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	requests := []usageRequest{firstRequest, {20, 0, 300, 15}}
	writeModelTranscript(t, transcript, "claude-mystery-1", -1, requests[0])
	first := sampleUsage(t, h, transcript)
	if first.Sample.USD != nil || first.Sample.Tokens == nil || !strings.Contains(first.Sample.UnknownReason, `no rates for model "claude-mystery-1"`) {
		t.Fatalf("first sample %+v, want known tokens and unknown dollars naming the model", first.Sample)
	}
	writeModelTranscript(t, transcript, "claude-mystery-1", -1, requests...)
	sampleUsage(t, h, transcript)
	for _, step := range []struct {
		add    usageRequest
		stored bool
	}{{usageRequest{output: 40}, true}, {usageRequest{output: 1}, false}} {
		requests = append(requests, step.add)
		writeModelTranscript(t, transcript, "claude-mystery-1", -1, requests...)
		if got := sampleUsage(t, h, transcript); got.Stored != step.stored || got.Usage.USD != nil {
			t.Fatalf("adding %+v: stored %v, usage %+v; want stored %v and unknown dollars", step.add, got.Stored, got.Usage, step.stored)
		}
	}
}
