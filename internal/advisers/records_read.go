package advisers

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

type recordHistory struct {
	rel           string
	binding       *Binding
	consultations []Consultation
	dispositions  map[string]Disposition
	decisions     []Disposition
}

// ReadConsultations returns all attempts, including interrupted pending ones.
func ReadConsultations(root, task string) ([]Consultation, error) {
	r, err := openRecordFS(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	history, err := r.history(task)
	if err != nil {
		return nil, err
	}
	return history.consultations, nil
}

// ReadConsultationsAt reads a history directory relative to the repository,
// including a full task's archive. The binding supplies the original task name.
func ReadConsultationsAt(root, rel string) ([]Consultation, error) {
	r, err := openRecordFS(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	binding, err := r.binding(rel, "")
	if err != nil {
		return nil, err
	}
	history, err := r.historyAt(rel, binding)
	if err != nil {
		return nil, err
	}
	return history.consultations, nil
}

// ReadDispositions retains the complete decision trail, oldest first.
func ReadDispositions(root, task string) ([]Disposition, error) {
	r, err := openRecordFS(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	history, err := r.history(task)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(history.decisions, func(a, b Disposition) int {
		if compared := a.CreatedAt.Compare(b.CreatedAt); compared != 0 {
			return compared
		}
		return strings.Compare(a.ID, b.ID)
	})
	return history.decisions, nil
}

// ValidateRecords checks structures and references, allowing pending calls and
// checkpoints whose workflow stage has not yet been reached.
func ValidateRecords(root, task string) ([]string, error) {
	r, err := openRecordFS(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	if _, err = r.location(task); errors.Is(err, ErrNoBinding) {
		return []string{}, nil
	}
	_, err = r.history(task)
	if err != nil {
		return []string{err.Error()}, nil
	}
	return []string{}, nil
}

func (r *recordFS) history(task string) (*recordHistory, error) {
	rel, err := r.location(task)
	if err != nil {
		return nil, err
	}
	binding, err := r.binding(rel, task)
	if err != nil {
		return nil, err
	}
	return r.historyAt(rel, binding)
}

func (r *recordFS) historyAt(rel string, binding *Binding) (*recordHistory, error) {
	history := &recordHistory{
		rel: rel, binding: binding, consultations: []Consultation{},
		dispositions: map[string]Disposition{}, decisions: []Disposition{},
	}
	revisions, err := r.revisions(rel, binding.Task)
	if err != nil {
		return nil, err
	}
	entries, err := r.entries(path.Join(rel, "exchanges"))
	if err != nil {
		return nil, err
	}
	byID := map[string]Consultation{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		if !entry.IsDir() || !validRecordID(entry.Name()) {
			return nil, fmt.Errorf("invalid consultation directory %q", entry.Name())
		}
		consultation, err := r.consultation(rel, binding.Task, entry.Name(), revisions)
		if err != nil {
			return nil, err
		}
		history.consultations = append(history.consultations, consultation)
		byID[consultation.ID] = consultation
	}
	if err = r.readDispositions(history, byID); err != nil {
		return nil, err
	}
	return history, nil
}

func (r *recordFS) revisions(rel, task string) (map[string]Binding, error) {
	entries, err := r.entries(path.Join(rel, "bindings"))
	if err != nil {
		return nil, err
	}
	revisions := map[string]Binding{}
	for _, entry := range entries {
		id, err := recordEntryID(entry)
		if err != nil {
			return nil, err
		}
		if id == "" {
			continue
		}
		binding, err := r.bindingRevision(rel, task, id)
		if err != nil {
			return nil, err
		}
		revisions[id] = *binding
	}
	return revisions, nil
}

func (r *recordFS) consultation(rel, task, id string, bindings map[string]Binding) (Consultation, error) {
	var consultation Consultation
	exchange := path.Join(rel, "exchanges", id)
	entries, err := r.entries(exchange)
	if err != nil {
		return consultation, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".tmp-") && entry.Name() != "request.json" && entry.Name() != "result.json" {
			return consultation, fmt.Errorf("unknown consultation file %q", entry.Name())
		}
	}
	if err := r.read(path.Join(exchange, "request.json"), &consultation); err != nil {
		return consultation, err
	}
	if consultation.ID != id || consultation.Task != task || consultation.Execution.Outcome != "pending" {
		return consultation, errors.New("consultation request must match its path and retain its pending execution")
	}
	binding, exists := bindings[consultation.BindingID]
	if !exists {
		return consultation, fmt.Errorf("consultation %s references an unknown binding", id)
	}
	if err := validateConsultation(consultation, binding); err != nil {
		return consultation, fmt.Errorf("consultation %s: %w", id, err)
	}
	var result Execution
	err = r.read(path.Join(exchange, "result.json"), &result)
	if errors.Is(err, fs.ErrNotExist) {
		return consultation, nil
	}
	if err != nil {
		return consultation, err
	}
	if err = validateExecution(result); err != nil {
		return consultation, fmt.Errorf("consultation %s result: %w", id, err)
	}
	if result.Outcome == "pending" || result.StartedAt.Before(consultation.Execution.StartedAt) {
		return consultation, errors.New("consultation result must be terminal and follow its pending request")
	}
	consultation.Execution = result
	if err := validateTypedExecution(consultation); err != nil {
		return consultation, err
	}
	return consultation, nil
}

func (r *recordFS) readDispositions(history *recordHistory, consultations map[string]Consultation) error {
	entries, err := r.entries(path.Join(history.rel, "dispositions"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id, err := recordEntryID(entry)
		if err != nil {
			return err
		}
		if id == "" {
			continue
		}
		var decision Disposition
		if err = r.read(path.Join(history.rel, "dispositions", entry.Name()), &decision); err != nil {
			return err
		}
		consultation, exists := consultations[decision.Consultation]
		if !exists || decision.ID != id {
			return errors.New("disposition has an unknown consultation or its id does not match its record path")
		}
		if err = validateDisposition(decision, consultation); err != nil {
			return fmt.Errorf("disposition %s: %w", id, err)
		}
		history.decisions = append(history.decisions, decision)
		previous, exists := history.dispositions[decision.Consultation]
		newer := decision.CreatedAt.After(previous.CreatedAt)
		tie := decision.CreatedAt.Equal(previous.CreatedAt) && decision.ID > previous.ID
		if !exists || newer || tie {
			history.dispositions[decision.Consultation] = decision
		}
	}
	return nil
}

func recordEntryID(entry fs.DirEntry) (string, error) {
	if strings.HasPrefix(entry.Name(), ".tmp-") {
		return "", nil
	}
	id, suffix := strings.CutSuffix(entry.Name(), ".json")
	if !suffix || !validRecordID(id) || !entry.Type().IsRegular() {
		return "", fmt.Errorf("invalid adviser record entry %q", entry.Name())
	}
	return id, nil
}

func validateBinding(binding Binding) error {
	if (binding.Version != 1 && binding.Version != 2) || !validRecordID(binding.ID) || !validTask(binding.Task) || binding.CreatedAt.IsZero() {
		return errors.New("binding requires version 1 or 2 and valid id, task and creation time")
	}
	if binding.Workflow != "small" && binding.Workflow != "full" {
		return errors.New("binding workflow must be small or full")
	}
	if err := validatePolicy(&Config{Access: binding.Access, Checkpoints: binding.Checkpoints}); err != nil {
		return err
	}
	if err := validateIdentities(binding.Advisers); err != nil {
		return err
	}
	for _, identity := range binding.Advisers {
		if binding.Version == 1 && identity.Provider != "" {
			return errors.New("native identities require binding version 2")
		}
	}
	for role, names := range binding.Access {
		for _, name := range names {
			if _, exists := binding.Advisers[name]; !exists {
				return fmt.Errorf("binding access %q names undefined adviser %q", role, name)
			}
		}
	}
	for _, checkpoint := range binding.Checkpoints {
		for _, adviser := range checkpoint.Advisers {
			if _, exists := binding.Advisers[adviser]; !exists || !Allowed(binding.Access, checkpoint.Role, adviser) {
				return fmt.Errorf("binding checkpoint %q has an undefined or disallowed adviser %q", checkpoint.ID, adviser)
			}
		}
	}
	return nil
}

func validateIdentities(advisers map[string]Identity) error {
	for name, identity := range advisers {
		digest, err := hex.DecodeString(identity.Fingerprint)
		validFingerprint := err == nil && len(digest) == 32 && hex.EncodeToString(digest) == identity.Fingerprint
		validModel := recordText(identity.Model, MaxInput, true) && !strings.ContainsRune(identity.Model, 0)
		if !configName.MatchString(name) || !validModel || !validFingerprint || Kinds(identity.Provider) == nil {
			return fmt.Errorf("binding adviser %q needs a valid name, model and definition fingerprint", name)
		}
	}
	return nil
}

func validateConsultation(consultation Consultation, binding Binding) error {
	validIDs := (consultation.Version == 1 || consultation.Version == 2) && validRecordID(consultation.ID) && consultation.BindingID == binding.ID
	if !validIDs || consultation.Task != binding.Task || !ValidRole(consultation.Role) {
		return errors.New("consultation requires version 1 or 2, matching task and binding, and valid id and requester role")
	}
	if !slices.Contains([]string{"lookup", "spec", "spec-review", "design", "design-review", "tests", "code", "review", "commit"}, consultation.Stage) {
		return errors.New("consultation stage is unknown")
	}
	identity, exists := binding.Advisers[consultation.Adviser]
	if !exists || identity != consultation.Identity || !Allowed(binding.Access, consultation.Role, consultation.Adviser) {
		return errors.New("consultation adviser identity or requester access differs from its binding")
	}
	if consultation.Checkpoint != "" && !matchesCheckpoint(consultation, binding.Checkpoints) {
		return errors.New("consultation does not match its checkpoint's stage, requester role and adviser")
	}
	if err := ValidateCapability(consultation.Identity.Provider, consultation.Request); err != nil {
		return err
	}
	if !recordText(consultation.Prompt, MaxInput, true) {
		return errors.New("consultation prompt must be nonempty UTF-8 text of at most 1 MiB")
	}
	for _, id := range []string{consultation.Content.Spec, consultation.Content.Design, consultation.Content.Code} {
		if !recordText(id, 256, false) {
			return errors.New("consultation content ids must be UTF-8 text of at most 256 bytes")
		}
	}
	if err := validateExecution(consultation.Execution); err != nil {
		return err
	}
	return validateTypedExecution(consultation)
}

func matchesCheckpoint(consultation Consultation, checkpoints []Checkpoint) bool {
	for _, checkpoint := range checkpoints {
		if checkpoint.ID == consultation.Checkpoint && checkpoint.Role == consultation.Role &&
			slices.Contains(checkpoint.Advisers, consultation.Adviser) && checkpointStage(checkpoint.Stage, consultation.Stage) {
			return true
		}
	}
	return false
}

func checkpointStage(checkpoint, stage string) bool {
	return checkpoint == stage || (checkpoint == "spec" && stage == "spec-review") ||
		(checkpoint == "design" && stage == "design-review")
}

func validateExecution(execution Execution) error {
	if execution.Version != 0 && execution.Version != 2 {
		return errors.New("execution version must be omitted or 2")
	}
	if !slices.Contains([]string{"pending", "success", "failed", "timed_out", "cancelled", "overflow"}, execution.Outcome) {
		return errors.New("execution outcome is unknown")
	}
	if !recordText(execution.Answer, MaxAnswer, false) || !recordText(execution.Detail, MaxDiagnostic, false) ||
		!recordText(execution.ActualModel, MaxInput, true) || execution.StartedAt.IsZero() {
		return errors.New("execution needs bounded UTF-8 answer, detail and actual model, and a start time")
	}
	if execution.Outcome == "pending" {
		return validatePendingExecution(execution)
	}
	if execution.EndedAt.IsZero() || execution.EndedAt.Before(execution.StartedAt) {
		return errors.New("terminal execution requires an end time at or after its start")
	}
	if execution.Outcome == "success" && strings.TrimSpace(execution.Answer) == "" {
		return errors.New("successful execution must retain its nonempty answer")
	}
	if execution.Outcome == "success" && execution.ExitStatus != nil && *execution.ExitStatus != 0 {
		return errors.New("successful execution cannot have a failing exit status")
	}
	return nil
}

func validatePendingExecution(e Execution) error {
	if e.Answer != "" || e.Detail != "" || !e.EndedAt.IsZero() || e.ExitStatus != nil || e.Result != nil || e.Usage != nil || e.UsageKnown {
		return errors.New("pending execution must not have an answer, terminal details or end time")
	}
	return nil
}

func validateDisposition(decision Disposition, consultation Consultation) error {
	validIDs := decision.Version == 1 && validRecordID(decision.ID) && decision.Consultation == consultation.ID
	if !validIDs || decision.CreatedAt.IsZero() || decision.CreatedAt.Before(consultation.Execution.StartedAt) {
		return errors.New("disposition needs version 1, valid ids and a creation time after its consultation")
	}
	if !slices.Contains([]string{"accept", "reject", "defer"}, decision.Disposition) || !recordText(decision.Reason, MaxInput, true) {
		return errors.New("disposition must be accept, reject or defer and include a nonempty rationale")
	}
	if decision.Choice != "" && !slices.ContainsFunc(consultation.Request.Choices, func(choice Choice) bool {
		return choice.ID == decision.Choice
	}) {
		return errors.New("selected choice must reference a choice in the original request")
	}
	return nil
}
