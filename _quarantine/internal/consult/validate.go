package consult

import (
	"encoding/json"
	"errors"

	"github.com/belevtsev/agent-workflows/v3/internal/consult/record"
)

// ProviderOutcome is one participating adviser's outcome in a consultation
// record; the type lives in internal/consult/record.
type ProviderOutcome = record.ProviderOutcome

// Record is the validated summary of a version 4 consultation record; the
// type lives in internal/consult/record, so the task flow can read it without
// linking the adviser providers.
type Record = record.Record

// ValidateRecord fully revalidates a consultation record written by cw consult
// (and, when decided, by cw consult --adjudicate), using no network, policy or
// credentials:
//
//   - the version is 4; any other is rejected with "consultation record
//     version N is not supported; this cw reads version 4";
//   - the request passes the authoring checks and its request_sha256 matches;
//   - each provider entry is a known adviser in fixed order, with a valid
//     pinned model, the shared fingerprint and normalized evidence, and a
//     task-bound request (state.task_context or state.task_contexts) recorded
//     exactly its bound advisers and models;
//   - the comparison and record status match the provider evidence;
//   - a pending decision holds no questions, and a resolved or unresolved
//     decision covers every question and satisfies every adjudication rule,
//     with resolved exactly when every question is resolved.
//
// A dry-run preview is not a consultation record and is rejected. Callers that
// need more than this summary must not reinterpret the raw record themselves.
func ValidateRecord(data []byte) (Record, error) {
	value, err := parseJSON(data)
	if err != nil {
		return Record{}, errors.New("consultation record is not valid JSON")
	}
	checked, err := checkRecord(value)
	if err != nil {
		return Record{}, err
	}
	status, err := checked.checkDecision(checked.record.get("final_decision"))
	if err != nil {
		return Record{}, err
	}
	var raw struct {
		Request struct {
			State json.RawMessage `json:"state"`
		} `json:"request"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Record{}, errors.New("consultation record is not valid JSON")
	}
	outcomes := make(map[string]ProviderOutcome, checked.providers.size())
	for _, name := range checked.providers.names {
		item := checked.providers.get(name).(*object)
		outcomes[name] = ProviderOutcome{Status: item.get("status").(string), RequestedModel: item.get("requested_model").(string)}
	}
	return Record{
		Version:        recordVersion,
		Status:         checked.record.get("status").(string),
		State:          raw.Request.State,
		Providers:      outcomes,
		DecisionStatus: status,
	}, nil
}
