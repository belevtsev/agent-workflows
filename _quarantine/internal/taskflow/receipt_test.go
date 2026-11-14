package taskflow

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// receiptRecord is briefRecord, verified incomplete two hours after start,
// with: a fix round t2 on finding r1/F1 and requirement limits (whose check
// vet last failed), each still open, and on r1/F2, resolved; the owner's round-budget words on t2 and
// cost-budget words on the record; two sessions priced by the built-in table;
// runs of unit (passed, then passed again) and vet (failed); and a
// verification that credits unit by reuse.
func receiptRecord() Record {
	record := briefRecord()
	at := func(minutes int) time.Time { return record.CreatedAt.Add(time.Duration(minutes) * time.Minute) }
	record.UpdatedAt = at(120)
	record.Plan.Requirements[0].CheckIDs = []string{"unit"}
	record.Plan.Requirements[1].CheckIDs = []string{"vet"}
	t2 := &record.Assignments[2]
	t2.Brief.Fixes = append(t2.Brief.Fixes, FixRef{Finding: "r1/F2"}, FixRef{Requirement: "limits"})
	t2.OwnerApprovals = []OwnerApproval{{Text: "One more tests round; keep the oracle comments.", For: ApprovalRoundBudget, At: at(30)}}
	record.OwnerApprovals = []OwnerApproval{{Text: "Go on to the full 15 dollars.", For: ApprovalCostBudget, At: at(90)}}
	first, second := budgetSample("s1", 4.5), budgetSample("s2", 8.25)
	first.Tokens = &UsageTokens{UncachedInput: 1200, CacheWrite: 3400, CacheRead: 56000, Output: 780}
	second.Tokens = &UsageTokens{UncachedInput: 10, CacheWrite: 20, CacheRead: 30, Output: 40}
	record.Usage = []UsageSample{first, second}
	record.Checks = []CheckRun{
		{ID: "c1", CheckID: "unit", Status: "passed"},
		{ID: "c2", CheckID: "vet", Status: "failed"},
		{ID: "c3", CheckID: "unit", Status: "passed"},
	}
	record.Verification = &Verification{TaskID: record.ID, SourceSHA256: strings.Repeat("e", 64),
		Problems:    []string{"latest run of vet is failed"},
		Limitations: []string{reuseLimitationPrefix("unit") + "c3, which " + reasonReused + " at commit 0123456789ab; it was not run again"}}
	return record
}

// Oracle: the exact bytes of testdata/receipt/task.txt and task.json, each at
// most MaxReceiptBytes: state incomplete; lite with tests at 2 of 2 rounds;
// 12.75 USD (4.50 + 8.25) of 15 USD with the stop raised by the cost-budget
// words, priced by one built-in table; tokens apart, summed over the two
// sessions; 2 checks planned, 3 runs, 1 failed, 1 reused; the findings by
// severity and status; 2h0m0s of wall time; the open findings r2/F1, r1/F1
// and r1/F3 newest first; one fix round on r1/F1 and on limits, both open,
// and none listed for the resolved r1/F2;
// and both approvals with their words, in time order.
func TestReceiptMatchesGolden(t *testing.T) {
	receipt := Receipt(receiptRecord())
	encoded, err := EncodeReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	text := []byte(receipt.Text())
	checkGoldenIn(t, "receipt", "task.txt", text)
	checkGoldenIn(t, "receipt", "task.json", encoded)
	if len(text) > MaxReceiptBytes || len(encoded) > MaxReceiptBytes {
		t.Fatalf("receipt is %d text bytes and %d JSON bytes; want at most %d each", len(text), len(encoded), MaxReceiptBytes)
	}
}

// zeroDollars matches a dollar amount that is 0, such as "0", "0.00" or
// "$0".
var zeroDollars = regexp.MustCompile(`(^|[^0-9.])\$?0(\.0+)?([^0-9.]|$)`)

// Oracle: D14, "unknown, never zero". With no usage sample, and with one
// session whose dollars are unknown and none known, the text says "unknown"
// with the reason, and no dollar amount on the usage line is 0; JSON usd and
// known_usd are absent or null. With 2.50 USD known beside the unknown
// session, the known part is given as "at least" that amount.
func TestReceiptShowsUnknownUsageWithItsReasonNeverZero(t *testing.T) {
	for _, c := range []struct {
		name    string
		samples []UsageSample
		reason  string
		known   string
	}{
		{"no sample", nil, "unknown (no usage sample is recorded)", ""},
		{"unpriced session", []UsageSample{unknownSample("s1")}, `unknown (claude_transcript session s1: the latest sample's dollars are unknown: the price table has no rates for model "m")`, ""},
		{"unpriced beside known", []UsageSample{budgetSample("s1", 2.5), unknownSample("s2")}, "unknown (claude_transcript session s2:", "at least an estimated 2.50 USD is known"},
	} {
		record := receiptRecord()
		record.Usage = c.samples
		receipt := Receipt(record)
		line := strings.SplitN(receipt.Text(), "\n", 5)[3]
		if !strings.HasPrefix(line, "usage: "+c.reason) || !strings.Contains(line, c.known) {
			t.Errorf("%s: usage line %q; want %q and %q", c.name, line, c.reason, c.known)
		}
		amounts := strings.ReplaceAll(line, c.reason, "")
		if zeroDollars.MatchString(amounts) {
			t.Errorf("%s: usage line %q shows a zero amount", c.name, line)
		}
		encoded, _ := EncodeReceipt(receipt)
		if !strings.Contains(string(encoded), `"usd":null`) || c.known == "" && strings.Contains(string(encoded), "known_usd") {
			t.Errorf("%s: JSON %s; want usd null and no known_usd", c.name, encoded)
		}
	}
}

// Oracle: the bound holds, and what is left out is counted. With 40 open
// findings of 200-byte summaries, 40 open fix-round keys and 12 approvals of
// 1,024 bytes, both encodings stay at most 4,096 bytes; the open findings
// come first, newest first, and each list's listed plus counted is its total,
// with the last line counting them.
func TestReceiptOfALargeTaskStaysWithinTheBound(t *testing.T) {
	record := receiptRecord()
	record.Verification = nil
	record.WorkerAcceptances = nil
	record.Assignments, record.WorkerResults = record.Assignments[:1], record.WorkerResults[:1]
	for i := 1; i <= 40; i++ {
		id := fmt.Sprintf("r%d", i)
		record.Assignments = append(record.Assignments, WorkerAssignment{Brief: WorkerBrief{ID: id, Stage: StageTestReview}})
		record.WorkerResults = append(record.WorkerResults, BoundWorkerResult{Result: WorkerResult{AssignmentID: id, Status: "completed",
			Findings: []WorkerFinding{{ID: "F1", Severity: "major", Summary: findingText(i, 300)}}}})
	}
	for i := 1; i <= 40; i++ {
		fix := WorkerAssignment{Brief: WorkerBrief{ID: fmt.Sprintf("t%d", i+10), Stage: StageTests, Fixes: []FixRef{{Finding: fmt.Sprintf("r%d/F1", i)}}}}
		if i <= 12 {
			fix.OwnerApprovals = []OwnerApproval{{Text: strings.Repeat("w", 1024), For: ApprovalEscalation, At: record.CreatedAt}}
		}
		record.Assignments = append(record.Assignments, fix)
	}
	receipt := Receipt(record)
	encoded, err := EncodeReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	text := receipt.Text()
	if len(text) > MaxReceiptBytes || len(encoded) > MaxReceiptBytes {
		t.Fatalf("receipt is %d text bytes and %d JSON bytes; want at most %d each", len(text), len(encoded), MaxReceiptBytes)
	}
	omitted := receipt.Omitted
	for _, c := range []struct {
		what            string
		listed, counted int
		total           int
	}{
		{"open findings", len(receipt.OpenFindings), omitted.OpenFindings, 40},
		{"fix rounds", len(receipt.FixRounds), omitted.FixRounds, 40},
		{"approvals", len(receipt.Approvals), omitted.Approvals, 13},
	} {
		if c.listed == 0 || c.counted == 0 || c.listed+c.counted != c.total {
			t.Errorf("%s: %d listed and %d counted; want both above 0 and %d in all", c.what, c.listed, c.counted, c.total)
		}
	}
	for i, finding := range receipt.OpenFindings {
		if want := fmt.Sprintf("r%d/F1", 40-i); finding.ID != want {
			t.Fatalf("open finding %d is %s; want %s (newest first)", i, finding.ID, want)
		}
	}
	want := fmt.Sprintf("not listed: %d older open findings, %d fix-round keys, %d owner approvals;", omitted.OpenFindings, omitted.FixRounds, omitted.Approvals)
	if !strings.Contains(text, want) {
		t.Fatalf("text lacks %q:\n%s", want, text)
	}
}
