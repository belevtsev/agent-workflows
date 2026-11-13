package cli

import (
	"context"
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
// below the hook's 15 second timeout in hooks/hooks.json.
const hookUsageTimeout = 10 * time.Second

// hookUsageTasks bounds how many tasks one command line samples.
const hookUsageTasks = 4

// cwGlobalValueOptions are cw's global options that take a separate value,
// which may stand between cw and task.
var cwGlobalValueOptions = map[string]bool{"--config-dir": true, "--home": true, "--codex-home": true, "--claude-home": true, "--state-dir": true}

// addTaskUsageHook adds cw hook task-usage.
func addTaskUsageHook(hook, root *cobra.Command, config Config) {
	hook.AddCommand(&cobra.Command{
		Use:   "task-usage",
		Short: "PreToolUse hook: before a cw task command, sample the Claude Code session's usage into that task's record; never decides anything",
		Long: "Reads the PreToolUse JSON from stdin. For each cw task command in the Bash command that names an open task with a budget by --task DIR or task ID, " +
			"it imports the session's transcript (transcript_path) within 10 seconds, as cw task usage --claude-transcript does with cw's built-in price table, " +
			"and stores the sample when it matters. It prints nothing and exits 0 in every case: an incomplete or unreadable transcript, " +
			"a command without an open task, Codex (which runs cw task usage --codex-session itself) and any error store nothing. See docs/hooks.md \"Usage sampling\".",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sampleHookUsage(cmd.Context(), root, config)
			return nil
		},
	})
}

// sampleHookUsage stores a usage sample from the hook input's transcript in
// each open task with a budget that a cw task command of the input names.
// It reads no transcript when no such task is open, and stores nothing from
// an import that is incomplete or skipped a malformed line or an unreadable
// file: a broken transcript would read as unknown, or as too little, and
// replace the session's last sample.
func sampleHookUsage(ctx context.Context, root *cobra.Command, config Config) {
	input, err := readHookInput(config.In)
	if err != nil || input.fromCodex() || !filepath.IsAbs(input.TranscriptPath) {
		return
	}
	home, err := config.HomeDir()
	if err != nil {
		home = ""
	}
	var open []taskflow.Record
	for _, dir := range hookTaskDirs(input.ToolInput.Command, input.CWD, home, func() (string, error) { return hookTaskStore(root, config) }) {
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

// hookTaskDirs returns the task directory each cw task command in a shell
// command line names with --task DIR or --task=DIR (the last one of a
// command), in command order and without duplicates, at most hookUsageTasks.
// A task ID becomes its directory in the task store; a relative directory is
// read from cwd and a leading ~/ from home. A directory written with a
// variable is skipped, as is everything when store is needed and fails.
func hookTaskDirs(command, cwd, home string, store func() (string, error)) []string {
	var dirs []string
	for _, simple := range shellwords.Split(command).Commands {
		value, ok := cwTaskOption(simple.Words)
		if !ok || strings.Contains(value, "$") {
			continue
		}
		var dir string
		switch {
		case taskflow.IsTaskID(value):
			path, err := store()
			if err != nil {
				continue
			}
			dir = filepath.Join(path, value)
		case strings.HasPrefix(value, "~/") && home != "":
			dir = filepath.Join(home, value[2:])
		case filepath.IsAbs(value):
			dir = filepath.Clean(value)
		case cwd != "":
			dir = filepath.Join(cwd, value)
		default:
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

// cwTaskOption returns the value of the last --task of words, a simple
// command, when it runs cw task: leading NAME=value assignments, then a
// program named cw, by any path, whose first operand, past cw's global
// options, is task.
func cwTaskOption(words []shellwords.Word) (string, bool) {
	for len(words) > 0 && isAssignment(words[0].Text) {
		words = words[1:]
	}
	if len(words) == 0 {
		return "", false
	}
	if name, _, _ := strings.Cut(filepath.Base(words[0].Text), "@"); name != "cw" {
		return "", false
	}
	i := 1
	for ; i < len(words) && strings.HasPrefix(words[i].Text, "-"); i++ {
		if cwGlobalValueOptions[words[i].Text] {
			i++
		}
	}
	if i >= len(words) || words[i].Text != "task" {
		return "", false
	}
	value, found := "", false
	for i++; i < len(words); i++ {
		name, attached, ok := strings.Cut(words[i].Text, "=")
		switch {
		case ok && name == "--task":
			value, found = attached, true
		case words[i].Text == "--task" && i+1 < len(words):
			i++
			value, found = words[i].Text, true
		}
	}
	return value, found && value != ""
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
