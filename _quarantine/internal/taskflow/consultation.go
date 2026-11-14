package taskflow

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	consultrecord "github.com/belevtsev/agent-workflows/v3/internal/consult/record"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

func sameKeys[V any](values map[string]V, keys []string) bool {
	if len(values) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

func fieldKeys(data []byte, keys ...string) error {
	var fields map[string]jsontext.Value
	if err := decodeBytes(data, &fields); err != nil {
		return err
	}
	if !sameKeys(fields, keys) {
		return errors.New("consultation fields differ from the helper contract")
	}
	return nil
}

func requestContext(state jsontext.Value, expected Context) error {
	var fields map[string]jsontext.Value
	if err := decodeBytes(state, &fields); err != nil {
		return errors.New("shared state must include a structured task_context or task_contexts")
	}
	single, one := fields["task_context"]
	multiple, many := fields["task_contexts"]
	if one == many {
		return errors.New("shared state needs exactly one task_context or task_contexts field")
	}
	var contexts []Context
	var rawContexts []jsontext.Value
	if one {
		var item Context
		if err := decodeBytes(single, &item); err != nil {
			return err
		}
		contexts = []Context{item}
		rawContexts = []jsontext.Value{single}
	} else if err := decodeBytes(multiple, &contexts); err != nil {
		return err
	} else if err := decodeBytes(multiple, &rawContexts); err != nil {
		return err
	}
	if len(contexts) == 0 || len(contexts) > 256 {
		return errors.New("task contexts must be a bounded nonempty array")
	}
	seen := map[string]bool{}
	matches := 0
	for i, item := range contexts {
		if err := fieldKeys(rawContexts[i], "task_id", "phase", "snapshot", "evidence_sha256", "advisers"); err != nil {
			return errors.New("shared task context must contain every exact emitted field")
		}
		var rawContext map[string]jsontext.Value
		if err := decodeBytes(rawContexts[i], &rawContext); err != nil {
			return err
		}
		if err := fieldKeys(rawContext["snapshot"], "root", "selected_path", "head", "branch", "fingerprint", "dirty", "file_count"); err != nil {
			return errors.New("shared task context snapshot must contain every exact emitted field")
		}
		key := item.TaskID + ":" + item.Phase
		if !nonblank(item.TaskID) || !phaseValid(item.Phase) || item.Snapshot.Fingerprint == "" || seen[key] || item.Phase == "before" && item.EvidenceSHA256 != "" || item.Phase == "final" && !validSHA256(item.EvidenceSHA256) {
			return errors.New("invalid or duplicate task context")
		}
		seen[key] = true
		if sameContext(item, expected) {
			matches++
		}
	}
	if matches != 1 {
		return errors.New("consultation task, phase, complete source snapshot or adviser binding differs")
	}
	return nil
}

// validateCheckpoint revalidates a consultation record with the Env's
// validator (cw consult's own in production), then binds it to this task: the
// request must carry the expected context, the record must cover exactly the
// bound advisers at their bound models, and every bound adviser must have been
// attempted.
func (env Env) validateCheckpoint(data []byte, expected Context) (consultrecord.Record, error) {
	checkpoint, err := env.validateConsultation(data)
	if err != nil {
		return checkpoint, err
	}
	if err := requestContext(jsontext.Value(checkpoint.State), expected); err != nil {
		return checkpoint, err
	}
	binding := expected.Advisers
	for _, name := range slices.Sorted(maps.Keys(checkpoint.Providers)) {
		if _, bound := binding[name]; !bound {
			return checkpoint, fmt.Errorf("consultation asked adviser %s, which is not bound to this task", name)
		}
	}
	var notConfigured []string
	for _, name := range slices.Sorted(maps.Keys(binding)) {
		outcome, ok := checkpoint.Providers[name]
		switch {
		case !ok:
			return checkpoint, fmt.Errorf("consultation omits bound adviser %s", name)
		case outcome.RequestedModel != binding[name]:
			return checkpoint, fmt.Errorf("consultation asked %s for model %s, but the task binds %s", name, outcome.RequestedModel, binding[name])
		case outcome.Status == "not_configured":
			variable, _ := consultrecord.CredentialVariable(name)
			notConfigured = append(notConfigured, fmt.Sprintf("%s (set %s)", name, variable))
		case !consultrecord.AttemptedStatus(outcome.Status):
			return checkpoint, fmt.Errorf("consultation records bound adviser %s as %s", name, outcome.Status)
		}
	}
	if len(notConfigured) > 0 {
		return checkpoint, fmt.Errorf("advisers bound to this task were not configured for the consultation: %s; consult again", strings.Join(notConfigured, ", "))
	}
	if len(binding) == 0 && checkpoint.Status != "no_advisers_configured" {
		return checkpoint, errors.New("a task bound to no advisers needs a no_advisers_configured consultation record")
	}
	return checkpoint, nil
}

func (env Env) AttachConsultation(ctx context.Context, path, phase, inputPath string) (Record, error) {
	if !phaseValid(phase) {
		return Record{}, invalid("phase must be before or final")
	}
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return Record{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Record{}, err
	}
	if !info.Mode().IsRegular() {
		return Record{}, errors.New("consultation input must be a regular file, not a symlink")
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Record{}, err
	}
	file, err := os.Open(abs)
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Record{}, errors.New("consultation input changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxInputBytes+1))
	if err != nil {
		return Record{}, err
	}
	if len(raw) > MaxInputBytes {
		return Record{}, errors.New("consultation exceeds the 8 MiB input limit")
	}
	var probes map[string]probedIdentity
	if phase == "final" {
		probes = env.probeReuse(ctx, path)
	}
	return update(ctx, env, path, func(root *os.Root, record *Record) error {
		for other, attachment := range record.Consultations {
			if other != phase {
				otherInfo, statErr := os.Stat(attachment.SourcePath)
				if attachment.SourcePath == abs || statErr == nil && os.SameFile(info, otherInfo) {
					return errors.New("before and final consultation must use distinct original artifact files")
				}
			}
		}
		current, err := env.source().Snapshot(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		expected, ok := record.Contexts[phase]
		if !ok || expected.TaskID != record.ID || expected.Phase != phase {
			return errors.New("prepare the matching task phase context first")
		}
		if !equalSnapshot(expected.Snapshot, current) {
			return errors.New("consultation context is stale against current source")
		}
		if phase == "before" && !equalSnapshot(expected.Snapshot, record.Baseline) {
			return errors.New("before consultation differs from task baseline")
		}
		if phase == "final" {
			if problems := localEvidenceProblems(taskDirectory{root}, *record, env.credit(ctx, taskDirectory{root}, current, nil, probes)); len(problems) > 0 {
				return fmt.Errorf("final consultation requires completed evidence: %v", problems)
			}
			binding, err := evidenceBinding(*record)
			if err != nil {
				return err
			}
			if expected.EvidenceSHA256 != binding {
				return errors.New("final consultation context is stale against completed task evidence")
			}
		}
		checkpoint, err := env.validateCheckpoint(raw, expected)
		if err != nil {
			return err
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		name := "consultation-" + phase + "-" + id + ".json"
		if err := writeNew(root, name, raw); err != nil {
			return err
		}
		statuses := map[string]string{}
		for name, value := range checkpoint.Providers {
			statuses[name] = value.Status
		}
		record.Consultations[phase] = Attachment{Phase: phase, SourcePath: abs, File: name, SHA256: digest(raw), Context: expected, DecisionStatus: checkpoint.DecisionStatus, ProviderStatuses: statuses}
		record.Verification = nil
		return nil
	})
}

// readConsultation revalidates an attached checkpoint against the current
// expected context (including the adviser binding) and requires a resolved
// decision. ProviderStatuses holds exactly the bound advisers' outcomes.
func (env Env) readConsultation(artifacts Artifacts, record Record, phase string, snapshot projectctx.Snapshot) (consultrecord.Record, error) {
	attachment, ok := record.Consultations[phase]
	if !ok {
		return consultrecord.Record{}, errors.New("required phase artifact is missing")
	}
	expected := Context{TaskID: record.ID, Phase: phase, Snapshot: snapshot, Advisers: record.Advisers}
	if phase == "final" {
		var err error
		expected.EvidenceSHA256, err = evidenceBinding(record)
		if err != nil {
			return consultrecord.Record{}, err
		}
	}
	if attachment.Phase != phase || !sameContext(attachment.Context, expected) {
		return consultrecord.Record{}, errors.New("phase artifact is bound to stale or mismatched source or completed task evidence")
	}
	data, err := artifacts.Read(attachment.File, MaxInputBytes)
	if err != nil {
		return consultrecord.Record{}, err
	}
	if digest(data) != attachment.SHA256 {
		return consultrecord.Record{}, errors.New("consultation artifact hash differs")
	}
	checkpoint, err := env.validateCheckpoint(data, expected)
	if err != nil {
		return checkpoint, err
	}
	if checkpoint.DecisionStatus != "resolved" || attachment.DecisionStatus != "resolved" {
		return checkpoint, errors.New("consultation decision remains pending or unresolved")
	}
	if !sameKeys(attachment.ProviderStatuses, slices.Collect(maps.Keys(record.Advisers))) {
		return checkpoint, errors.New("consultation availability metadata differs from the adviser binding")
	}
	for name, provider := range checkpoint.Providers {
		if attachment.ProviderStatuses[name] != provider.Status {
			return checkpoint, errors.New("consultation availability metadata differs")
		}
	}
	return checkpoint, nil
}
