package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
	"github.com/belevtsev/agent-workflows/internal/taskflow"
	"github.com/spf13/cobra"
)

// Claude Code plugin hooks. Both commands read the hook's JSON input from
// stdin and write the hook's JSON decision to stdout. They fail open: every
// condition that is not a positive gate decision, including any internal
// error, exits 0 without output, so a damaged record, an unreadable store or a
// missing git never blocks the user's own commands.
const (
	hookInputLimit        = 1 << 20
	hookGitTimeout        = 5 * time.Second
	hookDoctorTimeout     = 5 * time.Second
	hookDoctorCacheAge    = time.Hour
	enforceVerifyOption   = "CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY"
	sessionDoctorOption   = "CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR"
	smallTaskPromptOption = "CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT"
)

type hookInput struct {
	CWD       string `json:"cwd"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

type hookOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

func addHookCommands(root *cobra.Command, config Config) {
	hook := &cobra.Command{Use: "hook", Short: "Claude Code plugin hooks: load conventions and gate git commit and push on cw task verify", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	root.AddCommand(hook)
	hook.AddCommand(&cobra.Command{
		Use:   "pre-publish",
		Short: "PreToolUse hook: deny git commit and push while any open substantial task record for the repository lacks a passing cw task verify on the source being published",
		Long: "Reads the PreToolUse JSON from stdin. When the Bash command runs git commit or git push inside a Git repository and the task store holds " +
			"substantial, unabandoned records for that repository written within 14 days, it prints a deny decision naming every one whose verification has not passed " +
			"on the source being published, and an ask decision for a push of a verified repository whose working tree has uncommitted tracked changes. " +
			"With no such record, it prints an ask decision when the change looks substantial (two of: more than 300 changed lines, more than one top-level directory, " +
			"code changed without tests). Every other case, including any internal error, exits 0 without output. " +
			"Set the plugin option enforce_verify to false (" + enforceVerifyOption + "=false) to disable the gate, " +
			"and small_task_prompt to false (" + smallTaskPromptOption + "=false) to disable the ask for unrecorded changes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			decision, ok := prePublishDecision(cmd.Context(), root, config)
			if !ok {
				return nil
			}
			return writeJSON(cmd.OutOrStdout(), decision)
		},
	})
	var instructions, cache string
	sessionStart := &cobra.Command{
		Use:   "session-start",
		Short: "SessionStart hook: emit the instructions file, plus a cached cw doctor summary when the plugin option session_doctor is true, as additional context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if instructions == "" {
				return usage(errors.New("--instructions FILE is required"))
			}
			context, ok := sessionStartContext(cmd.Context(), root, config, instructions, cache)
			if !ok {
				return nil
			}
			return writeJSON(cmd.OutOrStdout(), hookOutput{hookSpecificOutput{HookEventName: "SessionStart", AdditionalContext: context}})
		},
	}
	sessionStart.Flags().StringVar(&instructions, "instructions", "", "Markdown file whose contents become the session's additional context")
	sessionStart.Flags().StringVar(&cache, "cache", "", "Directory for the one-hour doctor summary cache (no caching when empty)")
	hook.AddCommand(sessionStart)
}

// prePublishDecision returns the deny or ask decision for the hook input on
// stdin, or false when the command may proceed silently. A deny for any
// publication in the command wins over an ask for another.
func prePublishDecision(ctx context.Context, root *cobra.Command, config Config) (hookOutput, bool) {
	enforce := config.Getenv(enforceVerifyOption) != "false"
	prompt := config.Getenv(smallTaskPromptOption) != "false"
	if !enforce && !prompt {
		return hookOutput{}, false
	}
	input, err := readHookInput(config.In)
	if err != nil {
		return hookOutput{}, false
	}
	publications := gitPublications(input.ToolInput.Command, input.CWD)
	if len(publications) == 0 {
		return hookOutput{}, false
	}
	store, err := hookTaskStore(root, config)
	if err != nil {
		return hookOutput{}, false
	}
	var gating []taskflow.OpenRecord
	var asks []string
	// `git commit && git push` publishes from one worktree twice; find its
	// records, and digest its source, once.
	found := map[string][]taskflow.OpenRecord{}
	for _, publication := range publications {
		repo, err := repositoryOf(ctx, publication.dir)
		if err != nil {
			continue
		}
		open, seen := found[repo.Worktree]
		if !seen {
			if open, err = taskflow.FindOpen(ctx, store, repo, time.Now(), taskflow.SourceDigest); err != nil {
				continue
			}
			found[repo.Worktree] = open
		}
		blocked := false
		for _, record := range open {
			if record.Gates() {
				blocked = true
				if !slices.ContainsFunc(gating, func(seen taskflow.OpenRecord) bool { return seen.Record.ID == record.Record.ID }) {
					gating = append(gating, record)
				}
			}
		}
		var ask string
		switch {
		case blocked:
		case len(open) > 0:
			if enforce && publication.push && uncommittedTrackedChanges(ctx, repo.Worktree) {
				ask = uncommittedReason(open)
			}
		case prompt:
			if signs := substantialChangeSigns(ctx, repo.Worktree, publication); len(signs) >= 2 {
				ask = unrecordedChangeReason(repo.Worktree, publication, signs)
			}
		}
		if ask != "" && !slices.Contains(asks, ask) {
			asks = append(asks, ask)
		}
	}
	switch {
	case enforce && len(gating) > 0:
		return preToolUseDecision("deny", denyReason(gating)), true
	case len(asks) > 0:
		return preToolUseDecision("ask", strings.Join(asks, " ")), true
	}
	return hookOutput{}, false
}

func preToolUseDecision(decision, reason string) hookOutput {
	return hookOutput{hookSpecificOutput{HookEventName: "PreToolUse", PermissionDecision: decision, PermissionDecisionReason: reason}}
}

// denyReason names every gating record, why it gates, and how to verify or
// abandon it.
func denyReason(gating []taskflow.OpenRecord) string {
	var reason strings.Builder
	reason.WriteString("agent-workflows: git commit and push stay blocked while a task for this repository is open: each must pass cw task verify on the source being published, or be closed.")
	for _, open := range gating {
		record := open.Record
		fmt.Fprintf(&reason, " Task %s (%s): %s; run `cw task verify --task %s --summary`, or close it with `cw task abandon --task %s --reason TEXT` if it is finished or was given up.", record.ID, record.Plan.Title, open.Problem, record.Directory, record.Directory)
	}
	reason.WriteString(" To turn the gate off, set enforce_verify=false in the plugin config.")
	return reason.String()
}

// uncommittedReason explains the ask for a push whose verified records may
// not cover what is pushed.
func uncommittedReason(open []taskflow.OpenRecord) string {
	tasks := make([]string, 0, len(open))
	for _, record := range open {
		tasks = append(tasks, fmt.Sprintf("%s (%s)", record.Record.ID, record.Record.Plan.Title))
	}
	subject := "task"
	if len(tasks) > 1 {
		subject = "tasks"
	}
	return fmt.Sprintf("agent-workflows: %s %s passed cw task verify on the working tree, but the working tree has uncommitted changes to tracked files, so the pushed commits may not hold the verified source. Commit the changes and verify again, or confirm to push anyway.", subject, strings.Join(tasks, ", "))
}

func readHookInput(reader io.Reader) (hookInput, error) {
	var input hookInput
	if reader == nil {
		return input, errors.New("no hook input")
	}
	data, err := io.ReadAll(io.LimitReader(reader, hookInputLimit+1))
	if err != nil {
		return input, err
	}
	if len(data) > hookInputLimit {
		return input, errors.New("hook input exceeds the size limit")
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return input, err
	}
	if input.CWD == "" {
		return input, errors.New("hook input has no cwd")
	}
	return input, nil
}

// shellPrefixWords are words that may precede a simple command without being
// its program and take no argument of their own: shell keywords that open a
// compound command and the common command prefixes.
var shellPrefixWords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "while": true, "until": true, "do": true, "{": true, "!": true, "exec": true, "builtin": true, "nohup": true, "noglob": true, "eval": true}

// wrapperArgumentOptions lists the wrappers that run their remaining words
// as a command after options of their own, with the options that take a
// separate argument. `timeout` also takes a duration before the command.
// `command -v git` stays a lookup: git without a subcommand publishes
// nothing.
var wrapperArgumentOptions = map[string][]string{
	"command": {},
	"env":     {"-u", "--unset", "-C", "--chdir", "-P"},
	"time":    {"-f", "--format", "-o", "--output"},
	"nice":    {"-n", "--adjustment"},
	"timeout": {"-s", "--signal", "-k", "--kill-after"},
	"stdbuf":  {"-i", "-o", "-e", "--input", "--output", "--error"},
	"sudo":    {"-u", "--user", "-g", "--group", "-h", "--host", "-p", "--prompt", "-U", "--other-user", "-r", "--role", "-t", "--type", "-C", "--close-from", "-D", "--chdir", "-R", "--chroot", "-T", "--command-timeout"},
}

// shells run a command string given with -c; its words follow on the same
// line, so the hook reads them as the command.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// publication is one git commit or git push in a command line.
type publication struct {
	dir  string
	push bool
	// all is a commit with -a or --all, which also commits modified tracked
	// files that are not staged.
	all bool
}

// gitPublications returns every git commit or push in a shell command line,
// with the directory it runs in resolved from cwd through any preceding
// `cd DIR` or `pushd DIR`, a wrapper's `env -C DIR` or `sudo -D DIR` and
// git's own -C DIR options, without duplicates and in command order. The
// split is deliberately simple: simple commands are separated by `;`, `|`,
// `&`, newlines and parentheses, and words by whitespace with surrounding
// quotes removed, so the words of a quoted `sh -c '...'` or `eval "..."`
// string are read as the command they run. A `$(...)` or backtick
// substitution without nested parentheses or backticks stands for one word
// of the command around it, and the command inside it is read on its own
// from the directory in effect there. A directory change inside parentheses
// or a substitution ends with them, as in a subshell. After a directory
// change the hook cannot follow (`cd` with options, `cd -`, `~`, `$VAR`,
// quoted paths with spaces, `popd`), the directory is unknown and later git
// commands are not checked until an absolute `cd` or -C, since the hook
// fails open. Redirections on a cd (`cd DIR >/dev/null 2>&1`) are ignored,
// and `"$(pwd)"` or `"$(git rev-parse --show-toplevel)"` stays in the same
// repository.
func gitPublications(command, cwd string) []publication {
	// NUL is reserved for substitution markers; a command that carries one
	// could otherwise make a substitution reference itself.
	command = strings.ReplaceAll(command, "\x00", "")
	command = backtickSubstitution.ReplaceAllString(command, "$$($1)")
	command = toplevelSubstitution.ReplaceAllString(command, ".")
	var substitutions []string
	for {
		next := commandSubstitution.ReplaceAllStringFunc(command, func(match string) string {
			substitutions = append(substitutions, match[2:len(match)-1])
			return substitutionMarker(len(substitutions) - 1)
		})
		if next == command {
			break
		}
		command = next
	}
	scanner := publicationScanner{substitutions: substitutions}
	scanner.scan(command, cwd)
	return scanner.found
}

// backtickSubstitution matches a backtick substitution without nested
// backticks; it is rewritten to the equivalent `$(...)`.
var backtickSubstitution = regexp.MustCompile("`([^`]*)`")

// commandSubstitution matches a `$(...)` without nested parentheses.
var commandSubstitution = regexp.MustCompile(`\$\([^()]*\)`)

// substitutionReference matches the marker that stands for a substitution's
// word; gitPublications removes any NUL from the command first.
var substitutionReference = regexp.MustCompile("\x00([0-9]+)\x00")

func substitutionMarker(index int) string { return "\x00" + strconv.Itoa(index) + "\x00" }

// publicationScanner collects the publications of a command line whose
// substitutions are replaced by markers indexing substitutions.
type publicationScanner struct {
	substitutions []string
	found         []publication
}

// scan reads command from cwd. Each substitution referenced by a simple
// command is read, in its own scope, from the directory in effect for that
// simple command before the command is.
func (s *publicationScanner) scan(command, cwd string) {
	dir := cwd
	var scopes []string
	visit := func(segment string) {
		for _, match := range substitutionReference.FindAllStringSubmatch(segment, -1) {
			if index, err := strconv.Atoi(match[1]); err == nil && index < len(s.substitutions) {
				s.scan(s.substitutions[index], dir)
			}
		}
		words, chdirs := commandWords(substitutionReference.ReplaceAllString(segment, "_"))
		if len(words) > 0 && (words[0] == "cd" || words[0] == "pushd" || words[0] == "popd") {
			words = withoutRedirections(words)
			if len(words) == 2 && words[0] != "popd" && followablePath(words[1]) {
				dir = resolveDir(dir, words[1])
			} else {
				dir = ""
			}
			return
		}
		runs := dir
		for _, chdir := range chdirs {
			if !followablePath(chdir) {
				return
			}
			runs = resolveDir(runs, chdir)
		}
		target, subcommand, arguments := gitInvocation(words, runs)
		if target == "" || (subcommand != "commit" && subcommand != "push") {
			return
		}
		next := publication{dir: target, push: subcommand == "push", all: subcommand == "commit" && commitsAll(arguments)}
		if !slices.Contains(s.found, next) {
			s.found = append(s.found, next)
		}
	}
	start := 0
	for i, r := range command {
		switch r {
		case ';', '|', '&', '\n', '(', ')', '`':
		default:
			continue
		}
		visit(command[start:i])
		start = i + 1
		switch r {
		case '(':
			scopes = append(scopes, dir)
		case ')':
			if len(scopes) > 0 {
				dir = scopes[len(scopes)-1]
				scopes = scopes[:len(scopes)-1]
			}
		}
	}
	visit(command[start:])
}

// commitsAll reports whether git commit arguments include -a or --all,
// alone or in a cluster of short options such as -am, before an option
// that takes a value.
func commitsAll(arguments []string) bool {
	for _, argument := range arguments {
		switch {
		case argument == "--":
			return false
		case argument == "--all":
			return true
		case strings.HasPrefix(argument, "--") || !strings.HasPrefix(argument, "-"):
			continue
		}
		for _, option := range argument[1:] {
			if option == 'a' {
				return true
			}
			if strings.ContainsRune("mFcCt", option) {
				break // the rest of the cluster is this option's value
			}
		}
	}
	return false
}

// toplevelSubstitution matches a command substitution that names the
// current directory or repository's top level. A cd or -C into it stays in
// the same repository, so it is rewritten to `.` before the command is
// split.
var toplevelSubstitution = regexp.MustCompile(`"?\$\(\s*(git\s+rev-parse\s+--show-toplevel|pwd)\s*\)"?`)

// withoutRedirections drops redirection words (`>FILE`, `2>FILE`, `>>FILE`,
// `<FILE`, or the operator alone followed by its target word) from a simple
// command. `&` is already a command separator, so `2>&1` and `&>FILE` arrive
// here split.
func withoutRedirections(words []string) []string {
	kept := words[:1:1]
	for i := 1; i < len(words); i++ {
		operator := strings.TrimLeft(words[i], "0123456789")
		if !strings.HasPrefix(operator, ">") && !strings.HasPrefix(operator, "<") {
			kept = append(kept, words[i])
			continue
		}
		if strings.Trim(operator, "<>") == "" {
			i++ // the operator stands alone; its target is the next word
		}
	}
	return kept
}

// followablePath reports whether a cd argument is a literal path the hook
// can resolve without a shell: not an option, not `-`, and free of `~`, `$`
// and glob characters.
func followablePath(path string) bool {
	return path != "" && !strings.HasPrefix(path, "-") && !strings.ContainsAny(path, "~$*?[{")
}

// commandWords splits a simple command into words with surrounding quotes
// removed (a word that was only quotes is dropped), then drops what precedes
// its program: NAME=value assignments, shell prefix words, wrappers with
// their options (`env -i`, `nice -n 5`, `timeout 30`, `sudo -u x`), `sh -c`
// and a backslash that bypasses an alias. It also returns, in order, the
// directories `env -C DIR` and `sudo -D DIR` move the command to, one per
// wrapper (`env -C a env -C b` runs in a/b).
func commandWords(segment string) ([]string, []string) {
	var words, chdirs []string
	for _, word := range strings.Fields(segment) {
		if word = strings.Trim(word, `'"`); word != "" {
			words = append(words, word)
		}
	}
	for len(words) > 0 {
		word := strings.TrimPrefix(words[0], `\`)
		_, wrapper := wrapperArgumentOptions[word]
		switch {
		case shellPrefixWords[word] || (strings.Contains(word, "=") && !strings.HasPrefix(word, "-")):
			words = words[1:]
		case wrapper:
			var moves []string
			words, moves = wrapperOptions(word, words[1:])
			chdirs = append(chdirs, moves...)
			if word == "timeout" && len(words) > 0 {
				words = words[1:] // the duration
			}
		case shells[word] && len(words) > 1 && commandStringOption(words[1]):
			words = words[2:]
		default:
			words[0] = word
			return words, chdirs
		}
	}
	return words, chdirs
}

// wrapperOptions drops a wrapper's leading options, with the argument of
// each one listed in wrapperArgumentOptions, up to and including a `--`. A
// short option's argument may be attached (`-oL`, `-C/dir`) or follow a
// cluster (`-iC /dir`). It returns the remaining words and the directory
// `env -C`/`--chdir` or `sudo -D`/`--chdir` moves the command to, if any;
// as in env and sudo, the last one wins and a relative one is resolved from
// the directory the wrapper runs in.
func wrapperOptions(wrapper string, words []string) ([]string, []string) {
	takes := wrapperArgumentOptions[wrapper]
	chdir := map[string]string{"env": "-C", "sudo": "-D"}[wrapper]
	var chdirs []string
	for len(words) > 0 && strings.HasPrefix(words[0], "-") && words[0] != "-" {
		option := words[0]
		words = words[1:]
		if option == "--" {
			break
		}
		if strings.HasPrefix(option, "--") {
			name, value, attached := strings.Cut(option, "=")
			if !attached && slices.Contains(takes, name) && len(words) > 0 {
				value, attached = words[0], true
				words = words[1:]
			}
			if name == "--chdir" && attached && chdir != "" {
				chdirs = []string{value}
			}
			continue
		}
		for i := 1; i < len(option); i++ {
			letter := "-" + option[i:i+1]
			if !slices.Contains(takes, letter) {
				continue
			}
			value := option[i+1:]
			if value == "" && len(words) > 0 {
				value = words[0]
				words = words[1:]
			}
			if letter == chdir {
				chdirs = []string{value}
			}
			break // the rest of the cluster is this option's argument
		}
	}
	return words, chdirs
}

// commandStringOption reports whether a shell option cluster such as -c,
// -lc or -ec makes the shell read its command from the next argument.
func commandStringOption(word string) bool {
	return len(word) > 1 && word[0] == '-' && word[1] != '-' && strings.ContainsRune(word[1:], 'c')
}

// gitInvocation returns the directory git runs in, its subcommand and the
// subcommand's arguments when words invoke git, or "" when they do not.
// git's global options are skipped; the ones that take a separate argument
// consume it, and each -C DIR moves the directory like git does.
func gitInvocation(words []string, dir string) (string, string, []string) {
	if len(words) == 0 || filepath.Base(words[0]) != "git" {
		return "", "", nil
	}
	for i := 1; i < len(words); i++ {
		word := words[i]
		if !strings.HasPrefix(word, "-") {
			return dir, word, words[i+1:]
		}
		switch word {
		case "-C":
			if i+1 < len(words) {
				dir = resolveDir(dir, words[i+1])
			}
			i++
		case "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--super-prefix", "--config-env":
			i++
		}
	}
	return "", "", nil
}

// resolveDir follows a cd-like path change from dir. A relative path from
// an unknown directory ("") stays unknown. Paths the hook cannot resolve
// lexically, such as $VAR after git -C, are left to git, which then fails
// and lets the command through.
func resolveDir(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, path)
}

// repositoryOf returns where a command in dir publishes from: the canonical
// root of the worktree that contains dir and, for a linked worktree, the
// main worktree's root as well, so a record started in either location
// gates both.
func repositoryOf(ctx context.Context, dir string) (taskflow.Repository, error) {
	toplevel, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return taskflow.Repository{}, err
	}
	roots := []string{}
	add := func(path string) {
		if canonical, err := filepath.EvalSymlinks(path); err == nil && !slices.Contains(roots, canonical) {
			roots = append(roots, canonical)
		}
	}
	add(toplevel)
	if len(roots) == 0 {
		return taskflow.Repository{}, errors.New("worktree root is unavailable")
	}
	common, err := gitOutput(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		// Git before 2.31 has no --path-format; its plain form is relative to dir.
		common, err = gitOutput(ctx, dir, "rev-parse", "--git-common-dir")
	}
	if err == nil {
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		if filepath.Base(common) == ".git" {
			add(filepath.Dir(common))
		}
	}
	return taskflow.Repository{Worktree: roots[0], Roots: roots}, nil
}

// gitOutput runs git in dir and returns its output without trailing line
// ends; empty output is an error.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	output, err := gitRun(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	value := strings.TrimRight(output, "\r\n")
	if value == "" {
		return "", errors.New("git printed nothing")
	}
	return value, nil
}

// gitRun runs git in dir, with the file system monitor off, without
// optional index writes and with a deadline, and returns its raw output.
func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, hookGitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir}, args...)...)
	command.Stderr = io.Discard
	output, err := command.Output()
	return string(output), err
}

// hookTaskStore returns $CW_TASK_STORE, a hook-only override that
// `cw task start` does not read (it takes --store), else the store
// `cw task start` writes to: the state directory's workflow-tasks
// ($XDG_STATE_HOME/agent-workflows or ~/.local/state/agent-workflows).
func hookTaskStore(root *cobra.Command, config Config) (string, error) {
	if store := config.Getenv("CW_TASK_STORE"); store != "" {
		return store, nil
	}
	roots, err := resolveRoots(root, config)
	if err != nil {
		return "", err
	}
	return filepath.Join(roots.state, "workflow-tasks"), nil
}

// sessionStartContext returns the instructions file, followed by the doctor
// summary when the plugin option asks for it and it can be produced in time.
func sessionStartContext(ctx context.Context, root *cobra.Command, config Config, instructions, cache string) (string, bool) {
	data, err := os.ReadFile(instructions)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return "", false
	}
	text := strings.TrimRight(string(data), "\n")
	if config.Getenv(sessionDoctorOption) != "true" {
		return text, true
	}
	input, err := readHookInput(config.In)
	dir := input.CWD
	if err != nil {
		if dir, err = config.WorkingDir(); err != nil {
			return text, true
		}
	}
	if summary, ok := cachedDoctorSummary(ctx, root, config, dir, cache); ok {
		text += "\n\n" + summary
	}
	return text, true
}

// cachedDoctorSummary returns the one-line doctor summary for dir, served from the
// cache when it is younger than an hour. Any failure yields no summary.
func cachedDoctorSummary(ctx context.Context, root *cobra.Command, config Config, dir, cache string) (string, bool) {
	selected, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	path := ""
	if cache != "" {
		key := sha256.Sum256([]byte(selected))
		path = filepath.Join(cache, "doctor", hex.EncodeToString(key[:])+".json")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) < hookDoctorCacheAge {
			if data, err := os.ReadFile(path); err == nil && jsontext.Value(data).IsValid() {
				return strings.TrimSpace(string(data)), true
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, hookDoctorTimeout)
	defer cancel()
	roots, err := resolveRoots(root, config)
	if err != nil {
		return "", false
	}
	directory, err := credentialDirectory(root, config)
	if err != nil {
		return "", false
	}
	report, err := projectctx.Doctor(ctx, projectctx.DoctorOptions{Repo: selected, Home: roots.home, CodexHome: roots.codex, ClaudeHome: roots.claude, ConfigDir: directory, Timeout: hookDoctorTimeout, Getenv: config.Getenv})
	if err != nil {
		return "", false
	}
	data, err := json.Marshal(summarizeDoctor(report), json.Deterministic(true))
	if err != nil {
		return "", false
	}
	if path != "" {
		writeDoctorCache(path, data)
	}
	return string(data), true
}

// writeDoctorCache replaces the cache entry atomically; a failure only costs
// the next session a fresh doctor run.
func writeDoctorCache(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".doctor-*")
	if err != nil {
		return
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return
	}
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	_ = os.Rename(temporary.Name(), path)
}
