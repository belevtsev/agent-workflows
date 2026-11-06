package projectctx

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/internal/credentials"
	graphifyrunner "github.com/belevtsev/agent-workflows/internal/graphify"
	"github.com/pelletier/go-toml/v2"
)

const maxProbeOutput = 64 << 10

var credentialKeys = []string{"TYPESAFE_API_KEY", "SUBQ_API_KEY", "OPENAI_API_KEY"}

// Doctor performs metadata-only inspection unless Live is explicitly enabled.
// Optional probes have fixed argv, minimal environments, bounded output and
// deadlines. No instruction text, package provisioning, or model call is run.
func Doctor(ctx context.Context, options DoctorOptions) (DoctorReport, error) {
	if options.Timeout == 0 {
		options.Timeout = 15 * time.Second
	}
	if options.Timeout < 100*time.Millisecond || options.Timeout > time.Minute {
		return DoctorReport{}, errors.New("doctor timeout must be between 100ms and 1m")
	}
	originalCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	if options.Getenv == nil {
		options.Getenv = os.Getenv
	}
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.Run == nil {
		options.Run = func(ctx context.Context, command Command) (CommandResult, error) {
			return executeBounded(ctx, command, maxProbeOutput)
		}
	}
	if options.Home == "" {
		var err error
		options.Home, err = os.UserHomeDir()
		if err != nil {
			return DoctorReport{}, errors.New("resolve doctor home directory")
		}
	}
	if options.CodexHome == "" {
		options.CodexHome = options.Getenv("CODEX_HOME")
		if options.CodexHome == "" {
			options.CodexHome = filepath.Join(options.Home, ".codex")
		}
	}
	if options.ClaudeHome == "" {
		options.ClaudeHome = options.Getenv("CLAUDE_CONFIG_DIR")
		if options.ClaudeHome == "" {
			options.ClaudeHome = filepath.Join(options.Home, ".claude")
		}
	}
	skillRoots := []string{filepath.Join(options.Home, ".agents", "skills"), filepath.Join(options.ClaudeHome, "skills")}
	if options.SkillsHome != "" {
		skillRoots = []string{options.SkillsHome}
	}
	profile, err := Inspect(ctx, options.Repo)
	if err != nil {
		return DoctorReport{}, err
	}
	report := DoctorReport{Profile: profile, Live: options.Live, Checks: map[string]Check{}, Warnings: []string{}, Credentials: CredentialPresence{Environment: credentialMap(), Launcher: credentialMap(), CodexShell: credentialMap()}}
	put := func(name, status, detail, source string) {
		report.Checks[name] = Check{Status: status, Detail: detail, Source: source}
	}
	if strings.HasPrefix(profile.Snapshot.Fingerprint, "partial-") {
		put("project_source", "degraded", "Eligible source identity is partial; coverage warnings prevent a complete freshness claim", "filesystem_and_git")
	} else if profile.Snapshot.Head == "" {
		put("project_source", "verified", "Bounded eligible content identity captured; Git HEAD and branch are unavailable", "filesystem")
	} else {
		put("project_source", "verified", "Current Git identity and eligible on-disk source bytes captured; editor buffers are unchecked", "filesystem_and_git")
	}
	put("project_contracts", "detected", fmt.Sprintf("%d instruction files hashed as data; Git source coverage follows tracked and Git-unignored files, while marker scans skip dedicated dependency/cache trees", len(profile.Contracts)), "filesystem")
	for _, key := range credentialKeys {
		report.Credentials.Environment[key] = strings.TrimSpace(options.Getenv(key)) != ""
	}
	if options.ConfigDir != "" {
		presence, err := (credentials.Resolver{Directory: options.ConfigDir, Getenv: options.Getenv}).Presence()
		if err != nil {
			put("credentials_stored", "degraded", "Private credential storage could not be verified", "private_local_file")
		} else {
			report.Credentials.Stored = credentialMap()
			report.Credentials.Effective = credentialMap()
			report.Credentials.Sources = map[string]string{}
			for name, p := range presence {
				report.Credentials.Stored[name] = p.Stored
				report.Credentials.Effective[name] = p.Effective
				report.Credentials.Sources[name] = p.Source
			}
			put("credentials_stored", "configured", "Stored and effective presence inspected; authentication was not attempted", "private_local_file")
		}
	}
	readCodexMetadata(ctx, options.CodexHome, &report)
	readClaudeMetadata(options.ClaudeHome, &report)
	credentialCheck := func(name, source string, values map[string]bool) {
		count := 0
		for _, value := range values {
			if value {
				count++
			}
		}
		status := "missing"
		if count > 0 {
			status = "configured"
		}
		put(name, status, fmt.Sprintf("%d of %d provider credential variables present; authentication was not attempted", count, len(credentialKeys)), source)
	}
	credentialCheck("credentials_environment", "current_process_environment", report.Credentials.Environment)
	credentialCheck("credentials_launcher", "codex_mcp_literal_env", report.Credentials.Launcher)
	credentialCheck("credentials_codex_shell", "codex_shell_environment_policy_set", report.Credentials.CodexShell)
	skillCount := 0
	for _, root := range skillRoots {
		count := 0
		for _, skill := range []string{"task-orchestration", "production-plan", "code-review"} {
			if info, err := os.Stat(filepath.Join(root, skill, "SKILL.md")); err == nil && info.Mode().IsRegular() {
				count++
			}
		}
		skillCount = max(skillCount, count)
	}
	skillStatus := "configured"
	if skillCount == 0 {
		skillStatus = "missing"
	} else if skillCount != 3 {
		skillStatus = "degraded"
	}
	put("workflow_skills", skillStatus, fmt.Sprintf("%d of 3 core workflow skill entrypoints found; installed ownership and skill execution are unchecked", skillCount), "selected_skills_home")
	lookup := func(name string) string {
		path, lookupErr := options.LookPath(name)
		if lookupErr != nil {
			return ""
		}
		return path
	}
	jbcontext := lookup("jbcontext")
	if jbcontext == "" {
		put("jbcontext_cli", "missing", "Optional jbcontext executable was not detected", "executable_lookup")
	} else {
		put("jbcontext_cli", "detected", "Optional executable detected; availability does not prove retrieval", "executable_lookup")
	}
	put("jbcontext_index", "unchecked", "Index metadata has not been queried", "metadata_only")
	put("jbcontext_overlay", "unchecked", "Current dirty source and editor overlays have not been verified against an index", "no_retrieval_probe")
	put("jbcontext_watch", "unchecked", "Watch daemon and target enrollment have not been queried", "metadata_only")
	put("jbcontext_retrieval", "unchecked", "A status response does not prove semantic retrieval", "no_retrieval_probe")
	goApplicable := slices.Contains(profile.Kinds, "go")
	for _, name := range []string{"gopls_cli", "gopls_mcp", "gopls_dispatch", "graphify_cli", "graphify_version"} {
		status, detail := "not_applicable", "No eligible Go module or workspace marker was detected"
		if goApplicable {
			status, detail = "unchecked", "Go capability detected; live verification has not been requested"
		}
		put(name, status, detail, "project_markers")
	}
	var gopls, graphify string
	if goApplicable {
		gopls = lookup("gopls")
		if gopls == "" {
			put("gopls_cli", "missing", "Go project detected but optional gopls executable was not detected", "executable_lookup")
		} else {
			put("gopls_cli", "detected", "Optional gopls executable detected; version is unchecked", "executable_lookup")
		}
		name := "graphify"
		directory := "bin"
		if runtime.GOOS == "windows" {
			name, directory = "graphify.exe", "Scripts"
		}
		graphify = filepath.Join(options.Home, ".venvs", "agent-graphify", directory, name)
		if !executableFile(graphify) {
			graphify = lookup("graphify")
		}
		if graphify == "" {
			put("graphify_cli", "missing", "Go project detected but optional Graphify executable was not detected", "executable_lookup")
		} else {
			put("graphify_cli", "detected", "Optional Graphify executable detected; graph cache freshness remains owned by cw graphify", "executable_lookup")
		}
	}
	if options.Live {
		getenv := func(key string) string {
			if key == "HOME" || key == "USERPROFILE" {
				return options.Home
			}
			return options.Getenv(key)
		}
		probe := func(executable string, args []string, stdin string, responseID int) (CommandResult, error) {
			if ctx.Err() != nil {
				return CommandResult{}, ctx.Err()
			}
			probeCtx, probeCancel := context.WithTimeout(ctx, 3*time.Second)
			defer probeCancel()
			result, probeErr := options.Run(probeCtx, Command{Executable: executable, Args: args, Dir: profile.Snapshot.SelectedPath, Env: minimalEnvironment(getenv), Stdin: stdin, StopAfterJSONID: responseID})
			if len(result.Stdout) > maxProbeOutput {
				result.Stdout = result.Stdout[:maxProbeOutput]
				result.Truncated = true
			}
			if result.Truncated {
				return CommandResult{Truncated: true}, errors.New("probe exceeded its output limit")
			}
			return result, probeErr
		}
		if jbcontext != "" {
			result, probeErr := probe(jbcontext, []string{"status", "--json-output"}, "", 0)
			checkIndex(result, probeErr, profile.Snapshot, &report)
			result, probeErr = probe(jbcontext, []string{"watch", "status"}, "", 0)
			checkWatch(result, probeErr, profile.Snapshot.Root, &report)
		}
		if goApplicable && gopls != "" {
			result, probeErr := probe(gopls, []string{"version"}, "", 0)
			if probeErr != nil || !strings.Contains(string(result.Stdout), "gopls") {
				put("gopls_cli", "degraded", "Bounded gopls version probe failed or returned an unexpected response", "live_subprocess")
			} else {
				put("gopls_cli", "verified", "gopls answered a bounded version probe; project analysis remains unchecked", "live_subprocess")
			}
			initialize := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"clientInfo\":{\"name\":\"cw-doctor\",\"version\":\"1\"}}}\n"
			result, probeErr = probe(gopls, []string{"mcp"}, initialize, 1)
			if probeErr != nil || !validMCPInitialization(result.Stdout) {
				put("gopls_mcp", "degraded", "Bounded direct gopls MCP initialization was unavailable; Codex launcher dispatch remains unchecked", "live_direct_protocol")
			} else {
				put("gopls_mcp", "verified", "Direct gopls MCP initialization succeeded and the probe process was closed; Codex launcher dispatch remains unchecked", "live_direct_protocol")
			}
			put("gopls_dispatch", "unchecked", "Direct protocol initialization does not verify host interface dispatch or Go source analysis", "no_interface_dispatch_probe")
		}
		if goApplicable && graphify != "" {
			result, probeErr := probe(graphify, []string{"--version"}, "", 0)
			version := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(result.Stdout)), "graphify"))
			if probeErr != nil || version == "" {
				put("graphify_version", "degraded", "Bounded Graphify version probe was unavailable", "live_version_probe")
			} else if version != graphifyrunner.Version {
				put("graphify_version", "stale", "Graphify does not report the supported "+graphifyrunner.Version+" version", "live_version_probe")
			} else {
				put("graphify_version", "verified", "Graphify reports "+graphifyrunner.Version+"; graph construction and cache freshness were not queried", "live_version_probe")
			}
		}
	}
	if originalCtx.Err() != nil {
		return report, originalCtx.Err()
	}
	if ctx.Err() != nil {
		report.Warnings = append(report.Warnings, "Doctor reached its time limit; unverified checks retain their qualified status")
	}
	return report, nil
}

func credentialMap() map[string]bool {
	result := make(map[string]bool, len(credentialKeys))
	for _, key := range credentialKeys {
		result[key] = false
	}
	return result
}

func readCodexMetadata(ctx context.Context, codexHome string, report *DoctorReport) {
	for _, name := range []string{"mcp_gopls", "mcp_jbcontext"} {
		report.Checks[name] = Check{Status: "missing", Detail: "No matching enabled-state metadata found", Source: "codex_config"}
	}
	path := filepath.Join(codexHome, "config.toml")
	info, err := os.Stat(path)
	if err != nil {
		report.Checks["codex_config"] = Check{Status: "missing", Detail: "Selected Codex config is unavailable; no values were returned", Source: "selected_codex_home"}
		return
	}
	if !info.Mode().IsRegular() {
		report.Checks["codex_config"] = Check{Status: "degraded", Detail: "Selected Codex config must be a regular file; non-regular paths were not opened", Source: "selected_codex_home"}
		return
	}
	file, err := openMetadataReadOnly(path)
	if err != nil {
		report.Checks["codex_config"] = Check{Status: "degraded", Detail: "Selected Codex config could not be safely opened", Source: "selected_codex_home"}
		return
	}
	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		file.Close()
		report.Checks["codex_config"] = Check{Status: "degraded", Detail: "Selected Codex config changed while opening or was not a regular file", Source: "selected_codex_home"}
		return
	}
	data, readErr := io.ReadAll(io.LimitReader(sourceReader{ctx, file}, 1<<20+1))
	closeErr := file.Close()
	var config map[string]any
	if readErr != nil || closeErr != nil || len(data) > 1<<20 || toml.Unmarshal(data, &config) != nil {
		report.Checks["codex_config"] = Check{Status: "degraded", Detail: "Selected Codex config exceeded bounds or could not be parsed; raw diagnostics were suppressed", Source: "selected_codex_home"}
		return
	}
	report.Checks["codex_config"] = Check{Status: "detected", Detail: "Read enabled states and credential-presence booleans only", Source: "selected_codex_home"}
	if policy, ok := config["shell_environment_policy"].(map[string]any); ok {
		if values, ok := policy["set"].(map[string]any); ok {
			credentialPresence(values, report.Credentials.CodexShell)
		}
	}
	servers, _ := config["mcp_servers"].(map[string]any)
	for name, raw := range servers {
		server, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if values, ok := server["env"].(map[string]any); ok {
			credentialPresence(values, report.Credentials.Launcher)
		}
		kind := strings.ToLower(name)
		if command, ok := server["command"].(string); ok {
			switch strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe") {
			case "gopls", "jbcontext":
				kind = strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe")
			}
		}
		if kind != "gopls" && kind != "jbcontext" {
			continue
		}
		status, detail := "configured", "Matching MCP server is enabled by configuration; launch and interface dispatch are unchecked"
		if enabled, ok := server["enabled"].(bool); ok && !enabled {
			status, detail = "unchecked", "Matching MCP server is disabled by configuration"
		}
		if disabled, ok := server["disabled"].(bool); ok && disabled {
			status, detail = "unchecked", "Matching MCP server is disabled by configuration"
		}
		report.Checks["mcp_"+kind] = Check{Status: status, Detail: detail, Source: "codex_config_enabled_state"}
	}
}

// readClaudeMetadata reports only whether settings parse. Their values stay out
// of doctor output, which is safe to share.
func readClaudeMetadata(claudeHome string, report *DoctorReport) {
	path := filepath.Join(claudeHome, "settings.json")
	check := func(status, detail string) {
		report.Checks["claude_config"] = Check{Status: status, Detail: detail, Source: "selected_claude_settings"}
	}
	info, err := os.Stat(path)
	if err != nil {
		check("missing", "Selected Claude Code settings are unavailable; no values were returned")
		return
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		check("degraded", "Selected Claude Code settings must be a bounded regular file")
		return
	}
	data, err := os.ReadFile(path)
	var settings map[string]any
	if err != nil || json.Unmarshal(data, &settings) != nil {
		check("degraded", "Selected Claude Code settings could not be parsed; raw diagnostics were not returned")
		return
	}
	check("detected", "Settings parse; no values were returned")
}

func credentialPresence(values map[string]any, result map[string]bool) {
	for _, key := range credentialKeys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			result[key] = true
		}
	}
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode().Perm()&0111 != 0)
}

func checkIndex(result CommandResult, probeErr error, snapshot Snapshot, report *DoctorReport) {
	check := Check{Status: "degraded", Detail: "Bounded index status probe failed or returned an unsupported response; raw diagnostics were suppressed", Source: "live_jbcontext_status"}
	defer func() { report.Checks["jbcontext_index"] = check }()
	if probeErr != nil || result.Truncated {
		return
	}
	var status struct {
		Type    string `json:"type"`
		Indices []struct {
			Snapshots []struct {
				Revision string `json:"revision"`
			} `json:"snapshots"`
		} `json:"indices"`
	}
	if json.Unmarshal(result.Stdout, &status) != nil || status.Type != "status_result" {
		return
	}
	if snapshot.Head == "" {
		check.Status, check.Detail = "unchecked", "Status answered, but this folder has no captured Git HEAD to compare with index revisions"
		return
	}
	for _, index := range status.Indices {
		for _, indexed := range index.Snapshots {
			if indexed.Revision == snapshot.Head {
				check.Status, check.Detail = "verified", "Status reports an index snapshot for the selected Git HEAD; dirty source, editor overlays and retrieval remain unchecked"
				if snapshot.Dirty {
					report.Checks["jbcontext_overlay"] = Check{Status: "degraded", Detail: "The worktree is dirty; indexed HEAD does not verify current on-disk source or editor overlays", Source: "git_and_index_metadata"}
				}
				return
			}
		}
	}
	check.Status, check.Detail = "stale", "No reported index snapshot matches the selected Git HEAD; no automatic indexing was started"
}

func checkWatch(result CommandResult, probeErr error, root string, report *DoctorReport) {
	check := Check{Status: "degraded", Detail: "Bounded watch-status probe failed or returned an unsupported response; raw diagnostics were suppressed", Source: "live_jbcontext_watch_status"}
	defer func() { report.Checks["jbcontext_watch"] = check }()
	if probeErr != nil || result.Truncated {
		return
	}
	lines := strings.Split(string(result.Stdout), "\n")
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "Watch daemon running (pid ") {
		check.Status, check.Detail = "missing", "No running watch daemon was reported; nothing was started or enrolled"
		return
	}
	for _, line := range lines[1:] {
		// The supported text format separates repository id and path with
		// two spaces; splitting only once preserves spaces inside paths.
		_, path, ok := strings.Cut(strings.TrimLeft(line, " \t"), "  ")
		if !ok {
			continue
		}
		canonical, err := canonicalDirectory(strings.TrimSuffix(strings.TrimLeft(path, " "), "\r"))
		if err == nil && canonical == root {
			check.Status, check.Detail = "verified", "Watch daemon reports this exact canonical worktree enrolled; source processing and semantic retrieval remain unchecked"
			return
		}
	}
	check.Status, check.Detail = "detected", "Watch daemon is running, but this canonical worktree was not reported enrolled; nothing was enrolled"
}

func validMCPInitialization(output []byte) bool {
	for line := range bytes.SplitSeq(output, []byte{'\n'}) {
		var response struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Error   any    `json:"error"`
			Result  struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(line, &response) == nil && response.JSONRPC == "2.0" && response.ID == 1 && response.Error == nil && response.Result.ProtocolVersion != "" {
			return true
		}
	}
	return false
}
