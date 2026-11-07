// Package agentworkflows embeds the shipped files that the cw runtime falls
// back to when no installed copy is selected.
package agentworkflows

import _ "embed"

// DefaultModelPolicy is the shipped task-orchestration model policy. cw
// consult and cw task start read it when no policy is selected by --policy,
// by CW_MODEL_POLICY or by the personal task-orchestration skill.
//
//go:embed skills/task-orchestration/model-policy.yaml
var DefaultModelPolicy []byte
