package workflow

import (
	"context"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

// Object retains the deployed version-one ownership and recovery JSON schema.
type Object = map[string]any

type Paths struct {
	Source string
	Home   string
	Codex  string
	Claude string
	State  string
}

// RuntimeIdentity identifies a verified native manager independently of the
// active skill snapshot. Rolling back skills must not downgrade their manager.
type RuntimeIdentity struct {
	Revision string `json:"revision"`
	Version  string `json:"version"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

type Preset string

const (
	PresetRecommended Preset = "recommended"
	PresetBalanced    Preset = "balanced"
)

type ModelSelection struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// Nil fields follow the selected release; explicit values remain pinned even
// when they equal that release's defaults.
type ModelOverride struct {
	Model  *string `json:"model,omitempty"`
	Effort *string `json:"effort,omitempty"`
}

type HostPreferences struct {
	Roles    map[string]ModelOverride `json:"roles,omitempty"`
	Fallback *ModelOverride           `json:"fallback,omitempty"`
}

type SharedPreferences struct {
	ContextWindow      *int64  `json:"model_context_window,omitempty"`
	Memories           *bool   `json:"features.memories,omitempty"`
	GenerateMemories   *bool   `json:"memories.generate_memories,omitempty"`
	UseMemories        *bool   `json:"memories.use_memories,omitempty"`
	ExtractModel       *string `json:"memories.extract_model,omitempty"`
	ConsolidationModel *string `json:"memories.consolidation_model,omitempty"`
}

type Preferences struct {
	Version int                        `json:"version"`
	Hosts   map[string]HostPreferences `json:"hosts,omitempty"`
	Shared  *SharedPreferences         `json:"shared,omitempty"`
}

type ModelChoice struct {
	Model   string   `json:"model"`
	Efforts []string `json:"efforts"`
}

type RoleConfiguration struct {
	Role      string         `json:"role"`
	Scope     string         `json:"scope"`
	Default   ModelSelection `json:"default"`
	Effective ModelSelection `json:"effective"`
	Override  ModelOverride  `json:"override"`
	Choices   []ModelChoice  `json:"choices"`
}

type HostConfiguration struct {
	ID        string              `json:"id"`
	Root      string              `json:"root"`
	Detected  bool                `json:"detected"`
	Installed bool                `json:"installed"`
	Selected  bool                `json:"selected"`
	Roles     []RoleConfiguration `json:"roles"`
	Fallback  RoleConfiguration   `json:"fallback"`
}

type ConfigurationView struct {
	Command         string              `json:"command"`
	Installed       bool                `json:"installed"`
	Generation      string              `json:"generation"`
	ConfigDir       string              `json:"config_dir"`
	Target          bundled.Identity    `json:"target"`
	Hosts           []HostConfiguration `json:"hosts"`
	Preferences     Preferences         `json:"preferences"`
	SharedDefaults  Object              `json:"shared_defaults"`
	SharedEffective Object              `json:"shared_effective"`
}

type ConfigurationSelection struct {
	Hosts       []string     `json:"hosts,omitempty"`
	Preset      Preset       `json:"preset,omitempty"`
	Preferences *Preferences `json:"preferences,omitempty"`
}

type ConfigurationSelector func(context.Context, ConfigurationView) (ConfigurationSelection, error)

type Installation struct {
	Paths
	ConfigDir    string
	Generation   string
	StateVersion int
	Installed    bool
	Pending      bool
}
