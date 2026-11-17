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

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/proc"
)

type invocation struct {
	Argv        []string  `json:"argv"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at"`
	ElapsedNS   int64     `json:"elapsed_ns"`
	ExitCode    int       `json:"exit_code"`
	Stopped     bool      `json:"stopped"`
	Cancelled   bool      `json:"cancelled"`
	Error       string    `json:"error,omitempty"`
	Cleanup     string    `json:"cleanup_error,omitempty"`
	Stdout      asset     `json:"stdout"`
	Stderr      asset     `json:"stderr"`
	StdoutBytes int64     `json:"stdout_bytes"`
	StderrBytes int64     `json:"stderr_bytes"`
}

type runResult struct {
	Version           int             `json:"version"`
	Run               cell            `json:"run"`
	ManifestSHA256    string          `json:"manifest_sha256"`
	Status            string          `json:"status"`
	Measured          bool            `json:"measured"`
	Error             string          `json:"error,omitempty"`
	Prompt            asset           `json:"prompt"`
	BaselineTree      string          `json:"baseline_tree"`
	CandidateTree     string          `json:"candidate_tree"`
	Patch             asset           `json:"patch"`
	Execution         *invocation     `json:"execution,omitempty"`
	Grading           *invocation     `json:"grading,omitempty"`
	Grade             json.RawMessage `json:"grade,omitempty"`
	Usage             usageReport     `json:"usage"`
	Method            methodReport    `json:"method"`
	Protocol          *asset          `json:"protocol_audit,omitempty"`
	TrustedCandidate  string          `json:"trusted_candidate,omitempty"`
	ExportAttestation asset           `json:"export_attestation"`
}

func copyRegular(source, destination string, mode fs.FileMode) error {
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("copy source is not a regular file: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}

func copySnapshot(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.Name() == ".git" || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot contains history or a link: %s", path)
		}
		out := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyRegular(path, out, 0o600|info.Mode().Perm()&0o111)
	})
}

func executionEnv(host pilotHost, vars map[string]string) []string {
	values := map[string]string{}
	for _, key := range host.PassEnv {
		if value, ok := os.LookupEnv(key); ok {
			values[key] = value
		}
	}
	values["HOME"], values["USERPROFILE"] = vars["home"], vars["home"]
	values["XDG_CONFIG_HOME"] = filepath.Join(vars["home"], "config")
	values["CODEX_HOME"] = filepath.Join(vars["home"], ".codex")
	values["GIT_CONFIG_NOSYSTEM"], values["GIT_CONFIG_GLOBAL"] = "1", os.DevNull
	values["GIT_AUTHOR_NAME"], values["GIT_COMMITTER_NAME"] = "Kitsu Pilot", "Kitsu Pilot"
	values["GIT_AUTHOR_EMAIL"], values["GIT_COMMITTER_EMAIL"] = "pilot@example.invalid", "pilot@example.invalid"
	values["GIT_AUTHOR_DATE"], values["GIT_COMMITTER_DATE"] = "2000-01-01T00:00:00Z", "2000-01-01T00:00:00Z"
	for key, value := range host.Env {
		values[key] = replacements(value, vars)
	}
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func invoke(ctx context.Context, dir, prefix, input string, argv, env []string, timeout int) (invocation, error) {
	result := invocation{Argv: argv, ExitCode: -1}
	if len(argv) == 0 || timeout <= 0 {
		return result, errors.New("command needs argv and a positive timeout")
	}
	stdout, err := os.OpenFile(prefix+".stdout", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return result, err
	}
	stderr, err := os.OpenFile(prefix+".stderr", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		stdout.Close()
		return result, err
	}
	result.StartedAt = time.Now().UTC()
	start := time.Now()
	got := proc.Run(ctx, proc.Spec{Path: argv[0], Args: argv[1:], Dir: dir, Env: env,
		Stdin: strings.NewReader(input), Stdout: stdout, Stderr: stderr, Timeout: time.Duration(timeout) * time.Second})
	result.ElapsedNS, result.EndedAt = time.Since(start).Nanoseconds(), time.Now().UTC()
	closeErr := errors.Join(stdout.Close(), stderr.Close())
	result.ExitCode, result.Stopped, result.Cancelled = got.Code, got.Stopped, got.Cancelled
	if got.Err != nil {
		result.Error = got.Err.Error()
	}
	if got.Cleanup != nil {
		result.Cleanup = got.Cleanup.Error()
	}
	for _, target := range []struct {
		path string
		into *asset
		size *int64
	}{{prefix + ".stdout", &result.Stdout, &result.StdoutBytes}, {prefix + ".stderr", &result.Stderr, &result.StderrBytes}} {
		hash, err := hashFile(target.path)
		if err != nil {
			return result, err
		}
		info, err := os.Stat(target.path)
		if err != nil {
			return result, err
		}
		*target.into, *target.size = asset{target.path, hash}, info.Size()
	}
	return result, closeErr
}

func expandedArgv(argv []string, vars map[string]string) []string {
	out := make([]string, len(argv))
	for i, arg := range argv {
		out[i] = replacements(arg, vars)
	}
	return out
}

func promptFor(m manifest, c pilotCase, arm string) (string, error) {
	var sections []string
	for _, a := range []asset{m.CoordinatorPrompt, m.ObservationPrompts[arm], c.Prompt} {
		data, err := os.ReadFile(a.Path)
		if err != nil {
			return "", err
		}
		sections = append(sections, string(data))
	}
	return strings.ReplaceAll(strings.Join(sections, "\n\n"), "{task}", c.ID), nil
}

func calibrationApproved(path, model, effort string) error {
	var calibration struct {
		Version           int             `json:"version"`
		Approved          bool            `json:"approved"`
		Model             string          `json:"model"`
		Effort            string          `json:"effort"`
		SubagentsVerified bool            `json:"subagents_verified"`
		UsageVerified     bool            `json:"usage_verified"`
		IsolationVerified bool            `json:"isolation_verified"`
		CLI               string          `json:"cli_version"`
		Evidence          json.RawMessage `json:"evidence"`
		Usage             json.RawMessage `json:"usage"`
	}
	if err := readJSON(path, &calibration); err != nil {
		return err
	}
	if calibration.Version != 1 || !calibration.Approved || calibration.Model != model || calibration.Effort != effort || !calibration.SubagentsVerified || !calibration.UsageVerified || !calibration.IsolationVerified || calibration.CLI == "" {
		return errors.New("model, native fresh subagents, usage, and isolation calibration must be explicitly approved before measured execution")
	}
	return nil
}

func executeCell(ctx context.Context, m manifest, p plan, selected cell, output string) (returned error) {
	if err := calibrationApproved(m.Calibration.Path, m.Model, m.Effort); err != nil {
		return err
	}
	runDir := filepath.Join(output, "runs", selected.ID)
	if err := os.MkdirAll(filepath.Dir(runDir), 0o700); err != nil {
		return err
	}
	// A directory is an exclusive claim. Existing, failed, or interrupted runs
	// are never silently retried; the scheduler moves to another identity.
	if err := os.Mkdir(runDir, 0o700); err != nil {
		return fmt.Errorf("run already claimed or unavailable: %s: %w", selected.ID, err)
	}
	result := runResult{Version: 1, Run: selected, ManifestSHA256: p.ManifestSHA256, Status: "preparation_failed"}
	defer func() {
		if returned != nil {
			result.Error = returned.Error()
		}
		returned = errors.Join(returned, writeJSON(filepath.Join(runDir, "result.json"), result, false))
	}()
	if err := writeJSON(filepath.Join(runDir, "started.json"), map[string]any{"run": selected, "controller_pid": os.Getpid(), "at": time.Now().UTC()}, true); err != nil {
		return err
	}
	var chosen pilotCase
	for _, c := range m.Cases {
		if c.ID == selected.CaseID {
			chosen = c
		}
	}
	vars := map[string]string{"worktree": filepath.Join(runDir, "worktree"), "home": filepath.Join(runDir, "home"), "run_dir": runDir, "task": chosen.ID, "case_id": chosen.ID}
	if err := copySnapshot(chosen.Snapshot, vars["worktree"]); err != nil {
		return err
	}
	if digest, err := snapshotHash(ctx, vars["worktree"]); err != nil || digest != chosen.SnapshotSHA256 {
		return errors.New("copied public snapshot does not match frozen input")
	}
	if err := os.MkdirAll(vars["home"], 0o700); err != nil {
		return err
	}
	for _, file := range m.Host.HomeFiles {
		if err := copyRegular(file.Source, filepath.Join(vars["home"], file.Destination), 0o600); err != nil {
			return err
		}
	}
	env := executionEnv(m.Host, vars)
	for i, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "--force", "--all"}, {"commit", "-q", "-m", "Frozen public task baseline"}} {
		call, err := invoke(ctx, vars["worktree"], filepath.Join(runDir, fmt.Sprintf("git-prepare-%d", i)), "", append([]string{"git"}, args...), env, 60)
		if err != nil || call.ExitCode != 0 {
			return fmt.Errorf("initialize independent Git: %v; exit %d; see %s", err, call.ExitCode, call.Stderr.Path)
		}
	}
	repo, err := gitx.Open(ctx, vars["worktree"])
	if err != nil {
		return err
	}
	result.BaselineTree, _, err = repo.TreeOf(ctx, "HEAD")
	if err != nil {
		return err
	}
	for i, prep := range append(slices.Clone(m.Prepare), chosen.Prepare...) {
		call, err := invoke(ctx, vars["worktree"], filepath.Join(runDir, fmt.Sprintf("prepare-%d", i)), "", expandedArgv(prep.Argv, vars), env, prep.TimeoutSeconds)
		if err != nil || call.ExitCode != 0 || call.Cleanup != "" {
			return fmt.Errorf("preparation failed: %v; exit %d; see %s", err, call.ExitCode, call.Stderr.Path)
		}
	}
	initial, err := repo.Snapshot(ctx, filepath.Join(runDir, "snapshot-tmp"))
	if err != nil || initial != result.BaselineTree {
		return errors.New("preparation changed the frozen public source tree")
	}
	prompt, err := promptFor(m, chosen, selected.Arm)
	if err != nil {
		return err
	}
	promptPath := filepath.Join(runDir, "prompt.txt")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return err
	}
	result.Prompt.Path = promptPath
	result.Prompt.SHA256, err = hashFile(promptPath)
	if err != nil {
		return err
	}
	result.Measured, result.Status = true, "running"
	if err := writeJSON(filepath.Join(runDir, "running.json"), result, true); err != nil {
		return err
	}
	execution, invokeErr := invoke(ctx, vars["worktree"], filepath.Join(runDir, "host"), prompt, expandedArgv(m.Host.Argv, vars), env, m.TimeoutSeconds)
	result.Execution, result.Status = &execution, "completed"
	if invokeErr != nil || execution.ExitCode != 0 || execution.Cleanup != "" {
		result.Status = "execution_failed"
	}
	if execution.Stopped {
		result.Status = "timed_out"
	}
	usage, threads, usageErr := collectUsage(vars["home"], m.Host.Rollouts, runDir, stdoutThreadID(execution.Stdout.Path))
	result.Usage, result.Method = usage, auditMethod(threads, selected.Arm, m.Model, m.Effort)
	// Retain and grade a failed attempt's final candidate too. A process failure
	// is distinct from functional correctness, and its measured usage stays charged.
	postCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	result.TrustedCandidate, result.CandidateTree, result.Patch, result.ExportAttestation, err = exportCandidate(postCtx, chosen, selected, output, runDir, vars["worktree"], result.BaselineTree)
	if err != nil {
		return errors.Join(invokeErr, usageErr, err)
	}
	vars["grade_request"] = filepath.Join(runDir, "grade-request.json")
	gradePath := filepath.Join(runDir, "grade.json")
	request := map[string]any{"version": 1, "case_id": chosen.ID, "candidate_worktree": result.TrustedCandidate, "output_path": gradePath,
		"baseline_tree": result.BaselineTree, "candidate_tree": result.CandidateTree, "visible_prompt_sha256": result.Prompt.SHA256}
	if err := writeJSON(vars["grade_request"], request, true); err != nil {
		return err
	}
	gradeSettings := pilotHost{Env: m.Grader.Env, PassEnv: m.Grader.PassEnv}
	if gradeSettings.PassEnv == nil {
		gradeSettings.PassEnv = m.Host.PassEnv
	}
	graded, gradeErr := invoke(postCtx, runDir, filepath.Join(runDir, "grader"), "", expandedArgv(m.Grader.Argv, vars), executionEnv(gradeSettings, vars), 240)
	result.Grading = &graded
	data, readErr := os.ReadFile(gradePath)
	if readErr == nil && json.Valid(data) {
		result.Grade = data
	} else {
		readErr = fmt.Errorf("grader did not leave valid JSON: %v", readErr)
	}
	return errors.Join(invokeErr, usageErr, gradeErr, readErr)
}

func stdoutThreadID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 64<<20)
	root := ""
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
			ID   string `json:"thread_id"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Type == "thread.started" {
			if root != "" && root != event.ID {
				return ""
			}
			root = event.ID
		}
	}
	if scanner.Err() != nil {
		return ""
	}
	return root
}
