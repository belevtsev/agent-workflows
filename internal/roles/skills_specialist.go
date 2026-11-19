package roles

import (
	"fmt"
	"slices"

	"github.com/belevtsev/kitsu/v3/internal/skills"
)

// SpecialistSettings records the configuration supplied for a worker reservation.
// Actual model use and skill loading are observations of the host.
type SpecialistSettings struct {
	Host   string             `json:"host"`
	Model  Model              `json:"model"`
	Skills []skills.Reference `json:"skills"`
}

// SpecialistHost resolves a repository assignment or checks an explicit host
// for legacy policy. An explicit host on a configured role must match.
func (c *Config) SpecialistHost(host, id string) (string, error) {
	if !Known(id) {
		return "", fmt.Errorf("unknown specialist role %q", id)
	}
	if c != nil && c.Version == WorkflowConfigVersion {
		assigned := c.ConfiguredHost(id)
		if assigned == "" {
			return "", fmt.Errorf("%s specialist is not configured in %s", id, FileName)
		}
		if host != "" && host != assigned {
			return "", fmt.Errorf("%s is assigned to %s in %s, got host %q", id, assigned, FileName, host)
		}
		return assigned, nil
	}
	if !slices.Contains(Hosts, host) {
		return "", fmt.Errorf("%s host must be codex or claude", id)
	}
	return host, nil
}

// ResolveSpecialist returns the role's requested settings and original skills.
func ResolveSpecialist(root, host, id string) (*SpecialistSettings, error) {
	cfg, err := Load(root)
	if err != nil {
		return nil, err
	}
	return cfg.resolveSpecialist(root, host, id, map[string]skills.Catalog{})
}

// ResolveWorkflow resolves the complete version 2 repository role assignment.
func ResolveWorkflow(root string) (map[string]SpecialistSettings, error) {
	cfg, err := Load(root)
	if err != nil {
		return nil, err
	}
	return cfg.ResolveWorkflow(root)
}

// ResolveWorkflow resolves the roles and skills of this policy snapshot.
func (cfg *Config) ResolveWorkflow(root string) (map[string]SpecialistSettings, error) {
	if cfg.Version != WorkflowConfigVersion {
		return nil, fmt.Errorf("configured workflow requires version 2 in %s", FileName)
	}
	settings := make(map[string]SpecialistSettings, len(cfg.assignments))
	catalogs := map[string]skills.Catalog{}
	for _, id := range IDs() {
		if cfg.ConfiguredHost(id) == "" {
			continue
		}
		resolved, err := cfg.resolveSpecialist(root, "", id, catalogs)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", id, err)
		}
		settings[id] = *resolved
	}
	return settings, nil
}

func (c *Config) resolveSpecialist(root, host, id string, catalogs map[string]skills.Catalog) (*SpecialistSettings, error) {
	host, err := c.SpecialistHost(host, id)
	if err != nil {
		return nil, err
	}
	settings := SpecialistSettings{Host: host, Skills: []skills.Reference{}}
	for _, role := range defaults {
		if role.ID == id {
			settings.Model = c.roleModel(role, host)
			break
		}
	}
	selected := c.Skills[host][id]
	if len(selected) == 0 {
		return &settings, nil
	}
	catalog, ok := catalogs[host]
	if !ok {
		catalog, err = skills.Discover(root, host)
		if err != nil {
			return nil, err
		}
		catalogs[host] = catalog
	}
	entries, err := catalog.Resolve(selected)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		settings.Skills = append(settings.Skills, entry.Reference())
	}
	return &settings, nil
}
