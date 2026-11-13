package cli

import (
	"errors"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskContractExtractCommand returns cw task contract-extract. It reads no
// task and writes nothing.
func newTaskContractExtractCommand(deps taskDeps) *cobra.Command {
	var path, symbolList string
	command := &cobra.Command{Use: "contract-extract", Short: "Print requested Go declarations, without bodies, as a test-engineer contract extract", Long: "Print the requested declarations of the Go package in --path as JSON: each function or method (Type.Method) signature, type (with struct fields and interface methods), const and var, with its doc comment and path:line, and the SHA-256 of every source file a printed declaration comes from. " +
		"Function bodies, function literal bodies and the comments inside them are never printed, and only the requested symbols are. An unknown symbol, a directory without non-test Go source or a parse error fails with nothing printed. " +
		"Save the output outside the project source, review it, and give its path and sha256 as the test-engineer brief's contract; 'cw task assign' refuses an extract that differs from what the current source yields. Other languages keep a prose contract. The task record is not changed.", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if path == "" || symbolList == "" {
			return usage(errors.New("contract-extract needs --path and --symbols"))
		}
		symbols, err := taskflow.ParseContractSymbols(symbolList)
		if err != nil {
			return usage(err)
		}
		dir := path
		if !filepath.IsAbs(dir) {
			working, err := deps.config.WorkingDir()
			if err != nil {
				return err
			}
			dir = filepath.Join(working, dir)
		}
		contract, err := taskflow.ExtractContract(dir, symbols)
		if err != nil {
			return err
		}
		data, err := taskflow.EncodeContract(contract)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}}
	command.Flags().StringVar(&path, "path", "", "Directory of the Go package")
	command.Flags().StringVar(&symbolList, "symbols", "", "Comma-separated symbols: Name or Type.Method")
	return command
}
