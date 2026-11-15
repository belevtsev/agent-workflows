package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func roleAdviserAccess(ctx context.Context, task string) (map[string][]string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	repo, err := gitx.Open(ctx, dir)
	if errors.Is(err, gitx.ErrNotRepo) && task == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if task != "" {
		b, err := advisers.LoadBinding(repo.Root, task)
		if err != nil {
			return nil, err
		}
		return b.Access, nil
	}
	c, err := advisers.Load(repo.Root)
	if err != nil {
		return nil, err
	}
	return c.Access, nil
}

func allowedAdviserList(access map[string][]string, role string) []string {
	if list, exists := access[role]; exists {
		return list
	}
	return access["default"]
}

func adviserListText(access map[string][]string, role string) string {
	names := allowedAdviserList(access, role)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func printHostRoles(cfg Config, all []roles.Role, host string, access map[string][]string, asJSON bool) error {
	type row struct {
		Role      string   `json:"role"`
		Model     string   `json:"model"`
		Reasoning string   `json:"reasoning"`
		Advisers  []string `json:"advisers"`
	}
	rows := []row{{Role: "coordinator", Model: "host session", Reasoning: "host setting", Advisers: allowedAdviserList(access, "coordinator")}}
	for _, role := range all {
		selected := role.Models[host]
		rows = append(rows, row{role.ID, selected.Model, selected.Effort, allowedAdviserList(access, role.ID)})
	}
	if asJSON {
		return printJSON(cfg, struct {
			Host  string `json:"host"`
			Roles []row  `json:"roles"`
		}{host, rows})
	}
	w := tabwriter.NewWriter(cfg.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ROLE\tMODEL\tREASONING\tADVISERS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.Role, row.Model, row.Reasoning, adviserListText(access, row.Role))
	}
	return w.Flush()
}

func applyAdviserRequirements(root string, report *change.Report, historical string) error {
	status, err := advisers.Evaluate(root, report.Task, report.Content, historical, "")
	if errors.Is(err, advisers.ErrNoBinding) {
		return nil
	}
	if err != nil {
		return err
	}
	rank := map[string]int{"spec": 0, "design": 1, "tests": 2, "code": 3, "review": 4, "commit": 5, "done": 6}
	for _, requirement := range status.Requirements {
		if requirement.State == "satisfied" || rank[requirement.Stage] > rank[report.Stage] {
			continue
		}
		if rank[requirement.Stage] < rank[report.Stage] {
			report.Stage, report.Missing = requirement.Stage, nil
		}
		report.Missing = append(report.Missing, fmt.Sprintf("adviser checkpoint %s (%s, %s): %s", requirement.Checkpoint, requirement.Role, requirement.Adviser, requirement.State))
	}
	return nil
}
