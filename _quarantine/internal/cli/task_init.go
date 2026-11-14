package cli

import (
	"cmp"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// Starter plan constants: the one requirement, the check ids and the timeout
// of the check cw task init writes.
const (
	initRequirementID  = "r1"
	initPackCheckID    = "race"
	initPlaceholderID  = "test"
	initCheckTimeout   = 600
	initDefaultOutFile = "plan.json"
)

// initPlaceholderArgv is the check of a starter plan when no selected pack
// with a race command applies: it fails, so a task cannot pass on it.
var initPlaceholderArgv = []string{"false"}

// taskInitReport is the --json result of cw task init.
type taskInitReport struct {
	Plan        string   `json:"plan"`
	Written     bool     `json:"written"`
	Profile     string   `json:"profile"`
	Pack        string   `json:"pack,omitempty"`
	CheckID     string   `json:"check_id"`
	CheckArgv   []string `json:"check_argv"`
	Placeholder bool     `json:"placeholder"`
	Next        []string `json:"next"`
}

// newTaskInitCommand returns cw task init. It writes a plan file and no task
// state; with --dry-run it writes nothing.
func newTaskInitCommand(deps taskDeps) *cobra.Command {
	var title, repo, profile, out string
	var asJSON, dryRun bool
	command := &cobra.Command{Use: "init", Short: "Write a starter plan for cw task start", Long: helpText("task-init"), Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(title) == "" {
			return usage(errors.New("--title T is required"))
		}
		if _, ok := taskflow.BuiltInProfile(profile); !ok {
			return usage(fmt.Errorf("--profile %q is not lite, standard or strict", profile))
		}
		working, err := deps.config.WorkingDir()
		if err != nil {
			return err
		}
		repo, out = absoluteFrom(working, cmp.Or(repo, ".")), absoluteFrom(working, cmp.Or(out, initDefaultOutFile))
		pack, argv, err := initCheck(deps, repo)
		if err != nil {
			return err
		}
		plan := starterPlan(title, profile, pack, argv)
		if err := taskflow.ValidatePlan(&plan); err != nil {
			return err
		}
		data, err := json.Marshal(plan, jsontext.WithIndent("  "), json.Deterministic(true))
		if err != nil {
			return err
		}
		report := taskInitReport{Plan: out, Profile: profile, Pack: pack, CheckID: plan.Checks[0].ID, CheckArgv: argv, Placeholder: pack == "",
			Next: []string{"cw", "task", "start", "--repo", repo, "--plan", out}}
		if !dryRun {
			if err := writeNewFile(out, append(data, '\n')); err != nil {
				return err
			}
			report.Written = true
		}
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), report)
		}
		return writeInitText(cmd.OutOrStdout(), report)
	}}
	command.Flags().StringVar(&title, "title", "", "The task's title, also the starter requirement's description")
	command.Flags().StringVar(&repo, "repo", "", "Selected project directory whose top-level files choose the pack (default: current working directory)")
	command.Flags().StringVar(&profile, "profile", taskflow.DefaultProfile, "The profile the plan names: lite, standard or strict")
	command.Flags().StringVar(&out, "out", "", "The plan file to create; an existing file is not replaced (default: plan.json in the current working directory)")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Print the result without writing the plan file")
	command.Flags().BoolVar(&asJSON, "json", false, "Print the result as one JSON object instead of text")
	return command
}

// initCheck returns the first selected pack detected at the top of repo that
// declares a race command, and that command; without one it returns no pack
// and the placeholder argv.
func initCheck(deps taskDeps, repo string) (string, []string, error) {
	_, selected, err := startPacks(deps.root, deps.config)
	if err != nil {
		return "", nil, err
	}
	entries, err := os.ReadDir(repo)
	if err != nil {
		return "", nil, fmt.Errorf("cannot read the selected project: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, applied := range langpack.Applicable(selected, names) {
		for _, manifest := range selected {
			if manifest.ID == applied.ID && len(manifest.Commands.Race) > 0 {
				return manifest.ID, manifest.Commands.Race, nil
			}
		}
	}
	return "", initPlaceholderArgv, nil
}

// starterPlan is the plan cw task init writes: one substantial requirement
// bound to one check. Under lite and standard it marks the adviser
// checkpoints not required; strict keeps them required.
func starterPlan(title, profile, pack string, argv []string) taskflow.Plan {
	checkID := initPackCheckID
	if pack == "" {
		checkID = initPlaceholderID
	}
	plan := taskflow.Plan{
		Title:        title,
		Kind:         "substantial",
		Profile:      profile,
		Requirements: []taskflow.Requirement{{ID: initRequirementID, Description: title, CheckIDs: []string{checkID}}},
		Checks:       []taskflow.Check{{ID: checkID, Argv: argv, TimeoutSeconds: initCheckTimeout}},
	}
	if profile != "strict" {
		plan.Checkpoints = &taskflow.CheckpointPolicy{Reason: "The " + profile + " profile skips adviser checkpoints; set required to true to run them."}
	}
	return plan
}

// writeNewFile creates path, private to the user, with data and fails when
// it exists.
func writeNewFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s exists; pass --out FILE or move it", path)
	}
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return errors.Join(err, file.Close())
}

// writeInitText prints the result of cw task init for a person.
func writeInitText(w io.Writer, report taskInitReport) error {
	verb := "Wrote"
	if !report.Written {
		verb = "Would write (--dry-run)"
	}
	lines := []string{fmt.Sprintf("%s %s with profile %s.", verb, report.Plan, report.Profile)}
	if report.Placeholder {
		lines = append(lines, "No selected pack with a race command applies, so check "+report.CheckID+" runs `false` and fails: replace its argv with the command that tests your change.")
	} else {
		lines = append(lines, fmt.Sprintf("Check %s runs the %s pack's race command: %s", report.CheckID, report.Pack, shellJoin(report.CheckArgv)))
	}
	lines = append(lines, "Requirement r1 repeats the title; sharpen it or add requirements if you like, then run:", "  "+shellJoin(report.Next))
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}

// absoluteFrom returns path, made absolute against dir when relative.
func absoluteFrom(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}

// shellJoin joins argv for a POSIX shell, single-quoting each argument that
// holds anything but letters, digits and -_./=:,+@%.
func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		if arg != "" && strings.Trim(arg, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:,+@%") == "" {
			quoted[i] = arg
			continue
		}
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
