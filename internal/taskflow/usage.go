package taskflow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
)

// Usage sample sources, as the transcript import names them, and the two
// places a sample's dollars may come from. taskflow does not import the
// import package (internal/usagelog); internal/cli maps its report.
const (
	UsageSourceClaude = "claude_transcript"
	UsageSourceCodex  = "codex_rollout"
	// DollarsRecorded is the cost the transcript itself recorded.
	DollarsRecorded = "recorded"
	// DollarsPriceTable is an estimate from a price table the user supplied.
	DollarsPriceTable = "price_table"
)

// usageStoreShare is the share of the budget a sample must move the task's
// usage by to be stored, so frequent sampling does not multiply revisions.
const usageStoreShare = 0.05

// UsageTokens counts a session's tokens with cached and uncached input apart.
type UsageTokens struct {
	UncachedInput int64 `json:"uncached_input"`
	CacheWrite    int64 `json:"cache_write"`
	CacheRead     int64 `json:"cache_read"`
	Output        int64 `json:"output"`
}

// UsageSample is one reading of a session's cumulative usage. Tokens and USD
// are nil when unknown, never 0, and UnknownReason then says why. A sample
// with USD names where its dollars came from in DollarSource.
type UsageSample struct {
	At            time.Time    `json:"at"`
	Source        string       `json:"source"`
	SessionID     string       `json:"session_id"`
	Tokens        *UsageTokens `json:"tokens"`
	USD           *float64     `json:"usd"`
	DollarSource  string       `json:"dollar_source,omitempty"`
	UnknownReason string       `json:"unknown_reason,omitempty"`
}

// TaskUsage is what a task has used since its first sample of each session:
// per session, the latest sample minus the first, summed. A value is nil,
// with UnknownReason, when any session cannot give it.
type TaskUsage struct {
	USD           *float64     `json:"usd"`
	Tokens        *UsageTokens `json:"tokens"`
	UnknownReason string       `json:"unknown_reason,omitempty"`
}

// errUsageUnchanged ends RecordUsage's update without writing a revision.
var errUsageUnchanged = errors.New("usage sample not stored")

// UsageResult is RecordUsage's answer: the record after the call, the sample
// as stamped, and whether a revision stored it.
type UsageResult struct {
	Record Record
	Sample UsageSample
	Stored bool
}

// RecordUsage stores sample, stamped with the Env's clock, in the task at path
// when it matters: the first sample of its session, one that turns the task's
// dollar usage known or unknown, or one that moves it by at least 5% of the
// budget. A task without a profile has no budget, and its usage is refused.
func (env Env) RecordUsage(ctx context.Context, path string, sample UsageSample) (UsageResult, error) {
	sample.At = env.now()
	if err := validateUsageSample(sample); err != nil {
		return UsageResult{}, asInvalid(err)
	}
	var unchanged Record
	record, err := update(ctx, env, path, func(_ *os.Root, record *Record) error {
		if record.Profile == nil {
			return invalid("the task records no profile, so it has no budget to sample usage against; start a new task")
		}
		if !usageWorthStoring(*record, sample) {
			unchanged = *record
			return errUsageUnchanged
		}
		record.Usage = append(record.Usage, sample)
		return nil
	})
	switch {
	case errors.Is(err, errUsageUnchanged):
		return UsageResult{Record: unchanged, Sample: sample}, nil
	case err != nil:
		return UsageResult{}, err
	}
	return UsageResult{Record: record, Sample: sample, Stored: true}, nil
}

// usageWorthStoring reports whether record should keep sample.
func usageWorthStoring(record Record, sample UsageSample) bool {
	firstOfSession := true
	for _, stored := range record.Usage {
		if stored.Source == sample.Source && stored.SessionID == sample.SessionID {
			firstOfSession = false
		}
	}
	if firstOfSession {
		return true
	}
	before := UsageOf(record)
	next := record
	next.Usage = append(append([]UsageSample(nil), record.Usage...), sample)
	after := UsageOf(next)
	if (before.USD == nil) != (after.USD == nil) {
		return true
	}
	return before.USD != nil && math.Abs(*after.USD-*before.USD) >= usageStoreShare*record.Profile.Budget.MaxUSD
}

// UsageOf returns the task's usage from its stored samples.
func UsageOf(record Record) TaskUsage {
	if len(record.Usage) == 0 {
		return TaskUsage{UnknownReason: "no usage sample is recorded"}
	}
	type key struct{ source, session string }
	var order []key
	first, last, count := map[key]UsageSample{}, map[key]UsageSample{}, map[key]int{}
	for _, sample := range record.Usage {
		k := key{sample.Source, sample.SessionID}
		if count[k] == 0 {
			order = append(order, k)
			first[k] = sample
		}
		last[k] = sample
		count[k]++
	}
	usd, tokens := 0.0, UsageTokens{}
	var usdReason, tokensReason string
	for _, k := range order {
		a, b := first[k], last[k]
		session := fmt.Sprintf("%s session %s", k.source, k.session)
		if count[k] == 1 {
			reason := session + " has one usage sample; its usage is known only from a later sample"
			usdReason, tokensReason = firstOf(usdReason, reason), firstOf(tokensReason, reason)
			continue
		}
		switch {
		case a.USD == nil:
			usdReason = firstOf(usdReason, session+": the first sample's dollars are unknown: "+a.UnknownReason)
		case b.USD == nil:
			usdReason = firstOf(usdReason, session+": the latest sample's dollars are unknown: "+b.UnknownReason)
		case a.DollarSource != b.DollarSource:
			usdReason = firstOf(usdReason, fmt.Sprintf("%s: the samples' dollars come from different sources (%s, then %s)", session, a.DollarSource, b.DollarSource))
		case *b.USD < *a.USD:
			usdReason = firstOf(usdReason, session+": the dollars fell between samples")
		default:
			usd += *b.USD - *a.USD
		}
		switch {
		case a.Tokens == nil || b.Tokens == nil:
			tokensReason = firstOf(tokensReason, session+": a sample's tokens are unknown")
		case b.Tokens.UncachedInput < a.Tokens.UncachedInput || b.Tokens.CacheWrite < a.Tokens.CacheWrite || b.Tokens.CacheRead < a.Tokens.CacheRead || b.Tokens.Output < a.Tokens.Output:
			tokensReason = firstOf(tokensReason, session+": the token counts fell between samples")
		default:
			tokens.UncachedInput += b.Tokens.UncachedInput - a.Tokens.UncachedInput
			tokens.CacheWrite += b.Tokens.CacheWrite - a.Tokens.CacheWrite
			tokens.CacheRead += b.Tokens.CacheRead - a.Tokens.CacheRead
			tokens.Output += b.Tokens.Output - a.Tokens.Output
		}
	}
	var result TaskUsage
	if usdReason == "" {
		result.USD = &usd
	}
	if tokensReason == "" {
		result.Tokens = &tokens
	}
	result.UnknownReason = firstOf(usdReason, tokensReason)
	return result
}

func firstOf(current, next string) string {
	if current != "" {
		return current
	}
	return next
}

// validateUsageSample checks one sample's shape: a known source and session,
// non-negative counts, a finite non-negative amount with its source, and a
// reason exactly when a value is unknown.
func validateUsageSample(sample UsageSample) error {
	switch {
	case sample.At.IsZero():
		return errors.New("a usage sample needs a time")
	case sample.Source != UsageSourceClaude && sample.Source != UsageSourceCodex:
		return fmt.Errorf("usage sample source %q is not %s or %s", sample.Source, UsageSourceClaude, UsageSourceCodex)
	case !nonblank(sample.SessionID) || len(sample.SessionID) > 256:
		return errors.New("a usage sample needs a session ID of 1 to 256 bytes")
	}
	if tokens := sample.Tokens; tokens != nil && (tokens.UncachedInput < 0 || tokens.CacheWrite < 0 || tokens.CacheRead < 0 || tokens.Output < 0) {
		return errors.New("usage sample token counts must not be negative")
	}
	if sample.USD != nil {
		if math.IsNaN(*sample.USD) || math.IsInf(*sample.USD, 0) || *sample.USD < 0 {
			return errors.New("usage sample dollars must be a finite amount of at least 0")
		}
		if sample.DollarSource != DollarsRecorded && sample.DollarSource != DollarsPriceTable {
			return fmt.Errorf("usage sample dollar source %q is not %s or %s", sample.DollarSource, DollarsRecorded, DollarsPriceTable)
		}
	} else if sample.DollarSource != "" {
		return errors.New("a usage sample without dollars names no dollar source")
	}
	if unknown := sample.USD == nil || sample.Tokens == nil; unknown != (sample.UnknownReason != "") || sample.UnknownReason != "" && !boundedText(sample.UnknownReason) {
		return errors.New("a usage sample needs a reason of 1 to 16384 bytes exactly when its tokens or dollars are unknown")
	}
	return nil
}

// validateUsage checks the stored samples. Only a task with a profile has a
// budget to sample against.
func validateUsage(record Record) error {
	if len(record.Usage) > 0 && record.Profile == nil {
		return errors.New("invalid stored usage: the task has no profile")
	}
	if len(record.Usage) > maxTaskRevisions {
		return errors.New("invalid stored usage: too many samples")
	}
	for _, sample := range record.Usage {
		if err := validateUsageSample(sample); err != nil {
			return fmt.Errorf("invalid stored usage: %w", err)
		}
	}
	return nil
}

// validateUsageTransition keeps the samples append-only: a revision keeps
// every earlier sample unchanged and adds at most one.
func validateUsageTransition(previous, current Record) error {
	if len(current.Usage) < len(previous.Usage) || len(current.Usage) > len(previous.Usage)+1 {
		return errors.New("stored usage samples changed")
	}
	for i, sample := range previous.Usage {
		if !sameEncoded(sample, current.Usage[i]) {
			return errors.New("stored usage samples changed")
		}
	}
	return nil
}
