package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
)

// HasSelections includes malformed locations so change check can report them.
func HasSelections(root, task string) (bool, error) {
	if !validTask(task) {
		return false, fmt.Errorf("invalid skill task %q", task)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return false, err
	}
	defer func() { _ = r.Close() }()
	full, small := selectionPaths(task)
	for _, dir := range []string{full, small} {
		exists, err := realDirectory(r, dir, false)
		if exists || err != nil {
			return true, nil
		}
	}
	return false, nil
}

// Tasks lists full tasks and small skill histories without following linked
// task directories. The archive and hidden full-path entries are excluded.
func Tasks(root string) ([]string, error) {
	full, err := historyTasks(root, "openspec/changes", false)
	if err != nil {
		return nil, err
	}
	small, err := SmallTasks(root)
	return append(full, small...), err
}

// SmallTasks lists every small history, refusing malformed or linked roots.
func SmallTasks(root string) ([]string, error) {
	return historyTasks(root, smallSelections, true)
}

func historyTasks(root, dir string, small bool) ([]string, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	exists, err := realDirectory(r, dir, false)
	if err != nil || !exists {
		return nil, err
	}
	entries, err := fs.ReadDir(r.FS(), dir)
	if err != nil {
		return nil, err
	}
	var tasks []string
	for _, entry := range entries {
		if !small && (entry.Name() == "archive" || strings.HasPrefix(entry.Name(), ".")) {
			continue
		}
		if !small && !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		if !validTask(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s/%s is not a real task directory", dir, entry.Name())
		}
		tasks = append(tasks, entry.Name())
	}
	return tasks, nil
}

// ValidateSelections checks saved receipts offline, without reading installed
// skills. Full histories may retain immutable small receipts after promotion.
func ValidateSelections(root, task string) ([]string, error) {
	if !validTask(task) {
		return nil, fmt.Errorf("invalid skill task %q", task)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	full, small := selectionPaths(task)
	var problems []string
	found := 0
	for _, dir := range []string{full, small} {
		exists, err := realDirectory(r, dir, false)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if !exists {
			continue
		}
		found++
		entries, err := fs.ReadDir(r.FS(), dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := path.Join(dir, entry.Name())
			if strings.HasPrefix(entry.Name(), ".tmp-") {
				if info, err := r.Lstat(name); err == nil && info.Mode().IsRegular() {
					continue
				}
			}
			if err := checkSelectionFile(r, name, task, dir == small); err != nil {
				problems = append(problems, name+": "+err.Error())
			}
		}
	}
	if found > 1 {
		problems = append(problems, "task has competing full and small skill histories")
	}
	return problems, nil
}

func checkSelectionFile(r *os.Root, name, task string, small bool) error {
	id, ok := strings.CutSuffix(path.Base(name), ".json")
	info, err := r.Lstat(name)
	if err != nil {
		return err
	}
	if !ok || !validDigest(id) || !info.Mode().IsRegular() {
		return fmt.Errorf("receipt must be a regular <SHA256>.json file")
	}
	data, err := readSelection(r, name)
	if err != nil {
		return err
	}
	var doc selection
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if err := doc.validate(); err != nil {
		return err
	}
	if doc.Task != task || small && doc.Workflow != "small" {
		return fmt.Errorf("task or workflow differs from the receipt's location")
	}
	canonical, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if !bytes.Equal(append(canonical, '\n'), data) || id != fmt.Sprintf("%x", sha256.Sum256(data)) {
		return fmt.Errorf("receipt content is not canonical or its SHA256 filename differs")
	}
	return nil
}
