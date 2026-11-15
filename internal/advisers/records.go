package advisers

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type bindingPointer struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
}

func recordLocations(task string) (full, small string) {
	return "openspec/changes/" + task + "/advisers", "openspec/changes/.advice/" + task
}

// Location finds an existing history without creating files or directories.
func Location(root, task string) (string, error) {
	r, err := openRecordFS(root)
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }()
	return r.location(task)
}

func (r *recordFS) location(task string) (string, error) {
	if !validTask(task) {
		return "", fmt.Errorf("invalid task name %q", task)
	}
	full, small := recordLocations(task)
	fullExists, err := r.existsDir(full)
	if err != nil {
		return "", err
	}
	smallExists, err := r.existsDir(small)
	if err != nil {
		return "", err
	}
	switch {
	case fullExists && smallExists:
		return "", fmt.Errorf("task %q has adviser history in both %s and %s", task, full, small)
	case fullExists:
		return full, nil
	case smallExists:
		return small, nil
	default:
		return "", ErrNoBinding
	}
}

// LoadBinding reads the active immutable snapshot. It needs no local config.
func LoadBinding(root, task string) (*Binding, error) {
	r, err := openRecordFS(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	rel, err := r.location(task)
	if err != nil {
		return nil, err
	}
	return r.binding(rel, task)
}

func (r *recordFS) binding(rel, task string) (*Binding, error) {
	var pointer bindingPointer
	if err := r.read(path.Join(rel, "binding.json"), &pointer); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s/binding.json", ErrNoBinding, rel)
		}
		return nil, fmt.Errorf("read adviser binding: %w", err)
	}
	if pointer.Version != 1 || !validRecordID(pointer.ID) {
		return nil, errors.New("adviser binding pointer has an invalid version or id")
	}
	binding, err := r.bindingRevision(rel, task, pointer.ID)
	if err != nil {
		return nil, err
	}
	full, _ := recordLocations(task)
	if task != "" && (rel == full) != (binding.Workflow == "full") {
		return nil, errors.New("adviser binding workflow does not match its history location")
	}
	return binding, nil
}

func (r *recordFS) bindingRevision(rel, task, id string) (*Binding, error) {
	var binding Binding
	if err := r.read(path.Join(rel, "bindings", id+".json"), &binding); err != nil {
		return nil, err
	}
	if binding.ID != id || task != "" && binding.Task != task {
		return nil, errors.New("binding id or task does not match its record path")
	}
	if err := validateBinding(binding); err != nil {
		return nil, fmt.Errorf("binding %s: %w", id, err)
	}
	return &binding, nil
}

// Bind snapshots policy once. Only an explicit refresh replaces that snapshot.
// Promoting a small task moves its complete history before publishing a new
// full-workflow revision; an existing full destination is never merged.
func Bind(root, task, workflow string, cfg *Config, refresh bool) (*Binding, error) {
	if workflow != "small" && workflow != "full" {
		return nil, errors.New("adviser workflow must be small or full")
	}
	r, closeRecords, err := openWritableRecords(root, task)
	if err != nil {
		return nil, err
	}
	defer closeRecords()
	rel, previous, err := r.bindingForWrite(task, workflow, refresh)
	if err != nil {
		return nil, err
	}
	if previous != nil && !refresh {
		return previous, nil
	}
	if err = ValidateConfig(cfg); err != nil {
		return nil, err
	}
	destination, err := r.bindingDestination(task, workflow, rel, previous)
	if err != nil {
		return nil, err
	}
	binding := snapshotBinding(task, workflow, cfg)
	err = r.publish(path.Join(destination, "bindings", binding.ID+".json"), binding, false)
	if err == nil {
		err = r.publish(path.Join(destination, "binding.json"), bindingPointer{Version: 1, ID: binding.ID}, refresh)
	}
	if err != nil {
		if destination != rel && previous != nil {
			err = errors.Join(err, r.Rename(destination, rel))
		}
		return nil, err
	}
	return &binding, nil
}

func (r *recordFS) bindingForWrite(task, workflow string, refresh bool) (string, *Binding, error) {
	rel, err := r.location(task)
	if errors.Is(err, ErrNoBinding) {
		full, small := recordLocations(task)
		if workflow == "full" {
			return full, nil, nil
		}
		return small, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	previous, err := r.binding(rel, "")
	if err != nil {
		return "", nil, err
	}
	full, _ := recordLocations(task)
	locationMatches := (rel == full) == (previous.Workflow == "full")
	recoverPromotion := rel == full && previous.Workflow == "small" && refresh && workflow == "full"
	if previous.Task != task || !locationMatches && !recoverPromotion {
		return "", nil, errors.New("binding does not match its task or location; an interrupted small promotion needs bind --workflow full --refresh")
	}
	if previous.Workflow != workflow && (!refresh || workflow != "full") {
		return "", nil, errors.New("only bind --workflow full --refresh can promote a small adviser history")
	}
	return rel, previous, nil
}

func (r *recordFS) bindingDestination(task, workflow, rel string, previous *Binding) (string, error) {
	if previous == nil || previous.Workflow == workflow {
		return rel, nil
	}
	full, _ := recordLocations(task)
	if rel == full {
		return rel, nil // Explicit refresh completes an interrupted promotion.
	}
	if err := r.directory(path.Dir(full), true); err != nil {
		return "", err
	}
	if _, err := r.Lstat(full); !errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("full adviser history destination already exists or cannot be inspected")
	}
	return full, r.Rename(rel, full)
}

func snapshotBinding(task, workflow string, cfg *Config) Binding {
	binding := Binding{
		Version: 1, ID: newRecordID(), Task: task, Workflow: workflow, CreatedAt: time.Now().UTC(),
		Advisers: map[string]Identity{}, Access: map[string][]string{}, Checkpoints: []Checkpoint{},
	}
	for name, definition := range cfg.Advisers {
		binding.Advisers[name] = Identity{Model: definition.Model, Fingerprint: Fingerprint(definition)}
	}
	for role, names := range cfg.Access {
		binding.Access[role] = slices.Clone(names)
	}
	for _, checkpoint := range cfg.Checkpoints {
		checkpoint.Advisers = slices.Clone(checkpoint.Advisers)
		binding.Checkpoints = append(binding.Checkpoints, checkpoint)
	}
	return binding
}

// Begin publishes the complete pending request and prompt before execution.
func Begin(root string, consultation Consultation) (*Consultation, error) {
	r, closeRecords, err := openWritableRecords(root, consultation.Task)
	if err != nil {
		return nil, err
	}
	defer closeRecords()
	rel, err := r.location(consultation.Task)
	if err != nil {
		return nil, err
	}
	binding, err := r.binding(rel, consultation.Task)
	if err != nil {
		return nil, err
	}
	if consultation.ID == "" {
		consultation.ID = newRecordID()
	}
	if consultation.BindingID == "" {
		consultation.BindingID = binding.ID
	}
	if consultation.Identity == (Identity{}) {
		consultation.Identity = binding.Advisers[consultation.Adviser]
	}
	consultation.Version = 1
	consultation.Execution = Execution{Outcome: "pending", StartedAt: time.Now().UTC(), ActualModel: "unknown"}
	if consultation.BindingID != binding.ID {
		return nil, errors.New("consultation must use the current adviser binding")
	}
	if err = validateConsultation(consultation, *binding); err != nil {
		return nil, err
	}
	destination := path.Join(rel, "exchanges", consultation.ID)
	if _, err = r.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("consultation %q already exists or cannot be inspected", consultation.ID)
	}
	temporary := path.Join(rel, "exchanges", ".tmp-"+newRecordID())
	defer func() { _ = r.RemoveAll(temporary) }()
	if err = r.publish(path.Join(temporary, "request.json"), consultation, false); err != nil {
		return nil, err
	}
	if err = r.Rename(temporary, destination); err != nil {
		return nil, err
	}
	return &consultation, nil
}

// Finish appends a single terminal result without changing the saved request.
func Finish(root string, consultation Consultation) error {
	r, closeRecords, err := openWritableRecords(root, consultation.Task)
	if err != nil {
		return err
	}
	defer closeRecords()
	rel, err := r.location(consultation.Task)
	if err != nil {
		return err
	}
	if !validRecordID(consultation.ID) {
		return errors.New("invalid consultation id")
	}
	var pending Consultation
	exchange := path.Join(rel, "exchanges", consultation.ID)
	if err = r.read(path.Join(exchange, "request.json"), &pending); err != nil {
		return err
	}
	if !sameConsultation(pending, consultation) {
		return errors.New("a result cannot change its original consultation request")
	}
	if consultation.Execution.Outcome == "pending" {
		return errors.New("a consultation result must have a terminal outcome")
	}
	if err = validateExecution(consultation.Execution); err != nil {
		return err
	}
	if consultation.Execution.StartedAt.Before(pending.Execution.StartedAt) {
		return errors.New("consultation execution cannot start before its pending request")
	}
	return r.publish(path.Join(exchange, "result.json"), consultation.Execution, false)
}

func sameConsultation(a, b Consultation) bool {
	return a.Version == b.Version && a.ID == b.ID && a.Task == b.Task && a.BindingID == b.BindingID &&
		a.Role == b.Role && a.Stage == b.Stage && a.Adviser == b.Adviser && a.Identity == b.Identity &&
		a.Checkpoint == b.Checkpoint && a.Content == b.Content && a.Prompt == b.Prompt &&
		a.Request.Version == b.Request.Version && a.Request.Kind == b.Request.Kind &&
		a.Request.Question == b.Request.Question && slices.Equal(a.Request.Choices, b.Request.Choices) &&
		slices.Equal(a.Request.Context, b.Request.Context)
}

// Resolve appends a coordinator decision. Later decisions retain and supersede
// earlier ones; neither the adviser answer nor any attempt is rewritten.
func Resolve(root, task, id, disposition, reason, choice string) (*Disposition, error) {
	if !validRecordID(id) {
		return nil, errors.New("invalid consultation id")
	}
	r, closeRecords, err := openWritableRecords(root, task)
	if err != nil {
		return nil, err
	}
	defer closeRecords()
	history, err := r.history(task)
	if err != nil {
		return nil, err
	}
	var consultation *Consultation
	for i := range history.consultations {
		if history.consultations[i].ID == id {
			consultation = &history.consultations[i]
			break
		}
	}
	if consultation == nil {
		return nil, fmt.Errorf("unknown consultation %q", id)
	}
	decision := Disposition{
		Version: 1, ID: newRecordID(), Consultation: id, Disposition: disposition,
		Reason: reason, Choice: choice, CreatedAt: time.Now().UTC(),
	}
	if latest, exists := history.dispositions[id]; exists && !decision.CreatedAt.After(latest.CreatedAt) {
		decision.CreatedAt = latest.CreatedAt.Add(time.Nanosecond)
	}
	if err = validateDisposition(decision, *consultation); err != nil {
		return nil, err
	}
	if err = r.publish(path.Join(history.rel, "dispositions", decision.ID+".json"), decision, false); err != nil {
		return nil, err
	}
	return &decision, nil
}

func recordText(text string, limit int, nonempty bool) bool {
	return len(text) <= limit && utf8.ValidString(text) &&
		(!nonempty || strings.TrimSpace(text) != "")
}
