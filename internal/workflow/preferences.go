package workflow

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

// DecodePreferences accepts only the nonsecret, per-field preference schema.
// Release-dependent compatibility is checked when a bundle is selected.
func DecodePreferences(data []byte) (Preferences, error) {
	var prefs Preferences
	if err := json.Unmarshal(data, &prefs, json.RejectUnknownMembers(true)); err != nil {
		return prefs, fmt.Errorf("invalid preferences: %w", err)
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return prefs, err
	}
	var noNull func(any) bool
	noNull = func(v any) bool {
		if v == nil {
			return false
		}
		if m, ok := v.(map[string]any); ok {
			for _, x := range m {
				if !noNull(x) {
					return false
				}
			}
		}
		return true
	}
	if !noNull(raw) || prefs.Version != 1 {
		return prefs, errors.New("preferences require version 1 and non-null explicit fields")
	}
	for host, hp := range prefs.Hosts {
		if host != hostCodex && host != hostClaude {
			return prefs, fmt.Errorf("unknown preference host %q", host)
		}
		for role, value := range hp.Roles {
			if strings.TrimSpace(role) == "" {
				return prefs, errors.New("preference role must be nonblank")
			}
			if err := validateOverride(value); err != nil {
				return prefs, err
			}
		}
		if hp.Fallback != nil {
			if err := validateOverride(*hp.Fallback); err != nil {
				return prefs, err
			}
		}
	}
	if prefs.Shared != nil {
		if prefs.Shared.ContextWindow != nil && *prefs.Shared.ContextWindow <= 0 {
			return prefs, errors.New("model_context_window must be positive")
		}
		for _, value := range []*string{prefs.Shared.ExtractModel, prefs.Shared.ConsolidationModel} {
			if value != nil && (strings.TrimSpace(*value) == "" || strings.ContainsAny(*value, "\x00\r\n")) {
				return prefs, errors.New("memory model overrides must be nonblank single-line values")
			}
		}
	}
	if prefs.Advisers != nil {
		for i, id := range prefs.Advisers.Disabled {
			if !slices.Contains(ConsultationProviders(), id) {
				return prefs, fmt.Errorf("unknown adviser %q in advisers.disabled", id)
			}
			if slices.Contains(prefs.Advisers.Disabled[:i], id) {
				return prefs, fmt.Errorf("adviser %q is listed twice in advisers.disabled", id)
			}
		}
	}
	return normalizePreferences(prefs), nil
}

func validateOverride(value ModelOverride) error {
	for _, field := range []*string{value.Model, value.Effort} {
		if field != nil && (strings.TrimSpace(*field) == "" || strings.ContainsAny(*field, "\x00\r\n")) {
			return errors.New("model and effort overrides must be nonblank single-line values")
		}
	}
	return nil
}

func normalizePreferences(prefs Preferences) Preferences {
	for host, hp := range prefs.Hosts {
		for role, value := range hp.Roles {
			if value.Model == nil && value.Effort == nil {
				delete(hp.Roles, role)
			}
		}
		if hp.Fallback != nil && hp.Fallback.Model == nil && hp.Fallback.Effort == nil {
			hp.Fallback = nil
		}
		if len(hp.Roles) == 0 && hp.Fallback == nil {
			delete(prefs.Hosts, host)
		} else {
			prefs.Hosts[host] = hp
		}
	}
	if prefs.Shared != nil && len(sharedPreferenceValues(prefs.Shared)) == 0 {
		prefs.Shared = nil
	}
	if prefs.Advisers != nil {
		var disabled []string
		for _, id := range ConsultationProviders() {
			if slices.Contains(prefs.Advisers.Disabled, id) {
				disabled = append(disabled, id)
			}
		}
		prefs.Advisers = nil
		if len(disabled) > 0 {
			prefs.Advisers = &AdviserPreferences{Disabled: disabled}
		}
	}
	return prefs
}

func sharedPreferenceValues(p *SharedPreferences) Object {
	out := Object{}
	if p == nil {
		return out
	}
	if p.ContextWindow != nil {
		out["model_context_window"] = *p.ContextWindow
	}
	if p.Memories != nil {
		out["features.memories"] = *p.Memories
	}
	if p.GenerateMemories != nil {
		out["memories.generate_memories"] = *p.GenerateMemories
	}
	if p.UseMemories != nil {
		out["memories.use_memories"] = *p.UseMemories
	}
	if p.ExtractModel != nil {
		out["memories.extract_model"] = *p.ExtractModel
	}
	if p.ConsolidationModel != nil {
		out["memories.consolidation_model"] = *p.ConsolidationModel
	}
	return out
}

// PreferencesForSelection is the same pure preset/override compiler used by
// activation. A named preset intentionally replaces all saved model and shared
// overrides; the saved adviser selection stays unless selection.Advisers
// replaces it.
func PreferencesForSelection(view ConfigurationView, selection ConfigurationSelection) (Preferences, error) {
	if selection.Preset != "" && selection.Preferences != nil {
		return Preferences{}, errors.New("preset and explicit preferences are mutually exclusive")
	}
	if selection.Advisers != nil && selection.Preferences != nil {
		return Preferences{}, errors.New("adviser selection and explicit preferences are mutually exclusive")
	}
	for _, id := range selection.Hosts {
		if !slices.ContainsFunc(view.Hosts, func(h HostConfiguration) bool { return h.ID == id }) {
			return Preferences{}, fmt.Errorf("unknown selected host %q", id)
		}
	}
	prefs := view.Preferences
	switch selection.Preset {
	case "":
		if selection.Preferences != nil {
			prefs = *selection.Preferences
		}
	case PresetRecommended:
		prefs = Preferences{Version: 1}
	case PresetBalanced:
		prefs = Preferences{Version: 1, Hosts: map[string]HostPreferences{}}
		for _, host := range view.Hosts {
			model, effort := "gpt-6.1-sol", "xhigh"
			if host.ID == hostClaude {
				model, effort = "claude-opus-5-5", "high"
			}
			hp := HostPreferences{Roles: map[string]ModelOverride{}, Fallback: &ModelOverride{Model: &model, Effort: &effort}}
			for _, role := range host.Roles {
				// Claude roles that default to a cheaper model, such as follow-up
				// reviews on Sonnet, keep that default.
				if role.Scope == "substantive" && (host.ID != hostClaude || role.Default.Model == model) {
					hp.Roles[role.Role] = ModelOverride{Model: &model, Effort: &effort}
				}
			}
			prefs.Hosts[host.ID] = hp
		}
	default:
		return Preferences{}, fmt.Errorf("unknown preset %q", selection.Preset)
	}
	if selection.Preset != "" {
		prefs.Advisers = view.Preferences.Advisers
	}
	if selection.Advisers != nil {
		prefs.Advisers = selection.Advisers
	}
	if prefs.Version == 0 && len(prefs.Hosts) == 0 && prefs.Shared == nil {
		prefs.Version = 1
	}
	copy, err := DecodePreferences(legacyJSON(prefs))
	if err != nil {
		return Preferences{}, err
	}
	for id, hp := range copy.Hosts {
		idx := slices.IndexFunc(view.Hosts, func(h HostConfiguration) bool { return h.ID == id })
		if idx < 0 {
			return Preferences{}, fmt.Errorf("preference host %q is unavailable in this release", id)
		}
		host := view.Hosts[idx]
		for role, value := range hp.Roles {
			r := slices.IndexFunc(host.Roles, func(r RoleConfiguration) bool { return r.Role == role })
			if r < 0 {
				return Preferences{}, fmt.Errorf("preference role %q is unavailable in this release", role)
			}
			if _, err := selectedModel(host.Roles[r], value); err != nil {
				return Preferences{}, fmt.Errorf("%s role %s: %w", id, role, err)
			}
		}
		if hp.Fallback != nil {
			if _, err := selectedModel(host.Fallback, *hp.Fallback); err != nil {
				return Preferences{}, fmt.Errorf("%s fallback: %w", id, err)
			}
		}
	}
	for key := range sharedPreferenceValues(copy.Shared) {
		if _, ok := view.SharedDefaults[key]; !ok {
			return Preferences{}, fmt.Errorf("preference %s is unavailable in this release", key)
		}
	}
	return copy, nil
}

// SavedPreferences reads <configDir>/preferences.json. A missing file, or a
// missing directory, yields version-one defaults without checking the
// directory. An existing file is read under the installer's private-directory
// rules, and an unsafe, unreadable or invalid file is an error.
func SavedPreferences(configDir string) (Preferences, error) {
	i := &Installer{Options: Options{ConfigDir: configDir}}
	if _, err := os.Lstat(i.preferencesPath()); errors.Is(err, os.ErrNotExist) {
		return Preferences{Version: 1}, nil
	}
	prefs, _, err := i.readPreferences()
	return prefs, err
}

func selectedModel(role RoleConfiguration, override ModelOverride) (ModelSelection, error) {
	result := role.Default
	if override.Model != nil {
		result.Model = *override.Model
	}
	if override.Effort != nil {
		result.Effort = *override.Effort
	}
	for _, choice := range role.Choices {
		if result.Model == choice.Model && slices.Contains(choice.Efforts, result.Effort) {
			if role.Scope == "substantive" && result.Model == "gpt-6-luna" {
				break
			}
			return result, nil
		}
	}
	return result, errors.New("unsupported model/effort combination for this scope")
}

// ResolveConfigDir binds an existing installation before consulting XDG.
func ResolveConfigDir(home, explicit, recorded string, getenv func(string) string) (string, error) {
	if explicit != "" && (!filepath.IsAbs(explicit) || filepath.Clean(explicit) != explicit) {
		return "", errors.New("explicit configuration directory must be an absolute clean path")
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	if recorded != "" {
		if !safeAbsolute(recorded) {
			return "", errors.New("unsafe recorded configuration root")
		}
		if explicit != "" && Normalize(explicit) != recorded {
			return "", errors.New("explicit configuration directory differs from the installation")
		}
		return recorded, nil
	}
	if explicit != "" {
		root := Normalize(explicit)
		if !safeAbsolute(root) {
			return "", errors.New("unsafe explicit configuration root")
		}
		return root, nil
	}
	root := getenv("XDG_CONFIG_HOME")
	if root == "" {
		root = filepath.Join(home, ".config")
	}
	if !safeAbsolute(root) {
		return "", errors.New("XDG_CONFIG_HOME must be an absolute clean path")
	}
	return normalizeAlias(filepath.Join(root, "agent-workflows")), nil
}

func (i *Installer) configurationView(command, root string, manifest Object, state Object, prefs Preferences, identity bundled.Identity) (ConfigurationView, error) {
	view := ConfigurationView{Command: command, Installed: state != nil, Generation: text(state["integrity_sha256"]), ConfigDir: i.ConfigDir, Target: identity, Preferences: prefs}
	shared, err := suiteModelPolicy(filepath.Join(root, text(manifest["model_policy"])))
	if err != nil {
		return view, err
	}
	claude, err := suiteClaudeModelPolicy(filepath.Join(root, text(manifest["claude_model_policy"])), shared)
	if err != nil {
		return view, err
	}
	var currentShared, currentClaude, currentMemory Object
	if state != nil {
		currentRoot, currentManifest, e := i.release(text(state["release"]))
		if e != nil {
			return view, e
		}
		currentShared, e = suiteModelPolicy(filepath.Join(currentRoot, text(currentManifest["model_policy"])))
		if e != nil {
			return view, e
		}
		currentClaude, e = suiteClaudeModelPolicy(filepath.Join(currentRoot, text(currentManifest["claude_model_policy"])), currentShared)
		if e != nil {
			return view, e
		}
		currentMemory, e = SharedConfigDefaults(currentRoot)
		if e != nil {
			return view, e
		}
	}
	for _, id := range []string{hostCodex, hostClaude} {
		h := i.host(id)
		host := HostConfiguration{ID: id, Root: h.root, Detected: exists(h.root), Installed: hostRecord(state, id) != nil, Selected: slices.Contains(i.Hosts, id)}
		if len(i.Hosts) == 0 && (host.Installed || state == nil && host.Detected) {
			host.Selected = true
		}
		policy := shared
		if id == hostClaude {
			policy = claude
		}
		profiles := object(policy["profiles"])
		makeRole := func(role, profileName string) RoleConfiguration {
			profile := object(profiles[profileName])
			effort := text(profile["reasoning_effort"])
			if id == hostClaude {
				effort = text(profile["effort"])
			}
			r := RoleConfiguration{Role: role, Profile: profileName, Scope: text(profile["scope"]), Default: ModelSelection{Model: text(profile["model"]), Effort: effort}}
			if id == hostCodex {
				for _, model := range slices.Sorted(maps.Keys(policyModelEfforts)) {
					if model != "gpt-6-luna" || r.Scope != "substantive" {
						r.Choices = append(r.Choices, ModelChoice{Model: model, Efforts: slices.Clone(policyModelEfforts[model])})
					}
				}
			} else {
				for _, model := range claudePolicyModels {
					r.Choices = append(r.Choices, ModelChoice{Model: model, Efforts: slices.Clone(claudePolicyEfforts)})
				}
			}
			r.Override = prefs.Hosts[id].Roles[role]
			if role == "substantive_fallback" && prefs.Hosts[id].Fallback != nil {
				r.Override = *prefs.Hosts[id].Fallback
			}
			r.Effective, _ = selectedModel(r, r.Override)
			if !host.Installed {
				r.Effective = ModelSelection{}
			}
			if host.Installed {
				currentPolicy := currentShared
				if id == hostClaude {
					currentPolicy = currentClaude
				}
				name := text(object(currentPolicy["roles"])[role])
				if role == "substantive_fallback" {
					name = text(currentPolicy["unknown_role_profile"])
				}
				previous := object(object(currentPolicy["profiles"])[name])
				effort := text(previous["reasoning_effort"])
				if id == hostClaude {
					effort = text(previous["effort"])
				}
				r.Effective = ModelSelection{Model: text(previous["model"]), Effort: effort}
			}
			return r
		}
		for _, role := range keys(object(policy["roles"])) {
			host.Roles = append(host.Roles, makeRole(role, text(object(policy["roles"])[role])))
		}
		host.Fallback = makeRole("substantive_fallback", text(policy["unknown_role_profile"]))
		view.Hosts = append(view.Hosts, host)
	}
	providers := object(object(shared["consultation"])["providers"])
	for _, id := range ConsultationProviders() {
		if _, enabled := providers[id]; enabled {
			view.Advisers = append(view.Advisers, id)
		}
	}
	view.SharedDefaults, err = SharedConfigDefaults(root)
	if err != nil {
		return view, err
	}
	view.SharedEffective = clone(view.SharedDefaults)
	if view.SharedEffective == nil {
		view.SharedEffective = Object{}
	}
	for key, value := range sharedPreferenceValues(prefs.Shared) {
		view.SharedEffective[key] = value
	}
	if state != nil {
		view.SharedEffective = clone(currentMemory)
	}
	return view, nil
}
