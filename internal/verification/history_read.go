package verification

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
)

// ValidateRecords validates shared history offline. Missing histories are fine;
// malformed locations, broken bindings, and damaged old attempts are problems.
func ValidateRecords(root, task string) ([]string, error) {
	if !historyTask(task) {
		return nil, fmt.Errorf("invalid verification task %q", task)
	}
	r, err := openHistory(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	full, small := historyPaths(task)
	problems := []string{}
	found := 0
	for _, rel := range []string{full, small} {
		exists, err := r.directory(rel, false)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if !exists {
			continue
		}
		found++
		if _, err := r.history(rel, task, false); err != nil {
			problems = append(problems, rel+": "+err.Error())
		}
	}
	if found > 1 {
		problems = append(problems, "task has competing full and small verification histories")
	}
	return problems, nil
}

func (r *historyFS) history(rel, task string, recoverPromotion bool) (History, error) {
	if err := r.historyLayout(rel); err != nil {
		return History{}, err
	}
	var binding historyBinding
	if _, err := r.read(path.Join(rel, "binding.json"), &binding); err != nil {
		return History{}, fmt.Errorf("read verification binding: %w", err)
	}
	if binding.Version != 1 || !historyHash(binding.ID) {
		return History{}, errors.New("verification binding requires version 1 and a SHA256 attempt id")
	}
	entries, err := r.entries(path.Join(rel, "attempts"))
	if err != nil {
		return History{}, err
	}
	current := History{}
	for _, entry := range entries {
		name := path.Join(rel, "attempts", entry.Name())
		if historyTemporary(entry) {
			continue
		}
		attempt, err := r.attempt(name, task, rel)
		if err != nil {
			return History{}, err
		}
		if attempt.ID == binding.ID {
			current = attempt
		}
	}
	if current.ID == "" {
		return History{}, errors.New("verification binding references a missing attempt")
	}
	full, _ := historyPaths(task)
	if rel == full && current.Attempt.Workflow != "full" && !recoverPromotion {
		return current, fmt.Errorf("%w; publish with full workflow to recover", ErrPromotionPending)
	}
	return current, nil
}

func (r *historyFS) historyLayout(rel string) error {
	entries, err := r.entries(rel)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if historyTemporary(entry) {
			continue
		}
		switch entry.Name() {
		case "binding.json":
			if !entry.Type().IsRegular() {
				return errors.New("verification binding must be a regular file")
			}
		case "attempts":
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return errors.New("verification attempts must be a real directory")
			}
		default:
			return fmt.Errorf("unknown verification history entry %q", entry.Name())
		}
	}
	return nil
}

func historyTemporary(entry fs.DirEntry) bool {
	return strings.HasPrefix(entry.Name(), ".tmp-") && entry.Type().IsRegular()
}

func (r *historyFS) attempt(name, task, rel string) (History, error) {
	id, ok := strings.CutSuffix(path.Base(name), ".json")
	if !ok || !historyHash(id) {
		return History{}, fmt.Errorf("verification attempt %q must have a SHA256.json filename", name)
	}
	var attempt Attempt
	data, err := r.read(name, &attempt)
	if err != nil {
		return History{}, err
	}
	if err := validateAttempt(attempt); err != nil {
		return History{}, fmt.Errorf("verification attempt %s: %w", id, err)
	}
	_, small := historyPaths(task)
	if attempt.Task != task || (rel == small && attempt.Workflow != "small") {
		return History{}, errors.New("verification attempt task or workflow differs from its location")
	}
	if id != historyDigest(data) {
		return History{}, errors.New("verification attempt content does not match its SHA256 filename")
	}
	return History{ID: id, Attempt: attempt}, nil
}
