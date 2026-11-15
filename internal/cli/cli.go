// Package cli holds the commands of kitsu, their flags and their messages.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Config is what the program gives the commands.
type Config struct {
	Version  string
	Revision string
	BuiltAt  string
	In       io.Reader
	Out      io.Writer
	Err      io.Writer
}

// command is one entry of the command table.
type command struct {
	name    string
	args    string // the arguments, as the usage line shows them
	summary string
	options string // what `kitsu <command> --help` prints below the summary: one line for each argument and option
	run     func(ctx context.Context, cfg Config, args []string) error
}

// commands lists every command in the order the help prints them.
func commands() []command {
	return []command{
		{"advisers", "list [--role <role>] [--task <task>] [--json] | bind | resolve | status | check", "Configure task adviser bindings, inspect requirements, and record dispositions", adviserHelp(), runAdvisers},
		{"consult", "--task <task> --role <role> --stage <stage> --adviser <name> --request <file|-> [--checkpoint <id>] [--json]", "Ask a configured adviser and keep the complete exchange in the task record", consultHelp(), runConsult},
		{"roles", "[--host claude|codex] [--task <task>] [--json] | set <role> --host claude|codex [--model <model>] [--reasoning <effort>] [--json] | reset <role> --host claude|codex [--json] | check <role> --from <tree> [--task <task>] [--finalize] | text <role>", "Print or configure role models and reasoning, list advisers, check writes, or print instructions", roleTableHelp(), runRoles},
		{"change", "status [<task>] [--json] | check [<task>]", "Print the stage of a task's change folder, or check the rules its files must obey", "\n  status [<task>]  print the stage of one task, or of every task\n  --json           with status: print one JSON object instead of text\n  check [<task>]   exit 5 when the files of one task, or of any task, break a rule\n", runChange},
		{"tokens", "[--task <task>] [--json] | record [--host claude|codex]", "Report the tokens a task used and the share the workflow itself took, or record them from a host's notice", "\n  --task <task>               print the roles and the stages of one task\n  --json                      print one JSON object instead of text\n  record --host claude|codex  record the tokens from the host's notice on standard input; without --host, claude\n", runTokens},
		{"init", "[--observe]", "Create kitsu.toml if it is missing and install the git hooks", "\n  --observe  record what the hooks would decide, and allow every commit and push\n", runInit},
		{"add", "<id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]", "Append one check to kitsu.toml", "\n  <id>                          the name of the check\n  --paths <pattern>             a path pattern the check depends on; give it once for each pattern; without it, every path\n  --timeout <duration>          the time the check may take, from 1s to 6h; without it, 10m\n  -- <program> [<argument>...]  the program of the check and its arguments, started without a shell\n", runAdd},
		{"run", "[<id>...] [--force] [--base <rev>]", "Execute the declared checks that have no passing run on this content", "\n  <id>...       consider only these checks\n  --force       execute them even when they have a passing run on this content\n  --base <rev>  count a check as passed when its checked content is the same as in the revision <rev>\n", runRun},
		{"status", "[--json]", "Say whether a commit of the working tree would be allowed, and what is missing", "\n  --json  print one JSON object instead of text\n", runStatus},
		{"stats", "[--since <period>] [--json]", "Count the runs and the hook decisions this repository recorded", "\n  --since <period>  count the records of this period, such as 90m, 36h, 7d or 2w; without it, 30d\n  --json            print one JSON object instead of text\n", runStats},
		{"context", "[--host claude|codex]", "Print what an agent should know at the start of a session here", "\n  --host claude|codex  read the host's notice from standard input and judge the directory it names\n", runContext},
		{"hook", "<name> [<argument>...]", "Decide for git: pre-commit, pre-merge-commit or pre-push", "\n  <name>         pre-commit, pre-merge-commit or pre-push\n  <argument>...  what git gives that hook; the hook files that kitsu init writes pass them on\n", runHook},
		{"guard", "[--host claude|codex]", "For an agent host's hook: ask before a git call that skips the hooks, or an edit of kitsu.toml, a hook or a record", "\n  --host claude  the answer asks the user; this is the default\n  --host codex   the answer denies the call and gives the reason\n", runGuard},
		{"uninstall", "", "Remove the hooks kitsu wrote and its records; kitsu.toml stays", "", runUninstall},
		{"version", "", "Print the version, the revision and the build time", "", runVersion},
	}
}

// Execute runs the command the arguments name.
func Execute(ctx context.Context, args []string, cfg Config) error {
	if len(args) == 0 {
		printHelp(cfg.Err)
		return usagef("no command given")
	}
	name := args[0]
	switch name {
	case "help", "--help", "-h":
		if len(args) > 1 {
			return helpFor(cfg, args[1])
		}
		printHelp(cfg.Out)
		return nil
	case "--version":
		name = "version"
	}
	for _, c := range commands() {
		if c.name != name {
			continue
		}
		for _, a := range args[1:] {
			if a == "--" {
				break
			}
			if a == "--help" || a == "-h" {
				return helpFor(cfg, name)
			}
		}
		return c.run(ctx, cfg, args[1:])
	}
	return usagef("unknown command %q; run `kitsu help` for the list", name)
}

func helpFor(cfg Config, name string) error {
	for _, c := range commands() {
		if c.name == name {
			_, _ = fmt.Fprintf(cfg.Out, "usage: %s\n\n%s.\n%s", usageLine(c), c.summary, c.options)
			return nil
		}
	}
	return usagef("unknown command %q; run `kitsu help` for the list", name)
}

func usageLine(c command) string {
	return strings.TrimSpace("kitsu " + c.name + " " + c.args)
}

func printHelp(w io.Writer) {
	var b strings.Builder
	b.WriteString("kitsu coordinates coding-agent workflows with role instructions,\n")
	b.WriteString("shared task records, role checks and token reports. Your agent host runs\n")
	b.WriteString("the workflow. Its supporting Git gate requires passing check evidence\n")
	b.WriteString("on exactly the content being committed or pushed.\n\n")
	b.WriteString("usage: kitsu <command> [arguments]\n\ncommands:\n")
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %s\n      %s\n", usageLine(c), c.summary)
	}
	b.WriteString("\nexit statuses: 0 done; 1 could not; 2 wrong command line; 3 a hook denies;\n4 a hook of another tool is left for you to edit; 5 not verified.\n")
	_, _ = io.WriteString(w, b.String())
}

// noArguments refuses a command line that holds anything.
func noArguments(name string, args []string) error {
	p, err := parseArgs(newFlags(name), args)
	if err != nil {
		return err
	}
	if len(p.pos) > 0 || p.dash {
		return usagef("%s takes no arguments", name)
	}
	return nil
}
