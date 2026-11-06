package consult

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/belevtsev/agent-workflows/internal/workflow"
)

// validModel applies the policy pin rules of internal/workflow: a versioned
// Jev ID, the one Snap preview, and the one documented OpenAI Decisions model.
func validModel(provider string, model any) bool {
	name, ok := model.(string)
	return ok && workflow.ValidConsultationModel(provider, name)
}

// knownProvider reports whether name is a known adviser ID under the policy
// pin rules.
func knownProvider(name string) bool { return slices.Contains(providerOrder, name) }

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

// ValidateBinding applies the policy pin rules (internal/workflow) to an
// adviser binding (provider ID -> model): every ID must be a known adviser
// (jev, subq_snap, openai_decisions), jev must be a versioned Jev ID
// (jev-X.Y.Z; aliases such as jev-latest are refused), subq_snap must be
// subq-snap-preview and openai_decisions must be gpt-6-luna. An empty or nil
// binding is valid. Providers are checked in sorted order, so the error is
// deterministic.
func ValidateBinding(advisers map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(advisers)) {
		if !knownProvider(name) {
			return fmt.Errorf("adviser binding names unknown adviser %q", name)
		}
		if !validModel(name, advisers[name]) {
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

// policy is the validated adviser portion of a model policy: its enabled
// providers (provider -> pinned model, possibly empty) and the batch deadline.
type policy struct {
	deadline  time.Duration
	providers map[string]string
}

func loadPolicy(path string) (policy, error) {
	value, err := workflow.ValidateModelPolicy(path)
	if err != nil {
		return policy{}, err
	}
	consultation, ok := value["consultation"].(workflow.Object)
	if !ok {
		return policy{}, errors.New("policy does not configure adviser consultation")
	}
	var seconds int
	switch item := consultation["deadline_seconds"].(type) {
	case int:
		seconds = item
	case int64:
		seconds = int(item)
	}
	if seconds <= 0 {
		return policy{}, errors.New("policy consultation deadline is invalid")
	}
	providers := map[string]string{}
	if configured := consultation["providers"]; configured != nil {
		listed, ok := configured.(workflow.Object)
		if !ok {
			return policy{}, errors.New("policy consultation providers must be a mapping")
		}
		for name, model := range listed {
			if !knownProvider(name) {
				return policy{}, fmt.Errorf("policy names unknown adviser %q", name)
			}
			if !validModel(name, model) {
				return policy{}, fmt.Errorf("policy adviser %s has an invalid pinned model", name)
			}
			providers[name] = model.(string)
		}
	}
	return policy{deadline: time.Duration(seconds) * time.Second, providers: providers}, nil
}

// ConfiguredAdvisers returns the advisers that a consultation run under the
// model policy at policyPath would attempt: each provider the policy enables
// whose credential variable (TYPESAFE_API_KEY, SUBQ_API_KEY or OPENAI_API_KEY)
// is non-empty under getenv. The result maps provider ID to its pinned model,
// is never nil, and is empty when nothing is configured. Only the credential
// variables of enabled providers are read, and no credential value is
// returned. A nil getenv reads no credentials. The error is the policy
// validation failure, if any.
func ConfiguredAdvisers(policyPath string, getenv func(string) string) (map[string]string, error) {
	selected, err := loadPolicy(policyPath)
	if err != nil {
		return nil, err
	}
	configured := map[string]string{}
	for _, name := range providerOrder {
		model, enabled := selected.providers[name]
		if enabled && credential(getenv, name) != "" {
			configured[name] = model
		}
	}
	return configured, nil
}

func credential(getenv func(string) string, provider string) string {
	if getenv == nil {
		return ""
	}
	return getenv(credentialNames[provider])
}

// adviserBinding validates one task context's advisers mapping.
func adviserBinding(context any) (map[string]string, error) {
	item, ok := mapping(context)
	if !ok {
		return nil, errors.New("task context must be an object")
	}
	if !item.has("advisers") {
		return nil, errors.New("task context has no adviser binding; regenerate it with this cw's 'cw task context'")
	}
	advisers, ok := mapping(item.get("advisers"))
	if !ok {
		return nil, errors.New("task context advisers must map provider IDs to models")
	}
	binding := map[string]string{}
	for _, name := range advisers.names {
		if !knownProvider(name) {
			return nil, fmt.Errorf("task context binds unknown adviser %q", name)
		}
		if !validModel(name, advisers.get(name)) {
			return nil, fmt.Errorf("task context binds adviser %s to an invalid pinned model", name)
		}
		binding[name] = advisers.get(name).(string)
	}
	return binding, nil
}

func sameBinding(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, model := range a {
		if other, ok := b[name]; !ok || other != model {
			return false
		}
	}
	return true
}

// taskBinding returns the adviser binding a task-bound request carries in
// state.task_context or in every entry of state.task_contexts. bound is false
// when the state names no task context.
func taskBinding(state any) (binding map[string]string, bound bool, err error) {
	fields, ok := mapping(state)
	if !ok {
		return nil, false, nil
	}
	single, many := fields.has("task_context"), fields.has("task_contexts")
	switch {
	case single && many:
		return nil, false, errors.New("state must hold either task_context or task_contexts, not both")
	case single:
		binding, err := adviserBinding(fields.get("task_context"))
		return binding, true, err
	case many:
		contexts, ok := fields.get("task_contexts").([]any)
		if !ok || len(contexts) == 0 {
			return nil, false, errors.New("task_contexts must be a nonempty array of task contexts")
		}
		var common map[string]string
		for index, context := range contexts {
			binding, err := adviserBinding(context)
			if err != nil {
				return nil, false, err
			}
			if index > 0 && !sameBinding(common, binding) {
				return nil, false, errors.New("every task context in task_contexts must carry the same adviser binding")
			}
			common = binding
		}
		return common, true, nil
	}
	return nil, false, nil
}

// checkUntrusted validates the optional state.untrusted marking of text that
// came from untrusted sources.
func checkUntrusted(state any) error {
	fields, ok := mapping(state)
	if !ok || !fields.has("untrusted") {
		return nil
	}
	entries, ok := fields.get("untrusted").([]any)
	if !ok || len(entries) == 0 {
		return errors.New("state.untrusted must be a nonempty array of {source, text} objects")
	}
	for index, value := range entries {
		entry, ok := mapping(value)
		if !ok || !entry.exactly("source", "text") || !textValue(entry.get("source")) || !textValue(entry.get("text")) {
			return fmt.Errorf("state.untrusted entry %d must be an object with only nonblank source and text strings", index)
		}
	}
	return nil
}

func hasUntrusted(state any) bool {
	fields, ok := mapping(state)
	return ok && fields.has("untrusted")
}
