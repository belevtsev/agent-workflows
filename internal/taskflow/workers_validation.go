package taskflow

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

func sameEncoded(a, b any) bool {
	left, err := encode(a)
	if err != nil {
		return false
	}
	right, err := encode(b)
	return err == nil && bytes.Equal(left, right)
}

// validateBoundWorkerChecks checks the runs a stored result or acceptance
// binds: each terminal at snapshot or, for completed work, a reuse candidate
// whose source digest is sourceSHA256, the source the binding was made at.
func validateBoundWorkerChecks(record Record, snapshot projectctx.Snapshot, sourceSHA256 string, checkIDs, runIDs []string, completed bool) error {
	if len(checkIDs) != len(runIDs) {
		return errors.New("worker check bindings differ from declared check IDs")
	}
	seen := map[string]bool{}
	for i, id := range runIDs {
		var found *CheckRun
		for j := range record.Checks {
			if record.Checks[j].ID == id {
				found = &record.Checks[j]
				break
			}
		}
		reused := completed && sourceSHA256 != "" && found != nil && reuseCandidate(*found) && found.SourceSHA256 == sourceSHA256
		if found == nil || seen[id] || found.CheckID != checkIDs[i] || found.FinishedAt == nil || found.Status == "running" || found.After == nil || !equalSnapshot(snapshot, *found.After) && !reused {
			return errors.New("worker references a missing, nonterminal or mismatched actual check run")
		}
		seen[id] = true
		if completed && (found.Status != "passed" || found.ExitCode == nil || *found.ExitCode != 0 || !equalSnapshot(found.Before, *found.After)) {
			return errors.New("completed worker evidence references a check that did not pass at its source")
		}
	}
	return nil
}

func validateWorkerRecord(record Record) error {
	if !validAgentID(record.Plan.CoordinatorID) {
		return errors.New("task needs a declared coordinator identity")
	}
	if len(record.Assignments) > 256 || len(record.WorkerResults) > 256 || len(record.WorkerAcceptances) > 1024 {
		return errors.New("stored worker collections exceed their bounds")
	}
	assignments := map[string]bool{}
	for i, assigned := range record.Assignments {
		if assigned.TaskID != record.ID || assignments[assigned.Brief.ID] || !validWorkerSnapshot(record, assigned.Snapshot) || assigned.AssignedAt.Before(record.CreatedAt) || assigned.AssignedAt.After(record.UpdatedAt) {
			return errors.New("invalid stored worker assignment identity, snapshot or time")
		}
		assignments[assigned.Brief.ID] = true
		if err := validateBrief(record, assigned.Brief); err != nil {
			return err
		}
		// The round rule needs the results recorded when the brief was
		// assigned, which only the revision chain knows, not the clock;
		// validateWorkerTransition applies it to the revision that adds the
		// brief. Here the rest of the brief's stage rules apply.
		returned := func(string) bool { return false }
		if err := validateBriefStage(record, record.Assignments[:i], returned, assigned.Brief); err != nil {
			return err
		}
		hash, err := assignmentHash(assigned)
		if err != nil || !validSHA256(assigned.SHA256) || hash != assigned.SHA256 {
			return errors.New("stored worker assignment hash differs")
		}
		if (assigned.BundleFile != "") != (assigned.Brief.Bundle != nil) || assigned.BundleFile != "" && !bundleFileName.MatchString(assigned.BundleFile) {
			return errors.New("stored review assignment bundle binding is invalid")
		}
		if (assigned.ContractFile != "") != (assigned.Brief.Contract != nil) || assigned.ContractFile != "" && !contractFileName.MatchString(assigned.ContractFile) {
			return errors.New("stored assignment contract binding is invalid")
		}
		if err := validateInputFiles(assigned); err != nil {
			return err
		}
	}
	results := map[string]bool{}
	for _, bound := range record.WorkerResults {
		assigned := findAssignment(record, bound.Result.AssignmentID)
		if assigned == nil || results[bound.Result.AssignmentID] || bound.SubmittedAt.Before(assigned.AssignedAt) || bound.SubmittedAt.After(record.UpdatedAt) {
			return errors.New("stored worker result has a missing/duplicate assignment or invalid time")
		}
		results[bound.Result.AssignmentID] = true
		if err := validateResult(record, *assigned, bound.Result); err != nil {
			return err
		}
		if !validSHA256(bound.ScopeSHA256) || bound.SourceSHA256 != "" && !validSHA256(bound.SourceSHA256) || assigned.Brief.Role == "review" && !validSHA256(bound.ReviewEvidenceSHA256) || assigned.Brief.Role != "review" && bound.ReviewEvidenceSHA256 != "" {
			return errors.New("stored worker result scope or review dependency binding is invalid")
		}
		hash, err := workerResultHash(bound)
		if err != nil || hash != bound.SHA256 {
			return errors.New("stored worker result hash differs")
		}
		if err := validateBoundWorkerChecks(record, bound.Result.Snapshot, bound.SourceSHA256, bound.Result.CheckIDs, bound.CheckRuns, bound.Result.Status == "completed"); err != nil {
			return err
		}
	}
	for _, bound := range record.WorkerAcceptances {
		result := findWorkerResult(record, bound.Acceptance.AssignmentID)
		if result == nil || !validWorkerSnapshot(record, bound.Snapshot) || bound.AcceptedAt.Before(result.SubmittedAt) || bound.AcceptedAt.After(record.UpdatedAt) {
			return errors.New("stored worker acceptance has missing result, invalid snapshot or time")
		}
		if err := validateAcceptance(record, *result, bound.Acceptance); err != nil {
			return err
		}
		if bound.SourceSHA256 != "" && !validSHA256(bound.SourceSHA256) {
			return errors.New("stored worker acceptance source digest is invalid")
		}
		if err := validateBoundWorkerChecks(record, bound.Snapshot, bound.SourceSHA256, bound.Acceptance.CheckIDs, bound.CheckRuns, true); err != nil {
			return err
		}
	}
	return nil
}

// Every revision is validated against its predecessor as well as in isolation.
// This prevents rewritten briefs/results/acceptances even when someone appends
// syntactically valid JSON with a valid previous hash.
func validateWorkerTransition(previous, current Record) error {
	if len(current.Assignments) < len(previous.Assignments) || len(current.WorkerResults) < len(previous.WorkerResults) || len(current.WorkerAcceptances) < len(previous.WorkerAcceptances) {
		return errors.New("worker history is append-only")
	}
	if !sameEncoded(previous.Assignments, current.Assignments[:len(previous.Assignments)]) || !sameEncoded(previous.WorkerResults, current.WorkerResults[:len(previous.WorkerResults)]) || !sameEncoded(previous.WorkerAcceptances, current.WorkerAcceptances[:len(previous.WorkerAcceptances)]) {
		return errors.New("immutable worker history changed")
	}
	added := len(current.Assignments) - len(previous.Assignments) + len(current.WorkerResults) - len(previous.WorkerResults) + len(current.WorkerAcceptances) - len(previous.WorkerAcceptances)
	if added > 1 || added != 0 && current.EvidenceRevision != previous.EvidenceRevision+1 {
		return errors.New("worker mutation needs one independently bound append-only evidence revision")
	}
	if len(current.Assignments) > len(previous.Assignments) {
		assigned := current.Assignments[len(previous.Assignments)]
		if assigned.Brief.Role == "review" && assigned.BundleFile == "" {
			return errors.New("stored review assignment lacks its reviewer bundle")
		}
		// An assignment is the only worker change of its revision, so the
		// previous revision holds exactly the results AssignWorker saw.
		returned := func(id string) bool { return findWorkerResult(previous, id) != nil }
		if err := validateBriefStage(current, previous.Assignments, returned, assigned.Brief); err != nil {
			return err
		}
		if assigned.Brief.Role == "implementation" {
			for _, other := range previous.Assignments {
				if other.Brief.Role != "implementation" || findWorkerResult(previous, other.Brief.ID) != nil {
					continue
				}
				for _, a := range assigned.Brief.OwnedPaths {
					for _, b := range other.Brief.OwnedPaths {
						if scopeContains(a, b) || scopeContains(b, a) {
							return errors.New("stored assignment overlaps an active implementation writer")
						}
					}
				}
			}
		}
	}
	if len(current.WorkerResults) > len(previous.WorkerResults) {
		bound := current.WorkerResults[len(previous.WorkerResults)]
		if findAssignment(previous, bound.Result.AssignmentID) == nil {
			return errors.New("worker result must follow its assignment revision")
		}
		runs, err := workerChecks(previous, recordedCredit(bound.Result.Snapshot, bound.SourceSHA256), bound.Result.CheckIDs, bound.Result.Status == "completed")
		if err != nil || !slices.Equal(runs, bound.CheckRuns) {
			return errors.New("stored result did not bind the actual latest check runs")
		}
		assigned := findAssignment(previous, bound.Result.AssignmentID)
		if assigned.Brief.Role == "review" {
			binding, err := reviewEvidenceBinding(previous, bound.Result.AssignmentID)
			if err != nil || binding != bound.ReviewEvidenceSHA256 {
				return errors.New("stored review did not bind the preceding worker/check evidence")
			}
		}
	}
	if len(current.WorkerAcceptances) > len(previous.WorkerAcceptances) {
		bound := current.WorkerAcceptances[len(previous.WorkerAcceptances)]
		result := findWorkerResult(previous, bound.Acceptance.AssignmentID)
		if result == nil {
			return errors.New("worker acceptance must follow its result revision")
		}
		runs, err := bindChecks(previous, recordedCredit(bound.Snapshot, bound.SourceSHA256), bound.Acceptance.CheckIDs)
		if err != nil || !slices.Equal(runs, bound.CheckRuns) {
			return errors.New("stored acceptance did not bind actual latest passed checks")
		}
		assigned := findAssignment(previous, bound.Acceptance.AssignmentID)
		if bound.Acceptance.Status == "accepted" && assigned.Brief.Role == "review" {
			binding, err := reviewEvidenceBinding(previous, bound.Acceptance.AssignmentID)
			if err != nil || binding != result.ReviewEvidenceSHA256 || !equalSnapshot(bound.Snapshot, result.Result.Snapshot) {
				return errors.New("stored acceptance approved a stale review")
			}
		}
	}
	// Once a worker has referenced a terminal check, later revisions cannot
	// rewrite that run's status, output hashes or source identity.
	boundRuns := map[string]bool{}
	for _, result := range previous.WorkerResults {
		for _, id := range result.CheckRuns {
			boundRuns[id] = true
		}
	}
	for _, acceptance := range previous.WorkerAcceptances {
		for _, id := range acceptance.CheckRuns {
			boundRuns[id] = true
		}
	}
	for id := range boundRuns {
		var before, after *CheckRun
		for i := range previous.Checks {
			if previous.Checks[i].ID == id {
				before = &previous.Checks[i]
			}
		}
		for i := range current.Checks {
			if current.Checks[i].ID == id {
				after = &current.Checks[i]
			}
		}
		if before == nil || after == nil || !sameEncoded(*before, *after) {
			return fmt.Errorf("worker-bound terminal check %s changed", id)
		}
	}
	return nil
}
