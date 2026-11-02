package workflow

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

var claudePolicyModels = []string{"claude-opus-5-5", "claude-sonnet-5-5"}

// settings.json rejects max, so the policy cannot persist it as a default.
var claudePolicyEfforts = []string{"low", "medium", "high", "xhigh"}

// suiteClaudeModelPolicy validates the Claude Code role mapping. Its roles must
// match the shared policy so a role can never exist on only one host.
func suiteClaudeModelPolicy(policyPath string, shared Object) (Object, error) {
	data, err := suiteRead(policyPath, "Claude model policy")
	if err != nil {
		return nil, err
	}
	value, err := suiteYAML(data)
	if err != nil {
		return nil, err
	}
	policy, err := suiteMapping(value, "Claude policy", "version", "profiles", "roles", "unknown_role_profile")
	if err != nil {
		return nil, err
	}
	if !suiteInteger(policy["version"], 1) {
		return nil, errors.New("Claude policy version has an invalid value")
	}
	profiles, err := suiteMapping(policy["profiles"], "Claude profiles")
	if err != nil || len(profiles) == 0 {
		return nil, errors.New("Claude policy requires profiles")
	}
	for name, raw := range profiles {
		profile, err := suiteMapping(raw, "Claude profile", "model", "effort", "scope")
		if err != nil {
			return nil, err
		}
		if !slices.Contains(claudePolicyModels, text(profile["model"])) {
			return nil, fmt.Errorf("unsupported Claude model in profile %s", name)
		}
		if !slices.Contains(claudePolicyEfforts, text(profile["effort"])) {
			return nil, fmt.Errorf("unsupported Claude effort in profile %s", name)
		}
		if !slices.Contains([]string{"substantive", "bounded_execution", "bounded_evidence"}, text(profile["scope"])) {
			return nil, fmt.Errorf("unsupported Claude scope in profile %s", name)
		}
	}
	roles, err := suiteMapping(policy["roles"], "Claude roles")
	if err != nil {
		return nil, err
	}
	sharedRoles := object(shared["roles"])
	if !slices.Equal(slices.Sorted(maps.Keys(roles)), slices.Sorted(maps.Keys(sharedRoles))) {
		return nil, errors.New("Claude policy roles must match the shared policy")
	}
	for role, raw := range roles {
		profile := object(profiles[text(raw)])
		if profile == nil {
			return nil, fmt.Errorf("Claude role %s references an unknown profile", role)
		}
		required := "substantive"
		if slices.Contains(policyExecutionRoles, role) {
			required = "bounded_execution"
		} else if slices.Contains(policyEvidenceRoles, role) {
			required = "bounded_evidence"
		}
		if profile["scope"] != required {
			return nil, fmt.Errorf("Claude role %s requires %s scope", role, required)
		}
	}
	if object(profiles[text(policy["unknown_role_profile"])])["scope"] != "substantive" {
		return nil, errors.New("Claude unknown roles must use a substantive profile")
	}
	return policy, nil
}

// ClaudeModelDefaults resolves the coordinator's two managed settings.json keys
// after validating the shared policy and its Claude role mapping.
func ClaudeModelDefaults(root string, manifest Object) (map[string]string, error) {
	root, err := suiteRoot(root, false)
	if err != nil {
		return nil, err
	}
	sharedPath, err := suiteRelativePath(root, manifest["model_policy"], "model_policy")
	if err != nil {
		return nil, err
	}
	shared, err := suiteModelPolicy(sharedPath)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve validated Claude model defaults: %w", err)
	}
	policyPath, err := suiteRelativePath(root, manifest["claude_model_policy"], "claude_model_policy")
	if err != nil {
		return nil, err
	}
	policy, err := suiteClaudeModelPolicy(policyPath, shared)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve validated Claude model defaults: %w", err)
	}
	profile := object(object(policy["profiles"])[text(object(policy["roles"])["coordinator"])])
	return map[string]string{"model": text(profile["model"]), "effortLevel": text(profile["effort"])}, nil
}
