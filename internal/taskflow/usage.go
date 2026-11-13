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
// kinds of price table a sample's estimated dollars may come from. taskflow
// does not import the import package (internal/usagelog); internal/cli maps
// its report.
const (
	UsageSourceClaude = "claude_transcript"
	UsageSourceCodex  = "codex_rollout"
	// PriceTableBuiltIn is the dated table cw ships.
	PriceTableBuiltIn = "built_in"
	// PriceTableFile is a table the user supplied with --prices.
	PriceTableFile = "file"
)

// DollarSource names the price table that priced a sample's tokens: its kind,
// its date when it states one, and the SHA-256 of its bytes. Two samples have
// the same dollar source only when all three agree.
type DollarSource struct {
	Kind   string `json:"kind"`
	Date   string `json:"date,omitempty"`
	SHA256 string `json:"sha256"`
}

// label names the source in a reason.
func (d *DollarSource) label() string {
	if d == nil {
		return "none"
	}
	label := "built-in price table"
	if d.Kind == PriceTableFile {
		label = "price table file"
	}
	if d.Date != "" {
		label += " " + d.Date
	}
	return label + " (sha256 " + d.SHA256[:12] + ")"
}

// usageStoreShare is how far a sample must move its session's dollars, as a
// share of the budget, or, while they are unknown, its tokens, as a share of
// the session's last stored count, to be stored, so frequent sampling does
// not multiply revisions.
const usageStoreShare = 0.05

// UsageTokens counts a session's tokens with cached and uncached input apart.
type UsageTokens struct {
	UncachedInput int64 `json:"uncached_input"`
	CacheWrite    int64 `json:"cache_write"`
	CacheRead     int64 `json:"cache_read"`
	Output        int64 `json:"output"`
}

func (t UsageTokens) total() int64 { return t.UncachedInput + t.CacheWrite + t.CacheRead + t.Output }

// fellFrom reports whether any count of t is below earlier's.
func (t UsageTokens) fellFrom(earlier UsageTokens) bool {
	return t.UncachedInput < earlier.UncachedInput || t.CacheWrite < earlier.CacheWrite || t.CacheRead < earlier.CacheRead || t.Output < earlier.Output
}

// UsageSample is one reading of a session's cumulative usage. Tokens and USD
// are nil when unknown, never 0, and UnknownReason then says why. USD is an
// estimate, priced from the transcript's tokens by the price table that
// DollarSource names; USDEstimated says so on every sample with USD.
type UsageSample struct {
	At            time.Time     `json:"at"`
	Source        string        `json:"source"`
	SessionID     string        `json:"session_id"`
	Tokens        *UsageTokens  `json:"tokens"`
	USD           *float64      `json:"usd"`
	USDEstimated  bool          `json:"usd_estimated,omitempty"`
	DollarSource  *DollarSource `json:"dollar_source,omitempty"`
	UnknownReason string        `json:"unknown_reason,omitempty"`
}

// sameDollarSource reports whether a and b were priced by the same table.
func sameDollarSource(a, b UsageSample) bool {
	return a.DollarSource != nil && b.DollarSource != nil && *a.DollarSource == *b.DollarSource
}

// TaskUsage is what a task has used since its first sample of each session:
// per session, the latest sample minus the first, summed. A value is nil,
// with UnknownReason, when any session cannot give it; known dollars are
// always an estimate (USDEstimated).
type TaskUsage struct {
	USD           *float64     `json:"usd"`
	USDEstimated  bool         `json:"usd_estimated,omitempty"`
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
// when it matters to its session (usageWorthStoring). A task without a profile
// has no budget, and its usage is refused.
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

// usageWorthStoring judges sample against its own session's stored samples,
// never the task's total, so one session's unknown usage cannot hide another
// session's growth. It stores the session's first two samples, which give
// its usage; then a sample whose dollars turn known or unknown, change
// source or fall, or rise by at least 5% of the budget; and, while the
// session's dollars are unknown, one whose tokens turn known or unknown,
// fall, or grow by at least 5%.
func usageWorthStoring(record Record, sample UsageSample) bool {
	var stored []UsageSample
	for _, earlier := range record.Usage {
		if earlier.Source == sample.Source && earlier.SessionID == sample.SessionID {
			stored = append(stored, earlier)
		}
	}
	if len(stored) < 2 {
		return true
	}
	last := stored[len(stored)-1]
	switch {
	case (last.USD == nil) != (sample.USD == nil):
		return true
	case sample.USD == nil:
		return tokensMoved(last.Tokens, sample.Tokens)
	case !sameDollarSource(last, sample) || *sample.USD < *last.USD:
		return true
	}
	return *sample.USD-*last.USD >= usageStoreShare*record.Profile.Budget.MaxUSD
}

// tokensMoved reports whether next differs from last enough to store.
func tokensMoved(last, next *UsageTokens) bool {
	switch {
	case (last == nil) != (next == nil):
		return true
	case last == nil:
		return false
	case next.fellFrom(*last):
		return true
	}
	growth := next.total() - last.total()
	return growth > 0 && float64(growth) >= usageStoreShare*float64(last.total())
}

// UsageOf returns the task's usage from its stored samples. Within a session
// every consecutive pair of known amounts must keep one dollar source and
// never fall; a stale or truncated transcript copy that reads lower than the
// sample before it makes the session's usage unknown.
func UsageOf(record Record) TaskUsage {
	if len(record.Usage) == 0 {
		return TaskUsage{UnknownReason: "no usage sample is recorded"}
	}
	type key struct{ source, session string }
	var order []key
	sessions := map[key][]UsageSample{}
	for _, sample := range record.Usage {
		k := key{sample.Source, sample.SessionID}
		if len(sessions[k]) == 0 {
			order = append(order, k)
		}
		sessions[k] = append(sessions[k], sample)
	}
	usd, tokens := 0.0, UsageTokens{}
	var usdReason, tokensReason string
	for _, k := range order {
		samples := sessions[k]
		session := fmt.Sprintf("%s session %s", k.source, k.session)
		if len(samples) == 1 {
			reason := session + " has one usage sample; its usage is known only from a later sample"
			usdReason, tokensReason = firstOf(usdReason, reason), firstOf(tokensReason, reason)
			continue
		}
		if delta, reason := sessionDollars(session, samples); reason != "" {
			usdReason = firstOf(usdReason, reason)
		} else {
			usd += delta
		}
		if delta, reason := sessionTokens(session, samples); reason != "" {
			tokensReason = firstOf(tokensReason, reason)
		} else {
			tokens.UncachedInput += delta.UncachedInput
			tokens.CacheWrite += delta.CacheWrite
			tokens.CacheRead += delta.CacheRead
			tokens.Output += delta.Output
		}
	}
	var result TaskUsage
	if usdReason == "" {
		result.USD, result.USDEstimated = &usd, true
	}
	if tokensReason == "" {
		result.Tokens = &tokens
	}
	result.UnknownReason = firstOf(usdReason, tokensReason)
	return result
}

// sessionDollars is one session's latest dollars minus its first, or the
// reason they are unknown.
func sessionDollars(session string, samples []UsageSample) (float64, string) {
	first, last := samples[0], samples[len(samples)-1]
	switch {
	case first.USD == nil:
		return 0, session + ": the first sample's dollars are unknown: " + first.UnknownReason
	case last.USD == nil:
		return 0, session + ": the latest sample's dollars are unknown: " + last.UnknownReason
	}
	previous := first
	for _, sample := range samples[1:] {
		if sample.USD == nil {
			continue
		}
		if !sameDollarSource(previous, sample) {
			return 0, fmt.Sprintf("%s: the samples' dollars come from different sources (%s, then %s)", session, previous.DollarSource.label(), sample.DollarSource.label())
		}
		if *sample.USD < *previous.USD {
			return 0, session + ": the dollars fell between samples, so a transcript was stale or truncated"
		}
		previous = sample
	}
	return *last.USD - *first.USD, ""
}

// sessionTokens is one session's latest tokens minus its first, or the reason
// they are unknown.
func sessionTokens(session string, samples []UsageSample) (UsageTokens, string) {
	first, last := samples[0], samples[len(samples)-1]
	if first.Tokens == nil || last.Tokens == nil {
		return UsageTokens{}, session + ": a sample's tokens are unknown"
	}
	previous := *first.Tokens
	for _, sample := range samples[1:] {
		if sample.Tokens == nil {
			continue
		}
		if sample.Tokens.fellFrom(previous) {
			return UsageTokens{}, session + ": the token counts fell between samples, so a transcript was stale or truncated"
		}
		previous = *sample.Tokens
	}
	return UsageTokens{UncachedInput: last.Tokens.UncachedInput - first.Tokens.UncachedInput, CacheWrite: last.Tokens.CacheWrite - first.Tokens.CacheWrite,
		CacheRead: last.Tokens.CacheRead - first.Tokens.CacheRead, Output: last.Tokens.Output - first.Tokens.Output}, ""
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
		source := sample.DollarSource
		if !sample.USDEstimated || source == nil || source.Kind != PriceTableBuiltIn && source.Kind != PriceTableFile || !validSHA256(source.SHA256) || len(source.Date) > 10 {
			return errors.New("usage sample dollars must be marked estimated and name their price table: kind built_in or file, a date of at most 10 bytes and a SHA-256")
		}
	} else if sample.DollarSource != nil || sample.USDEstimated {
		return errors.New("a usage sample without dollars names no dollar source and is not estimated")
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
