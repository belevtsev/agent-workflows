package consult

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/consult/record"
	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
)

// validModel applies the policy pin rules of internal/modelpolicy: a
// versioned Jev ID, the one Snap preview, and the one documented OpenAI
// Decisions model.
func validModel(provider string, model any) bool {
	name, ok := model.(string)
	return ok && modelpolicy.ValidModel(provider, name)
}

// knownProvider reports whether name is a known adviser ID under the policy
// pin rules.
func knownProvider(name string) bool { return slices.Contains(providerOrder, name) }

// policy is the validated adviser portion of a model policy: its enabled
// providers (provider -> pinned model, possibly empty) and the batch deadline.
type policy struct {
	deadline  time.Duration
	providers map[string]string
}

// loadPolicy reads the adviser portion of the selected policy. Its errors
// are the validation failures of internal/modelpolicy.
func loadPolicy(source modelpolicy.Source) (policy, error) {
	selected, err := source.Load()
	if err != nil {
		return policy{}, err
	}
	return policy{deadline: selected.Deadline, providers: selected.Providers}, nil
}

// ConfiguredAdvisers returns the advisers that a consultation run under the
// selected model policy would attempt: each provider the policy enables
// whose credential variable (TYPESAFE_API_KEY, SUBQ_API_KEY or OPENAI_API_KEY)
// is non-empty under getenv. The result maps provider ID to its pinned model,
// is never nil, and is empty when nothing is configured. Only the credential
// variables of enabled providers are read, and no credential value is
// returned. A nil getenv reads no credentials. The error is the policy
// validation failure, if any. A credential alone is not consent: cw task start
// removes the advisers the user has disabled or not opted in to.
func ConfiguredAdvisers(selected modelpolicy.Source, getenv func(string) string) (map[string]string, error) {
	loaded, err := loadPolicy(selected)
	if err != nil {
		return nil, err
	}
	configured := map[string]string{}
	for _, name := range providerOrder {
		model, enabled := loaded.providers[name]
		if enabled && credential(getenv, name) != "" {
			configured[name] = model
		}
	}
	return configured, nil
}

// credentialVariable returns the credential variable of a known adviser, or ""
// for an unknown one.
func credentialVariable(provider string) string {
	name, _ := record.CredentialVariable(provider)
	return name
}

func credential(getenv func(string) string, provider string) string {
	if getenv == nil {
		return ""
	}
	return getenv(credentialVariable(provider))
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
