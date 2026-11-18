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

func ResolveSpecialist(root, host, id string) (*SpecialistSettings, error) {
	if !slices.Contains(Hosts, host) {
		return nil, fmt.Errorf("%s host must be codex or claude", id)
	}
	if !Known(id) {
		return nil, fmt.Errorf("unknown specialist role %q", id)
	}
	cfg, err := Load(root)
	if err != nil {
		return nil, err
	}
	settings := SpecialistSettings{Host: host, Skills: []skills.Reference{}}
	for _, role := range All(cfg) {
		if role.ID == id {
			settings.Model = role.Models[host]
			break
		}
	}
	selected := cfg.Skills[host][id]
	if len(selected) == 0 {
		return &settings, nil
	}
	catalog, err := skills.Discover(root, host)
	if err != nil {
		return nil, err
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
