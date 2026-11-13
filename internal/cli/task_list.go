package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// taskList is what cw task list --json prints.
type taskList struct {
	Tasks []taskflow.Summary `json:"tasks"`
	// Hidden counts the abandoned and stale tasks of the repository that
	// the list leaves out without --all.
	Hidden int `json:"hidden,omitzero"`
}

// newTaskListCommand returns cw task list. It only reads.
func newTaskListCommand(deps taskDeps) *cobra.Command {
	var repoDir string
	var all, asJSON bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List the tasks of this repository and their state",
		Long: "List the tasks in the task store ($CW_TASK_STORE, else the state directory's workflow-tasks), newest first, " +
			"each with its task ID, state, kind, last update and title. A state is open, verified (its last cw task verify passed; the source may have changed since), " +
			"abandoned, stale (untouched for 14 days, so it no longer gates commits) or unreadable (cw cannot load its record). " +
			"By default it lists the tasks of the Git repository the working directory, or --repo DIR, is in (its main worktree's too) and hides abandoned and stale ones; " +
			"--all lists them, and without --repo lists the tasks of every repository. --json prints one JSON object. The command is read-only.",
		Example: "  cw task list\n  cw task list --all --json\n  cw task list --repo ../other-checkout",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := hookTaskStore(deps.root, deps.config)
			if err != nil {
				return err
			}
			var roots []string
			repoName := ""
			if !all || repoDir != "" {
				dir, err := absolute(deps.config, repoDir)
				if err != nil {
					return err
				}
				repo, err := taskflow.ResolveRepository(cmd.Context(), dir, gateGit)
				if err != nil {
					return fmt.Errorf("%s is not in a Git repository (%v); pass --repo DIR, or --all for every task in the store", dir, err)
				}
				roots, repoName = repo.Roots, repo.Worktree
			}
			tasks, err := taskflow.List(store, roots, deps.config.Now())
			if err != nil {
				return err
			}
			value := taskList{Tasks: []taskflow.Summary{}}
			for _, task := range tasks {
				if !all && (task.State == taskflow.StateAbandoned || task.State == taskflow.StateStale) {
					value.Hidden++
					continue
				}
				value.Tasks = append(value.Tasks, task)
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), value)
			}
			return writeTaskList(cmd.OutOrStdout(), value, repoName, store)
		},
	}
	list.Flags().StringVar(&repoDir, "repo", "", "List the tasks of the Git repository DIR is in instead of the working directory's")
	list.Flags().BoolVar(&all, "all", false, "Also list abandoned and stale tasks, and, without --repo, the tasks of every repository")
	list.Flags().BoolVar(&asJSON, "json", false, "Print one JSON object instead of text")
	return list
}

// writeTaskList writes value as a table, one task per line. repo is the
// repository the list is for, or "" when it lists every repository, which
// adds a REPOSITORY column.
func writeTaskList(out io.Writer, value taskList, repo, store string) error {
	var text strings.Builder
	switch {
	case len(value.Tasks) == 0 && repo != "":
		fmt.Fprintf(&text, "No tasks for %s in %s.\n", repo, store)
	case len(value.Tasks) == 0:
		fmt.Fprintf(&text, "No tasks in %s.\n", store)
	default:
		table := tabwriter.NewWriter(&text, 0, 0, 2, ' ', 0)
		header := "ID\tSTATE\tKIND\tUPDATED\tTITLE"
		if repo == "" {
			header += "\tREPOSITORY"
		}
		fmt.Fprintln(table, header)
		for _, task := range value.Tasks {
			line := strings.Join([]string{task.ID, task.State, orDash(task.Kind), updated(task.UpdatedAt), orDash(oneLine(task.Title))}, "\t")
			if repo == "" {
				line += "\t" + orDash(task.Root)
			}
			fmt.Fprintln(table, line)
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}
	switch value.Hidden {
	case 0:
	case 1:
		text.WriteString("1 abandoned or stale task is hidden; --all lists it.\n")
	default:
		fmt.Fprintf(&text, "%d abandoned or stale tasks are hidden; --all lists them.\n", value.Hidden)
	}
	_, err := io.WriteString(out, text.String())
	return err
}

// updated is a task's last update in UTC to the minute, or "-" when unknown.
func updated(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	return at.UTC().Format("2006-01-02 15:04")
}

// oneLine replaces each control character in title, such as a tab or a line
// end, with a space, so a title cannot break the table.
func oneLine(title string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, title)
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
