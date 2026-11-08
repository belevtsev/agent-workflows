package usagelog

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/bits"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// Composition statuses.
const (
	CompositionReported    = "reported"
	CompositionWithheld    = "withheld"
	CompositionUnsupported = "unsupported"

	// CompositionTolerance bounds |recorded - price-table| / recorded.
	CompositionTolerance = 0.01

	maxRepeatedCalls  = 20
	maxTopBlocks      = 10
	maxLabelBytes     = 120
	maxBriefBytes     = 64 << 10
	maxBriefTotal     = 16 << 20
	sourceInitialPart = "initial_context"
)

// Composition attributes each agent's input to the context blocks that
// produced it. It is present only with Options.Composition; its results are
// null unless Status is reported.
type Composition struct {
	Status         string                     `json:"status"`
	Reason         string                     `json:"reason,omitempty"`
	Reconciliation *CompositionReconciliation `json:"reconciliation"`
	Roles          *[]RoleComposition         `json:"roles"`
	RoleCoverage   *RoleCoverage              `json:"role_coverage"`
	Categories     *[]CategoryComposition     `json:"categories"`
	Agents         *[]AgentComposition        `json:"agents"`
	Notes          []string                   `json:"notes"`
}

// CompositionReconciliation compares the recorded cost with the price-table
// total of the same requests and the attributed with the transcript tokens.
type CompositionReconciliation struct {
	RecordedTotal         *float64 `json:"recorded_total"`
	PriceTableTotal       *float64 `json:"price_table_total"`
	RelativeDifference    *float64 `json:"relative_difference"`
	Tolerance             float64  `json:"tolerance"`
	Status                string   `json:"status"` // within_tolerance | exceeds_tolerance | not_compared
	AttributedInputTokens int64    `json:"attributed_input_tokens"`
	TranscriptInputTokens int64    `json:"transcript_input_tokens"`
}

// RoleComposition sums the agents mapped to one role.
type RoleComposition struct {
	Role          string  `json:"role"`
	Agents        int     `json:"agents"`
	Requests      int64   `json:"requests"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	InputDollars  float64 `json:"input_dollars"`
	OutputDollars float64 `json:"output_dollars"`
	Dollars       float64 `json:"dollars"`
}

// RoleCoverage reports how much of the session was mapped to a known role.
type RoleCoverage struct {
	Agents            int   `json:"agents"`
	MappedAgents      int   `json:"mapped_agents"`
	InputTokens       int64 `json:"input_tokens"`
	MappedInputTokens int64 `json:"mapped_input_tokens"`
}

// CategoryComposition is one source category across the session.
type CategoryComposition struct {
	Category      string  `json:"category"`
	InputTokens   int64   `json:"input_tokens"`
	InputShare    float64 `json:"input_share"`
	InputDollars  float64 `json:"input_dollars"`
	RereadDollars float64 `json:"reread_dollars"`
}

// AgentComposition is one agent's input by source.
type AgentComposition struct {
	AgentID              string              `json:"agent_id"`
	Role                 string              `json:"role"`
	RoleBasis            string              `json:"role_basis"`
	Requests             int64               `json:"requests"`
	InputTokens          int64               `json:"input_tokens"`
	OutputTokens         int64               `json:"output_tokens"`
	InputDollars         float64             `json:"input_dollars"`
	OutputDollars        float64             `json:"output_dollars"`
	BytesPerToken        *float64            `json:"bytes_per_token"`
	InitialContextTokens int64               `json:"initial_context_tokens"`
	Sources              []SourceComposition `json:"sources"`
	TopBlocks            []BlockComposition  `json:"top_blocks"`
	RepeatedCalls        []RepeatedCall      `json:"repeated_calls"`
	Phases               *Phases             `json:"phases"`
	UnrecordedTokens     int64               `json:"unrecorded_tokens"`
	ContextReductions    ContextReductions   `json:"context_reductions"`
	Compactions          int                 `json:"compactions"`
}

// SourceComposition sums one agent's blocks of one source.
type SourceComposition struct {
	Source        string  `json:"source"`
	Category      string  `json:"category"`
	Blocks        int     `json:"blocks"`
	NewTokens     int64   `json:"new_tokens"`
	InputTokens   int64   `json:"input_tokens"`
	InputShare    float64 `json:"input_share"`
	InputDollars  float64 `json:"input_dollars"`
	RereadTokens  int64   `json:"reread_tokens"`
	RereadDollars float64 `json:"reread_dollars"`
}

// BlockComposition is one block with a high re-read cost.
type BlockComposition struct {
	Source        string  `json:"source"`
	Label         string  `json:"label"`
	Request       int64   `json:"request"`
	Tokens        int64   `json:"tokens"`
	LaterRequests int64   `json:"later_requests"`
	RereadTokens  int64   `json:"reread_tokens"`
	RereadDollars float64 `json:"reread_dollars"`
}

// RepeatedCall is a group of identical tool calls (same tool, same input).
type RepeatedCall struct {
	Tool         string `json:"tool"`
	InputSHA256  string `json:"input_sha256"`
	Label        string `json:"label"`
	Calls        int    `json:"calls"`
	ResultTokens int64  `json:"result_tokens"`
}

// Phases describes cw task phase boundaries and the stale-phase re-read.
type Phases struct {
	Boundaries           []PhaseBoundary `json:"boundaries"`
	PhaseCount           int             `json:"phase_count"`
	StaleRereadTokens    int64           `json:"stale_reread_tokens"`
	StaleRereadDollars   float64         `json:"stale_reread_dollars"`
	StaleInputDollars    float64         `json:"stale_input_dollars"`
	PrefixRewriteDollars float64         `json:"prefix_rewrite_dollars"`
}

// PhaseBoundary is one boundary call; Request is the first request of the new
// phase, or null when no request followed.
type PhaseBoundary struct {
	Request *int64 `json:"request"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
}

// ContextReductions counts requests whose input shrank without a compaction.
type ContextReductions struct {
	Events int64 `json:"events"`
	Tokens int64 `json:"tokens"`
}

// Source categories.
const (
	catFixedPrefix     = "fixed_prefix"
	catUserTurn        = "user_turn"
	catAssistantOutput = "assistant_output"
	catToolOutput      = "tool_output"
	catCwOutput        = "cw_output"
	catSkillLoad       = "skill_load"
	catReferenceRead   = "reference_read"
	catAgentHandoff    = "agent_handoff"
	catContext         = "context"
	catUnrecorded      = "unrecorded"
)

var (
	redirectPattern   = regexp.MustCompile(`(?:>|\btee)\s*(\S+)\s*<<|<<-?\s*['"]?[A-Za-z_][A-Za-z0-9_]*['"]?\s*(?:>|\|\s*tee)\s*(\S+)`)
	separatorPattern  = regexp.MustCompile(`&&|\|\||[;|&(){}]`)
	assignmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=\S*$`)
	agentNotification = regexp.MustCompile(`<summary>Agent\b`)
	// Brief keys may be quoted JSON ("role": "x") or jq object syntax (role:"x").
	briefRolePattern  = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])"?role"?\s*:\s*"([A-Za-z_]+)"`)
	briefIDPattern    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])"?id"?\s*:\s*"([^"\s]+)"`)
	redirectTarget    = regexp.MustCompile(`>>?\s*([^\s>&|;<]+)`)
	testAuthorPattern = regexp.MustCompile(`(?i)test[-_ ]?author`)
	followupPattern   = regexp.MustCompile(`follow[- ]?up|re-?review|scoped|confirm|re-?bind`)
	githubPattern     = regexp.MustCompile(`\bcommit|github|pull request|\bpr\b|publish`)
	implementPattern  = regexp.MustCompile(`implement|\bfix|coding|refactor`)
	evidencePattern   = regexp.MustCompile(`fetch|research|investigat|explor|look ?up|search|\bfind`)
)

// block is one appended item of an agent's context.
type block struct {
	source, category, label string
	bytes                   int64
	tokens                  int64 // tokens resident now
	newTokens               int64 // tokens when first assigned
	request                 int64 // request where first resident; 0 while pending
	phase                   int
	initial, resident       bool
	visible                 bool // counts toward byte shares (not an output block)
	callKey                 string

	inputTokens, rereadTokens, laterRequests int64
	inputDollars, rereadDollars              float64
}

type toolCall struct {
	name, source, category, label, key string
}

type brief struct {
	id, role, text string
}

type agentLink struct {
	brief                     *brief
	description, subagentType string
	order                     int
}

type boundaryEvent struct {
	kind, label string
	request     int64 // first request of the new phase
}

type repeatGroup struct {
	tool, hash, label string
	calls             int
	order             int
}

// compAgent tracks one agent file's context while it is read.
type compAgent struct {
	id, stem            string
	main                bool
	blocks              []*block
	pending, resident   []*block
	firstTurn           []*block
	initial             *block
	out                 *block
	prevOut             int64
	requests            int64
	phase               int
	samples             []float64
	calls               map[string]toolCall
	repeats             map[string]*repeatGroup
	boundaries          []boundaryEvent
	briefs              []*brief
	lastKey             string
	firstModel          string
	outputTokens        int64
	inputDollars        float64
	outputDollars       float64
	attributed          int64
	unrecorded          int64
	reductions          ContextReductions
	compactions         int
	staleTokens         int64
	staleRead, staleIn  float64
	missingRates        map[string]bool
	initialTokens       int64
	hasTaskStart        bool
	written             map[string]string
	writtenBase         map[string]string
	userSource, userCat string
}

// composer collects context composition for a Claude session.
type composer struct {
	prices     *PriceTable
	session    string
	agents     []*compAgent
	links      map[string]*agentLink // subagent id -> Agent call
	linkOrder  int
	briefBytes int
}

func newComposer(prices *PriceTable, session string) *composer {
	return &composer{prices: prices, session: session, links: make(map[string]*agentLink)}
}

func (c *composer) addAgent(agent Agent, main bool) *compAgent {
	a := &compAgent{id: agent.AgentID, stem: strings.TrimPrefix(agent.AgentID, "agent-"), main: main,
		calls: make(map[string]toolCall), repeats: make(map[string]*repeatGroup), missingRates: make(map[string]bool),
		written: make(map[string]string), writtenBase: make(map[string]string)}
	a.userSource, a.userCat = "user_turn", catUserTurn
	if !main {
		a.userSource, a.userCat = "agent_brief", catAgentHandoff
	}
	c.agents = append(c.agents, a)
	return a
}

func (a *compAgent) add(b *block) *block {
	b.phase = a.phase
	a.blocks = append(a.blocks, b)
	a.pending = append(a.pending, b)
	return b
}

// compContent is one item of a message content array.
type compContent struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     jsontext.Value `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
	Content   jsontext.Value `json:"content"`
}

type compLine struct {
	SessionID        string         `json:"sessionId"`
	IsMeta           bool           `json:"isMeta"`
	IsCompactSummary bool           `json:"isCompactSummary"`
	SourceToolUseID  string         `json:"sourceToolUseID"`
	Subtype          string         `json:"subtype"`
	ToolUseResult    jsontext.Value `json:"toolUseResult"`
	Message          *struct {
		Content jsontext.Value `json:"content"`
	} `json:"message"`
	Attachment *struct {
		Type string `json:"type"`
	} `json:"attachment"`
	Rendered []struct {
		Content string `json:"content"`
	} `json:"rendered"`
}

func contentItems(raw jsontext.Value) (text string, items []compContent, isText bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	switch raw.Kind() {
	case '"':
		if json.Unmarshal(raw, &text, lenient) == nil {
			return text, nil, true
		}
	case '[':
		_ = json.Unmarshal(raw, &items, lenient)
	}
	return "", items, false
}

func resultBytes(raw jsontext.Value) int64 {
	text, items, isText := contentItems(raw)
	if isText {
		return int64(len(text))
	}
	var n int64
	for _, item := range items {
		if item.Type == "text" {
			n += int64(len(item.Text))
		}
	}
	return n
}

// request processes a newly recorded request of agent a.
func (c *composer) request(a *compAgent, key, model string, r request) {
	total := r.uncached + r.write5m + r.write1h + r.writeUnsplit + r.read
	a.requests++
	k := a.requests
	if k == 1 {
		a.firstModel = model
		a.firstTurn = a.pending
		a.initial = &block{source: sourceInitialPart, category: catFixedPrefix, initial: true, tokens: total, newTokens: total, request: 1, resident: true}
		a.initialTokens = total
		a.resident = []*block{a.initial}
	} else {
		var resident int64
		for _, b := range a.resident {
			resident += b.tokens
		}
		delta := total - resident
		var visible []*block
		var visibleBytes int64
		for _, b := range a.pending {
			if b.visible && b.bytes > 0 {
				visible = append(visible, b)
				visibleBytes += b.bytes
			}
		}
		out := a.prevOut
		if a.out == nil {
			out = 0
		}
		switch {
		case delta >= out:
			if a.out != nil {
				a.out.tokens = out
			}
			rest := delta - out
			if rest > 0 {
				if visibleBytes > 0 {
					weights := make([]int64, len(visible))
					for i, b := range visible {
						weights[i] = b.bytes
					}
					for i, share := range largestRemainder(rest, weights) {
						visible[i].tokens = share
					}
					a.samples = append(a.samples, float64(visibleBytes)/float64(rest))
				} else {
					u := a.add(&block{source: "unrecorded", category: catUnrecorded, tokens: rest})
					a.unrecorded += rest
					_ = u
				}
			}
		case delta >= 0:
			if a.out != nil {
				a.out.tokens = delta
			}
		default:
			a.reduce(-delta)
		}
		for _, b := range a.pending {
			b.newTokens = b.tokens
			b.request = k
			b.resident = true
			a.resident = append(a.resident, b)
		}
	}
	a.account(c.prices, model, r, k)
	a.pending = nil
	a.prevOut = r.output
	a.lastKey = key
	a.out = a.add(&block{source: "assistant_output", category: catAssistantOutput})
}

// reduce removes tokens from resident blocks, non-initial ones first.
func (a *compAgent) reduce(tokens int64) {
	a.reductions.Events++
	a.reductions.Tokens += tokens
	var later, early []*block
	for _, b := range a.resident {
		if b.initial {
			early = append(early, b)
		} else {
			later = append(later, b)
		}
	}
	for _, group := range [][]*block{later, early} {
		if tokens <= 0 {
			return
		}
		var sum int64
		weights := make([]int64, len(group))
		for i, b := range group {
			weights[i] = b.tokens
			sum += b.tokens
		}
		take := min(tokens, sum)
		for i, share := range largestRemainder(take, weights) {
			group[i].tokens -= share
		}
		tokens -= take
	}
}

func (a *compAgent) compact() {
	a.compactions++
	for _, b := range a.resident {
		if !b.initial {
			b.resident = false
			b.tokens = 0
		}
	}
	if a.initial != nil {
		a.resident = []*block{a.initial}
	}
	for _, b := range a.pending {
		b.resident = false
	}
	a.pending = nil
	a.out = nil
	a.prevOut = 0
}

type segment struct {
	tokens int64
	rate   float64
}

// account adds one request's resident tokens and dollars to its blocks.
func (a *compAgent) account(prices *PriceTable, model string, r request, k int64) {
	rates := modelRates(prices, model)
	rate := func(name string, value *float64, tokens int64) float64 {
		if value == nil {
			if tokens > 0 {
				a.missingRates[name+" for "+model] = true
			}
			return 0
		}
		return *value
	}
	unsplit := 0.0
	if r.writeUnsplit > 0 {
		if rates.CacheWrite5mPerMillion != nil && rates.CacheWrite1hPerMillion != nil && *rates.CacheWrite5mPerMillion == *rates.CacheWrite1hPerMillion {
			unsplit = *rates.CacheWrite5mPerMillion
		} else {
			a.missingRates["equal 5m/1h cache write rates for "+model] = true
		}
	}
	read := rate("cache_read_per_million", rates.CacheReadPerMillion, r.read)
	readRate := 0.0
	if rates.CacheReadPerMillion != nil {
		readRate = *rates.CacheReadPerMillion
	}
	segments := []segment{
		{r.read, read},
		{r.write1h, rate("cache_write_1h_per_million", rates.CacheWrite1hPerMillion, r.write1h)},
		{r.write5m, rate("cache_write_5m_per_million", rates.CacheWrite5mPerMillion, r.write5m)},
		{r.writeUnsplit, unsplit},
		{r.uncached, rate("input_per_million", rates.InputPerMillion, r.uncached)},
	}
	a.outputTokens += r.output
	a.outputDollars += float64(r.output) / 1e6 * rate("output_per_million", rates.OutputPerMillion, r.output)
	index := 0
	for _, b := range a.resident {
		t := b.tokens
		a.attributed += t
		dollars := 0.0
		for remaining := t; remaining > 0 && index < len(segments); {
			if segments[index].tokens == 0 {
				index++
				continue
			}
			used := min(remaining, segments[index].tokens)
			dollars += float64(used) / 1e6 * segments[index].rate
			segments[index].tokens -= used
			remaining -= used
		}
		b.inputTokens += t
		b.inputDollars += dollars
		a.inputDollars += dollars
		if b.request < k {
			b.rereadTokens += t
			b.rereadDollars += float64(t) / 1e6 * readRate
			b.laterRequests++
		}
		// Stale: a re-read (not the first read) of an earlier phase's block.
		if !b.initial && b.phase < a.phase && b.request < k {
			a.staleTokens += t
			a.staleRead += float64(t) / 1e6 * readRate
			a.staleIn += dollars
		}
	}
	// Tokens beyond the resident blocks (never expected) are still priced.
	for ; index < len(segments); index++ {
		a.inputDollars += float64(segments[index].tokens) / 1e6 * segments[index].rate
	}
	if rates.CacheReadPerMillion == nil {
		var later int64
		for _, b := range a.resident {
			if b.request < k {
				later += b.tokens
			}
		}
		if later > 0 {
			a.missingRates["cache_read_per_million for "+model] = true
		}
	}
	// Blocks with no tokens left add nothing to later requests; dropping
	// them keeps each request's walk to the blocks that are in context.
	a.resident = slices.DeleteFunc(a.resident, func(b *block) bool { return b.tokens == 0 && !b.initial })
}

func modelRates(prices *PriceTable, model string) ModelRates {
	if prices == nil {
		return ModelRates{}
	}
	return prices.Models[model]
}

// assistant handles one assistant line; recorded reports whether it started
// a new request of this agent, and history whether the line is another
// session's, copied into a resumed one (context, but it sets no boundary).
func (c *composer) assistant(a *compAgent, line []byte, key, model string, r request, recorded, history bool) {
	if recorded {
		c.request(a, key, model, r)
	}
	var wire struct {
		Message *struct {
			Content jsontext.Value `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &wire, lenient) != nil || wire.Message == nil {
		return
	}
	text, items, isText := contentItems(wire.Message.Content)
	target := a.out
	if !recorded && (key == "" || key != a.lastKey) || target == nil {
		// Content outside a recorded request, such as history copied into
		// a resumed session, is visible context.
		target = a.add(&block{source: "assistant_output", category: catAssistantOutput, visible: true})
	}
	if isText {
		target.bytes += int64(len(text))
		return
	}
	for _, item := range items {
		switch item.Type {
		case "text":
			target.bytes += int64(len(item.Text))
		case "tool_use":
			target.bytes += int64(len(item.Name) + len(item.Input))
			c.toolUse(a, item, history)
		}
	}
}

func (c *composer) toolUse(a *compAgent, item compContent, history bool) {
	var input map[string]any
	_ = json.Unmarshal(item.Input, &input, lenient)
	str := func(name string) string {
		value, _ := input[name].(string)
		return value
	}
	call := toolCall{name: item.Name, source: "tool:" + item.Name, category: catToolOutput}
	if call.name == "" {
		call.source = "tool:unknown"
	}
	switch item.Name {
	case "Bash":
		command := str("command")
		call.label = labelText(command)
		shell := parseShell(command)
		if source, ok := shell.cwSource(); ok {
			call.source, call.category = source, catCwOutput
		} else if ref, ok := shell.reference(); ok {
			call.source, call.category = ref, catReferenceRead
		}
		c.recordWrites(a, shell)
		if !history {
			c.boundaries(a, shell)
		}
	case "Read":
		call.label = labelText(str("file_path"))
		if isReferencePath(str("file_path")) {
			call.source, call.category = referenceSource(str("file_path")), catReferenceRead
		}
	case "Grep", "Glob":
		call.label = labelText(str("pattern"))
	case "Skill":
		call.label = labelText(str("skill"))
		call.source, call.category = "skill:"+str("skill"), catSkillLoad
	case "Agent", "Task":
		call.label = labelText(str("description"))
		call.source, call.category = "agent_result", catAgentHandoff
		link := &agentLink{description: str("description"), subagentType: str("subagent_type"), order: c.linkOrder, brief: a.briefNamedIn(str("prompt"))}
		c.linkOrder++
		c.links["call:"+item.ID] = link
	case "Write":
		c.remember(a, str("file_path"), str("content"))
	}
	call.key = callKey(item.Name, item.Input)
	group := a.repeats[call.key]
	if group == nil {
		group = &repeatGroup{tool: item.Name, hash: strings.TrimPrefix(call.key, item.Name+"\x00"), label: call.label, order: len(a.repeats)}
		a.repeats[call.key] = group
	}
	group.calls++
	a.calls[item.ID] = call
}

// remember keeps a written file's text when it looks like a cw task brief, so
// a later cw task assign --input can be resolved.
func (c *composer) remember(a *compAgent, file, text string) {
	if file == "" || text == "" || len(text) > maxBriefBytes || c.briefBytes+len(text) > maxBriefTotal || !briefRolePattern.MatchString(text) {
		return
	}
	c.briefBytes += len(text)
	a.written[file] = text
	a.writtenBase[path.Base(file)] = text
}

// recordWrites remembers briefs written by heredocs (cat > FILE <<EOF).
func (c *composer) recordWrites(a *compAgent, shell shellCommand) {
	for _, doc := range shell.heredocs {
		if doc.target != "" {
			c.remember(a, doc.target, doc.body)
		}
	}
}

// boundaries records cw task phase boundaries, at most one per kind per
// command, and the briefs of cw task assign calls.
func (c *composer) boundaries(a *compAgent, shell shellCommand) {
	seen := make(map[string]bool)
	for _, call := range shell.calls {
		if !call.isCw() || len(call.fields) < 3 || call.fields[1] != "task" {
			continue
		}
		kind := ""
		switch call.fields[2] {
		case "start":
			kind = "task_start"
			a.hasTaskStart = true
		case "consult":
			kind = "task_consult"
		case "verify":
			kind = "task_verify"
		case "assign":
			b := a.briefFor(shell, call)
			if b != nil {
				a.briefs = append(a.briefs, b)
			}
			if b != nil && b.role == "review" {
				kind = "review_assign"
			}
		}
		if kind != "" && !seen[kind] {
			seen[kind] = true
			a.boundaries = append(a.boundaries, boundaryEvent{kind: kind, label: labelText(call.text), request: a.requests + 1})
			a.phase++
		}
	}
}

// briefFor resolves an assign call's brief: its --input file written earlier
// (by Write or a heredoc; exact path, else base name), or without a file a
// heredoc or inline JSON on the assign's own line.
func (a *compAgent) briefFor(shell shellCommand, call shellCall) *brief {
	file := ""
	for i, field := range call.fields {
		if value, ok := strings.CutPrefix(field, "--input="); ok {
			file = value
		} else if field == "--input" && i+1 < len(call.fields) {
			file = call.fields[i+1]
		}
	}
	text := ""
	switch {
	case file == "" || file == "-" || file == "/dev/stdin":
		for _, doc := range shell.heredocs {
			if !doc.redirect && doc.line >= call.line && doc.line <= call.last && briefRolePattern.MatchString(doc.body) {
				text = doc.body
			}
		}
		if text == "" && briefRolePattern.MatchString(call.raw) {
			text = call.raw // a brief given inline on the assign's own line
		}
	default:
		if written, ok := a.written[file]; ok {
			text = written
		} else if written, ok := a.writtenBase[path.Base(file)]; ok {
			text = written
		}
	}
	if text == "" {
		return nil
	}
	b := &brief{text: text}
	for _, match := range briefRolePattern.FindAllStringSubmatch(text, -1) {
		if b.role == "" || match[1] == "review" {
			b.role = match[1]
		}
	}
	if match := briefIDPattern.FindStringSubmatch(text); match != nil {
		b.id = match[1]
	}
	return b
}

// briefNamedIn returns the assign brief whose assignment id the Agent prompt
// names as a whole word (the longest such id; of equal lengths the latest
// brief), or nil.
func (a *compAgent) briefNamedIn(prompt string) *brief {
	var best *brief
	for _, b := range a.briefs {
		if b.id != "" && (best == nil || len(b.id) >= len(best.id)) && containsWord(prompt, b.id) {
			best = b
		}
	}
	return best
}

func containsWord(text, word string) bool {
	isWord := func(r byte) bool {
		return r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
	}
	for offset := 0; ; {
		i := strings.Index(text[offset:], word)
		if i < 0 {
			return false
		}
		start, end := offset+i, offset+i+len(word)
		if (start == 0 || !isWord(text[start-1])) && (end == len(text) || !isWord(text[end])) {
			return true
		}
		offset = start + 1
	}
}

// user handles a user line: prompts, tool results and skill bodies.
func (c *composer) user(a *compAgent, line []byte) {
	var wire compLine
	if json.Unmarshal(line, &wire, lenient) != nil || wire.Message == nil {
		return
	}
	text, items, isText := contentItems(wire.Message.Content)
	var source, category, label string
	switch call, ok := a.calls[wire.SourceToolUseID]; {
	case wire.SourceToolUseID != "" && ok && call.category == catSkillLoad:
		source, category, label = call.source, call.category, call.label
	case wire.IsCompactSummary:
		source, category = "compaction_summary", catContext
	case wire.IsMeta:
		source, category = "context:meta", catContext
	default:
		source, category = a.userSource, a.userCat
	}
	if isText {
		a.add(&block{source: source, category: category, label: label, bytes: int64(len(text)), visible: true})
		return
	}
	var prompt *block
	for _, item := range items {
		switch item.Type {
		case "tool_result":
			call, ok := a.calls[item.ToolUseID]
			if !ok {
				call = toolCall{source: "tool:unknown", category: catToolOutput}
			}
			a.add(&block{source: call.source, category: call.category, label: call.label, bytes: resultBytes(item.Content), visible: true, callKey: call.key})
			if call.category == catAgentHandoff && len(wire.ToolUseResult) > 0 {
				var result struct {
					AgentID string `json:"agentId"`
				}
				if json.Unmarshal(wire.ToolUseResult, &result, lenient) == nil && result.AgentID != "" {
					if link, ok := c.links["call:"+item.ToolUseID]; ok {
						if _, seen := c.links[result.AgentID]; !seen {
							c.links[result.AgentID] = link
						}
					}
				}
			}
		case "text":
			if prompt == nil {
				prompt = a.add(&block{source: source, category: category, label: label, visible: true})
			}
			prompt.bytes += int64(len(item.Text))
		}
	}
}

// attachment handles an attachment line with rendered text.
func (c *composer) attachment(a *compAgent, line []byte) {
	var wire compLine
	if json.Unmarshal(line, &wire, lenient) != nil || wire.Attachment == nil {
		return
	}
	var size int64
	agentMessage, notification := false, false
	for _, part := range wire.Rendered {
		size += int64(len(part.Content))
		if strings.Contains(part.Content, "<agent-message") || (strings.Contains(part.Content, "<task-notification") && agentNotification.MatchString(part.Content)) {
			agentMessage = true
		} else if strings.Contains(part.Content, "<task-notification") {
			notification = true
		}
	}
	if size == 0 {
		return
	}
	kind := wire.Attachment.Type
	b := &block{bytes: size, visible: true, label: kind}
	switch {
	case kind == "skill_listing":
		b.source, b.category = "skill_listing", catFixedPrefix
	case kind == "instructions" || kind == "nested_memory":
		b.source, b.category = "instructions", catFixedPrefix
	case kind == "queued_command" && agentMessage:
		b.source, b.category = "agent_result", catAgentHandoff
	case kind == "queued_command" && notification:
		b.source, b.category = "context:task_notification", catContext
	default:
		b.source, b.category = "context:"+kind, catContext
		if a.requests == 0 {
			b.category = catFixedPrefix
		}
	}
	a.add(b)
}

func (c *composer) system(a *compAgent, line []byte) {
	var wire compLine
	if json.Unmarshal(line, &wire, lenient) != nil || wire.Subtype != "compact_boundary" {
		return
	}
	if wire.SessionID != c.session || a.requests == 0 {
		// A compaction inside copied history: what preceded it is not in
		// this session's context, and it is not this session's compaction.
		for _, b := range a.pending {
			b.resident = false
		}
		a.pending = nil
		return
	}
	a.compact()
}

// cwCommands are the cw manager's top-level commands; another word after a
// path ending in /cw is not a cw call.
var cwCommands = map[string]bool{"consult": true, "doctor": true, "graphify": true, "help": true, "install": true,
	"metrics": true, "recover": true, "rollback": true, "setup": true, "status": true, "task": true,
	"uninstall": true, "update": true, "version": true}

var readPrograms = map[string]bool{"cat": true, "sed": true, "head": true, "tail": true, "less": true, "nl": true}

// shellCall is one simple command: its words with quoted text that contains
// whitespace replaced by "_", and the logical line it is on.
type shellCall struct {
	fields []string
	text   string
	raw    string // the whole logical line, quotes kept
	line   int    // first and last physical (non-heredoc) line of that logical line
	last   int
}

type heredoc struct {
	line         int
	target, body string
	redirect     bool // a redirected line rather than a heredoc body
}

// shellCommand is a Bash tool command split into simple commands. Heredoc
// bodies are kept apart, so text inside them, inside quoted strings or after
// another program's arguments is never taken for a cw call.
type shellCommand struct {
	calls    []shellCall
	heredocs []heredoc
}

func (call shellCall) program() string {
	if len(call.fields) == 0 {
		return ""
	}
	return call.fields[0]
}

func (call shellCall) isCw() bool {
	program := call.program()
	return program == "cw" || strings.HasSuffix(program, "/cw") || program == "$CW" || program == "${CW}"
}

// parseShell approximates shell word splitting well enough to find which
// programs a command runs. It is not a shell parser: unusual quoting can
// still be misread, but text in heredoc bodies and quoted strings is ignored.
func parseShell(command string) shellCommand {
	shell, _ := splitShell(command)
	return shell
}

// splitShell is parseShell that also returns its work: the bytes it passed
// through its quote scanner, the bytes of each line it searched for a
// redirect target, and the quote-nesting frames that any step copied or
// searched. The work must stay linear in len(command): the quote state at
// each "<<" of a line carries on from the previous one and is never rescanned
// from the line's start, a line is searched for a redirect at most once, and
// no per-line or per-marker step may copy or search the whole nesting.
func splitShell(command string) (shellCommand, int) {
	var shell shellCommand
	work := 0
	lines := strings.Split(strings.ReplaceAll(command, "\\\n", " "), "\n")
	var kept []string
	quotes := quoteScanner{work: &work} // the quoting state after the kept lines
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		var markers [][2]int
		within, scanned := quotes.clone(), 0
		target, targetKnown := "", false
		for from := 0; ; {
			at := strings.Index(line[from:], "<<")
			if at < 0 {
				break
			}
			at += from
			from = at + 1
			// <<< is a here-string; << inside a quoted string (but not in a
			// $(...) or `...` substitution within one) is text, and inside
			// $((...)) it is a shift. The quote state is only needed when the
			// << is not a here-string, and the marker only matched outside
			// quotes. quoted and arithmetic take constant time, so the order
			// of the marker and arithmetic checks below does not affect cost.
			if at > 0 && line[at-1] == '<' {
				continue
			}
			within.feed(line[scanned:at])
			scanned = at
			if within.quoted() {
				continue
			}
			end, delimiter, ok := heredocMarker(line, at)
			if !ok || within.arithmetic() {
				continue
			}
			from = end
			markers = append(markers, [2]int{at, end})
			var body []string
			for i+1 < len(lines) {
				i++
				if strings.TrimSpace(lines[i]) == delimiter {
					break
				}
				body = append(body, lines[i])
			}
			if !targetKnown { // once per line: the line's first redirect
				work += len(line)
				if match := redirectPattern.FindStringSubmatch(line); match != nil {
					target = strings.Trim(cmp.Or(match[1], match[2]), `"'`)
				}
				targetKnown = true
			}
			doc := heredoc{line: len(kept), body: strings.Join(body, "\n"), target: target}
			shell.heredocs = append(shell.heredocs, doc)
		}
		if len(markers) == 0 {
			// The line is kept as it is, so the scan of its start carries on.
			within.feed(line[scanned:])
			within.feed("\n")
			quotes = within
			kept = append(kept, line)
			continue
		}
		var b strings.Builder
		from := 0
		for _, m := range markers {
			b.WriteString(line[from:m[0]])
			from = m[1]
		}
		b.WriteString(line[from:])
		line = b.String()
		kept = append(kept, line)
		quotes.feed(line)
		quotes.feed("\n")
	}
	for _, logical := range splitLogical(strings.Join(kept, "\n")) {
		// A line that redirects to a file (jq -n '{...}' > FILE) may write a brief.
		if targets := redirectTarget.FindAllStringSubmatch(logical.words, -1); targets != nil {
			shell.heredocs = append(shell.heredocs, heredoc{line: logical.first, target: targets[len(targets)-1][1], body: logical.raw, redirect: true})
		}
		for _, segment := range separatorPattern.Split(logical.words, -1) {
			fields := commandFields(strings.Fields(segment))
			if len(fields) > 0 {
				shell.calls = append(shell.calls, shellCall{fields: fields, text: strings.Join(fields, " "), raw: logical.raw, line: logical.first, last: logical.last})
			}
		}
	}
	return shell, work
}

// heredocMarker matches a heredoc marker at line[at:], which starts with
// "<<": an optional "-", blanks, an optional quote, the delimiter word and an
// optional quote, the shape of <<-?\s*(['"]?)([A-Za-z_][A-Za-z0-9_]*)['"]?.
// It reads only the marker's own bytes, so finding every marker of a line
// stays linear.
func heredocMarker(line string, at int) (end int, delimiter string, ok bool) {
	i := at + 2
	if i < len(line) && line[i] == '-' {
		i++
	}
	for i < len(line) && strings.IndexByte(" \t\n\f\r", line[i]) >= 0 {
		i++
	}
	if i < len(line) && (line[i] == '\'' || line[i] == '"') {
		i++
	}
	start := i
	for i < len(line) && isDelimiterByte(line[i], i == start) {
		i++
	}
	if i == start {
		return 0, "", false
	}
	delimiter = line[start:i]
	if i < len(line) && (line[i] == '\'' || line[i] == '"') {
		i++
	}
	return i, delimiter, true
}

// isDelimiterByte reports whether ch can be part of a heredoc delimiter word
// ([A-Za-z_] first, then [A-Za-z0-9_]).
func isDelimiterByte(ch byte, first bool) bool {
	return ch == '_' || 'A' <= ch && ch <= 'Z' || 'a' <= ch && ch <= 'z' || !first && '0' <= ch && ch <= '9'
}

// commandFields drops what precedes a command word: assignments, compound
// command keywords (if, then, do, ...) and wrappers that run their arguments
// (env, time, nohup, sudo, nice, timeout DURATION, ...) with their options.
func commandFields(fields []string) []string {
	for len(fields) > 0 {
		word := fields[0]
		switch {
		case assignmentPattern.MatchString(word), shellKeywords[word]:
			fields = fields[1:]
		case wrapperValueFlags[word] != nil:
			values := wrapperValueFlags[word]
			fields = fields[1:]
			for len(fields) > 0 && strings.HasPrefix(fields[0], "-") && fields[0] != "-" {
				flag := fields[0]
				fields = fields[1:]
				if flag == "--" {
					break
				}
				if values[flag] && len(fields) > 0 {
					fields = fields[1:] // the flag's value
				}
			}
			if word == "timeout" && len(fields) > 0 {
				fields = fields[1:] // the duration
			}
		default:
			return fields
		}
	}
	return fields
}

var shellKeywords = map[string]bool{
	"!": true, "if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true,
}

// wrapperValueFlags lists the wrappers that run their arguments and, for each,
// the options that take the next word as their value.
var wrapperValueFlags = map[string]map[string]bool{
	"env":     {"-u": true, "-C": true, "-S": true, "--unset": true, "--chdir": true, "--split-string": true},
	"time":    {"-o": true, "-f": true, "--output": true, "--format": true},
	"exec":    {"-a": true},
	"command": {},
	"builtin": {},
	"nohup":   {},
	"nice":    {"-n": true, "--adjustment": true},
	"sudo":    {"-u": true, "-g": true, "-C": true, "-D": true, "-h": true, "-p": true, "-r": true, "-t": true, "-U": true, "-T": true, "--user": true, "--group": true, "--chdir": true, "--host": true, "--prompt": true, "--role": true, "--type": true, "--other-user": true, "--command-timeout": true},
	"timeout": {"-k": true, "-s": true, "--kill-after": true, "--signal": true},
}

// logicalLine is one shell line; a quoted string may span several physical
// lines (first..last). words has quotes removed: a quoted string without
// whitespace keeps its text, one with whitespace becomes "_" so its words
// cannot be commands. Comments are dropped.
type logicalLine struct {
	words, raw  string
	first, last int
}

// quoteScanner follows shell quoting through text fed to it in pieces, in
// one pass: '...', "...", $'...' (with backslash escapes), $(...), (...),
// $((...)), ((...)) and `...` (whose text is unquoted, even within double
// quotes), backslash escapes (an escaped byte opens nothing) and comments.
type quoteScanner struct {
	frames   *quoteFrame // the innermost open frame; nil outside any
	prev     byte        // the previous byte, 0 at the start
	escaped  bool        // the next byte is escaped
	comment  bool        // inside a comment, until the newline
	opened   bool        // the previous byte opened a $( or ( frame
	closeArg bool        // the previous byte closed a $((...)); its second ) is due
	work     *int        // counts the bytes fed to this scanner and its clones; nil: strayScanBytes
}

// quoteFrame is one open quote or parenthesis, linked to the frame it opened
// in. Frames are never changed once made, so a scanner and its clones share
// them: a clone takes constant time however deep the nesting is.
type quoteFrame struct {
	up    *quoteFrame
	ch    byte // ' " C ($'...') S ($( ) ( A ($(( ) `
	arith int  // the A frames at or below this one
}

// strayScanBytes counts the bytes fed to quote scanners that no splitShell
// call counts (a zero quoteScanner, or one whose work was cleared). Splitting
// never adds to it; tests check that it stays zero, so no scan escapes the
// work count.
var strayScanBytes atomic.Int64

// strayScanWork reports strayScanBytes.
func strayScanWork() int64 { return strayScanBytes.Load() }

// count adds n to the scanner's work, or to strayScanBytes when it has none.
func (q *quoteScanner) count(n int) {
	if q.work != nil {
		*q.work += n
	} else {
		strayScanBytes.Add(int64(n))
	}
}

// clone copies the scanner. The copy shares the immutable frames, so it
// copies no frame and adds no work.
func (q *quoteScanner) clone() quoteScanner { return *q }

func (q *quoteScanner) top() byte {
	if q.frames == nil {
		return 0
	}
	return q.frames.ch
}

// push opens a frame inside the current one.
func (q *quoteScanner) push(ch byte) {
	arith := 0
	if q.frames != nil {
		arith = q.frames.arith
	}
	if ch == 'A' {
		arith++
	}
	q.frames = &quoteFrame{up: q.frames, ch: ch, arith: arith}
}

// replace turns the innermost frame into ch. It makes a new frame rather than
// changing the old one, which a clone may share. Changing it in place would
// give the same results today, so no test can tell the two apart: a frame is
// replaced only by the byte right after the one that pushed it, and splitShell
// clones only after a newline, so the frame is never shared yet. Keeping every
// frame immutable does not depend on that.
func (q *quoteScanner) replace(ch byte) {
	q.pop()
	q.push(ch)
}

func (q *quoteScanner) pop() { q.frames = q.frames.up }

// quoted reports whether the text so far ends inside a quoted string.
func (q *quoteScanner) quoted() bool {
	top := q.top()
	return top == '\'' || top == '"' || top == 'C'
}

// arithmetic reports whether the text so far ends inside $((...)). Each frame
// records how many A frames lie at or below it, so this searches no frame and
// adds no work.
func (q *quoteScanner) arithmetic() bool { return q.frames != nil && q.frames.arith > 0 }

func (q *quoteScanner) feed(text string) {
	q.count(len(text))
	for i := 0; i < len(text); i++ {
		ch := text[i]
		opened := false
		if q.closeArg {
			// The second ) of $((...)) or ((...)), whatever the context.
			q.closeArg = false
			if ch == ')' {
				q.prev = ch
				continue
			}
		}
		switch {
		case q.escaped:
			q.escaped = false
			ch = '\\' // an escaped byte opens nothing: \$( is no substitution
		case q.comment:
			q.comment = ch != '\n'
		case q.top() == '\'':
			if ch == '\'' {
				q.pop()
			}
		case q.top() == 'C':
			switch ch {
			case '\\':
				q.escaped = true
			case '\'':
				q.pop()
			}
		case q.top() == '"':
			switch {
			case ch == '\\':
				q.escaped = true
			case ch == '"':
				q.pop()
			case ch == '(' && q.prev == '$':
				q.push('S')
				opened = true
			case ch == '`':
				q.push('`')
			}
		default:
			switch {
			case ch == '\\':
				q.escaped = true
			case ch == '\'' && q.prev == '$':
				q.push('C')
			case ch == '\'' || ch == '"':
				q.push(ch)
			case ch == '#' && (q.prev == 0 || strings.IndexByte(" \t\n;&|()", q.prev) >= 0):
				q.comment = true
			case ch == '(' && q.opened:
				q.replace('A')
			case ch == '(' && q.prev == '$':
				q.push('S')
				opened = true
			case ch == '(':
				q.push('(')
				opened = true // (( starts an arithmetic command
			case ch == ')' && q.top() == 'A':
				q.pop()
				q.closeArg = true
			case ch == ')' && (q.top() == 'S' || q.top() == '('):
				q.pop()
			case ch == '`' && q.top() == '`':
				q.pop()
			case ch == '`':
				q.push('`')
			}
		}
		q.opened = opened
		q.prev = ch
	}
}

func splitLogical(text string) []logicalLine {
	var out []logicalLine
	var words strings.Builder
	start, physical, first := 0, 0, 0
	escapedAt := -1 // index of the last byte escaped by a backslash
	flush := func(end int) {
		out = append(out, logicalLine{words: words.String(), raw: text[start:end], first: first, last: physical})
		words.Reset()
	}
	for i := 0; i < len(text); i++ {
		switch ch := text[i]; {
		case ch == '\n':
			flush(i)
			physical++
			start, first = i+1, physical
		case ch == '#' && (i == 0 || strings.ContainsRune(" \t\n;&|()", rune(text[i-1]))):
			for i+1 < len(text) && text[i+1] != '\n' {
				i++
			}
		case ch == '\\' && i+1 < len(text) && text[i+1] != '\n':
			// An escaped byte is literal: it neither quotes nor separates.
			i++
			escapedAt = i
			if strings.IndexByte(" \t;&|(){}<>#'\"`$", text[i]) >= 0 {
				words.WriteByte('_')
			} else {
				words.WriteByte(text[i])
			}
		case ch == '\'' || ch == '"':
			end := i + 1
			escapes := ch == '"' || i > 0 && text[i-1] == '$' && escapedAt != i-1 // "..." and $'...'
			for end < len(text) && text[end] != ch {
				if escapes && text[end] == '\\' {
					end++
				}
				end++
			}
			content := text[i+1 : min(end, len(text))]
			physical += strings.Count(content, "\n")
			if content == "" || strings.ContainsAny(content, " \t\n") {
				words.WriteString("_")
			} else {
				words.WriteString(content)
			}
			i = end
		default:
			words.WriteByte(ch)
		}
	}
	flush(len(text))
	return out
}

// cwSource names the first cw call, with its subcommand for the task,
// metrics and graphify groups.
func (shell shellCommand) cwSource() (string, bool) {
	for _, call := range shell.calls {
		if !call.isCw() || len(call.fields) < 2 || !cwCommands[call.fields[1]] {
			continue
		}
		source := "cw " + call.fields[1]
		if len(call.fields) > 2 && (call.fields[1] == "task" || call.fields[1] == "metrics" || call.fields[1] == "graphify") && !strings.HasPrefix(call.fields[2], "-") {
			source += " " + call.fields[2]
		}
		return source, true
	}
	return "", false
}

// reference finds a cat/sed/head/tail/less/nl call reading a reference file.
func (shell shellCommand) reference() (string, bool) {
	for _, call := range shell.calls {
		if !readPrograms[path.Base(call.program())] {
			continue
		}
		for _, field := range call.fields[1:] {
			if isReferencePath(field) {
				return referenceSource(field), true
			}
		}
	}
	return "", false
}

func isReferencePath(p string) bool {
	switch path.Ext(p) {
	case ".md", ".yaml", ".yml", ".json":
	default:
		return false
	}
	if path.Base(p) == "SKILL.md" {
		return true
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "skills" || segment == "references" {
			return true
		}
	}
	return false
}

func referenceSource(p string) string {
	parts := strings.Split(strings.TrimRight(p, "/"), "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return "reference:" + strings.Join(parts, "/")
}

func labelText(text string) string {
	text = strings.ToValidUTF8(text, "?")
	text = strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(text, "\r", " "), "\n", " ")), " ")
	if len(text) > maxLabelBytes {
		cut := maxLabelBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "..."
	}
	return text
}

func callKey(name string, input jsontext.Value) string {
	canonical := slices.Clone([]byte(input))
	value := jsontext.Value(canonical)
	if value.Canonicalize() != nil {
		value = jsontext.Value(input)
	}
	sum := sha256.Sum256(value)
	return name + "\x00" + hex.EncodeToString(sum[:])
}

// largestRemainder splits total by weights into integers that sum to total;
// ties go to the earlier index. All weights zero yields zeros.
func largestRemainder(total int64, weights []int64) []int64 {
	shares := make([]int64, len(weights))
	var sum uint64
	for _, w := range weights {
		sum += uint64(max(w, 0))
	}
	if total <= 0 || sum == 0 {
		return shares
	}
	type rem struct {
		index int
		r     uint64
	}
	remainders := make([]rem, len(weights))
	var assigned int64
	for i, w := range weights {
		hi, lo := bits.Mul64(uint64(total), uint64(max(w, 0)))
		q, r := bits.Div64(hi, lo, sum)
		shares[i] = int64(q)
		assigned += int64(q)
		remainders[i] = rem{i, r}
	}
	slices.SortStableFunc(remainders, func(x, y rem) int { return cmp.Compare(y.r, x.r) })
	for i := 0; assigned < total; i++ {
		shares[remainders[i%len(remainders)].index]++
		assigned++
	}
	return shares
}

func median(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2], true
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2, true
}

// splitInitial replaces the initial-context block by system_and_tools and the
// first-turn blocks, dividing its accumulated amounts by token share.
func (a *compAgent) splitInitial(ratio *float64) {
	init := a.initial
	if init == nil {
		return
	}
	var visible []*block
	var bytesSum int64
	for _, b := range a.firstTurn {
		if b.visible && b.bytes > 0 {
			visible = append(visible, b)
			bytesSum += b.bytes
		}
	}
	estimate := int64(0)
	if ratio != nil && *ratio > 0 && bytesSum > 0 {
		estimate = min(int64(math.Round(float64(bytesSum)/(*ratio))), init.newTokens)
	}
	weights := make([]int64, len(visible))
	for i, b := range visible {
		weights[i] = b.bytes
	}
	system := &block{source: "system_and_tools", category: catFixedPrefix, label: "system prompt and tool definitions"}
	parts := append([]*block{system}, visible...)
	tokens := append([]int64{init.newTokens - estimate}, largestRemainder(estimate, weights)...)
	inputs := largestRemainder(init.inputTokens, tokens)
	rereads := largestRemainder(init.rereadTokens, tokens)
	for i, b := range parts {
		share := 0.0
		if init.newTokens > 0 {
			share = float64(tokens[i]) / float64(init.newTokens)
		}
		b.tokens, b.newTokens, b.request, b.initial, b.phase = tokens[i], tokens[i], 1, true, 0
		b.inputTokens, b.rereadTokens, b.laterRequests = inputs[i], rereads[i], init.laterRequests
		b.inputDollars, b.rereadDollars = init.inputDollars*share, init.rereadDollars*share
	}
	a.blocks = append([]*block{system}, a.blocks...)
	for _, b := range a.firstTurn {
		if !b.visible || b.bytes == 0 {
			b.request, b.initial = 1, true
		}
	}
}

// build assembles the composition after the import report is final.
func (c *composer) build(report *Report, cost *costState) *Composition {
	comp := &Composition{Notes: []string{
		"Each request's new input (its total input minus the previous request's) is assigned to the blocks appended since then: the previous request's output gets its output_tokens (all of the new input when that is smaller) and the rest is split by byte share; new input with no visible block is 'unrecorded'.",
		"The first request is split into the visible first-turn blocks, estimated with the agent's bytes_per_token (median of its later requests, else of the session), and system_and_tools (the remainder: system prompt and tool definitions).",
		"input_dollars allocates each request's priced input along context order: cache reads first, then 1h and 5m cache writes, then uncached input. reread_dollars = tokens x later requests x cache-read rate.",
		"Stale-phase re-read counts blocks from an earlier cw task phase (boundaries: task start, task consult, review assign, task verify) still in context in a later phase, excluding the initial context; prefix_rewrite_dollars estimates re-sending the initial context at each boundary at the 1h cache-write rate.",
		"Roles: a cw task assign brief applies to the Agent call whose prompt names its assignment id; otherwise the Agent description, then the agent type; unmatched agents are 'other'. Agents without requests are left out of roles and coverage.",
		"cw calls (sources and phase boundaries) are read from command positions in Bash commands only (after separators, assignments, compound-command keywords and wrappers such as env, nohup, nice, sudo or timeout, with their options); heredoc bodies, comments and quoted text are ignored, cw run through a script or eval is not seen, and copied history in a resumed session sets no boundary.",
		"Dollars are price-table estimates (--prices); the composition is reported only when their total is within 1% of the recorded cost-state.",
	}}
	recon := &CompositionReconciliation{Tolerance: CompositionTolerance, Status: "not_compared", TranscriptInputTokens: report.Totals.TotalInputTokens}
	comp.Reconciliation = recon
	for _, a := range c.agents {
		recon.AttributedInputTokens += a.attributed
	}
	if recorded := report.Dollars.Recorded; recorded != nil {
		total := recorded.Total
		recon.RecordedTotal = &total
	}
	if table := report.Dollars.PriceTable; table != nil && table.Total != nil && table.UnattributedRecordedTotal != nil {
		total := *table.Total + *table.UnattributedRecordedTotal
		recon.PriceTableTotal = &total
	}
	if recon.RecordedTotal != nil && recon.PriceTableTotal != nil {
		recordedTotal, priceTotal := *recon.RecordedTotal, *recon.PriceTableTotal
		switch {
		case recordedTotal > 0:
			difference := math.Abs(recordedTotal-priceTotal) / recordedTotal
			recon.RelativeDifference = &difference
			recon.Status = "within_tolerance"
			if difference > CompositionTolerance {
				recon.Status = "exceeds_tolerance"
			}
		case priceTotal == 0:
			zero := 0.0
			recon.RelativeDifference = &zero
			recon.Status = "within_tolerance"
		default:
			recon.Status = "exceeds_tolerance"
		}
	}
	if reason := c.withheldReason(report, cost, recon); reason != "" {
		comp.Status, comp.Reason = CompositionWithheld, reason
		return comp
	}
	comp.Status = CompositionReported
	c.fill(comp, report)
	return comp
}

func (c *composer) withheldReason(report *Report, cost *costState, recon *CompositionReconciliation) string {
	if report.Status != StatusComplete {
		return "the import is incomplete (" + strings.Join(report.IncompleteReasons, ", ") + "); the composition is withheld"
	}
	if cost == nil || report.Dollars.Recorded == nil {
		return "the transcript has no recorded cost-state, so the composition cannot be reconciled and is withheld"
	}
	if cost.HasUnknownModelCost != nil && *cost.HasUnknownModelCost {
		return "cost-state reports hasUnknownModelCost=true, so the recorded total is incomplete and the composition is withheld"
	}
	// Recorded tokens above the transcript's are requests the transcript does
	// not carry (background calls); the 1% dollar check bounds them. Transcript
	// tokens the recorded cost lacks mean the two describe different requests.
	var mismatched []string
	for _, item := range report.Dollars.Recorded.Reconciliation {
		exceeds := item.Status == "transcript_only"
		for _, delta := range item.TranscriptMinusRecorded {
			exceeds = exceeds || delta > 0
		}
		if exceeds {
			mismatched = append(mismatched, item.Model)
		}
	}
	if len(mismatched) > 0 {
		return "the transcript has tokens the recorded cost-state does not cover for model(s) " + strings.Join(mismatched, ", ") + "; the composition is withheld"
	}
	var missing []string
	for _, a := range c.agents {
		for name := range a.missingRates {
			missing = append(missing, name)
		}
	}
	if recon.PriceTableTotal == nil || len(missing) > 0 {
		slices.Sort(missing)
		missing = slices.Compact(missing)
		reason := "the price-table total is unknown"
		if table := report.Dollars.PriceTable; table != nil && table.TotalUnknownReason != "" {
			reason += " (" + table.TotalUnknownReason + ")"
		} else if table != nil && table.UnattributedRecordedUnknownReason != "" {
			reason += " (" + table.UnattributedRecordedUnknownReason + ")"
		}
		if len(missing) > 0 {
			reason += "; missing rates: " + strings.Join(missing, ", ")
		}
		return reason + "; the composition is withheld"
	}
	if recon.Status != "within_tolerance" {
		return "the price-table total differs from the recorded cost by more than 1%; the composition is withheld"
	}
	if recon.AttributedInputTokens != recon.TranscriptInputTokens {
		return "attributed input tokens differ from the transcript's input tokens; the composition is withheld"
	}
	return ""
}

func (c *composer) fill(comp *Composition, report *Report) {
	var all []float64
	for _, a := range c.agents {
		all = append(all, a.samples...)
	}
	sessionRatio, sessionKnown := median(all)
	roles := c.roles(report)
	var agents []AgentComposition
	roleTotals := make(map[string]*RoleComposition)
	categoryTotals := make(map[string]*CategoryComposition)
	coverage := &RoleCoverage{}
	var sessionInput int64
	for i, a := range c.agents {
		var ratio *float64
		if value, ok := median(a.samples); ok {
			ratio = &value
		} else if sessionKnown {
			ratio = &sessionRatio
		}
		a.splitInitial(ratio)
		role := roles[i]
		item := AgentComposition{AgentID: a.id, Role: role.role, RoleBasis: role.basis, Requests: a.requests,
			OutputTokens: a.outputTokens, InputDollars: a.inputDollars, OutputDollars: a.outputDollars,
			BytesPerToken: ratio, InitialContextTokens: a.initialTokens, UnrecordedTokens: a.unrecorded,
			ContextReductions: a.reductions, Compactions: a.compactions}
		sources := make(map[string]*SourceComposition)
		var order []string
		resultTokens := make(map[string]int64)
		for _, b := range a.blocks {
			item.InputTokens += b.inputTokens
			s := sources[b.source]
			if s == nil {
				s = &SourceComposition{Source: b.source, Category: b.category}
				sources[b.source] = s
				order = append(order, b.source)
			}
			s.Blocks++
			s.NewTokens += b.newTokens
			s.InputTokens += b.inputTokens
			s.InputDollars += b.inputDollars
			s.RereadTokens += b.rereadTokens
			s.RereadDollars += b.rereadDollars
			if b.callKey != "" {
				resultTokens[b.callKey] += b.newTokens
			}
			cat := categoryTotals[b.category]
			if cat == nil {
				cat = &CategoryComposition{Category: b.category}
				categoryTotals[b.category] = cat
			}
			cat.InputTokens += b.inputTokens
			cat.InputDollars += b.inputDollars
			cat.RereadDollars += b.rereadDollars
		}
		for _, name := range order {
			s := sources[name]
			s.InputShare = share(s.InputTokens, item.InputTokens)
			item.Sources = append(item.Sources, *s)
		}
		slices.SortStableFunc(item.Sources, func(x, y SourceComposition) int {
			return cmp.Or(cmp.Compare(y.InputTokens, x.InputTokens), strings.Compare(x.Source, y.Source))
		})
		item.TopBlocks = topBlocks(a.blocks)
		item.RepeatedCalls = repeated(a.repeats, resultTokens)
		item.Phases = a.phases(c.prices)
		sessionInput += item.InputTokens
		agents = append(agents, item)
		if item.Requests == 0 {
			// An agent without requests (such as copied history) has no
			// cost and is left out of roles and coverage.
			continue
		}
		coverage.Agents++
		coverage.InputTokens += item.InputTokens
		if role.role != "other" {
			coverage.MappedAgents++
			coverage.MappedInputTokens += item.InputTokens
		}
		total := roleTotals[role.role]
		if total == nil {
			total = &RoleComposition{Role: role.role}
			roleTotals[role.role] = total
		}
		total.Agents++
		total.Requests += item.Requests
		total.InputTokens += item.InputTokens
		total.OutputTokens += item.OutputTokens
		total.InputDollars += item.InputDollars
		total.OutputDollars += item.OutputDollars
		total.Dollars += item.InputDollars + item.OutputDollars
	}
	var roleList []RoleComposition
	for _, name := range sortedKeys(roleTotals) {
		roleList = append(roleList, *roleTotals[name])
	}
	slices.SortStableFunc(roleList, func(x, y RoleComposition) int {
		return cmp.Or(cmp.Compare(y.Dollars, x.Dollars), strings.Compare(x.Role, y.Role))
	})
	var categories []CategoryComposition
	for _, name := range sortedKeys(categoryTotals) {
		cat := *categoryTotals[name]
		cat.InputShare = share(cat.InputTokens, sessionInput)
		categories = append(categories, cat)
	}
	slices.SortStableFunc(categories, func(x, y CategoryComposition) int {
		return cmp.Or(cmp.Compare(y.InputTokens, x.InputTokens), strings.Compare(x.Category, y.Category))
	})
	if agents == nil {
		agents = []AgentComposition{}
	}
	comp.Agents, comp.Roles, comp.Categories, comp.RoleCoverage = &agents, &roleList, &categories, coverage
}

func share(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

func topBlocks(blocks []*block) []BlockComposition {
	var list []BlockComposition
	for _, b := range blocks {
		if b.rereadTokens == 0 {
			continue
		}
		list = append(list, BlockComposition{Source: b.source, Label: b.label, Request: b.request, Tokens: b.newTokens,
			LaterRequests: b.laterRequests, RereadTokens: b.rereadTokens, RereadDollars: b.rereadDollars})
	}
	slices.SortStableFunc(list, func(x, y BlockComposition) int {
		return cmp.Or(cmp.Compare(y.RereadDollars, x.RereadDollars), cmp.Compare(x.Request, y.Request))
	})
	if len(list) > maxTopBlocks {
		list = list[:maxTopBlocks]
	}
	if list == nil {
		list = []BlockComposition{}
	}
	return list
}

func repeated(groups map[string]*repeatGroup, resultTokens map[string]int64) []RepeatedCall {
	list := []RepeatedCall{}
	for key, group := range groups {
		if group.calls < 2 {
			continue
		}
		list = append(list, RepeatedCall{Tool: group.tool, InputSHA256: group.hash, Label: group.label, Calls: group.calls, ResultTokens: resultTokens[key]})
	}
	slices.SortFunc(list, func(x, y RepeatedCall) int {
		return cmp.Or(cmp.Compare(y.ResultTokens, x.ResultTokens), cmp.Compare(y.Calls, x.Calls),
			strings.Compare(x.Tool, y.Tool), strings.Compare(x.InputSHA256, y.InputSHA256))
	})
	if len(list) > maxRepeatedCalls {
		list = list[:maxRepeatedCalls]
	}
	return list
}

func (a *compAgent) phases(prices *PriceTable) *Phases {
	if len(a.boundaries) == 0 {
		return nil
	}
	p := &Phases{PhaseCount: len(a.boundaries) + 1, StaleRereadTokens: a.staleTokens, StaleRereadDollars: a.staleRead, StaleInputDollars: a.staleIn}
	rates := modelRates(prices, a.firstModel)
	writeRate := rates.CacheWrite1hPerMillion
	if writeRate == nil {
		writeRate = rates.CacheWrite5mPerMillion
	}
	resumed := 0
	for _, event := range a.boundaries {
		boundary := PhaseBoundary{Kind: event.kind, Label: event.label}
		if event.request <= a.requests {
			request := event.request
			boundary.Request = &request
			resumed++
		}
		p.Boundaries = append(p.Boundaries, boundary)
	}
	if writeRate != nil {
		p.PrefixRewriteDollars = float64(a.initialTokens) * float64(resumed) * *writeRate / 1e6
	}
	return p
}

type roleChoice struct {
	role, basis string
}

// roles maps each agent to a role: main by its task start, subagents by their
// cw task assign brief, then description, then agent type.
func (c *composer) roles(report *Report) []roleChoice {
	type linked struct {
		index int
		link  *agentLink
	}
	choices := make([]roleChoice, len(c.agents))
	var reviews []linked
	for i, a := range c.agents {
		if a.main {
			choices[i] = roleChoice{"main", "main"}
			if a.hasTaskStart {
				choices[i].role = "coordinator"
			}
			continue
		}
		var agent Agent
		if i < len(report.Agents) {
			agent = report.Agents[i]
		}
		link := c.links[a.stem]
		description := agent.Description
		if link != nil {
			description = cmp.Or(description, link.description)
		}
		if link != nil && link.brief != nil {
			switch link.brief.role {
			case "review":
				// An agent with no requests is left out of roles and takes
				// no place in the review order.
				if a.requests == 0 {
					choices[i] = roleChoice{"review", "cw_task_assign"}
				} else {
					reviews = append(reviews, linked{i, link})
				}
				continue
			case "implementation", "evidence", "verification":
				// Native roles have no GitHub worker or test author: a commit,
				// PR or test-author description names them more precisely.
				if described := describedRole(description, ""); described.role == "github" || (described.role == "test_author" && link.brief.role == "implementation") {
					choices[i] = described
					continue
				}
				choices[i] = roleChoice{link.brief.role, "cw_task_assign"}
				if link.brief.role == "implementation" && testAuthorPattern.MatchString(link.brief.text) {
					choices[i].role = "test_author"
				}
				continue
			}
		}
		agentType := agent.AgentType
		if link != nil {
			agentType = cmp.Or(agentType, link.subagentType)
		}
		choices[i] = describedRole(description, agentType)
	}
	slices.SortStableFunc(reviews, func(x, y linked) int { return cmp.Compare(x.link.order, y.link.order) })
	for n, item := range reviews {
		choices[item.index] = roleChoice{"review", "cw_task_assign"}
		if n > 0 {
			choices[item.index].role = "review_followup"
		}
	}
	return choices
}

func describedRole(description, agentType string) roleChoice {
	d := strings.ToLower(description)
	switch {
	case d == "":
	case testAuthorPattern.MatchString(d):
		return roleChoice{"test_author", "description"}
	case strings.Contains(d, "review") && followupPattern.MatchString(d):
		return roleChoice{"review_followup", "description"}
	case strings.Contains(d, "review"):
		return roleChoice{"review", "description"}
	case githubPattern.MatchString(d):
		return roleChoice{"github", "description"}
	case implementPattern.MatchString(d):
		return roleChoice{"implementation", "description"}
	case evidencePattern.MatchString(d):
		return roleChoice{"evidence", "description"}
	}
	switch agentType {
	case "Explore", "web-fetch", "claude-code-guide":
		return roleChoice{"evidence", "agent_type"}
	case "Plan":
		return roleChoice{"planning", "agent_type"}
	case "cw-reviewer":
		return roleChoice{"review", "agent_type"}
	case "cw-test-author":
		return roleChoice{"test_author", "agent_type"}
	case "cw-github-worker":
		return roleChoice{"github", "agent_type"}
	}
	return roleChoice{"other", "none"}
}

// unsupportedComposition is the Codex result: no recorded cost exists to
// reconcile a composition with.
func unsupportedComposition(report Report) *Composition {
	if report.Status != StatusComplete {
		return &Composition{Status: CompositionWithheld,
			Reason: "the import is incomplete (" + strings.Join(report.IncompleteReasons, ", ") + "); the composition is withheld",
			Notes:  []string{}}
	}
	return &Composition{Status: CompositionUnsupported,
		Reason: "Codex rollouts record no cost, so a context composition cannot be reconciled with a recorded amount within 1% and is not produced; per-agent Codex token usage is reported above.",
		Notes:  []string{}}
}

const (
	summarySources  = 6
	summaryRepeated = 3
)

// WriteCompositionSummary prints a bounded plain-text view of a report's
// composition: reconciliation, roles, categories and per-agent top sources,
// stale-phase re-read and repeated calls.
func WriteCompositionSummary(w io.Writer, report Report) error {
	comp := report.Composition
	if comp == nil {
		_, err := fmt.Fprintln(w, "composition: not requested")
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "composition: %s", comp.Status)
	if recon := comp.Reconciliation; recon != nil && recon.RecordedTotal != nil && recon.PriceTableTotal != nil {
		fmt.Fprintf(&b, " recorded $%.4f price-table $%.4f", *recon.RecordedTotal, *recon.PriceTableTotal)
		if recon.RelativeDifference != nil {
			fmt.Fprintf(&b, " diff %.2f%%", *recon.RelativeDifference*100)
		}
	}
	b.WriteString("\n")
	if comp.Status != CompositionReported || comp.Agents == nil {
		fmt.Fprintf(&b, "reason: %s\n", comp.Reason)
		_, err := io.WriteString(w, b.String())
		return err
	}
	b.WriteString("roles:\n")
	for _, role := range *comp.Roles {
		fmt.Fprintf(&b, "  %-16s agents=%d requests=%d input=%d $%.4f (input $%.4f, output $%.4f)\n",
			role.Role, role.Agents, role.Requests, role.InputTokens, role.Dollars, role.InputDollars, role.OutputDollars)
	}
	if coverage := comp.RoleCoverage; coverage != nil {
		fmt.Fprintf(&b, "coverage: %d/%d agents, %.1f%% of input tokens\n", coverage.MappedAgents, coverage.Agents,
			share(coverage.MappedInputTokens, coverage.InputTokens)*100)
	}
	b.WriteString("categories:\n")
	for _, cat := range *comp.Categories {
		fmt.Fprintf(&b, "  %-16s share=%.1f%% input=%d $%.4f reread=$%.4f\n", cat.Category, cat.InputShare*100, cat.InputTokens, cat.InputDollars, cat.RereadDollars)
	}
	for _, agent := range *comp.Agents {
		ratio := "unknown"
		if agent.BytesPerToken != nil {
			ratio = fmt.Sprintf("%.2f", *agent.BytesPerToken)
		}
		fmt.Fprintf(&b, "agent %s (%s): requests=%d input=%d $%.4f (input $%.4f, output $%.4f) bytes/token=%s\n",
			agent.AgentID, agent.Role, agent.Requests, agent.InputTokens, agent.InputDollars+agent.OutputDollars,
			agent.InputDollars, agent.OutputDollars, ratio)
		for i, source := range agent.Sources {
			if i == summarySources {
				fmt.Fprintf(&b, "  ... %d more sources\n", len(agent.Sources)-summarySources)
				break
			}
			fmt.Fprintf(&b, "  %-32s share=%.1f%% input=%d $%.4f reread=$%.4f\n", source.Source, source.InputShare*100, source.InputTokens, source.InputDollars, source.RereadDollars)
		}
		if p := agent.Phases; p != nil {
			fmt.Fprintf(&b, "  stale-phase re-read: $%.4f over %d phases (%d tokens; allocated input $%.4f; prefix re-write estimate $%.4f)\n",
				p.StaleRereadDollars, p.PhaseCount, p.StaleRereadTokens, p.StaleInputDollars, p.PrefixRewriteDollars)
		}
		for i, call := range agent.RepeatedCalls {
			if i == summaryRepeated {
				break
			}
			fmt.Fprintf(&b, "  repeated: %s x%d %s (%d result tokens)\n", call.Tool, call.Calls, call.Label, call.ResultTokens)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
