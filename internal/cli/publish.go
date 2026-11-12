package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/belevtsev/agent-workflows/v3/internal/publish"
	"github.com/spf13/cobra"
)

const publishLong = `Commit the task's verified files, push the commit to REMOTE's branch BRANCH and, with --pr create or --pr edit, create or edit the pull request, only after cw task verify passed on the current source and no other open task record gates the repository.

cw publish authorizes nothing: the coordinator that runs it carries the user's authorization for this publication, and cw is only the mechanism. The coordinator drafts the commit message, PR title and body from verified facts into UTF-8 files.

The commit holds whole files only: the task's verified change set since cw task start, or the --path values (each in that set), as cw task verify saw them. The local branch must be BRANCH and HEAD must be --expect-head, or the commit an earlier run of the same request made. Every step is read back. A re-run of the same command continues from the observed state: it commits, pushes or edits only what is missing. Each run appends a started and a finished entry, publication-NNNNNN.json, to the task directory.

Output: one JSON line (the report, or with --summary a one-line summary); any status but 0 also writes "agent-workflows: publish STATUS (REASON): DETAIL" to stderr. Git 2.24 or newer is required.

Exit statuses:
  0    published  every requested step is done, already present, not needed or not requested
  1    failed     a definite failure (git-missing, locked, task-unreadable, record-invalid, capture-failed, read-failed, record-write-failed, commit-failed); intent-to-add entries may remain after commit-failed
  2    usage error; nothing is printed on stdout
  3    denied     the verify gate refused (task-abandoned, no-baseline-manifest, task-not-verified, source-changed, open-record): verify or abandon
  4    refused    a precondition does not hold (not-a-repository, nothing-to-publish, path-not-verified, branch-mismatch, head-moved, hunk-scope, content-conversion, git-locked, remote-unknown, remote-split, remote-moved, not-fast-forward, gh-missing, repo-mismatch, pr-exists, pr-missing, pr-ambiguous): fix it or use the GitHub worker
  6    unknown    a commit, push or pull-request command did not confirm its outcome (commit-unknown, push-unknown, pr-unknown); read the reconciliation before re-running
  7    mismatch   a step succeeded but its readback differs (commit-mismatch, push-mismatch, pr-mismatch); inspect, never amend
  130  cancelled  interrupted while no commit, push or pull-request command ran, or during a commit that left HEAD unchanged`

func addPublishCommand(root *cobra.Command, config Config) {
	var task, messageFile, expectHead, remote, branch, expectRemote, pr, repo, base, title, bodyFile, timeout string
	var paths []string
	var summary bool
	command := &cobra.Command{
		Use:   "publish",
		Short: "Commit, push and open or edit the pull request for a task verified on the current source",
		Long:  publishLong,
		Args:  noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cmd.Flags()
			if flags.Changed("dry-run") {
				return usage(errors.New("cw publish has no dry run; nothing was changed"))
			}
			given := func(name string, value *string) *string {
				if flags.Changed(name) {
					return value
				}
				return nil
			}
			in := publish.Input{Task: task, ExpectHead: expectHead, Remote: remote, Branch: branch,
				ExpectRemote: given("expect-remote", &expectRemote), PR: given("pr", &pr), Repo: given("repo", &repo),
				Base: given("base", &base), Title: given("title", &title), Timeout: given("timeout", &timeout)}
			if flags.Changed("path") {
				in.Paths = paths
			}
			if flags.Changed("message-file") {
				data, err := readPublishFile(messageFile)
				if err != nil {
					return usage(fmt.Errorf("--message-file: %w", err))
				}
				in.Message = data
			}
			if flags.Changed("body-file") {
				data, err := readPublishFile(bodyFile)
				if err != nil {
					return usage(fmt.Errorf("--body-file: %w", err))
				}
				in.Body, in.BodyGiven = data, true
			}
			req, err := publish.ParseRequest(in)
			if err != nil {
				return usage(err)
			}
			report := publish.Run(cmd.Context(), req, publish.Deps{
				LookPath:  exec.LookPath,
				Environ:   os.Environ(),
				HookStore: func() (string, error) { return hookTaskStore(root, config) },
			})
			var printed any = report
			if summary {
				printed = report.Summary()
			}
			if err := writeJSON(cmd.OutOrStdout(), printed); err != nil {
				return err
			}
			code := report.ExitCode()
			if code != 0 {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "agent-workflows: publish %s (%s): %s\n", report.Status, report.Reason, report.Detail); err != nil {
					return err
				}
			}
			return finish(code, nil)
		},
	}
	flags := command.Flags()
	flags.StringVar(&task, "task", "", "Task directory, as cw task start printed it (required)")
	flags.StringVar(&messageFile, "message-file", "", "UTF-8 commit message file ending with a newline (required)")
	flags.StringVar(&expectHead, "expect-head", "", "Full commit ID HEAD must be (required)")
	flags.StringVar(&remote, "push", "", "Remote to push to (required)")
	flags.StringVar(&branch, "branch", "", "Branch to commit on and push to; the current branch must be this one (required)")
	flags.StringArrayVar(&paths, "path", nil, "Publish only this file of the verified change set, by its manifest spelling (repeatable)")
	flags.StringVar(&expectRemote, "expect-remote", "", "Commit ID the remote branch must be, or absent")
	flags.StringVar(&pr, "pr", publish.PRNone, "Pull request: none, create or edit")
	flags.StringVar(&repo, "repo", "", "GitHub repository OWNER/NAME of the pull request")
	flags.StringVar(&base, "base", "", "Base branch of the pull request")
	flags.StringVar(&title, "title", "", "Pull request title")
	flags.StringVar(&bodyFile, "body-file", "", "UTF-8 pull request body file")
	flags.StringVar(&timeout, "timeout", "10m", "Bound on each git and gh command, from 1s to 2h")
	flags.BoolVar(&summary, "summary", false, "Print a one-line summary instead of the whole report")
	root.AddCommand(command)
}

// readPublishFile reads a message or body file: a regular file of at most
// publish.ReadLimit bytes.
func readPublishFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, publish.ReadLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > publish.ReadLimit {
		return nil, fmt.Errorf("larger than %d bytes", publish.ReadLimit)
	}
	return data, nil
}
