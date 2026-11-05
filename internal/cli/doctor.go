package cli

import (
	"errors"
	"time"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
	"github.com/spf13/cobra"
)

func addDoctorCommand(root *cobra.Command, config Config, o options) {
	var repo, skillsHome string
	var live bool
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Inspect the selected project and optional local capabilities without changing it",
		Long:  "Inspect the current project or --repo PATH independently of the workflow installation source. Default inspection reads local metadata only. --live permits bounded status, protocol and package-version probes; it does not provision packages, index sources, enroll watchers or call models.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usage(errors.New("doctor accepts no positional arguments; use --repo PATH"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if timeout < 100*time.Millisecond || timeout > time.Minute {
				return usage(errors.New("--timeout must be between 100ms and 1m"))
			}
			selected := repo
			if selected == "" {
				var err error
				selected, err = config.WorkingDir()
				if err != nil {
					return errors.New("resolve current project directory")
				}
			}
			roots, err := resolveRoots(root, config)
			if err != nil {
				return err
			}
			directory, err := credentialDirectory(root, config)
			if err != nil {
				return err
			}
			report, err := projectctx.Doctor(cmd.Context(), projectctx.DoctorOptions{Repo: selected, Home: roots.home, CodexHome: roots.codex, ClaudeHome: roots.claude, SkillsHome: skillsHome, ConfigDir: directory, Live: live, Timeout: timeout, Getenv: config.Getenv})
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), report)
		},
	}
	command.Flags().StringVar(&repo, "repo", "", "Selected project directory (defaults to the current directory)")
	command.Flags().StringVar(&skillsHome, "skills-home", "", "Skill entrypoint directory (defaults to the Codex and Claude Code skill roots)")
	command.Flags().BoolVar(&live, "live", false, "Run bounded optional local status, protocol and package-version probes")
	command.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "Complete inspection deadline (100ms to 1m)")
	root.AddCommand(command)
}
