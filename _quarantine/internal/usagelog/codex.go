package usagelog

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

type codexMeta struct {
	ID             string `json:"id"`
	ParentThreadID string `json:"parent_thread_id"`
	AgentNickname  string `json:"agent_nickname"`
	AgentRole      string `json:"agent_role"`
	AgentPath      string `json:"agent_path"`
}

type codexUsageRecord struct {
	ResponseID string      `json:"response_id"`
	ThreadID   string      `json:"thread_id"`
	Usage      *codexUsage `json:"usage"`
}

// codexUsage input_tokens includes cached_input_tokens and, as observed in
// real rollouts, cache_write_input_tokens.
type codexUsage struct {
	InputTokens           *int64 `json:"input_tokens"`
	CachedInputTokens     *int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens *int64 `json:"cache_write_input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	ReasoningOutputTokens *int64 `json:"reasoning_output_tokens"`
}

type codexTurnContext struct {
	Model  *string `json:"model"`
	Effort *string `json:"effort"`
}

type codexEventType struct {
	Type string `json:"type"`
}

func decodePayload[T any](line []byte) (T, bool) {
	var wire struct {
		Payload *T `json:"payload"`
	}
	if json.Unmarshal(line, &wire, lenient) != nil || wire.Payload == nil {
		var zero T
		return zero, false
	}
	return *wire.Payload, true
}

// errNotSessionMeta marks a readable rollout whose first line is a JSON
// object of another kind, such as the legacy {id,timestamp,instructions} header.
var errNotSessionMeta = errors.New("first line is not a codex session_meta entry")

func readCodexMeta(path string) (codexMeta, error) {
	line, err := firstLine(path, MaxScanLineBytes)
	if err != nil {
		return codexMeta{}, err
	}
	kind, ok := decodeType(line)
	if !ok {
		return codexMeta{}, errors.New("first line is not a JSON object")
	}
	if kind != "session_meta" {
		return codexMeta{}, errNotSessionMeta
	}
	meta, ok := decodePayload[codexMeta](line)
	if !ok || meta.ID == "" {
		return codexMeta{}, errors.New("session_meta has no usable payload id")
	}
	return meta, nil
}

// codexRequest converts a usage record. Cache writes are treated as part of
// input_tokens when they fit beside cached input (always so in observed data);
// otherwise they are kept outside input and the request is flagged.
func codexRequest(usage *codexUsage, report *Report) (request, bool) {
	if usage.InputTokens == nil || usage.OutputTokens == nil || usage.CachedInputTokens == nil {
		return request{}, false
	}
	input, cached, output := *usage.InputTokens, *usage.CachedInputTokens, *usage.OutputTokens
	written := int64(0)
	if usage.CacheWriteInputTokens != nil {
		written = *usage.CacheWriteInputTokens
	}
	for _, value := range []int64{input, cached, output, written} {
		if !validCount(value) {
			return request{}, false
		}
	}
	if cached > input {
		return request{}, false
	}
	r := request{read: cached, writeUnsplit: written, output: output, uncached: input - cached - written}
	if written > input-cached {
		r.uncached = input - cached
		report.Anomalies.CodexCacheWriteOutsideInputRequests++
	}
	if reasoning := usage.ReasoningOutputTokens; reasoning != nil {
		if !validCount(*reasoning) || *reasoning > output {
			return request{}, false
		}
		r.thinking = reasoning
	}
	return r, true
}

type codexThread struct {
	meta  codexMeta
	paths []string // Absolute, sorted.
	depth int
}

func importCodex(ctx context.Context, path, sessionsRoot string, prices *PriceTable, collected *[]collectedRequest) (Report, error) {
	report := newReport(SourceCodex)
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Report{}, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Report{}, fmt.Errorf("read codex session: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Report{}, errors.New("codex session must be a regular file")
	}
	rootMeta, err := readCodexMeta(absolute)
	if err != nil {
		return Report{}, fmt.Errorf("read codex session: %w", err)
	}
	// The walk does not follow symlinks, so compare against resolved paths.
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Report{}, fmt.Errorf("read codex session: %w", err)
	}
	report.SessionID = rootMeta.ID
	s := newSession(&report, SourceCodex)
	s.collected = collected

	discovery := &ChildDiscovery{Status: "not_attempted"}
	report.ChildDiscovery = discovery
	root := sessionsRoot
	if root == "" {
		for dir := filepath.Dir(absolute); ; dir = filepath.Dir(dir) {
			if filepath.Base(dir) == "sessions" {
				root = dir
				break
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	threads := []*codexThread{{meta: rootMeta, paths: []string{resolved}}}
	if root == "" {
		discovery.Reason = "no ancestor directory named sessions and no --codex-sessions-root; child agents were not discovered"
		s.hit("child_discovery_not_attempted")
	} else {
		root, err = filepath.Abs(root)
		if err != nil {
			return Report{}, err
		}
		// filepath.WalkDir does not descend into a symlinked root.
		if root, err = filepath.EvalSymlinks(root); err != nil {
			return Report{}, fmt.Errorf("codex sessions root is not a readable directory: %w", err)
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			return Report{}, fmt.Errorf("codex sessions root %q is not a readable directory", root)
		}
		discovery.Status, discovery.SessionsRoot = "scanned", root
		descendants, err := discoverCodexChildren(ctx, root, resolved, rootMeta.ID, discovery, &threads[0].paths)
		if err != nil {
			return Report{}, err
		}
		threads = append(threads, descendants...)
		discovery.Descendants = len(descendants)
		if discovery.Truncated {
			s.hit("max_scan_files")
		}
		if discovery.UnreadableFiles > 0 {
			s.hit("unreadable_file")
			report.Notes = append(report.Notes, "some rollout files could not be inspected during child discovery; descendants among them are missing")
		}
		if !discovery.RootFound {
			s.hit("root_not_in_sessions_root")
			report.Notes = append(report.Notes, "the scan did not encounter the root rollout's session id under the sessions root, so its descendants may be elsewhere")
		}
	}

	relative := func(file string) string {
		if root != "" {
			if rel, err := filepath.Rel(root, file); err == nil && filepath.IsLocal(rel) {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.Base(file)
	}
	r := newReader(MaxLineBytes)
	fileCount := 0
	for index, thread := range threads {
		agent := Agent{AgentID: thread.meta.ID, Role: "subagent", Depth: thread.depth, Nickname: thread.meta.AgentNickname,
			AgentRole: thread.meta.AgentRole, AgentPath: thread.meta.AgentPath, ParentID: thread.meta.ParentThreadID}
		if index == 0 {
			agent.Role = "main"
		}
		for _, file := range thread.paths {
			agent.Files = append(agent.Files, relative(file))
		}
		state := newAgentState(agent)
		s.agents = append(s.agents, state)
		for _, file := range thread.paths {
			if r.exhausted {
				break
			}
			if fileCount >= MaxFiles {
				s.hit("max_files")
				break
			}
			fileCount++
			var model, effort string
			stats, err := r.eachLine(ctx, file, &report.Skipped, func(line []byte) {
				kind, ok := decodeType(line)
				if !ok {
					report.Skipped.MalformedLines++
					return
				}
				switch kind {
				case "turn_context":
					turn, ok := decodePayload[codexTurnContext](line)
					if !ok {
						report.Skipped.InvalidEntries++
						return
					}
					model, effort = "", ""
					if turn.Model != nil {
						model = *turn.Model
					}
					if turn.Effort != nil {
						effort = *turn.Effort
					}
				case "event_msg":
					if bytes.Contains(line, []byte(`"token_count"`)) {
						if event, ok := decodePayload[codexEventType](line); ok && event.Type == "token_count" {
							report.Skipped.IgnoredTokenCountEvents++
						}
					}
				case "token_usage_record":
					record, ok := decodePayload[codexUsageRecord](line)
					if !ok {
						report.Skipped.InvalidEntries++
						return
					}
					if record.Usage == nil {
						report.Skipped.NoUsageEntries++
						return
					}
					if record.ThreadID != "" && record.ThreadID != thread.meta.ID {
						report.Skipped.OtherSessionEntries++
						return
					}
					req, ok := codexRequest(record.Usage, &report)
					if !ok {
						report.Skipped.InvalidEntries++
						return
					}
					key := ""
					if record.ResponseID != "" {
						key = "response:" + record.ResponseID
					}
					timestamp := ""
					if collected != nil {
						var stamp struct {
							Timestamp jsontext.Value `json:"timestamp"`
						}
						if json.Unmarshal(line, &stamp, lenient) == nil {
							timestamp = metaText(stamp.Timestamp)
						}
					}
					before := s.requests
					s.record(state, key, model, timestamp, req)
					if s.requests > before && effort != "" {
						state.efforts[effort] = true
					}
				}
			})
			report.Files = append(report.Files, FileRead{Path: relative(file), AgentID: thread.meta.ID, BytesRead: stats.bytes, Lines: stats.lines, Complete: stats.complete})
			if err != nil {
				if ctx.Err() != nil {
					return Report{}, err
				}
				if index == 0 && file == resolved {
					return Report{}, fmt.Errorf("read codex session: %w", err)
				}
				report.Skipped.UnreadableFiles++
				s.hit("unreadable_file")
			}
		}
	}
	if r.exhausted {
		s.hit("max_total_bytes")
	}
	if report.Skipped.OversizedLines > 0 {
		s.hit("max_line_bytes")
	}
	if report.Skipped.IgnoredTokenCountEvents > 0 && s.requests == 0 {
		report.Notes = append(report.Notes, "the rollout has token_count events but no token_usage_record entries; token_count events are not counted as requests, so usage is not reported")
	}
	s.finish(prices)
	report.Dollars.RecordedUnknownReason = "codex rollouts record no dollar cost"
	applyPriceTable(&report, prices, nil)
	finishReport(&report, nil)
	return report, nil
}

// discoverCodexChildren scans rollout session_meta lines under root and
// returns every descendant of rootID, breadth first, siblings by file path.
// Other rollouts carrying rootID itself are appended to rootPaths.
func discoverCodexChildren(ctx context.Context, root, rootFile, rootID string, discovery *ChildDiscovery, rootPaths *[]string) ([]*codexThread, error) {
	byID := make(map[string]*codexThread)
	children := make(map[string][]string)
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if entry != nil && entry.IsDir() && file != root {
				discovery.UnreadableFiles++
				return fs.SkipDir
			}
			if file == root {
				return err
			}
			discovery.UnreadableFiles++
			return nil
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		if !entry.Type().IsRegular() {
			discovery.UnreadableFiles++
			return nil
		}
		if discovery.FilesScanned >= MaxScanFiles {
			discovery.Truncated = true
			return fs.SkipAll
		}
		discovery.FilesScanned++
		if file == rootFile {
			discovery.RootFound = true
			return nil
		}
		meta, err := readCodexMeta(file)
		if errors.Is(err, errNotSessionMeta) {
			discovery.OtherFormatFiles++
			return nil
		}
		if err != nil {
			discovery.UnreadableFiles++
			return nil
		}
		if meta.ID == rootID {
			discovery.RootFound = true
			*rootPaths = append(*rootPaths, file)
			return nil
		}
		thread := byID[meta.ID]
		if thread == nil {
			thread = &codexThread{meta: meta}
			byID[meta.ID] = thread
			if meta.ParentThreadID != "" {
				children[meta.ParentThreadID] = append(children[meta.ParentThreadID], meta.ID)
			}
		}
		thread.paths = append(thread.paths, file)
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("scan codex sessions root: %w", err)
	}
	slices.Sort((*rootPaths)[1:])
	var result []*codexThread
	visited := map[string]bool{rootID: true}
	queue := []string{rootID}
	depth := map[string]int{rootID: 0}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		var level []*codexThread
		for _, id := range children[parent] {
			if visited[id] {
				continue
			}
			visited[id] = true
			thread := byID[id]
			slices.Sort(thread.paths)
			thread.depth = depth[parent] + 1
			depth[id] = thread.depth
			level = append(level, thread)
		}
		slices.SortFunc(level, func(a, b *codexThread) int { return cmp.Compare(a.paths[0], b.paths[0]) })
		for _, thread := range level {
			queue = append(queue, thread.meta.ID)
		}
		result = append(result, level...)
	}
	// Report subagents in file order, matching the Claude layout.
	slices.SortFunc(result, func(a, b *codexThread) int { return cmp.Compare(a.paths[0], b.paths[0]) })
	return result, nil
}
