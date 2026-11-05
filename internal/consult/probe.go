package consult

import (
	"context"
	"errors"
	"time"
)

// CheckCredentials sends only a fixed synthetic Choice request. It reuses the
// pinned models, validated responses, no-retry transport and common deadline.
// No request, raw response or credential is written to disk or returned.
func CheckCredentials(ctx context.Context, getenv func(string) string) (map[string]string, error) {
	shared, err := sharedRequest(newObject("state", newObject("evidence", "The marker is blue."), "questions", newObject("marker", newObject("type", "choice", "instructions", "What color is the marker?", "criteria", newObject("blue", "The evidence says blue.", "other", "The evidence says another color.")))))
	if err != nil {
		return nil, errors.New("credential check could not prepare synthetic request")
	}
	p := policy{providers: map[string]string{jevProvider: sharedJevModel, snapProvider: snapModel, openAIProvider: openAIModel}}
	prepared, err := p.prepareAll(shared)
	if err != nil {
		return nil, errors.New("credential check could not prepare pinned providers")
	}
	keys := map[string]string{}
	for _, name := range providerOrder {
		keys[name] = getenv(credentialNames[name])
	}
	raw, err := newTransport().collect(ctx, prepared, batch{credentials: keys, deadline: 30 * time.Second})
	if err != nil {
		return nil, err
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
