// Package agentworkflows embeds the shipped files that the cw runtime falls
// back to when no installed copy is selected.
package agentworkflows

import "embed"

// DefaultModelPolicy is the shipped task-orchestration model policy. cw
// consult and cw task start read it when no policy is selected by --policy,
// by CW_MODEL_POLICY or by the personal task-orchestration skill.
//
//go:embed skills/task-orchestration/model-policy.yaml
var DefaultModelPolicy []byte

// Packs holds the pack manifests built into cw, at packs/<id>/pack.yaml.
// cw reads pack manifests only from here, never from an installed plugin.
//
//go:embed packs/*/pack.yaml
var Packs embed.FS
