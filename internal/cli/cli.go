// Package cli holds the commands of cw, their flags and their messages.
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
	run     func(ctx context.Context, cfg Config, args []string) error
}

// commands lists every command in the order the help prints them.
func commands() []command {
	return []command{
		{"init", "[--observe]", "Create cw.toml if it is missing and install the git hooks", runInit},
		{"add", "<id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]", "Append one check to cw.toml", runAdd},
		{"run", "[<id>...] [--force] [--base <rev>]", "Execute the declared checks that have no passing run on this content", runRun},
		{"status", "[--json]", "Say whether a commit of the working tree would be allowed, and what is missing", runStatus},
		{"version", "", "Print the version, the revision and the build time", runVersion},
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
	return usagef("unknown command %q; run `cw help` for the list", name)
}

func helpFor(cfg Config, name string) error {
	for _, c := range commands() {
		if c.name == name {
			_, _ = fmt.Fprintf(cfg.Out, "usage: %s\n\n%s.\n", usageLine(c), c.summary)
			return nil
		}
	}
	return usagef("unknown command %q; run `cw help` for the list", name)
}

func usageLine(c command) string {
	return strings.TrimSpace("cw " + c.name + " " + c.args)
}

func printHelp(w io.Writer) {
	var b strings.Builder
	b.WriteString("cw allows a commit or a push only when every check the repository\n")
	b.WriteString("declares in cw.toml has a recorded passing run on exactly that content.\n\n")
	b.WriteString("usage: cw <command> [arguments]\n\ncommands:\n")
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %s\n      %s\n", usageLine(c), c.summary)
	}
	b.WriteString("\nexit statuses: 0 done; 1 could not; 2 wrong command line; 3 a hook denies;\n")
	b.WriteString("4 a hook of another tool is left for you to edit; 5 not verified.\n")
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
