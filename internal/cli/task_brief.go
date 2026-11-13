package cli

import (
	"errors"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskBriefCommand returns cw task brief. It only reads.
func newTaskBriefCommand(deps taskDeps) *cobra.Command {
	var directory, stage string
	var asJSON bool
	brief := &cobra.Command{
		Use:   "brief",
		Short: "Print the brief a fresh worker of one stage starts from, at most 4 KiB",
		Long: "Read a task record and print what a fresh worker of --stage needs: the task ID and title, the profile and the author rounds left for the stage, " +
			"the one role contract the worker loads, the stored contract, bundle and input files of the stage's latest assignment, the task's checks, " +
			"the plan requirements, and the stage's open findings, newest first, with each result's report file. " +
			"The output is at most 4,096 bytes: every entry is listed whole or not at all, and the last line counts what is not listed, such as older findings. " +
			"--json prints the same brief as one JSON object. A stage that is not in the task's profile exits 2. The command is read-only.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.requireTask(&directory); err != nil {
				return err
			}
			if stage == "" {
				return usage(errors.New("--stage S is required"))
			}
			record, err := taskflow.Load(directory)
			if err != nil {
				return err
			}
			value, err := taskflow.StageBrief(record, stage)
			if errors.Is(err, taskflow.ErrUnknownStage) {
				return usage(err)
			}
			if err != nil {
				return err
			}
			out := []byte(value.Text())
			if asJSON {
				if out, err = taskflow.EncodeBrief(value); err != nil {
					return err
				}
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
	taskFlag(brief, &directory)
	brief.Flags().StringVar(&stage, "stage", "", "The stage the worker answers, one of the task profile's stages")
	brief.Flags().BoolVar(&asJSON, "json", false, "Print the brief as one JSON object instead of text")
	return brief
}
