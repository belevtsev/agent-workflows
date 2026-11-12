package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskBundleCommand returns cw task bundle.
func newTaskBundleCommand(deps taskDeps) *cobra.Command {
	var directory, reviewID, outputFile string
	bundle := &cobra.Command{Use: "bundle", Short: "Write a review's reviewer bundle, with one reach row per changed file, for the coordinator to fill", Long: "Write the reviewer bundle for a review about to be assigned: the requirements, prior findings with their dispositions, the files changed since the task start and since the previous reviewed source, the checks with their logs, and one reach row per file changed since that review. " +
		"Each row carries hints (the shebang and up to 10 lines of docs and scripts that name the file, invocations first) and four empty facets: callers, invocation_modes, platforms and consumers. " +
		"Fill every facet from source or write 'n/a: <reason>', then give the file's path and sha256 as the review brief's bundle; 'cw task assign' refuses a missing row, an empty facet, or any other difference from the bundle it regenerates. " +
		"Prints one line with the bundle path, its sha256, the kind (full or follow_up), the row count and the changed-file count. The task record is not changed.", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if reviewID == "" || outputFile == "" {
			return usage(errors.New("bundle needs --review-id and --output"))
		}
		target, err := filepath.Abs(outputFile)
		if err != nil {
			return err
		}
		value, err := taskflow.BuildBundle(cmd.Context(), directory, reviewID)
		if err != nil {
			return err
		}
		// Resolve the parent so a symlinked spelling of a path inside the
		// project (such as /tmp for /private/tmp) is still recognized.
		resolved := target
		if parent, parentErr := filepath.EvalSymlinks(filepath.Dir(target)); parentErr == nil {
			resolved = filepath.Join(parent, filepath.Base(target))
		}
		if relative, relErr := filepath.Rel(value.Snapshot.Root, resolved); relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("write the reviewer bundle outside the project source; a file inside it changes the source the review is bound to")
		}
		data, err := taskflow.EncodeBundle(value)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), bundleSummary(target, data, value))
	}}
	bundle.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	bundle.Flags().StringVar(&reviewID, "review-id", "", "Assignment ID the review will use")
	bundle.Flags().StringVar(&outputFile, "output", "", "New file for the bundle, outside the project source")
	return bundle
}
