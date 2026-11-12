package taskflow

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
)

// taskIDPattern is the form of the IDs cw task start gives a task (randomID),
// which also name the task's directory in its store.
var taskIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// CheckRequestContexts reports whether task records in store back the task
// contexts of a consultation request, each the JSON object that cw task
// context printed. A context's task_id must name a task directory in store
// whose record holds that context for its phase, and the contexts together
// must pass the check that cw task consult applies when it attaches the
// consultation (requestContext). An abandoned task backs no context: cw task
// consult can never attach its consultation. cw consult runs it before it
// asks a task's advisers, so it asks only the advisers that a live task
// record binds.
func CheckRequestContexts(store string, contexts [][]byte) error {
	values := make([]jsontext.Value, len(contexts))
	for i, context := range contexts {
		values[i] = jsontext.Value(context)
	}
	state, err := json.Marshal(map[string][]jsontext.Value{"task_contexts": values})
	if err != nil {
		return fmt.Errorf("a task context is not the JSON that 'cw task context' prints: %w", err)
	}
	expected := make([]Context, len(contexts))
	for i, context := range contexts {
		var named struct {
			TaskID string `json:"task_id"`
			Phase  string `json:"phase"`
		}
		if err := json.Unmarshal(context, &named); err != nil || !taskIDPattern.MatchString(named.TaskID) {
			return errors.New("a task context names no task that cw task start created; put the context that 'cw task context' printed in the request")
		}
		record, err := Load(filepath.Join(store, named.TaskID))
		if err != nil {
			return fmt.Errorf("no task record in the task store %s backs task context %s: %w; a task started with --store DIR needs CW_TASK_STORE=DIR", store, named.TaskID, err)
		}
		if record.Abandonment != nil {
			return fmt.Errorf("task %s was abandoned; start a new task with 'cw task start'", named.TaskID)
		}
		prepared, ok := record.Contexts[named.Phase]
		if !ok || prepared.TaskID != record.ID || prepared.Phase != named.Phase {
			return fmt.Errorf("task %s has no prepared %q context; run 'cw task context' for it", named.TaskID, named.Phase)
		}
		expected[i] = prepared
	}
	for _, prepared := range expected {
		if err := requestContext(jsontext.Value(state), prepared); err != nil {
			return fmt.Errorf("task context %s (%s) differs from its task record: %w; put the context that 'cw task context' prints now in the request", prepared.TaskID, prepared.Phase, err)
		}
	}
	return nil
}
