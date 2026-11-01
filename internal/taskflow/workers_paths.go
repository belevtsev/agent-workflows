package taskflow

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
)

type workerPathInfo struct {
	final     os.FileInfo
	ancestors []os.FileInfo
}

// inspectWorkerPath rejects aliases in every existing component. Checking the
// exact directory entry spelling also rejects case aliases on case-insensitive
// hosts while permitting genuinely distinct names on case-sensitive hosts.
// A missing suffix is permitted only for future ownership or explicit deletion.
func inspectWorkerPath(root *os.Root, name string, allowMissing bool) (workerPathInfo, error) {
	if !cleanWorkerPath(name, true) {
		return workerPathInfo{}, errors.New("worker path must be clean and repository-relative")
	}
	parentInfo, err := root.Stat(".")
	if err != nil {
		return workerPathInfo{}, err
	}
	if name == "." {
		return workerPathInfo{final: parentInfo}, nil
	}
	result := workerPathInfo{}
	parent := "."
	parts := strings.Split(name, "/")
	for i, part := range parts {
		candidate := part
		if parent != "." {
			candidate = parent + "/" + part
		}
		info, err := root.Lstat(candidate)
		if errors.Is(err, fs.ErrNotExist) && allowMissing {
			return result, nil
		}
		if err != nil {
			return workerPathInfo{}, fmt.Errorf("worker path %s: %w", candidate, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return workerPathInfo{}, fmt.Errorf("worker paths cannot contain symlink components: %s", candidate)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return workerPathInfo{}, fmt.Errorf("worker path component must be a regular file or directory: %s", candidate)
		}
		directory, err := root.Open(parent)
		if err != nil {
			return workerPathInfo{}, err
		}
		opened, statErr := directory.Stat()
		if statErr != nil || !os.SameFile(parentInfo, opened) {
			directory.Close()
			return workerPathInfo{}, errors.New("worker path ancestor changed while opening")
		}
		entries, readErr := directory.ReadDir(100001)
		directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return workerPathInfo{}, readErr
		}
		if len(entries) > 100000 {
			return workerPathInfo{}, errors.New("worker path directory exceeds the 100000 entry inspection bound")
		}
		exact := false
		for _, entry := range entries {
			if entry.Name() == part {
				exact = true
				break
			}
		}
		if !exact {
			return workerPathInfo{}, fmt.Errorf("worker path must use exact on-disk component spelling: %s", candidate)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return workerPathInfo{}, fmt.Errorf("worker path ancestor is not a directory: %s", candidate)
		}
		result.ancestors = append(result.ancestors, info)
		parent, parentInfo = candidate, info
	}
	result.final = parentInfo
	return result, nil
}

func validateWorkerOwnedPaths(snapshot projectctx.Snapshot, owned []string) error {
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, name := range owned {
		if _, err := inspectWorkerPath(root, name, true); err != nil {
			return err
		}
	}
	return nil
}

// Physical comparisons cover the named scopes and their existing ancestors.
// They detect direct hardlinked file scopes or aliased directory roots without
// attributing edits or scanning distinct subtrees for every possible hardlink.
func workerScopesOverlap(snapshot projectctx.Snapshot, left, right []string) (bool, error) {
	for _, a := range left {
		for _, b := range right {
			if scopeContains(a, b) || scopeContains(b, a) {
				return true, nil
			}
		}
	}
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return false, err
	}
	defer root.Close()
	leftInfo, rightInfo := []workerPathInfo{}, []workerPathInfo{}
	for _, name := range left {
		info, err := inspectWorkerPath(root, name, true)
		if err != nil {
			return false, err
		}
		leftInfo = append(leftInfo, info)
	}
	for _, name := range right {
		info, err := inspectWorkerPath(root, name, true)
		if err != nil {
			return false, err
		}
		rightInfo = append(rightInfo, info)
	}
	for _, a := range leftInfo {
		for _, b := range rightInfo {
			if a.final != nil {
				for _, ancestor := range b.ancestors {
					if os.SameFile(a.final, ancestor) {
						return true, nil
					}
				}
			}
			if b.final != nil {
				for _, ancestor := range a.ancestors {
					if os.SameFile(b.final, ancestor) {
						return true, nil
					}
				}
			}
		}
	}
	return false, nil
}

func validateWorkerOwnership(record Record, snapshot projectctx.Snapshot, brief WorkerBrief) error {
	if err := validateWorkerOwnedPaths(snapshot, brief.OwnedPaths); err != nil {
		return err
	}
	if brief.Role != "implementation" {
		return nil
	}
	for _, assigned := range record.Assignments {
		if assigned.Brief.ID == brief.ID || assigned.Brief.Role != "implementation" || findWorkerResult(record, assigned.Brief.ID) != nil {
			continue
		}
		overlaps, err := workerScopesOverlap(snapshot, brief.OwnedPaths, assigned.Brief.OwnedPaths)
		if err != nil {
			return err
		}
		if overlaps {
			return fmt.Errorf("implementation ownership overlaps active assignment %s; record its terminal handoff before reassigning", assigned.Brief.ID)
		}
	}
	return nil
}

func validateWorkerRefs(snapshot projectctx.Snapshot, refs []SourceRef) error {
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, ref := range refs {
		if _, err := inspectWorkerPath(root, ref.Path, false); err != nil {
			return err
		}
	}
	return validateRefs(snapshot, refs)
}
