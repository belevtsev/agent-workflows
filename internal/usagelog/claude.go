package usagelog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// claudeHead validates a line as a JSON object and reads its identity; a
// non-string sessionId is ignored rather than making the line malformed.
type claudeHead struct {
	Type      string         `json:"type"`
	SessionID jsontext.Value `json:"sessionId"`
}

type claudeEntry struct {
	SessionID string         `json:"sessionId"`
	RequestID string         `json:"requestId"`
	Timestamp jsontext.Value `json:"timestamp"`
	Effort    jsontext.Value `json:"effort"`
	Message   *claudeMessage `json:"message"`
}

type claudeMessage struct {
	ID    string       `json:"id"`
	Model string       `json:"model"`
	Usage *claudeUsage `json:"usage"`
}

type claudeUsage struct {
	InputTokens         *int64 `json:"input_tokens"`
	CacheCreation       *int64 `json:"cache_creation_input_tokens"`
	CacheRead           *int64 `json:"cache_read_input_tokens"`
	OutputTokens        *int64 `json:"output_tokens"`
	OutputTokensDetails *struct {
		ThinkingTokens *int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
	CacheCreationSplit *struct {
		Ephemeral5m *int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h *int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

type costState struct {
	SessionID           string                    `json:"sessionId"`
	TotalCostUSD        *float64                  `json:"totalCostUSD"`
	ModelUsage          map[string]costStateModel `json:"modelUsage"`
	HasUnknownModelCost *bool                     `json:"hasUnknownModelCost"`
}

type costStateModel struct {
	InputTokens         *int64   `json:"inputTokens"`
	OutputTokens        *int64   `json:"outputTokens"`
	ThinkingTokens      *int64   `json:"thinkingTokens"`
	CacheReadTokens     *int64   `json:"cacheReadInputTokens"`
	CacheCreationTokens *int64   `json:"cacheCreationInputTokens"`
	CacheCreation5m     *int64   `json:"cacheCreationEphemeral5mInputTokens"`
	CacheCreation1h     *int64   `json:"cacheCreationEphemeral1hInputTokens"`
	WebSearchRequests   *int64   `json:"webSearchRequests"`
	CostUSD             *float64 `json:"costUSD"`
}

func (c costState) valid() bool {
	if c.TotalCostUSD == nil || !validAmount(*c.TotalCostUSD) {
		return false
	}
	for _, model := range c.ModelUsage {
		if model.CostUSD != nil && !validAmount(*model.CostUSD) {
			return false
		}
		for _, value := range []*int64{model.InputTokens, model.OutputTokens, model.ThinkingTokens, model.CacheReadTokens, model.CacheCreationTokens, model.CacheCreation5m, model.CacheCreation1h, model.WebSearchRequests} {
			if value != nil && !validCount(*value) {
				return false
			}
		}
	}
	return true
}

func validAmount(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1e12
}

// claudeRequest converts usage to a request; ok=false marks an invalid entry.
func claudeRequest(usage *claudeUsage, report *Report) (request, bool) {
	if usage.InputTokens == nil || usage.OutputTokens == nil || usage.CacheCreation == nil || usage.CacheRead == nil {
		return request{}, false
	}
	r := request{uncached: *usage.InputTokens, read: *usage.CacheRead, output: *usage.OutputTokens}
	values := []int64{r.uncached, r.read, r.output, *usage.CacheCreation}
	if details := usage.OutputTokensDetails; details != nil && details.ThinkingTokens != nil {
		r.thinking = details.ThinkingTokens
		values = append(values, *r.thinking)
	}
	written := *usage.CacheCreation
	if split := usage.CacheCreationSplit; split != nil && split.Ephemeral5m != nil && split.Ephemeral1h != nil {
		values = append(values, *split.Ephemeral5m, *split.Ephemeral1h)
		if *split.Ephemeral5m+*split.Ephemeral1h == written {
			r.write5m, r.write1h = *split.Ephemeral5m, *split.Ephemeral1h
		} else {
			r.writeUnsplit = written
			report.Anomalies.CacheSplitMismatchRequests++
		}
	} else {
		r.writeUnsplit = written
	}
	for _, value := range values {
		if !validCount(value) {
			return request{}, false
		}
	}
	if r.thinking != nil && *r.thinking > r.output {
		return request{}, false
	}
	return r, true
}

type claudeMeta struct {
	AgentType         jsontext.Value `json:"agentType"`
	Description       jsontext.Value `json:"description"`
	Model             jsontext.Value `json:"model"`
	Effort            jsontext.Value `json:"effort"`
	PickedEffortModel jsontext.Value `json:"pickedEffortModel"`
}

// metaText keeps only a bounded string value; other JSON kinds are ignored.
func metaText(value jsontext.Value) string {
	var text string
	if len(value) == 0 || value.Kind() != '"' || json.Unmarshal(value, &text, lenient) != nil {
		return ""
	}
	text = strings.ToValidUTF8(text, "?")
	if len(text) > 512 {
		cut := 512
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "..."
	}
	return text
}

func importClaude(ctx context.Context, path string, prices *PriceTable, composition bool, collected *[]collectedRequest) (Report, error) {
	report := newReport(SourceClaude)
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Report{}, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Report{}, fmt.Errorf("read claude transcript: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Report{}, errors.New("claude transcript must be a regular file")
	}
	dir, base := filepath.Split(absolute)
	sessionID := strings.TrimSuffix(base, ".jsonl")
	report.SessionID = sessionID
	s := newSession(&report, SourceClaude)
	s.collected = collected

	type agentFile struct {
		state *agentState
		path  string
	}
	files := []agentFile{{newAgentState(Agent{AgentID: "main", Role: "main", Files: []string{base}}), absolute}}
	subagents := filepath.Join(dir, sessionID, "subagents")
	entries, err := os.ReadDir(subagents)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		report.Skipped.UnreadableFiles++
		report.Notes = append(report.Notes, "the subagents directory could not be listed; subagent usage is missing")
		s.hit("unreadable_subagents_directory")
	}
	for _, entry := range entries { // os.ReadDir sorts by file name.
		name := entry.Name()
		if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if !entry.Type().IsRegular() {
			// A symlink or directory is not read; its usage is missing.
			report.Skipped.UnreadableFiles++
			s.hit("unreadable_file")
			continue
		}
		if len(files) >= MaxFiles {
			s.hit("max_files")
			break
		}
		stem := strings.TrimSuffix(name, ".jsonl")
		agent := Agent{AgentID: stem, Role: "subagent", Depth: 1, Files: []string{filepath.ToSlash(filepath.Join(sessionID, "subagents", name))}}
		if data, err := readSmallFile(filepath.Join(subagents, stem+".meta.json")); err == nil {
			var meta claudeMeta
			if json.Unmarshal(data, &meta, lenient) != nil {
				report.Skipped.MetaErrors++
			} else {
				agent.AgentType, agent.Description = metaText(meta.AgentType), metaText(meta.Description)
				agent.ConfiguredModel, agent.ConfiguredEffort = metaText(meta.Model), metaText(meta.Effort)
				agent.PickedEffortModel = metaText(meta.PickedEffortModel)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			report.Skipped.MetaErrors++
		}
		files = append(files, agentFile{newAgentState(agent), filepath.Join(subagents, name)})
	}

	var comp *composer
	if composition {
		comp = newComposer(prices, sessionID)
	}
	var lastCost *costState
	costStates := 0
	var matchingEntries, otherEntries int64
	r := newReader(MaxLineBytes)
	for index, file := range files {
		s.agents = append(s.agents, file.state)
		main := index == 0
		var compAgent *compAgent
		if comp != nil {
			compAgent = comp.addAgent(file.state.agent, main)
		}
		if r.exhausted {
			continue
		}
		stats, err := r.eachLine(ctx, file.path, &report.Skipped, func(line []byte) {
			var head claudeHead
			if json.Unmarshal(line, &head, lenient) != nil {
				report.Skipped.MalformedLines++
				return
			}
			// Any line kind counts toward session identity: a resumed session
			// starts with copied history carrying the previous session id.
			if id := metaText(head.SessionID); id == sessionID {
				matchingEntries++
			} else if id != "" {
				otherEntries++
			}
			switch head.Type {
			case "cost-state":
				var state costState
				switch {
				case json.Unmarshal(line, &state, lenient) != nil || !state.valid():
					report.Skipped.InvalidEntries++
				case !main || state.SessionID != sessionID:
					report.Skipped.IgnoredCostStates++
				default:
					costStates++
					lastCost = &state
				}
			case "assistant":
				var entry claudeEntry
				if json.Unmarshal(line, &entry, lenient) != nil {
					report.Skipped.InvalidEntries++
					return
				}
				key := entry.RequestID
				if key == "" && entry.Message != nil && entry.Message.ID != "" {
					key = "message:" + entry.Message.ID
				}
				// Lines that are not requests (synthetic, no usage, another
				// session's copied history) are still context for the composer.
				context := func(history bool) {
					if compAgent != nil {
						comp.assistant(compAgent, line, key, "", request{}, false, history)
					}
				}
				if entry.Message != nil && entry.Message.Model == "<synthetic>" {
					report.Skipped.SyntheticEntries++
					context(false)
					return
				}
				if entry.Message == nil || entry.Message.Usage == nil {
					report.Skipped.NoUsageEntries++
					context(false)
					return
				}
				if entry.SessionID != sessionID {
					report.Skipped.OtherSessionEntries++
					context(true)
					return
				}
				req, ok := claudeRequest(entry.Message.Usage, &report)
				if !ok {
					report.Skipped.InvalidEntries++
					context(false)
					return
				}
				recorded := s.record(file.state, key, entry.Message.Model, metaText(entry.Timestamp), req)
				if effort := metaText(entry.Effort); recorded && effort != "" {
					file.state.efforts[effort] = true
				}
				if compAgent != nil {
					comp.assistant(compAgent, line, key, entry.Message.Model, req, recorded, false)
				}
			case "user":
				if compAgent != nil {
					comp.user(compAgent, line)
				}
			case "attachment":
				if compAgent != nil {
					comp.attachment(compAgent, line)
				}
			case "system":
				if compAgent != nil {
					comp.system(compAgent, line)
				}
			}
		})
		report.Files = append(report.Files, FileRead{Path: file.state.agent.Files[0], AgentID: file.state.agent.AgentID, BytesRead: stats.bytes, Lines: stats.lines, Complete: stats.complete})
		if err != nil {
			if ctx.Err() != nil {
				return Report{}, err
			}
			if main {
				return Report{}, fmt.Errorf("read claude transcript: %w", err)
			}
			report.Skipped.UnreadableFiles++
			s.hit("unreadable_file")
		}
	}
	if matchingEntries == 0 && otherEntries > 0 {
		return Report{}, fmt.Errorf("claude transcript %s has no lines for session %q but has lines of another session; pass the main <session-id>.jsonl transcript (not a renamed copy or a subagent agent-*.jsonl file)", base, sessionID)
	}
	if r.exhausted {
		s.hit("max_total_bytes")
	}
	if report.Skipped.OversizedLines > 0 {
		s.hit("max_line_bytes")
	}
	s.finish(prices)
	applyClaudeDollars(&report, lastCost, costStates, prices)
	finishReport(&report, lastCost)
	if comp != nil {
		report.Composition = comp.build(&report, lastCost)
	}
	return report, nil
}

// applyClaudeDollars fills recorded and price-table dollars and reconciles
// transcript-attributed tokens with the recorded per-model totals.
func applyClaudeDollars(report *Report, cost *costState, entries int, prices *PriceTable) {
	attributed := make(map[string]Tokens)
	for _, model := range report.Models {
		attributed[model.Model] = model.Tokens
	}
	if cost == nil {
		report.Dollars.RecordedUnknownReason = "the main transcript has no cost-state entry for this session"
	} else {
		recorded := &RecordedDollars{Basis: BasisRecorded, Evidence: "claude transcript cost-state " + report.SessionID,
			CostStateEntries: entries, Total: *cost.TotalCostUSD, HasUnknownModelCost: cost.HasUnknownModelCost}
		var unattributed *float64
		unattributedKnown := true
		for _, model := range sortedKeys(cost.ModelUsage) {
			m := cost.ModelUsage[model]
			_, seen := attributed[model]
			recorded.Models = append(recorded.Models, RecordedModel{Model: model, Cost: m.CostUSD, InputTokens: m.InputTokens,
				OutputTokens: m.OutputTokens, ThinkingTokens: m.ThinkingTokens, CacheReadTokens: m.CacheReadTokens,
				CacheWriteTokens: m.CacheCreationTokens, CacheWrite5mTokens: m.CacheCreation5m, CacheWrite1hTokens: m.CacheCreation1h,
				WebSearchRequests: m.WebSearchRequests, Attributed: seen})
			if !seen {
				recorded.UnattributedModels = append(recorded.UnattributedModels, model)
				if m.CostUSD == nil {
					unattributedKnown = false
				} else {
					add(&unattributed, *m.CostUSD)
				}
			}
			if m.WebSearchRequests != nil && *m.WebSearchRequests > 0 {
				report.Notes = append(report.Notes, fmt.Sprintf("model %s recorded web search requests; their cost is in the recorded amount but not in price-table dollars", model))
			}
		}
		if unattributedKnown {
			if unattributed == nil {
				unattributed = new(float64)
			}
			recorded.UnattributedTotal = unattributed
		}
		for _, model := range sortedKeys(attributed) {
			if _, ok := cost.ModelUsage[model]; !ok {
				recorded.Reconciliation = append(recorded.Reconciliation, Reconciliation{Model: model, Status: "transcript_only", TranscriptMinusRecorded: map[string]int64{}, NotCompared: []string{}})
			}
		}
		for _, model := range sortedKeys(cost.ModelUsage) {
			tokens, seen := attributed[model]
			if !seen {
				recorded.Reconciliation = append(recorded.Reconciliation, Reconciliation{Model: model, Status: "recorded_only", TranscriptMinusRecorded: map[string]int64{}, NotCompared: []string{}})
				continue
			}
			recorded.Reconciliation = append(recorded.Reconciliation, reconcile(model, tokens, cost.ModelUsage[model]))
		}
		slices.SortFunc(recorded.Reconciliation, func(a, b Reconciliation) int { return strings.Compare(a.Model, b.Model) })
		report.Dollars.Recorded = recorded
		if cost.HasUnknownModelCost != nil && *cost.HasUnknownModelCost {
			report.Notes = append(report.Notes, "cost-state reports hasUnknownModelCost=true: the recorded total omits some model cost, so no observation cost is emitted")
		}
	}
	applyPriceTable(report, prices, cost)
}

func reconcile(model string, tokens Tokens, recorded costStateModel) Reconciliation {
	result := Reconciliation{Model: model, Status: "equal", TranscriptMinusRecorded: map[string]int64{}, NotCompared: []string{}}
	compared := 0
	compare := func(name string, transcript int64, value *int64) {
		if value == nil {
			result.NotCompared = append(result.NotCompared, name)
			return
		}
		if name != "thinking_tokens" {
			compared++
		}
		if delta := transcript - *value; delta != 0 {
			result.TranscriptMinusRecorded[name] = delta
		}
	}
	compare("uncached_input_tokens", tokens.UncachedInputTokens, recorded.InputTokens)
	compare("output_tokens", tokens.OutputTokens, recorded.OutputTokens)
	compare("cache_read_tokens", tokens.CacheReadTokens, recorded.CacheReadTokens)
	compare("cache_write_tokens", tokens.CacheWrite5mTokens+tokens.CacheWrite1hTokens+tokens.CacheWriteUnsplitTokens, recorded.CacheCreationTokens)
	if tokens.CacheWriteUnsplitTokens == 0 {
		compare("cache_write_5m_tokens", tokens.CacheWrite5mTokens, recorded.CacheCreation5m)
		compare("cache_write_1h_tokens", tokens.CacheWrite1hTokens, recorded.CacheCreation1h)
	} else {
		result.NotCompared = append(result.NotCompared, "cache_write_5m_tokens", "cache_write_1h_tokens")
	}
	if tokens.ThinkingTokens != nil && tokens.ThinkingReportedRequests == tokens.Requests {
		compare("thinking_tokens", *tokens.ThinkingTokens, recorded.ThinkingTokens)
	} else {
		result.NotCompared = append(result.NotCompared, "thinking_tokens")
	}
	switch {
	case len(result.TranscriptMinusRecorded) > 0:
		result.Status = "differs"
	case compared == 0:
		// No recorded token field to check: agreement is unknown, not equal.
		result.Status = "not_compared"
	}
	return result
}

// recordedTokens converts a recorded-only model's cost-state totals to tokens
// for pricing and session usage; ok=false when a needed field is absent.
func recordedTokens(m costStateModel) (Tokens, bool) {
	if m.InputTokens == nil || m.OutputTokens == nil || m.CacheReadTokens == nil || m.CacheCreationTokens == nil {
		return Tokens{}, false
	}
	t := Tokens{UncachedInputTokens: *m.InputTokens, OutputTokens: *m.OutputTokens, CacheReadTokens: *m.CacheReadTokens, ThinkingTokens: m.ThinkingTokens}
	if m.CacheCreation5m != nil && m.CacheCreation1h != nil && *m.CacheCreation5m+*m.CacheCreation1h == *m.CacheCreationTokens {
		t.CacheWrite5mTokens, t.CacheWrite1hTokens = *m.CacheCreation5m, *m.CacheCreation1h
	} else {
		t.CacheWriteUnsplitTokens = *m.CacheCreationTokens
	}
	t.TotalInputTokens = t.UncachedInputTokens + t.CacheWrite5mTokens + t.CacheWrite1hTokens + t.CacheWriteUnsplitTokens + t.CacheReadTokens
	return t, true
}

func add(target **float64, value float64) {
	if *target == nil {
		*target = new(float64)
	}
	**target += value
}
