package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/skills"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

const smallAdviserChanges = change.Dir + "/.advice"

// smallAdviserTasks includes malformed histories so checks cannot omit them.
// Inspect every path component before reading; record directories cannot be links.
func smallAdviserTasks(root string) ([]string, error) {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	component := ""
	for _, name := range strings.Split(smallAdviserChanges, "/") {
		component = path.Join(component, name)
		info, err := directory.Lstat(component)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is not a real adviser record directory", component)
		}
	}
	entries, err := fs.ReadDir(directory.FS(), smallAdviserChanges)
	if err != nil {
		return nil, err
	}
	tasks := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !taskName.MatchString(entry.Name()) {
			return nil, fmt.Errorf("%s/%s is not a real task directory with a valid task name",
				smallAdviserChanges, entry.Name())
		}
		tasks = append(tasks, entry.Name())
	}
	return tasks, nil
}

// checkChanges validates current full and small records, including unfinished
// tasks. Checkpoint readiness is checked separately by advisers check.
func (s *session) checkChanges(tasks []string) error {
	out := s.cfg.Out
	if len(tasks) == 0 {
		var err error
		tasks, err = skills.Tasks(s.repo.Root)
		if err != nil {
			fmt.Fprintf(out, "%v\nbroken rules: 1; skill records could not be checked\n", err)
			return exit(ExitNotVerified)
		}
		small, err := smallAdviserTasks(s.repo.Root)
		if err != nil {
			fmt.Fprintln(out, err)
			fmt.Fprintln(out, "broken rules: 1; adviser records could not be checked")
			return exit(ExitNotVerified)
		}
		tasks = append(tasks, small...)
		verified, err := verification.Tasks(s.repo.Root)
		if err != nil {
			return failf("%v", err)
		}
		tasks = append(tasks, verified...)
		tasks, err = appendUsageBudgetTasks(s.repo.Root, tasks)
		if err != nil {
			return failf("%v", err)
		}
		slices.Sort(tasks)
		tasks = slices.Compact(tasks)
	}
	broken := 0
	for _, task := range tasks {
		count, err := s.checkChange(task)
		if err != nil {
			return err
		}
		broken += count
	}
	switch {
	case len(tasks) == 0:
		fmt.Fprintf(out, "%s/ holds no change folder\n", change.Dir)
	case broken > 0:
		fmt.Fprintf(out, "broken rules: %d; each line above names the file, the line and the rule\n", broken)
		return exit(ExitNotVerified)
	}
	return nil
}

func (s *session) checkChange(task string) (int, error) {
	if count, err := validateUsageBudget(s, task); err != nil || count != 0 {
		return count, err
	}
	verificationProblems, err := verification.ValidateRecords(s.repo.Root, task)
	if err != nil {
		return 0, failf("%v", err)
	}
	for _, problem := range verificationProblems {
		fmt.Fprintf(s.cfg.Out, "%s verification records: %s\n", task, problem)
	}
	if len(verificationProblems) != 0 {
		return len(verificationProblems), nil
	}
	skillProblems, err := skills.ValidateSelections(s.repo.Root, task)
	if err != nil {
		return 0, failf("%v", err)
	}
	for _, problem := range skillProblems {
		fmt.Fprintf(s.cfg.Out, "%s skill records: %s\n", task, problem)
	}
	if len(skillProblems) > 0 {
		return len(skillProblems), nil
	}
	folder, err := change.Read(s.repo.Root, task)
	if errors.Is(err, change.ErrNoFolder) {
		if err := s.checkSmallTaskExists(task); err != nil {
			return 0, failf("%v", err)
		}
	} else if err != nil {
		return 0, failf("%v", err)
	}
	count := 0
	if folder != nil {
		problems, err := change.Check(folder)
		if err != nil {
			return 0, failf("%v", err)
		}
		for _, problem := range problems {
			fmt.Fprintln(s.cfg.Out, problem)
		}
		count += len(problems)
	}
	problems, err := advisers.ValidateRecords(s.repo.Root, task)
	if err != nil {
		return 0, failf("%v", err)
	}
	for _, problem := range problems {
		fmt.Fprintf(s.cfg.Out, "%s adviser records: %s\n", task, problem)
	}
	if count += len(problems); count == 0 {
		fmt.Fprintf(s.cfg.Out, "%s: the files obey their rules\n", task)
	}
	return count, nil
}

func (s *session) checkSmallTaskExists(task string) error {
	hasSkills, err := skills.HasSelections(s.repo.Root, task)
	if err != nil {
		return err
	}
	hasVerification, err := hasVerificationHistory(s.repo.Root, task)
	if err != nil {
		return err
	}
	hasBudget, err := hasUsageBudget(s.repo.Root, task)
	if err != nil {
		return err
	}
	_, adviceErr := advisers.Location(s.repo.Root, task)
	// Malformed adviser locations remain task evidence to validate below.
	// ValidateRecords reports their structural problems as broken content rules.
	if errors.Is(adviceErr, advisers.ErrNoBinding) && !hasSkills && !hasVerification && !hasBudget {
		return fmt.Errorf("%s/%s/ does not exist", change.Dir, task)
	}
	return nil
}
