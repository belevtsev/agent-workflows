package pilot

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

type traceCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type traceInfo struct {
	ID                string   `json:"id"`
	ParentID          string   `json:"parent_id,omitempty"`
	File              asset    `json:"trace"`
	Bytes             int64    `json:"bytes"`
	Models            []string `json:"models"`
	Efforts           []string `json:"efforts"`
	Stage             string   `json:"stage,omitempty"`
	AgentPath         string   `json:"agent_path,omitempty"`
	StageSource       string   `json:"stage_source,omitempty"`
	Task              string   `json:"task,omitempty"`
	Known             bool     `json:"known"`
	Why               string   `json:"why,omitempty"`
	UnknownCategories []string `json:"unknown_categories,omitempty"`
	ReasoningOutput   *int64   `json:"reasoning_output_tokens"`
	InputUncached     int64    `json:"input_uncached"`
	InputTotal        int64    `json:"input_total"`
	tokens.Counts
	Calls []traceCall `json:"-"`
}

type usageReport struct {
	Complete        bool        `json:"complete"`
	Total           int64       `json:"total"`
	Threads         []traceInfo `json:"threads"`
	Unknown         []string    `json:"unknown"`
	ReasoningOutput *int64      `json:"reasoning_output_tokens"`
	RawTraces       []asset     `json:"raw_traces"`
	InputUncached   int64       `json:"input_uncached"`
	InputTotal      int64       `json:"input_total"`
	tokens.Counts
	// stdout turn.completed repeats rollout-owned usage and is never added.
	Ownership string `json:"ownership"`
}

type methodReport struct {
	Status   string         `json:"status"`
	Facts    map[string]any `json:"facts"`
	Findings []string       `json:"findings"`
}

func parentID(value any) string {
	if object, ok := value.(map[string]any); ok {
		if id, ok := object["parent_thread_id"].(string); ok && id != "" {
			return id
		}
		for _, key := range []string{"subagent", "thread_spawn", "spawn"} {
			if id := parentID(object[key]); id != "" {
				return id
			}
		}
	}
	return ""
}

func traceText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		for _, part := range parts {
			if part.Text != "" {
				return part.Text
			}
		}
	}
	return ""
}

func inspectTrace(path string) (traceInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return traceInfo{}, err
	}
	defer f.Close()
	var info traceInfo
	var previousReasoning *int64
	var reasoning int64
	reasoningKnown := true
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 64<<20)
	for scanner.Scan() {
		var event struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return info, fmt.Errorf("invalid rollout JSON: %w", err)
		}
		switch event.Type {
		case "event_msg":
			var payload struct {
				Type string `json:"type"`
				Info *struct {
					Total map[string]json.RawMessage `json:"total_token_usage"`
				} `json:"info"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return info, err
			}
			if payload.Type != "token_count" || payload.Info == nil {
				continue
			}
			for _, field := range []string{"input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens"} {
				if value, ok := payload.Info.Total[field]; (!ok || string(value) == "null") && !slices.Contains(info.UnknownCategories, field) {
					info.UnknownCategories = append(info.UnknownCategories, field)
				}
			}
			var current int64
			if value, ok := payload.Info.Total["reasoning_output_tokens"]; !ok || string(value) == "null" || json.Unmarshal(value, &current) != nil || current < 0 {
				reasoningKnown = false
			} else {
				if previousReasoning == nil || current < *previousReasoning {
					reasoning += current
				} else {
					reasoning += current - *previousReasoning
				}
				previousReasoning = &current
			}
		case "session_meta":
			var metadata map[string]any
			if err := json.Unmarshal(event.Payload, &metadata); err != nil {
				return info, err
			}
			info.ID, _ = metadata["id"].(string)
			info.ParentID = parentID(metadata)
			if info.ParentID == "" {
				info.ParentID = parentID(metadata["source"])
			}
			if source, ok := metadata["source"].(map[string]any); ok {
				if subagent, ok := source["subagent"].(map[string]any); ok {
					if spawn, ok := subagent["thread_spawn"].(map[string]any); ok {
						info.AgentPath, _ = spawn["agent_path"].(string)
					}
				}
			}
		case "turn_context":
			var turn struct{ Model, Effort string }
			if err := json.Unmarshal(event.Payload, &turn); err != nil {
				return info, err
			}
			if turn.Model != "" && !slices.Contains(info.Models, turn.Model) {
				info.Models = append(info.Models, turn.Model)
			}
			if turn.Effort != "" && !slices.Contains(info.Efforts, turn.Effort) {
				info.Efforts = append(info.Efforts, turn.Effort)
			}
		case "response_item":
			var item struct {
				Type      string          `json:"type"`
				Role      string          `json:"role"`
				Name      string          `json:"name"`
				Arguments string          `json:"arguments"`
				Content   json.RawMessage `json:"content"`
				Input     string          `json:"input"`
			}
			if err := json.Unmarshal(event.Payload, &item); err != nil {
				return info, err
			}
			if item.Type == "function_call" {
				info.Calls = append(info.Calls, traceCall{item.Name, json.RawMessage(item.Arguments)})
			}
			if item.Type == "custom_tool_call" && item.Name == "exec" {
				data, _ := json.Marshal(map[string]string{"code": item.Input})
				info.Calls = append(info.Calls, traceCall{"code-mode-exec", data})
			}
			if item.Type == "message" && item.Role == "user" && info.Stage == "" {
				if label, ok := tokens.ParseLabel(traceText(item.Content)); ok {
					info.Task, info.Stage = label.Task, label.Stage
					info.StageSource = "plaintext_brief_label"
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return info, err
	}
	if info.ID == "" {
		return info, fmt.Errorf("rollout has no thread identity: %s", path)
	}
	if info.Stage == "" {
		if match := regexp.MustCompile(`(?:^|[/_-])(lookup|tests|code|review|commit)(?:[_-][0-9]+)?$`).FindStringSubmatch(info.AgentPath); match != nil {
			info.Stage, info.StageSource = match[1], "native_agent_path_suffix; assignment text may be encrypted"
		}
	}
	if reasoningKnown && previousReasoning != nil {
		info.ReasoningOutput = &reasoning
	}
	return info, nil
}

func collectUsage(home, rolloutDir, runDir, rootID string) (usageReport, []traceInfo, error) {
	report := usageReport{Complete: true, Ownership: "unique per-thread cumulative rollout totals; stdout usage is corroboration only", Unknown: []string{}}
	traceDir := filepath.Join(runDir, "traces")
	if err := os.MkdirAll(traceDir, 0o700); err != nil {
		return report, nil, err
	}
	byID := map[string]traceInfo{}
	err := filepath.WalkDir(filepath.Join(home, rolloutDir), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked rollout is not evidence: %s", path)
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := inspectTrace(path)
		if err != nil {
			report.Unknown = append(report.Unknown, err.Error())
			digest, hashErr := hashFile(path)
			if hashErr != nil {
				return hashErr
			}
			destination := filepath.Join(traceDir, "unparsed-"+digest+".jsonl")
			if _, statErr := os.Stat(destination); os.IsNotExist(statErr) {
				if copyErr := copyRegular(path, destination, 0o600); copyErr != nil {
					return copyErr
				}
			}
			report.RawTraces = append(report.RawTraces, asset{destination, digest})
			return nil
		}
		hash, err := hashFile(path)
		if err != nil {
			return err
		}
		if previous, exists := byID[info.ID]; exists {
			if previous.File.SHA256 != hash {
				report.Unknown = append(report.Unknown, "conflicting files for thread "+info.ID)
				destination := filepath.Join(traceDir, "conflict-"+hash+".jsonl")
				if _, statErr := os.Stat(destination); os.IsNotExist(statErr) {
					if err := copyRegular(path, destination, 0o600); err != nil {
						return err
					}
				}
				report.RawTraces = append(report.RawTraces, asset{destination, hash})
			}
			return nil
		}
		name := info.ID + ".jsonl"
		if !slugOrUUID(info.ID) {
			return fmt.Errorf("unsafe thread identity %q", info.ID)
		}
		destination := filepath.Join(traceDir, name)
		if err := copyRegular(path, destination, 0o600); err != nil {
			return err
		}
		copied, err := hashFile(destination)
		if err != nil || copied != hash {
			return fmt.Errorf("rollout changed during capture: %s", info.ID)
		}
		stat, err := os.Stat(destination)
		if err != nil {
			return err
		}
		info.File, info.Bytes = asset{destination, copied}, stat.Size()
		report.RawTraces = append(report.RawTraces, info.File)
		byID[info.ID] = info
		return nil
	})
	if err != nil {
		report.Unknown = append(report.Unknown, err.Error())
	}
	if rootID == "" {
		report.Unknown = append(report.Unknown, "stdout has no root thread.started identity")
	}
	if _, exists := byID[rootID]; !exists {
		report.Unknown = append(report.Unknown, "root rollout missing: "+rootID)
	}
	var ids []string
	for id := range byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		info := byID[id]
		if id != rootID && !descendsFrom(id, rootID, byID) {
			report.Unknown = append(report.Unknown, "orphan thread in fresh home: "+id)
		}
		storeDir := filepath.Join(runDir, "usage-records")
		if err := tokens.Record(context.Background(), storeDir, tokens.Codex, tokens.Notice{Session: id, Transcript: info.File.Path}); err != nil {
			report.Unknown = append(report.Unknown, id+": "+err.Error())
		}
		starts, damaged, err := tokens.Load(storeDir)
		if err != nil || len(damaged) != 0 {
			report.Unknown = append(report.Unknown, fmt.Sprintf("usage records unavailable: %v %v", err, damaged))
		}
		for _, start := range starts {
			if start.Session != id {
				continue
			}
			info.Known, info.Why = start.Known && !start.Partial, start.Why
			for _, counts := range start.Models {
				addCounts(&info.Counts, counts)
			}
		}
		if !info.Known {
			report.Unknown = append(report.Unknown, id+": "+info.Why)
		}
		if len(info.UnknownCategories) != 0 {
			report.Unknown = append(report.Unknown, id+": provider categories unavailable: "+strings.Join(info.UnknownCategories, ", "))
		}
		addCounts(&report.Counts, info.Counts)
		info.InputUncached, info.InputTotal = info.Input, info.Input+info.CacheRead+info.CacheWrite
		report.Threads = append(report.Threads, info)
	}
	var reasoningTotal int64
	reasoningComplete := len(report.Threads) != 0
	for _, thread := range report.Threads {
		if thread.ReasoningOutput == nil {
			reasoningComplete = false
		} else {
			reasoningTotal += *thread.ReasoningOutput
		}
	}
	if reasoningComplete {
		report.ReasoningOutput = &reasoningTotal
	}
	spawns := 0
	for _, thread := range report.Threads {
		for _, call := range thread.Calls {
			if call.Name == "spawn_agent" {
				spawns++
			}
		}
	}
	if spawns > len(report.Threads)-1 {
		report.Unknown = append(report.Unknown, "not every native spawn has a separately captured worker rollout")
	}
	report.Complete, report.Total = len(report.Unknown) == 0, report.Counts.Sum()
	report.InputUncached, report.InputTotal = report.Input, report.Input+report.CacheRead+report.CacheWrite
	return report, report.Threads, nil
}

func slugOrUUID(id string) bool { return slug.MatchString(id) && len(id) <= 128 }

func descendsFrom(id, root string, threads map[string]traceInfo) bool {
	for visited := map[string]bool{}; id != "" && !visited[id]; {
		if id == root {
			return true
		}
		visited[id] = true
		id = threads[id].ParentID
	}
	return false
}

func addCounts(sum *tokens.Counts, counts tokens.Counts) {
	sum.Input += counts.Input
	sum.CacheWrite += counts.CacheWrite
	sum.CacheRead += counts.CacheRead
	sum.Output += counts.Output // reasoning is already included in this category
}

func auditMethod(threads []traceInfo, arm, model, effort string) methodReport {
	report := methodReport{Status: "unknown", Facts: map[string]any{}, Findings: []string{}}
	stages := map[string]int{}
	spawns, compactCalls := 0, 0
	for _, thread := range threads {
		if !slices.Equal(thread.Models, []string{model}) || !slices.Equal(thread.Efforts, []string{effort}) {
			report.Findings = append(report.Findings, "actual model/effort missing or different in "+thread.ID)
		}
		if thread.ParentID != "" {
			stages[thread.Stage]++
		}
		for _, call := range thread.Calls {
			if call.Name == "spawn_agent" {
				spawns++
				var args struct {
					ForkTurns string `json:"fork_turns"`
					Model     string `json:"model"`
					Effort    string `json:"reasoning_effort"`
				}
				if json.Unmarshal(call.Arguments, &args) != nil || args.ForkTurns != "none" || args.Model != model || args.Effort != effort {
					report.Findings = append(report.Findings, "spawn did not explicitly request fresh Sol/max context")
				}
			}
			// Instructions or author claims inside spawn messages are not proof
			// that an observation command executed. Count actual shell calls only.
			if call.Name != "exec_command" && call.Name != "shell_command" && call.Name != "code-mode-exec" {
				continue
			}
			var args struct {
				Cmd     string `json:"cmd"`
				Command string `json:"command"`
				Code    string `json:"code"`
			}
			if json.Unmarshal(call.Arguments, &args) != nil {
				continue
			}
			text := args.Cmd + args.Command + args.Code
			if strings.Contains(text, "kitsu context collect") || strings.Contains(text, "kitsu task brief") || strings.Contains(text, "kitsu check run") && (strings.Contains(text, "--compact") || strings.Contains(text, "--json")) {
				compactCalls++
			}
		}
	}
	for _, stage := range []string{"tests", "code", "review", "commit"} {
		if stages[stage] == 0 {
			report.Findings = append(report.Findings, "missing independently recorded worker stage: "+stage)
		}
	}
	if arm == "normal" && compactCalls != 0 || arm == "compact" && compactCalls == 0 {
		report.Findings = append(report.Findings, "observed compact operations do not match assigned observation arm")
	}
	report.Facts["stages"], report.Facts["native_spawns"], report.Facts["compact_commands"] = stages, spawns, compactCalls
	if len(report.Findings) != 0 {
		report.Status = "violated"
	}
	// These mechanical checks do not prove semantic red-test validity, reviewer
	// independence, write scopes, or authorized finding dispositions. Host-side
	// protocol review must certify those; unknown is deliberately not a pass.
	report.Facts["full_protocol_review"] = "required"
	report.Facts["brief_observability"] = "Native assignment messages may be encrypted; saved plaintext briefs and native task names do not prove the encrypted payload was identical."
	report.Facts["command_counter_scope"] = "Requested shell commands or code-mode cells; semantic audit must verify results and distinguish command text from executed branches."
	return report
}
