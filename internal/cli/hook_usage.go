package cli

import (
	"cmp"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/shellwords"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
	"github.com/spf13/cobra"
)

// hookUsageTimeout bounds the task-usage hook's transcript import. It stays
// below the hook's 15 second timeout in hooks/hooks.json and
// hooks/codex-hooks.json.
const hookUsageTimeout = 10 * time.Second

// hookUsageTasks bounds how many tasks one command line samples.
const hookUsageTasks = 4

// cwGlobalValueOptions are cw's global options that take a separate value,
// which may stand between cw and task.
var cwGlobalValueOptions = map[string]bool{"--config-dir": true, "--home": true, "--codex-home": true, "--claude-home": true, "--state-dir": true}

// addTaskUsageHook adds cw hook task-usage.
func addTaskUsageHook(hook *cobra.Command, config Config) {
	var host string
	taskUsage := &cobra.Command{
		Use:   "task-usage",
		Short: "PreToolUse hook: before a cw task command, sample the session's usage into that task's record; never decides anything",
		Long: "Reads the PreToolUse JSON from stdin. For each cw task command in the Bash command that names an open task with a budget by --task DIR or task ID, " +
			"it imports the session's transcript (transcript_path: a Claude Code transcript, or under Codex (--host codex) its rollout) within 10 seconds, " +
			"as cw task usage does with cw's built-in price table, and stores the sample when it matters. It prints nothing and exits 0 in every case: " +
			"an incomplete or broken transcript, a command without an open task and any error store nothing. See docs/hooks.md \"Usage sampling\".",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			parsed, err := parseHookHost(host)
			if err != nil {
				return err
			}
			sampleHookUsage(cmd.Context(), config, parsed)
			return nil
		},
	}
	addHookHostFlag(taskUsage, &host)
	hook.AddCommand(taskUsage)
}

// sampleHookUsage stores a usage sample from the hook input's transcript in
// each open task with a budget that a cw task command of the input names:
// a Claude Code transcript, or under Codex (host, else the input) the
// session's rollout. It reads
// no transcript when no such task is open, and stores nothing from an import
// that is incomplete or skipped a malformed line or an unreadable file: a
// broken transcript would read as unknown, or as too little, and replace the
// session's last sample.
func sampleHookUsage(ctx context.Context, config Config, host hookHost) {
	input, err := readHookInput(config.In)
	if err != nil || !filepath.IsAbs(input.TranscriptPath) {
		return
	}
	var open []taskflow.Record
	for _, dir := range hookTaskDirs(input.ToolInput.Command, input.CWD, config) {
		record, err := taskflow.Load(dir)
		if err == nil && record.Abandonment == nil && record.Profile != nil {
			open = append(open, record)
		}
	}
	if len(open) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, hookUsageTimeout)
	defer cancel()
	table, source, err := usagePrices("")
	if err != nil {
		return
	}
	for _, record := range open {
		options := usagelog.Options{ClaudeTranscript: input.TranscriptPath, Prices: &table, Since: record.CreatedAt}
		if host.codex(input.fromCodex) {
			options.ClaudeTranscript, options.CodexSession = "", input.TranscriptPath
		}
		report, estimate, err := usagelog.EstimateSession(ctx, options)
		if err != nil || report.Status != usagelog.StatusComplete || report.Skipped.MalformedLines > 0 || report.Skipped.UnreadableFiles > 0 {
			return
		}
		sample := usageSample(report, estimate, source)
		if sample.SessionID == "" {
			sample.SessionID = input.SessionID
		}
		_, _ = taskEnv(config).RecordUsage(ctx, record.Directory, sample)
	}
}

// taskCommand is one cw task command of a shell command line: the value of
// its last --task, and the words that choose the task store a task ID is
// read from: its NAME=value assignments and cw's global options.
type taskCommand struct {
	task    string
	env     map[string]string
	globals map[string]string
}

// hookTaskDirs returns the task directory each cw task command in a shell
// command line names with --task DIR or --task=DIR (the last one of a
// command), in command order and without duplicates, at most hookUsageTasks.
// A relative directory is read from cwd and a leading ~/ from the home
// directory. A task ID becomes its directory in the task store the command
// itself would read (commandTaskStore). A value written with a variable is
// skipped.
func hookTaskDirs(command, cwd string, config Config) []string {
	var dirs []string
	for _, simple := range shellwords.Split(command).Commands {
		found, ok := cwTaskCommand(simple.Words)
		if !ok {
			continue
		}
		dir := ""
		if taskflow.IsTaskID(found.task) {
			store, err := commandTaskStore(found, cwd, config)
			if err != nil {
				continue
			}
			dir = filepath.Join(store, found.task)
		} else if dir = commandPath(found.task, cwd, found.home(config)); dir == "" {
			continue
		}
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
		if len(dirs) == hookUsageTasks {
			break
		}
	}
	return dirs
}

// home is the home directory the command runs cw with: its --home, else a
// HOME assignment, else the hook's.
func (c taskCommand) home(config Config) string {
	if home := cmp.Or(c.globals["--home"], c.env["HOME"]); home != "" {
		return home
	}
	home, err := config.HomeDir()
	if err != nil {
		return ""
	}
	return home
}

// commandPath resolves a path word of the command: "" when it holds a
// variable or cannot be resolved, a leading ~/ from home, a relative one
// from cwd.
func commandPath(word, cwd, home string) string {
	switch {
	case word == "" || strings.Contains(word, "$"):
		return ""
	case strings.HasPrefix(word, "~/"):
		if home == "" {
			return ""
		}
		return filepath.Join(home, word[2:])
	case filepath.IsAbs(word):
		return filepath.Clean(word)
	case cwd != "":
		return filepath.Join(cwd, word)
	}
	return ""
}

// errCommandStore: the command's task store cannot be resolved.
var errCommandStore = errors.New("the command's task store cannot be resolved")

// commandTaskStore is the task store the cw task command reads a task ID
// from, resolved as hookTaskStore and resolveRoots resolve cw's own:
// CW_TASK_STORE, else --state-dir's workflow-tasks, else XDG_STATE_HOME's or
// the home directory's state directory. An assignment on the command line
// overrides the hook's environment, and --home or HOME the home directory.
func commandTaskStore(command taskCommand, cwd string, config Config) (string, error) {
	getenv := func(name string) string {
		if value, ok := command.env[name]; ok {
			return value
		}
		return config.Getenv(name)
	}
	home := commandPath(command.home(config), cwd, "")
	if store := getenv("CW_TASK_STORE"); store != "" {
		if path := commandPath(store, cwd, home); path != "" {
			return path, nil
		}
		return "", errCommandStore
	}
	state := command.globals["--state-dir"]
	switch xdg := getenv("XDG_STATE_HOME"); {
	case state != "":
	case home == "":
		return "", errCommandStore
	case xdg == "":
		state = filepath.Join(home, ".local", "state", "agent-workflows")
	case filepath.IsAbs(xdg):
		state = filepath.Join(xdg, "agent-workflows")
	default:
		return "", errCommandStore
	}
	if state = commandPath(state, cwd, home); state == "" {
		return "", errCommandStore
	}
	return filepath.Join(state, "workflow-tasks"), nil
}

// cwTaskCommand reads words, a simple command, as a cw task command:
// leading NAME=value assignments, then a program named cw, by any path,
// whose first operand, past cw's global options, is task, with a --task.
func cwTaskCommand(words []shellwords.Word) (taskCommand, bool) {
	found := taskCommand{env: map[string]string{}, globals: map[string]string{}}
	for len(words) > 0 && isAssignment(words[0].Text) {
		name, value, _ := strings.Cut(words[0].Text, "=")
		found.env[name] = value
		words = words[1:]
	}
	if len(words) == 0 {
		return found, false
	}
	if name, _, _ := strings.Cut(filepath.Base(words[0].Text), "@"); name != "cw" {
		return found, false
	}
	i := 1
	for ; i < len(words) && strings.HasPrefix(words[i].Text, "-"); i++ {
		name, value, attached := strings.Cut(words[i].Text, "=")
		switch {
		case attached && cwGlobalValueOptions[name]:
			found.globals[name] = value
		case cwGlobalValueOptions[name] && i+1 < len(words):
			i++
			found.globals[name] = words[i].Text
		}
	}
	if i >= len(words) || words[i].Text != "task" {
		return found, false
	}
	for i++; i < len(words); i++ {
		name, value, attached := strings.Cut(words[i].Text, "=")
		switch {
		case attached && name == "--task":
			found.task = value
		case words[i].Text == "--task" && i+1 < len(words):
			i++
			found.task = words[i].Text
		}
	}
	return found, found.task != ""
}

// isAssignment reports whether word is a shell assignment, NAME=value.
func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}
