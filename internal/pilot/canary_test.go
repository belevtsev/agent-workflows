package pilot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const canaryProfile = "selective-canary-v1"

// This canary isolates check observations. Native short reads in either arm
// and native short Go outcomes are allowed; the original study audit is kept.
func canaryMethod(threads []traceInfo, selected cell, model, effort string) methodReport {
	r := auditMethod(threads, selected.Arm, model, effort)
	r.Findings = slices.DeleteFunc(r.Findings, func(s string) bool { return s == "observed compact operations do not match assigned observation arm" })
	compactChecks, nativeChecks, expansions, retrieval, prepares, finishes := 0, 0, 0, 0, 0, 0
	for _, thread := range threads {
		for _, call := range thread.Calls {
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
			if strings.Contains(text, "kitsu check run") {
				if strings.Contains(text, "--compact") || strings.Contains(text, "--json") {
					compactChecks++
				} else {
					nativeChecks++
				}
			}
			if strings.Contains(text, "kitsu context show") {
				expansions++
			}
			if strings.Contains(text, "kitsu context collect") || strings.Contains(text, "kitsu context read") || strings.Contains(text, "kitsu context search") {
				retrieval++
			}
			if strings.Contains(text, "kitsu task prepare") {
				prepares++
			}
			if strings.Contains(text, "kitsu task finish") {
				finishes++
			}
		}
	}
	if retrieval != 0 || selected.Arm == "normal" && (compactChecks != 0 || expansions != 0) || selected.Arm == "compact" && selected.CaseID == "qs-sparse-array-limit" && compactChecks == 0 {
		r.Findings = append(r.Findings, "requested observations differ from frozen check-only canary arm")
	}
	r.Status = "unknown"
	if len(r.Findings) != 0 {
		r.Status = "violated"
	}
	r.Facts["profile"] = canaryProfile
	r.Facts["requested_check_calls"] = map[string]int{"native": nativeChecks, "compact": compactChecks, "retained_log_expansion": expansions, "repository_compaction": retrieval}
	r.Facts["requested_managed_calls"] = map[string]int{"prepare": prepares, "finish": finishes}
	r.Facts["observation_caveat"] = "Requested shell/code cells are observable, not proof a branch executed; actual check logs, outcomes, returned byte counts, native truncation/fallback, saved briefs and stage receipts require independent trace audit. Unknown is not success."
	return r
}

type canaryBudget struct {
	Limit            int64 `json:"limit"`
	StopReserve      int64 `json:"stop_reserve"`
	LaunchReserve    int64 `json:"launch_reserve"`
	PollMilliseconds int   `json:"poll_milliseconds"`
	NoCounterSeconds int   `json:"no_counter_seconds"`
}

func validCanaryProfile(m manifest) bool {
	b := m.Budget
	return m.Profile == canaryProfile && m.Version == 1 && slug.MatchString(m.ID) && m.Topology == "kitsu-small-path" && m.Model == "gpt-6.1-sol" && m.Effort == "high" && len(m.Cases) == 2 && m.Repeats == 1 && m.Controllers == 1 && m.TimeoutSeconds == 480 && b != nil && b.Limit > 0 && b.Limit <= 4_000_000 && b.StopReserve > 0 && b.StopReserve < b.Limit && b.LaunchReserve >= b.StopReserve && b.LaunchReserve < b.Limit && b.PollMilliseconds >= 10 && b.PollMilliseconds <= 500 && b.NoCounterSeconds > 0 && b.NoCounterSeconds <= 90
}

type canaryCounter struct {
	Parent   string `json:"parent,omitempty"`
	Captured int64  `json:"captured"`
	Events   int    `json:"events"`
}

type canaryLedger struct {
	Version     int                      `json:"version"`
	Limit       int64                    `json:"limit"`
	Captured    int64                    `json:"captured"`
	Counters    map[string]canaryCounter `json:"unique_run_thread_counters"`
	Updated     time.Time                `json:"updated_at"`
	StopReason  string                   `json:"stop_reason,omitempty"`
	TailUnknown bool                     `json:"tail_unknown"`
	Caveat      string                   `json:"caveat"`
}

func canaryReadLedger(output string, b canaryBudget) (canaryLedger, error) {
	l := canaryLedger{Version: 1, Limit: b.Limit, Counters: map[string]canaryCounter{}, Caveat: "Unique cumulative input (including cached input) plus output for all run descendants; reasoning is an output subset. Discrete counters and in-flight/missing tails are unknown. This is not a hard provider billing limit."}
	err := readJSON(filepath.Join(output, "canary-budget.json"), &l)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if l.Version != 1 || l.Limit != b.Limit || l.Counters == nil {
		return l, errors.New("captured budget ledger differs from frozen policy")
	}
	var total int64
	for _, c := range l.Counters {
		if c.Captured < 0 {
			return l, errors.New("negative captured counter")
		}
		total += c.Captured
	}
	if total != l.Captured {
		return l, errors.New("captured ledger total mismatch")
	}
	return l, nil
}

func canaryReconcile(output string, selected cell, b canaryBudget, usage *usageReport) error {
	l, err := canaryReadLedger(output, b)
	if err != nil {
		usage.Complete = false
		return err
	}
	var captured int64
	for key, c := range l.Counters {
		if strings.HasPrefix(key, selected.ID+"/") {
			captured += c.Captured
		}
	}
	if captured == usage.Total {
		return nil
	}
	// Preserve whichever source saw more captured usage and fail closed on the
	// disagreement. A reconciliation adjustment is explicit, never a new call.
	if usage.Total > captured {
		delta := usage.Total - captured
		l.Counters[selected.ID+"/offline-reconciliation"] = canaryCounter{Captured: delta}
		l.Captured += delta
	}
	l.StopReason = "live_offline_counter_mismatch"
	usage.Complete = false
	usage.Unknown = append(usage.Unknown, "live and retained offline usage disagree; larger captured lower bound retained")
	return errors.Join(errors.New("live/offline captured usage disagreement; no further model launch"), writeJSON(filepath.Join(output, "canary-budget.json"), l, false))
}

// A crash leaves the exclusive controller claim for inspection, never retry.
func canaryClaim(output string, b canaryBudget) (func(), error) {
	lock := filepath.Join(output, ".canary-controller")
	if err := os.Mkdir(lock, 0o700); err != nil {
		return nil, fmt.Errorf("canary controller active or interrupted: %w", err)
	}
	release := func() { _ = os.Remove(lock) }
	l, err := canaryReadLedger(output, b)
	if err == nil && (l.StopReason != "" || l.TailUnknown || l.Captured >= b.Limit-b.LaunchReserve) {
		err = fmt.Errorf("no further model launch: captured=%d limit=%d reserve=%d stop=%q tail_unknown=%v", l.Captured, b.Limit, b.LaunchReserve, l.StopReason, l.TailUnknown)
	}
	if err == nil {
		runs, readErr := os.ReadDir(filepath.Join(output, "runs"))
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			err = readErr
		}
		for _, run := range runs {
			if _, statErr := os.Stat(filepath.Join(output, "canary-budget.json")); statErr != nil {
				err = errors.New("prior run has no retained budget ledger; no further launch")
				break
			}
			var result runResult
			if e := readJSON(filepath.Join(output, "runs", run.Name(), "result.json"), &result); e != nil || !result.Measured || !result.Usage.Complete || result.Execution == nil || result.Execution.Stopped || result.Execution.Cancelled || result.Execution.Cleanup != "" {
				err = fmt.Errorf("previous run %s has incomplete execution evidence; no further launch", run.Name())
				break
			}
		}
	}
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// Only complete lines are observations. Repeated cumulative totals add zero;
// a reset starts a new segment, as in the trusted offline usage reader.
func canaryTrace(path string) (string, canaryCounter, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", canaryCounter{}, err
	}
	defer f.Close()
	reader := bufio.NewReaderSize(f, 64<<10)
	id := ""
	c := canaryCounter{}
	var previous [4]int64
	seen := false
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return id, c, err
		}
		var e struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &e); err != nil {
			return id, c, errors.New("malformed complete rollout event")
		}
		if e.Type == "session_meta" {
			var meta map[string]any
			if err := json.Unmarshal(e.Payload, &meta); err != nil {
				return id, c, err
			}
			id, _ = meta["id"].(string)
			c.Parent = parentID(meta)
			if c.Parent == "" {
				c.Parent = parentID(meta["source"])
			}
		}
		if e.Type != "event_msg" {
			continue
		}
		var p struct {
			Type string `json:"type"`
			Info *struct {
				Total map[string]*int64 `json:"total_token_usage"`
			} `json:"info"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return id, c, err
		}
		if p.Type != "token_count" || p.Info == nil {
			continue
		}
		t := p.Info.Total
		if t["input_tokens"] == nil || t["output_tokens"] == nil {
			return id, c, errors.New("token event lacks input/output counters")
		}
		raw := [4]int64{*t["input_tokens"], 0, 0, *t["output_tokens"]}
		if t["cached_input_tokens"] != nil {
			raw[1] = *t["cached_input_tokens"]
		}
		if t["cache_write_input_tokens"] != nil {
			raw[2] = *t["cache_write_input_tokens"]
		}
		raw[0] -= raw[1] + raw[2]
		reset := false
		for i, n := range raw {
			if n < 0 {
				return id, c, errors.New("inconsistent token count")
			}
			if seen && n < previous[i] {
				reset = true
			}
		}
		for i, n := range raw {
			if !seen || reset {
				c.Captured += n
			} else {
				c.Captured += n - previous[i]
			}
		}
		previous, seen = raw, true
		c.Events++
	}
	if id == "" && c.Events != 0 {
		return id, c, errors.New("rollout lacks session identity")
	}
	return id, c, nil
}

func canaryRefresh(output string, m manifest, l *canaryLedger) (returned error) {
	defer func() {
		l.Captured = 0
		for _, c := range l.Counters {
			l.Captured += c.Captured
		}
		l.Updated = time.Now().UTC()
	}()
	for _, selected := range schedule(m, "").Runs {
		root := filepath.Join(output, "runs", selected.ID, "home", m.Host.Rollouts)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) && path == root {
				return nil
			}
			if walkErr != nil {
				returned = errors.Join(returned, walkErr)
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				returned = errors.Join(returned, errors.New("symlink in live usage evidence"))
				return nil
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			id, c, err := canaryTrace(path)
			if id == "" && err != nil {
				info, statErr := os.Stat(path)
				if statErr == nil && info.Size() == 0 {
					return nil
				}
			}
			returned = errors.Join(returned, err)
			if id == "" {
				return nil // A just-created file may contain only a trailing fragment.
			}
			key := selected.ID + "/" + id
			old, ok := l.Counters[key]
			if ok && old.Parent != c.Parent {
				returned = errors.Join(returned, errors.New("conflicting causal identity in live usage"))
				return nil
			}
			if !ok || c.Captured > old.Captured {
				l.Counters[key] = c
			}
			return nil
		})
		if err != nil {
			returned = errors.Join(returned, err)
		}
	}
	return returned
}

func canaryInvoke(ctx context.Context, m manifest, output string, selected cell, vars map[string]string, prompt string, env []string) (invocation, error) {
	if m.Profile != canaryProfile {
		return invoke(ctx, vars["worktree"], filepath.Join(vars["run_dir"], "host"), prompt, expandedArgv(m.Host.Argv, vars), env, m.TimeoutSeconds)
	}
	b := *m.Budget
	l, err := canaryReadLedger(output, b)
	if err != nil {
		return invocation{}, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	finished := make(chan struct{})
	var monitorErr error
	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Duration(b.PollMilliseconds) * time.Millisecond)
		defer ticker.Stop()
		lastCounter, previous := time.Now(), l.Captured
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			if err := canaryRefresh(output, m, &l); err != nil {
				monitorErr = err
				l.StopReason = "usage_monitor_error"
			}
			if l.Captured != previous {
				lastCounter, previous = time.Now(), l.Captured
			}
			if l.Captured >= b.Limit-b.StopReserve {
				l.StopReason = "captured_soft_stop_reserve"
			}
			if time.Since(lastCounter) >= time.Duration(b.NoCounterSeconds)*time.Second {
				l.StopReason = "counter_silence_unknown_inflight_usage"
			}
			if err := writeJSON(filepath.Join(output, "canary-budget.json"), l, false); err != nil {
				monitorErr = err
				l.StopReason = "ledger_write_error"
			}
			if l.StopReason != "" {
				cancel()
				return
			}
		}
	}()
	execution, callErr := invoke(runCtx, vars["worktree"], filepath.Join(vars["run_dir"], "host"), prompt, expandedArgv(m.Host.Argv, vars), env, m.TimeoutSeconds)
	close(done)
	<-finished
	if err := canaryRefresh(output, m, &l); err != nil {
		monitorErr = errors.Join(monitorErr, err)
		l.StopReason = "final_usage_capture_error"
	}
	if execution.Stopped || execution.Cancelled || execution.Cleanup != "" || execution.ExitCode != 0 {
		l.TailUnknown = true
	}
	if l.Captured >= b.Limit-b.StopReserve && l.StopReason == "" {
		l.StopReason = "captured_soft_stop_reserve"
	}
	ledgerErr := writeJSON(filepath.Join(output, "canary-budget.json"), l, false)
	if l.StopReason != "" {
		ledgerErr = errors.Join(ledgerErr, writeJSON(filepath.Join(vars["run_dir"], "budget-stop.json"), map[string]any{"run": selected, "reason": l.StopReason, "captured_total": l.Captured, "tail_unknown": l.TailUnknown}, true))
	}
	return execution, errors.Join(callErr, monitorErr, ledgerErr)
}
