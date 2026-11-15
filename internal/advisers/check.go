package advisers

import (
	"fmt"
)

// Evaluate checks portable checkpoint evidence without loading configuration
// or running an adviser. An earlier successful attempt may satisfy a checkpoint
// after a later failed retry; every successful answer still needs disposition.
func Evaluate(root, task string, current Content, historicalCode, checkpoint string) (*Status, error) {
	r, err := openRecordFS(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	history, err := r.history(task)
	if err != nil {
		return nil, err
	}
	status := &Status{
		Task: task, Workflow: history.binding.Workflow, Ready: true,
		Requirements: []Requirement{}, Problems: []string{},
	}
	found := checkpoint == ""
	for _, required := range history.binding.Checkpoints {
		if checkpoint != "" && required.ID != checkpoint {
			continue
		}
		found = true
		if status.Workflow == "small" && required.Stage != "review" {
			continue
		}
		for _, adviser := range required.Advisers {
			requirement := history.requirement(required, adviser, current, historicalCode)
			status.Requirements = append(status.Requirements, requirement)
			if requirement.State != "satisfied" {
				status.Ready = false
				status.Problems = append(status.Problems, fmt.Sprintf("checkpoint %s (%s, %s): %s",
					required.ID, required.Role, adviser, requirement.State))
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown adviser checkpoint %q", checkpoint)
	}
	return status, nil
}

func (h *recordHistory) requirement(checkpoint Checkpoint, adviser string, current Content, historicalCode string) Requirement {
	requirement := Requirement{
		Checkpoint: checkpoint.ID, Stage: checkpoint.Stage, Role: checkpoint.Role, Adviser: adviser, State: "missing",
	}
	best := 0
	for _, consultation := range h.consultations {
		matches := consultation.Checkpoint == checkpoint.ID && consultation.Role == checkpoint.Role &&
			consultation.Adviser == adviser && checkpointStage(checkpoint.Stage, consultation.Stage)
		if !matches || !Allowed(h.binding.Access, consultation.Role, adviser) {
			continue
		}
		state, priority := "stale", 1
		identityMatches := consultation.Identity == h.binding.Advisers[adviser]
		if identityMatches && freshContent(consultation.Content, current, historicalCode, checkpoint.Stage) {
			state, priority = consultation.Execution.Outcome, 2
			if consultation.Execution.Outcome == "success" {
				state, priority = "undisposed", 3
				if _, exists := h.dispositions[consultation.ID]; exists {
					state, priority = "satisfied", 4
				}
			}
		}
		if priority >= best {
			requirement.State, requirement.Consultation, best = state, consultation.ID, priority
		}
	}
	return requirement
}

func freshContent(recorded, current Content, historicalCode, stage string) bool {
	switch stage {
	case "spec":
		return recorded.Spec != "" && recorded.Spec == current.Spec
	case "design":
		return recorded.Design != "" && recorded.Design == current.Design
	case "review":
		return recorded.Code != "" &&
			(recorded.Code == current.Code || historicalCode != "" && recorded.Code == historicalCode)
	default:
		return false
	}
}
