package consult

import (
	"context"
	"errors"
	"time"
)

// probeModels are the pinned models each credential is checked against.
var probeModels = map[string]string{jevProvider: sharedJevModel, snapProvider: snapModel, openAIProvider: openAIModel}

// CheckCredentials sends only a fixed synthetic Choice request, once to each
// adviser whose credential is set, against that adviser's pinned model. It
// reuses the validated responses, no-retry transport and common deadline. The
// result maps every adviser to verified, unavailable or missing (credential
// unset, never contacted); with no credential set it makes no network call.
// No request, raw response or credential is written to disk or returned.
func CheckCredentials(ctx context.Context, getenv func(string) string) (map[string]string, error) {
	shared, err := sharedRequest(newObject("state", newObject("evidence", "The marker is blue."), "questions", newObject("marker", newObject("type", "choice", "instructions", "What color is the marker?", "criteria", newObject("blue", "The evidence says blue.", "other", "The evidence says another color.")))))
	if err != nil {
		return nil, errors.New("credential check could not prepare synthetic request")
	}
	prepared := newObject()
	keys := map[string]string{}
	for _, name := range providerOrder {
		key := credential(getenv, name)
		if key == "" {
			continue
		}
		value, err := prepareProvider(name, shared, probeModels[name])
		if err != nil {
			return nil, errors.New("credential check could not prepare pinned providers")
		}
		prepared.set(name, value)
		keys[name] = key
	}
	raw := map[string]any{}
	if prepared.size() > 0 {
		raw, err = newTransport().collect(ctx, prepared, batch{credentials: keys, deadline: 30 * time.Second})
		if err != nil {
			return nil, err
		}
	}
	result := map[string]string{}
	failed := false
	for _, name := range providerOrder {
		if keys[name] == "" {
			result[name] = "missing"
			continue
		}
		value, ok := mapping(raw[name])
		if ok && value.get("status") == "success" {
			result[name] = "verified"
		} else {
			result[name] = "unavailable"
			failed = true
		}
	}
	if failed {
		return result, errors.New("one or more credential checks failed; settings and saved keys remain applied")
	}
	return result, nil
}
