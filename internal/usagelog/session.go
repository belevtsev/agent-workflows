package usagelog

import (
	"slices"

	"github.com/belevtsev/agent-workflows/internal/outcomes"
)

const unknownModel = "unknown"

// request is one de-duplicated provider request.
type request struct {
	uncached, write5m, write1h, writeUnsplit, read, output int64
	thinking                                               *int64
}

func (r request) snapshot() [7]int64 {
	thinking := int64(-1)
	if r.thinking != nil {
		thinking = *r.thinking
	}
	return [7]int64{r.uncached, r.write5m, r.write1h, r.writeUnsplit, r.read, r.output, thinking}
}

func validCount(value int64) bool { return value >= 0 && value <= int64(outcomes.MaxMetric) }

func (t *Tokens) add(r request) {
	t.Requests++
	t.UncachedInputTokens += r.uncached
	t.CacheWrite5mTokens += r.write5m
	t.CacheWrite1hTokens += r.write1h
	t.CacheWriteUnsplitTokens += r.writeUnsplit
	t.CacheReadTokens += r.read
	t.OutputTokens += r.output
	t.TotalInputTokens += r.uncached + r.write5m + r.write1h + r.writeUnsplit + r.read
	if r.thinking != nil {
		if t.ThinkingTokens == nil {
			t.ThinkingTokens = new(int64)
		}
		*t.ThinkingTokens += *r.thinking
		t.ThinkingReportedRequests++
	}
}

func (t *Tokens) merge(other Tokens) {
	t.Requests += other.Requests
	t.UncachedInputTokens += other.UncachedInputTokens
	t.CacheWrite5mTokens += other.CacheWrite5mTokens
	t.CacheWrite1hTokens += other.CacheWrite1hTokens
	t.CacheWriteUnsplitTokens += other.CacheWriteUnsplitTokens
	t.CacheReadTokens += other.CacheReadTokens
	t.OutputTokens += other.OutputTokens
	t.TotalInputTokens += other.TotalInputTokens
	if other.ThinkingTokens != nil {
		if t.ThinkingTokens == nil {
			t.ThinkingTokens = new(int64)
		}
		*t.ThinkingTokens += *other.ThinkingTokens
	}
	t.ThinkingReportedRequests += other.ThinkingReportedRequests
}

// agentState accumulates one agent's requests by model.
type agentState struct {
	agent   Agent
	models  map[string]*Tokens
	efforts map[string]bool
}

func newAgentState(agent Agent) *agentState {
	return &agentState{agent: agent, models: make(map[string]*Tokens), efforts: make(map[string]bool)}
}

// session collects requests across files with global de-duplication.
type session struct {
	report    *Report
	source    string
	agents    []*agentState
	seen      map[string][7]int64
	requests  int
	capped    bool
	limitsHit map[string]bool
}

func newSession(report *Report, source string) *session {
	return &session{report: report, source: source, seen: make(map[string][7]int64), limitsHit: make(map[string]bool)}
}

func (s *session) hit(limit string) { s.limitsHit[limit] = true }

// record adds a request unless its key was already seen. An empty key cannot
// be de-duplicated and is included and counted.
func (s *session) record(agent *agentState, key, model string, r request) {
	if key == "" {
		s.report.Anomalies.EntriesWithoutRequestID++
	} else if previous, ok := s.seen[key]; ok {
		s.report.Skipped.DuplicateEntries++
		if previous != r.snapshot() {
			s.report.Anomalies.ConflictingDuplicates++
		}
		return
	}
	if s.requests >= MaxRequests {
		s.hit("max_requests")
		return
	}
	if key != "" {
		s.seen[key] = r.snapshot()
	}
	s.requests++
	if model == "" {
		model = unknownModel
		s.report.Anomalies.RequestsWithoutModel++
	}
	tokens := agent.models[model]
	if tokens == nil {
		tokens = new(Tokens)
		agent.models[model] = tokens
	}
	tokens.add(r)
}

// finish computes agent, model and session totals plus price-table dollars.
func (s *session) finish(prices *PriceTable) {
	sessionModels := make(map[string]*Tokens)
	for _, state := range s.agents {
		agent := state.agent
		agent.ModelsSeen = sortedKeys(state.models)
		agent.EffortsSeen = sortedKeys(state.efforts)
		var amounts []Amount
		for _, model := range agent.ModelsSeen {
			tokens := *state.models[model]
			agent.Tokens.merge(tokens)
			amount := prices.price(model, s.source, tokens)
			amounts = append(amounts, amount)
			agent.ByModel = append(agent.ByModel, ModelUsage{Model: model, Tokens: tokens, Dollars: amount})
			if sessionModels[model] == nil {
				sessionModels[model] = new(Tokens)
			}
			sessionModels[model].merge(tokens)
		}
		switch {
		case prices != nil:
			agent.Dollars = sumAmounts(amounts, BasisPriceTable)
		case s.source == SourceClaude:
			agent.Dollars = Amount{UnknownReason: "per-agent dollars need --prices; the recorded cost-state is per model for the whole session and is not allocated to agents"}
		default:
			agent.Dollars = Amount{UnknownReason: "codex rollouts record no dollar cost; supply --prices"}
		}
		s.report.Totals.merge(agent.Tokens)
		s.report.Agents = append(s.report.Agents, agent)
	}
	for _, model := range sortedKeys(sessionModels) {
		tokens := *sessionModels[model]
		amount := prices.price(model, s.source, tokens)
		if prices == nil {
			amount.UnknownReason = "per-model price-table dollars need --prices; recorded per-model cost, when present, is under dollars.recorded.models"
		}
		s.report.Models = append(s.report.Models, ModelUsage{Model: model, Tokens: tokens, Dollars: amount})
	}
	for limit := range s.limitsHit {
		s.report.IncompleteReasons = append(s.report.IncompleteReasons, limit)
	}
	slices.Sort(s.report.IncompleteReasons)
}
