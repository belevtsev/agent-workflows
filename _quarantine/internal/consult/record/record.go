// Package record holds the consultation record summary and the adviser
// predicates that the task flow needs without linking the adviser HTTP
// providers. internal/consult validates records into this summary; a task
// rule reads only the summary and these predicates.
package record

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
)

// ProviderOutcome is one participating adviser's outcome in a consultation record.
type ProviderOutcome struct {
	// Status is success, partial, refusal, unavailable or not_configured.
	Status string
	// RequestedModel is the pinned model the adviser was (or would have been) asked.
	RequestedModel string
}

// Record is the validated summary of a version 4 consultation record.
type Record struct {
	// Version is always 4.
	Version int
	// Status is success, partial, unavailable or no_advisers_configured.
	Status string
	// State is request.state exactly as its JSON text appears in the record.
	State json.RawMessage
	// Providers holds one outcome for each participating adviser, keyed by
	// provider ID (jev, subq_snap, openai_decisions). It is empty, never nil,
	// when no adviser participated.
	Providers map[string]ProviderOutcome
	// DecisionStatus is the final_decision status: pending, resolved or unresolved.
	DecisionStatus string
}

// credentialNames maps each known adviser to the environment variable that
// holds its credential.
var credentialNames = map[string]string{
	"jev":              "TYPESAFE_API_KEY",
	"subq_snap":        "SUBQ_API_KEY",
	"openai_decisions": "OPENAI_API_KEY",
}

// AttemptedStatus reports whether status is the outcome of an attempted
// adviser: success, partial, refusal or unavailable. It is false for
// not_configured (never contacted) and for any other value.
func AttemptedStatus(status string) bool {
	switch status {
	case "success", "partial", "refusal", "unavailable":
		return true
	}
	return false
}

// ValidateBinding applies the policy pin rules (internal/modelpolicy) to an
// adviser binding (provider ID -> model): every ID must be a known adviser
// (jev, subq_snap, openai_decisions), jev must be a versioned Jev ID
// (jev-X.Y.Z; aliases such as jev-latest are refused), subq_snap must be
// subq-snap-preview and openai_decisions must be gpt-6-luna. An empty or nil
// binding is valid. Providers are checked in sorted order, so the error is
// deterministic.
func ValidateBinding(advisers map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(advisers)) {
		if !modelpolicy.Known(name) {
			return fmt.Errorf("adviser binding names unknown adviser %q", name)
		}
		if !modelpolicy.ValidModel(name, advisers[name]) {
			return fmt.Errorf("adviser binding pins %s to an unsupported model %q", name, advisers[name])
		}
	}
	return nil
}

// CredentialVariable returns the environment variable that holds a known
// adviser's credential (TYPESAFE_API_KEY, SUBQ_API_KEY or OPENAI_API_KEY);
// ok is false for an unknown provider ID.
func CredentialVariable(provider string) (name string, ok bool) {
	name, ok = credentialNames[provider]
	return name, ok
}
