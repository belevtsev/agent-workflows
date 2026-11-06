package taskflow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
)

// Coordinators previously ran cw doctor and hashed files by hand to fill values
// that have exactly one valid answer. The helpers below fill such values only
// when the input omits them. Explicit values are kept and validated exactly as
// before, and fields with meaning (status, findings, evidence lists, check IDs)
// are never defaulted. A filled file hash binds the bytes present when the
// record is written; give the hash explicitly to detect an edit made after the
// worker computed it.

// fillRefHashes returns refs with each omitted SHA256 set to the hash of the
// referenced file's current bytes. Paths that are not clean worker paths are
// left unchanged for the normal reference validation to reject.
func fillRefHashes(root *os.Root, refs []SourceRef) ([]SourceRef, error) {
	if refs == nil {
		return nil, nil
	}
	filled := slices.Clone(refs)
	for i := range filled {
		if filled[i].SHA256 != "" || !cleanWorkerPath(filled[i].Path, false) {
			continue
		}
		data, err := readWorkerFile(root, filled[i].Path)
		if err != nil {
			return nil, fmt.Errorf("source reference %s: %w", filled[i].Path, err)
		}
		filled[i].SHA256 = digest(data)
	}
	return filled, nil
}

func fillChangedFileHashes(root *os.Root, changes []WorkerChangedFile) ([]WorkerChangedFile, error) {
	if changes == nil {
		return nil, nil
	}
	filled := slices.Clone(changes)
	for i := range filled {
		if filled[i].SHA256 != "" || filled[i].Deleted || !cleanWorkerPath(filled[i].Path, false) {
			continue
		}
		data, err := readWorkerFile(root, filled[i].Path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("changed file %s is absent; declare it with deleted: true", filled[i].Path)
		}
		if err != nil {
			return nil, fmt.Errorf("changed file %s: %w", filled[i].Path, err)
		}
		filled[i].SHA256 = digest(data)
	}
	return filled, nil
}

// completeWorkerResult fills the task, assignment hash, agent and snapshot from
// the assignment and the current source, and omitted file hashes from current
// bytes.
func completeWorkerResult(record Record, assigned WorkerAssignment, snapshot projectctx.Snapshot, input WorkerResult) (WorkerResult, error) {
	if input.TaskID == "" {
		input.TaskID = record.ID
	}
	if input.AssignmentSHA256 == "" {
		input.AssignmentSHA256 = assigned.SHA256
	}
	if input.AgentID == "" {
		input.AgentID = assigned.Brief.AgentID
	}
	if input.Snapshot == (projectctx.Snapshot{}) {
		input.Snapshot = snapshot
	}
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return input, err
	}
	defer root.Close()
	if input.Evidence, err = fillRefHashes(root, input.Evidence); err != nil {
		return input, err
	}
	if input.Findings != nil {
		input.Findings = slices.Clone(input.Findings)
		for i := range input.Findings {
			if input.Findings[i].Evidence, err = fillRefHashes(root, input.Findings[i].Evidence); err != nil {
				return input, err
			}
		}
	}
	input.ChangedFiles, err = fillChangedFileHashes(root, input.ChangedFiles)
	return input, err
}

// completeAcceptance fills the task, the declared coordinator and the hash of
// the assignment's single immutable result, and omitted file hashes.
func completeAcceptance(record Record, result BoundWorkerResult, snapshot projectctx.Snapshot, input WorkerAcceptance) (WorkerAcceptance, error) {
	if input.TaskID == "" {
		input.TaskID = record.ID
	}
	if input.CoordinatorID == "" {
		input.CoordinatorID = record.Plan.CoordinatorID
	}
	if input.ResultSHA256 == "" {
		input.ResultSHA256 = result.SHA256
	}
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return input, err
	}
	defer root.Close()
	if input.Evidence, err = fillRefHashes(root, input.Evidence); err != nil {
		return input, err
	}
	if input.Findings != nil {
		input.Findings = slices.Clone(input.Findings)
		for i := range input.Findings {
			if input.Findings[i].Evidence, err = fillRefHashes(root, input.Findings[i].Evidence); err != nil {
				return input, err
			}
		}
	}
	return input, nil
}

// completeAssessment fills omitted file hashes in requirement and decision
// evidence.
func completeAssessment(snapshot projectctx.Snapshot, input AssessmentInput) (AssessmentInput, error) {
	root, err := os.OpenRoot(snapshot.Root)
	if err != nil {
		return input, err
	}
	defer root.Close()
	if input.Requirements != nil {
		input.Requirements = slices.Clone(input.Requirements)
		for i := range input.Requirements {
			if input.Requirements[i].Evidence, err = fillRefHashes(root, input.Requirements[i].Evidence); err != nil {
				return input, err
			}
		}
	}
	if input.Decision != nil {
		decision := *input.Decision
		if decision.Evidence, err = fillRefHashes(root, decision.Evidence); err != nil {
			return input, err
		}
		input.Decision = &decision
	}
	return input, nil
}
