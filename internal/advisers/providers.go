package advisers

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Provider describes a built-in preset. Listing presets performs no I/O.
type Provider struct {
	ID        string   `json:"id"`
	Model     string   `json:"model,omitempty"`
	BaseURL   string   `json:"base_url,omitempty"`
	APIKeyEnv string   `json:"api_key_env,omitempty"`
	Kinds     []string `json:"kinds"`
}

func Providers() []Provider {
	return []Provider{
		{ID: "typesafe", Model: "jev-1.14.0", BaseURL: "https://api.typesafe.ai/v1", APIKeyEnv: "TYPESAFE_API_KEY", Kinds: Kinds("typesafe")}, // #nosec G101 -- Environment variable name, not a credential.
		{ID: "subq", Model: "subq-snap-1.2", BaseURL: "https://api.subq.ai/v1", APIKeyEnv: "SUBQ_API_KEY", Kinds: Kinds("subq")},              // #nosec G101 -- Environment variable name, not a credential.
		{ID: "systemone", Kinds: Kinds("systemone")},
	}
}

// Kinds returns the request kinds supported by a provider; empty means CLI.
func Kinds(provider string) []string {
	switch provider {
	case "":
		return []string{"advice", "decision"}
	case "typesafe", "subq", "systemone":
		return []string{"decision", "score", "boolean"}
	default:
		return nil
	}
}

func definitionFromConfig(raw configAdviser, version int) (Definition, error) {
	d := Definition{Model: raw.Model, Command: raw.Command, Timeout: DefaultTimeout}
	if version == 1 && (raw.Provider != nil || raw.BaseURL != nil || raw.APIKeyEnv != nil) {
		return d, errors.New("native provider fields require configuration version 2")
	}
	if raw.Provider == nil {
		if raw.BaseURL != nil || raw.APIKeyEnv != nil {
			return d, errors.New("base_url and api_key_env require a provider")
		}
		return d, nil
	}
	d.Provider = *raw.Provider
	if d.Provider == "" || Kinds(d.Provider) == nil {
		return d, errors.New("provider must be typesafe, subq or systemone")
	}
	for _, preset := range Providers() {
		if preset.ID == d.Provider {
			if d.Model == "" {
				d.Model = preset.Model
			}
			d.BaseURL, d.APIKeyEnv = preset.BaseURL, preset.APIKeyEnv
		}
	}
	if raw.BaseURL != nil {
		d.BaseURL = *raw.BaseURL
	}
	if raw.APIKeyEnv != nil {
		d.APIKeyEnv = *raw.APIKeyEnv
	}
	d.BaseURL = strings.TrimRight(d.BaseURL, "/")
	return d, nil
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateNativeDefinition(d Definition) error {
	if Kinds(d.Provider) == nil {
		return errors.New("unknown native provider")
	}
	if d.Command != nil {
		return errors.New("native providers cannot have a command")
	}
	u, err := url.Parse(d.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(d.BaseURL, "#") {
		return errors.New("base_url must be an https URL without credentials, query or fragment")
	}
	if !envName.MatchString(d.APIKeyEnv) {
		return errors.New("api_key_env must name an environment variable")
	}
	return nil
}

// ValidateCapability runs before context reads, record creation or execution.
func ValidateCapability(provider string, request Request) error {
	if err := ValidateRequest(request); err != nil {
		return err
	}
	if !slices.Contains(Kinds(provider), request.Kind) {
		return fmt.Errorf("adviser does not support %s requests; supported kinds: %s", request.Kind, strings.Join(Kinds(provider), ", "))
	}
	if provider != "" && len(request.Choices) > 255 {
		return errors.New("native decisions support at most 255 choices")
	}
	return nil
}
