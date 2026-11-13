package cli

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/codexinstall"
	"github.com/spf13/cobra"
)

const claudePluginHint = "Claude Code installs agent-workflows as a plugin: run /plugin marketplace add belevtsev/agent-workflows, then /plugin install agent-workflows@agent-workflows"

// addHostCommands registers cw install and cw uninstall.
func addHostCommands(root *cobra.Command, config Config) {
	for _, install := range []bool{true, false} {
		var host, from string
		command := &cobra.Command{
			Use:   "uninstall --host codex [--from DIR]",
			Short: "Remove the Codex skill links and the AGENTS.md block",
			Long: "Remove the symlinks in ~/.agents/skills that point into the checkout and the managed block from $CODEX_HOME/AGENTS.md. " +
				"Entries that do not point into the checkout stay.",
			Args: noArgs,
		}
		if install {
			command.Use = "install --host codex [--from DIR]"
			command.Short = "Link the skills into Codex and write the AGENTS.md block"
			command.Long = "Link every skills-manifest.json registration from a checkout into ~/.agents/skills and write the managed block into $CODEX_HOME/AGENTS.md. " +
				"Running it again after git pull updates the links and the block. It refuses to replace any entry that does not already point into the checkout, " +
				"and prints recommended config.toml settings instead of editing config.toml. The checkout defaults to $XDG_DATA_HOME/agent-workflows/src " +
				"(git clone https://github.com/belevtsev/agent-workflows there)."
		}
		command.RunE = func(cmd *cobra.Command, _ []string) error {
			switch {
			case host == "claude":
				return usage(errors.New(claudePluginHint))
			case host != "codex":
				return usage(errors.New("--host codex is required"))
			}
			dirs, err := resolveRoots(root, config)
			if err != nil {
				return err
			}
			source, err := absolute(config, cmp.Or(from, defaultCheckout(dirs.home, config)))
			if err != nil {
				return err
			}
			options := codexinstall.Options{Source: source, Home: dirs.home, CodexHome: dirs.codex}
			if install {
				report, err := codexinstall.Install(options)
				if err != nil {
					return err
				}
				printInstall(cmd.OutOrStdout(), report, filepath.Join(dirs.codex, "config.toml"))
				return nil
			}
			report, err := codexinstall.Uninstall(options)
			if err != nil {
				return err
			}
			printInstall(cmd.OutOrStdout(), report, "")
			if !slices.ContainsFunc(report.Changes, func(c codexinstall.Change) bool { return c.Action == "unlink" }) {
				fmt.Fprintf(cmd.OutOrStdout(), "no skill links into %s; if you installed with --from DIR, run uninstall with the same --from\n", source)
			}
			return nil
		}
		command.Flags().StringVar(&host, "host", "", "Agent host: codex (Claude Code uses the plugin)")
		command.Flags().StringVar(&from, "from", "", "agent-workflows checkout (default $XDG_DATA_HOME/agent-workflows/src)")
		root.AddCommand(command)
	}
}

// defaultCheckout is $XDG_DATA_HOME/agent-workflows/src, else
// ~/.local/share/agent-workflows/src.
func defaultCheckout(home string, config Config) string {
	data := cmp.Or(config.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	return filepath.Join(data, "agent-workflows", "src")
}

func printInstall(out io.Writer, report codexinstall.Report, configPath string) {
	for _, change := range report.Changes {
		line := change.Action + " " + change.Path
		if change.Target != "" {
			line += " -> " + change.Target
		}
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "%s: managed block %s\n", report.Instructions, report.Block)
	if configPath != "" && report.RecommendedConfig != "" {
		fmt.Fprintf(out, "\nRecommended settings for %s (cw does not edit it):\n\n%s\n", configPath, strings.TrimRight(report.RecommendedConfig, "\n"))
	}
}
