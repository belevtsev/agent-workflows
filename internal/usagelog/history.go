package usagelog

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	json "encoding/json/v2"

	"github.com/belevtsev/agent-workflows/v3/internal/outcomes"
)

// The cost history is a JSONL file of per-request token records. Each line is
// one HistoryRecord identified by (source, session_id, request_id); the file
// stores token counts only, so dollars are recomputed from a price table when
// the history is read.
const (
	// HistorySchemaVersion is the "v" field of every history record.
	HistorySchemaVersion = 1
	// HistoryRecordKind is the "kind" field of every history record.
	HistoryRecordKind = "cw.metrics_request"
	// HistoryAppendKind is the kind of an AppendResult.
	HistoryAppendKind = "cw.metrics_history_append"
	// HistoryReportKind is the kind of a HistoryReport.
	HistoryReportKind = "cw.metrics_history"

	// MaxHistoryBytes bounds the history file; an append that would grow it
	// past this size fails without writing.
	MaxHistoryBytes = 1 << 30
	// MaxHistoryLineBytes bounds one history line, newline excluded; a longer
	// line is damaged.
	MaxHistoryLineBytes = 4096
	// MaxHistoryLabelBytes bounds each text field of a record.
	MaxHistoryLabelBytes = 256
	// MaxHistoryDamagedListed bounds HistoryReport.Damaged; DamagedLines still
	// counts every damaged line.
	MaxHistoryDamagedListed = 100
)

// HistoryRecord is one line of the history: the token counts of one
// de-duplicated provider request. It holds no prompt, tool input, file content
// or agent description.
type HistoryRecord struct {
	V         int    `json:"v"`
	Kind      string `json:"kind"`
	Source    string `json:"source"` // claude_transcript | codex_rollout
	SessionID string `json:"session_id"`
	// RequestID is the import's de-duplication key: the Claude requestId,
	// "message:<id>" when only a message id exists, or "response:<id>" for a
	// Codex response id.
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	AgentRole string `json:"agent_role"` // main | subagent
	// AgentType is the Claude subagent type or the Codex agent role, truncated
	// to MaxHistoryLabelBytes; empty when unknown.
	AgentType string `json:"agent_type,omitempty"`
	Model     string `json:"model"`
	// Timestamp is the request line's time in UTC RFC 3339; empty when the
	// line had no parseable timestamp.
	Timestamp  string `json:"timestamp,omitempty"`
	ImportedAt string `json:"imported_at"`

	UncachedInputTokens     int64  `json:"uncached_input_tokens"`
	CacheWrite5mTokens      int64  `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens      int64  `json:"cache_write_1h_tokens"`
	CacheWriteUnsplitTokens int64  `json:"cache_write_unsplit_tokens"`
	CacheReadTokens         int64  `json:"cache_read_tokens"`
	OutputTokens            int64  `json:"output_tokens"`
	ThinkingTokens          *int64 `json:"thinking_tokens"`
}

// AppendResult reports one AppendHistory call. Every request the import
// counted lands in exactly one of Appended, AlreadyPresent, Conflicting,
// Unkeyed or Rejected, so they sum to Requests.
type AppendResult struct {
	SchemaVersion     int      `json:"schema_version"`
	Kind              string   `json:"kind"`
	History           string   `json:"history"`
	Source            string   `json:"source"`
	SessionID         string   `json:"session_id"`
	ImportStatus      string   `json:"import_status"` // the import report's status
	IncompleteReasons []string `json:"incomplete_reasons"`
	// Requests is the number of distinct requests the import counted.
	Requests int64 `json:"requests"`
	// Appended requests were written as new lines.
	Appended int64 `json:"appended"`
	// AlreadyPresent requests had an identical record in the history.
	AlreadyPresent int64 `json:"already_present"`
	// Conflicting requests had a record with the same identity but different
	// token counts; the stored record is kept and nothing is written.
	Conflicting int64 `json:"conflicting"`
	// Unkeyed requests had no request or message id and cannot be
	// de-duplicated, so they are never written.
	Unkeyed int64 `json:"unkeyed"`
	// Rejected requests had a field outside the record bounds.
	Rejected int64 `json:"rejected"`
	// HistoryDamagedLines counts existing history lines that were skipped
	// while reading identities.
	HistoryDamagedLines int64 `json:"history_damaged_lines"`
}

// HistoryFilter selects records. Empty fields match everything. Since and
// Until are YYYY-MM-DD dates compared with the record's UTC date, both
// inclusive; a record without a timestamp never matches a date bound.
type HistoryFilter struct {
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`
	Since     string `json:"since,omitempty"`
	Until     string `json:"until,omitempty"`
}

// DamagedLine is one skipped history line, numbered from 1.
type DamagedLine struct {
	Line   int64  `json:"line"`
	Reason string `json:"reason"`
}

// HistoryGroup aggregates the records sharing Key. Dollars is the
// price-table estimate for the group's tokens, or unknown with a reason.
type HistoryGroup struct {
	Key string `json:"key"`
	Tokens
	Dollars Amount `json:"dollars"`
}

// HistoryReport aggregates the valid records matching a filter.
type HistoryReport struct {
	SchemaVersion int           `json:"schema_version"`
	Kind          string        `json:"kind"`
	History       string        `json:"history"`
	Filter        HistoryFilter `json:"filter"`
	// Lines counts nonblank lines read.
	Lines int64 `json:"lines"`
	// Records counts valid, distinct records matching the filter.
	Records int64 `json:"records"`
	// DuplicateLines counts valid lines whose identity appeared earlier in
	// the file; only the first is counted.
	DuplicateLines int64 `json:"duplicate_lines"`
	// DamagedLines counts lines skipped as unreadable or invalid.
	DamagedLines int64         `json:"damaged_lines"`
	Damaged      []DamagedLine `json:"damaged"`
	// PriceTableEvidence echoes the supplied price table's evidence.
	PriceTableEvidence string `json:"price_table_evidence,omitempty"`
	// Totals has an empty Key.
	Totals HistoryGroup `json:"totals"`
	// ByModel, ByDay (UTC YYYY-MM-DD, or "unknown") and BySession are sorted
	// by Key.
	ByModel   []HistoryGroup `json:"by_model"`
	ByDay     []HistoryGroup `json:"by_day"`
	BySession []HistoryGroup `json:"by_session"`
}

// AppendHistory imports the transcript selected by options and appends each
// keyed request that the history does not already hold, as one line per
// request, under an exclusive lock that every writer takes. The history and
// its parent directory are created when missing, with modes 0600 and 0700. It
// fails with ErrInvalidOptions when options carry a price table, and fails
// without writing when the import fails, the history path is not a regular
// file (a symlink included), or the append would exceed MaxHistoryBytes.
func AppendHistory(ctx context.Context, path string, options Options) (AppendResult, error) {
	switch {
	case options.Prices != nil:
		return AppendResult{}, fmt.Errorf("%w: the cost history stores token counts only; it takes no composition or price table", ErrInvalidOptions)
	case path == "":
		return AppendResult{}, fmt.Errorf("%w: a cost history path is required", ErrInvalidOptions)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return AppendResult{}, err
	}
	// Import before touching the history, so a failed import writes nothing.
	var collected []collectedRequest
	report, err := importSession(ctx, options, &collected, nil)
	if err != nil {
		return AppendResult{}, err
	}
	result := AppendResult{SchemaVersion: HistorySchemaVersion, Kind: HistoryAppendKind, History: path, Source: report.Source,
		SessionID: report.SessionID, ImportStatus: report.Status, IncompleteReasons: append([]string{}, report.IncompleteReasons...),
		Requests: int64(len(collected))}
	importedAt := time.Now().UTC().Format(time.RFC3339)
	type candidate struct {
		key      historyKey
		snapshot [7]int64
		line     []byte
	}
	var candidates []candidate
	for _, c := range collected {
		if c.key == "" {
			result.Unkeyed++
			continue
		}
		record := newHistoryRecord(report, c, importedAt)
		line, err := json.Marshal(record, json.Deterministic(true))
		if err != nil || validateRecord(record) != "" || len(line) > MaxHistoryLineBytes {
			result.Rejected++
			continue
		}
		candidates = append(candidates, candidate{record.key(), record.snapshot(), append(line, '\n')})
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return AppendResult{}, fmt.Errorf("create cost history directory: %w", err)
	}
	release, err := lockHistory(ctx, path, true)
	if err != nil {
		return AppendResult{}, err
	}
	defer release()
	file, created, err := openHistory(path, true)
	if err != nil {
		return AppendResult{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return AppendResult{}, err
	}
	if info.Size() > MaxHistoryBytes {
		return AppendResult{}, fmt.Errorf("cost history %s exceeds %d bytes; move it aside to start a new one", path, int64(MaxHistoryBytes))
	}
	// Read identities under the lock: a concurrent writer's records are
	// either fully visible here or not yet written.
	stored := make(map[historyKey][7]int64)
	scan, err := scanHistory(ctx, file, func(_ int64, record HistoryRecord) {
		if _, ok := stored[record.key()]; !ok {
			stored[record.key()] = record.snapshot()
		}
	})
	if err != nil {
		return AppendResult{}, fmt.Errorf("read cost history: %w", err)
	}
	result.HistoryDamagedLines = scan.damaged
	var out bytes.Buffer
	if scan.size > 0 && !scan.endsWithNewline {
		// Terminate a torn final line so the new records stay whole lines.
		out.WriteByte('\n')
	}
	for _, c := range candidates {
		switch previous, ok := stored[c.key]; {
		case !ok:
			out.Write(c.line)
			result.Appended++
		case previous == c.snapshot:
			result.AlreadyPresent++
		default:
			result.Conflicting++
		}
	}
	if result.Appended == 0 {
		return result, nil
	}
	if scan.size+int64(out.Len()) > MaxHistoryBytes {
		return AppendResult{}, fmt.Errorf("appending to cost history %s would exceed %d bytes; move it aside to start a new one", path, int64(MaxHistoryBytes))
	}
	// One write of whole lines; O_APPEND places it at the end.
	if _, err := file.Write(out.Bytes()); err != nil {
		return AppendResult{}, fmt.Errorf("append to cost history: %w", err)
	}
	if err := file.Sync(); err != nil {
		return AppendResult{}, fmt.Errorf("sync cost history: %w", err)
	}
	if created {
		if err := syncDirectory(filepath.Dir(path)); err != nil {
			return AppendResult{}, fmt.Errorf("sync cost history directory: %w", err)
		}
	}
	return result, nil
}

// historyKey is a record's identity.
type historyKey struct{ source, session, request string }

func (r HistoryRecord) key() historyKey { return historyKey{r.Source, r.SessionID, r.RequestID} }

func (r HistoryRecord) snapshot() [7]int64 {
	return r.request().snapshot()
}

func (r HistoryRecord) request() request {
	return request{uncached: r.UncachedInputTokens, write5m: r.CacheWrite5mTokens, write1h: r.CacheWrite1hTokens,
		writeUnsplit: r.CacheWriteUnsplitTokens, read: r.CacheReadTokens, output: r.OutputTokens, thinking: r.ThinkingTokens}
}

func newHistoryRecord(report Report, c collectedRequest, importedAt string) HistoryRecord {
	agentType := c.agent.AgentType
	if report.Source == SourceCodex {
		agentType = c.agent.AgentRole
	}
	timestamp := ""
	if at, err := time.Parse(time.RFC3339Nano, c.timestamp); err == nil {
		timestamp = at.UTC().Format(time.RFC3339Nano)
	}
	r := c.request
	return HistoryRecord{V: HistorySchemaVersion, Kind: HistoryRecordKind, Source: report.Source, SessionID: report.SessionID,
		RequestID: c.key, AgentID: c.agent.AgentID, AgentRole: c.agent.Role, AgentType: truncateLabel(agentType),
		Model: c.model, Timestamp: timestamp, ImportedAt: importedAt,
		UncachedInputTokens: r.uncached, CacheWrite5mTokens: r.write5m, CacheWrite1hTokens: r.write1h,
		CacheWriteUnsplitTokens: r.writeUnsplit, CacheReadTokens: r.read, OutputTokens: r.output, ThinkingTokens: r.thinking}
}

// truncateLabel cuts a label to MaxHistoryLabelBytes on a rune boundary.
func truncateLabel(text string) string {
	if len(text) <= MaxHistoryLabelBytes {
		return text
	}
	cut := MaxHistoryLabelBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// validateRecord returns why a record is outside the schema, or "".
func validateRecord(r HistoryRecord) string {
	switch {
	case r.V != HistorySchemaVersion:
		return fmt.Sprintf("unsupported schema version %d", r.V)
	case r.Kind != HistoryRecordKind:
		return fmt.Sprintf("unknown kind %q", truncateLabel(r.Kind))
	case r.Source != SourceClaude && r.Source != SourceCodex:
		return "source must be claude_transcript or codex_rollout"
	case r.AgentRole != "main" && r.AgentRole != "subagent":
		return "agent_role must be main or subagent"
	}
	for _, field := range []struct {
		name, value string
		required    bool
	}{
		{"session_id", r.SessionID, true}, {"request_id", r.RequestID, true}, {"agent_id", r.AgentID, true},
		{"model", r.Model, true}, {"agent_type", r.AgentType, false}, {"timestamp", r.Timestamp, false}, {"imported_at", r.ImportedAt, false},
	} {
		switch {
		case field.required && field.value == "":
			return field.name + " is missing or empty"
		case len(field.value) > MaxHistoryLabelBytes:
			return fmt.Sprintf("%s exceeds %d bytes", field.name, MaxHistoryLabelBytes)
		case !utf8.ValidString(field.value):
			return field.name + " is not valid UTF-8"
		}
	}
	for _, field := range []struct{ name, value string }{{"timestamp", r.Timestamp}, {"imported_at", r.ImportedAt}} {
		if _, err := time.Parse(time.RFC3339Nano, field.value); field.value != "" && err != nil {
			return field.name + " is not an RFC 3339 time"
		}
	}
	counts := []int64{r.UncachedInputTokens, r.CacheWrite5mTokens, r.CacheWrite1hTokens, r.CacheWriteUnsplitTokens, r.CacheReadTokens, r.OutputTokens}
	if r.ThinkingTokens != nil {
		counts = append(counts, *r.ThinkingTokens)
	}
	for _, count := range counts {
		if count < 0 || count > int64(outcomes.MaxMetric) {
			return "a token count is negative or too large"
		}
	}
	if r.ThinkingTokens != nil && *r.ThinkingTokens > r.OutputTokens {
		return "thinking_tokens exceeds output_tokens"
	}
	return ""
}

type historyScan struct {
	lines, damaged  int64
	damagedList     []DamagedLine
	size            int64
	endsWithNewline bool
}

func (s *historyScan) damage(line int64, reason string) {
	s.damaged++
	if len(s.damagedList) < MaxHistoryDamagedListed {
		s.damagedList = append(s.damagedList, DamagedLine{Line: line, Reason: reason})
	}
}

// scanHistory calls visit for every valid record and counts the rest as
// damaged. Line numbers count every physical line, blank ones included.
func scanHistory(ctx context.Context, file io.Reader, visit func(line int64, record HistoryRecord)) (historyScan, error) {
	var scan historyScan
	reader := bufio.NewReaderSize(file, 64<<10)
	var number int64
	for {
		if number%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return scan, err
			}
		}
		data, err := reader.ReadSlice('\n')
		oversized := false
		for errors.Is(err, bufio.ErrBufferFull) {
			oversized = true
			scan.size += int64(len(data))
			data, err = reader.ReadSlice('\n')
		}
		if err != nil && err != io.EOF {
			return scan, err
		}
		if len(data) == 0 && err == io.EOF && !oversized {
			return scan, nil
		}
		number++
		scan.size += int64(len(data))
		scan.endsWithNewline = len(data) > 0 && data[len(data)-1] == '\n'
		line := bytes.TrimSuffix(data, []byte("\n"))
		switch {
		case oversized || len(line) > MaxHistoryLineBytes:
			scan.lines++
			scan.damage(number, fmt.Sprintf("line exceeds %d bytes", MaxHistoryLineBytes))
		case len(bytes.TrimSpace(line)) == 0:
		default:
			scan.lines++
			var record HistoryRecord
			if err := json.Unmarshal(line, &record, json.RejectUnknownMembers(true)); err != nil {
				scan.damage(number, "not a valid history record: "+jsonReason(err))
			} else if reason := validateRecord(record); reason != "" {
				scan.damage(number, reason)
			} else {
				visit(number, record)
			}
		}
		if err == io.EOF {
			return scan, nil
		}
	}
}

// jsonReason keeps a decode error short and free of the line's content.
func jsonReason(err error) string {
	var semantic *json.SemanticError
	if errors.As(err, &semantic) && semantic.JSONPointer != "" {
		return "invalid or unknown field " + truncateLabel(string(semantic.JSONPointer))
	}
	return "invalid JSON"
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// ReadHistory reads the history at path, under a shared lock, and aggregates
// the valid records matching filter. Damaged lines are counted and listed,
// never silently dropped. With prices, each group carries price-table dollars.
// It fails with ErrInvalidOptions for a malformed date or Since after Until,
// and with an error wrapping os.ErrNotExist when the history does not exist.
func ReadHistory(ctx context.Context, path string, filter HistoryFilter, prices *PriceTable) (HistoryReport, error) {
	for _, bound := range []struct{ name, value string }{{"since", filter.Since}, {"until", filter.Until}} {
		if _, err := time.Parse(time.DateOnly, bound.value); bound.value != "" && err != nil {
			return HistoryReport{}, historyFilterError(fmt.Sprintf("--%s must be a date as YYYY-MM-DD, got %q", bound.name, bound.value))
		}
	}
	if filter.Since != "" && filter.Until != "" && filter.Since > filter.Until {
		return HistoryReport{}, historyFilterError(fmt.Sprintf("--since %s is after --until %s", filter.Since, filter.Until))
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return HistoryReport{}, err
	}
	release, err := lockHistory(ctx, path, false)
	if err != nil {
		return HistoryReport{}, err
	}
	defer release()
	file, _, err := openHistory(path, false)
	if err != nil {
		return HistoryReport{}, err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return HistoryReport{}, err
	} else if info.Size() > MaxHistoryBytes {
		return HistoryReport{}, fmt.Errorf("cost history %s exceeds %d bytes", path, int64(MaxHistoryBytes))
	}

	report := HistoryReport{SchemaVersion: HistorySchemaVersion, Kind: HistoryReportKind, History: path, Filter: filter}
	if prices != nil {
		report.PriceTableEvidence = prices.Evidence
	}
	totals := newHistoryGroup()
	groups := [3]map[string]*historyGroup{{}, {}, {}} // model, day, session
	seen := make(map[historyKey]bool)
	scan, err := scanHistory(ctx, file, func(_ int64, record HistoryRecord) {
		if seen[record.key()] {
			report.DuplicateLines++
			return
		}
		seen[record.key()] = true
		day := "unknown"
		if at, err := time.Parse(time.RFC3339Nano, record.Timestamp); err == nil {
			day = at.UTC().Format(time.DateOnly)
		}
		dated := day != "unknown"
		switch {
		case filter.SessionID != "" && record.SessionID != filter.SessionID,
			filter.Model != "" && record.Model != filter.Model,
			filter.Since != "" && (!dated || day < filter.Since),
			filter.Until != "" && (!dated || day > filter.Until):
			return
		}
		report.Records++
		totals.add(record)
		for index, key := range []string{record.Model, day, record.SessionID} {
			group := groups[index][key]
			if group == nil {
				group = newHistoryGroup()
				groups[index][key] = group
			}
			group.add(record)
		}
	})
	if err != nil {
		return HistoryReport{}, fmt.Errorf("read cost history: %w", err)
	}
	report.Lines, report.DamagedLines, report.Damaged = scan.lines, scan.damaged, append([]DamagedLine{}, scan.damagedList...)
	report.Totals = totals.finish("", prices)
	lists := [3]*[]HistoryGroup{&report.ByModel, &report.ByDay, &report.BySession}
	for index, list := range lists {
		*list = []HistoryGroup{}
		for _, key := range sortedKeys(groups[index]) {
			*list = append(*list, groups[index][key].finish(key, prices))
		}
	}
	return report, nil
}

// historyFilterError is an ErrInvalidOptions error whose message names the
// history filter, not the import options.
type historyFilterError string

func (e historyFilterError) Error() string        { return "invalid history filter: " + string(e) }
func (e historyFilterError) Is(target error) bool { return target == ErrInvalidOptions }

// historyGroup sums tokens, kept per (model, source) so each is priced with
// its own rates.
type historyGroup struct {
	tokens Tokens
	priced map[[2]string]*Tokens
}

func newHistoryGroup() *historyGroup { return &historyGroup{priced: make(map[[2]string]*Tokens)} }

func (g *historyGroup) add(record HistoryRecord) {
	request := record.request()
	g.tokens.add(request)
	key := [2]string{record.Model, record.Source}
	if g.priced[key] == nil {
		g.priced[key] = new(Tokens)
	}
	g.priced[key].add(request)
}

func (g *historyGroup) finish(key string, prices *PriceTable) HistoryGroup {
	group := HistoryGroup{Key: key, Tokens: g.tokens}
	if prices == nil {
		group.Dollars = Amount{UnknownReason: "the history stores token counts only; pass --prices to estimate dollars"}
		return group
	}
	keys := make([][2]string, 0, len(g.priced))
	for k := range g.priced {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b [2]string) int { return cmp.Or(strings.Compare(a[0], b[0]), strings.Compare(a[1], b[1])) })
	var amounts []Amount
	for _, k := range keys {
		amounts = append(amounts, prices.price(k[0], k[1], *g.priced[k]))
	}
	group.Dollars = sumAmounts(amounts, BasisPriceTable)
	return group
}
