package cli

import (
	"fmt"
	"io"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskReceiptCommand returns cw task receipt. It only reads.
func newTaskReceiptCommand(deps taskDeps) *cobra.Command {
	var directory string
	var asJSON bool
	receipt := &cobra.Command{
		Use:   "receipt",
		Short: "Print what a task used and where it stands, at most 4 KiB",
		Long: "Read a task record and print its receipt: the profile and the author rounds each capped stage used against its cap, " +
			"the fix rounds on each open finding or requirement, every owner approval with the owner's quoted words, " +
			"the estimated dollars with their price table and the tokens (cached and uncached apart) against the budget, or unknown with the reason and never 0, " +
			"the checks planned, the runs executed and failed and the checks verify credited by reuse, the findings by severity and status with the open ones listed, " +
			"and the wall time. The output is at most 4,096 bytes: every entry is listed whole or not at all, and the last line counts what is not listed. " +
			"cw task verify, cw task abandon and a budget stop print the same text on stderr. --json prints the receipt as one JSON object. The command is read-only.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.requireTask(&directory); err != nil {
				return err
			}
			record, err := taskflow.Load(directory)
			if err != nil {
				return err
			}
			value := taskflow.Receipt(record)
			out := []byte(value.Text())
			if asJSON {
				if out, err = taskflow.EncodeReceipt(value); err != nil {
					return err
				}
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
	taskFlag(receipt, &directory)
	receipt.Flags().BoolVar(&asJSON, "json", false, "Print the receipt as one JSON object instead of text")
	return receipt
}

// writeReceipt writes the text receipt of the task at directory after a
// command's own output; a record it cannot read leaves a line saying so.
func writeReceipt(w io.Writer, directory string) {
	record, err := taskflow.Load(directory)
	if err != nil {
		fmt.Fprintf(w, "task receipt unavailable: %v\n", err)
		return
	}
	fmt.Fprint(w, taskflow.Receipt(record).Text())
}
