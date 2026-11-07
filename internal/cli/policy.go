package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	agentworkflows "github.com/belevtsev/agent-workflows"
	"github.com/belevtsev/agent-workflows/internal/modelpolicy"
	"github.com/spf13/cobra"
)

// policyEnvironment names the policy file a command without --policy uses.
const policyEnvironment = "CW_MODEL_POLICY"

// personalPolicyPath is the policy of the task-orchestration skill installed
// as a personal skill, relative to the home directory.
var personalPolicyPath = filepath.Join(".agents", "skills", "task-orchestration", "model-policy.yaml")

const policyFlagHelp = "defaults to $CW_MODEL_POLICY, then ~/.agents/skills/task-orchestration/model-policy.yaml, then the policy embedded in cw"

// selectPolicy resolves the model policy a command runs under: the --policy
// value, then $CW_MODEL_POLICY, then the personal task-orchestration skill's
// model-policy.yaml under the home the keys are read from, then the shipped
// policy embedded in cw. The embedded policy is used only when nothing exists
// at the personal path: a path that exists but cannot be read, such as a
// dangling symlink or a directory, is selected and reported by the command
// that loads it, so a broken personal policy never silently enables the
// shipped providers.
func selectPolicy(root *cobra.Command, config Config, explicit string) (modelpolicy.Source, error) {
	if explicit != "" {
		return modelpolicy.Source{Path: explicit}, nil
	}
	if path := config.Getenv(policyEnvironment); path != "" {
		return modelpolicy.Source{Path: path}, nil
	}
	home, err := userHome(root, config)
	if err != nil {
		return modelpolicy.Source{}, err
	}
	personal := filepath.Join(home, personalPolicyPath)
	if _, err := os.Lstat(personal); errors.Is(err, fs.ErrNotExist) {
		return modelpolicy.Source{Data: agentworkflows.DefaultModelPolicy}, nil
	}
	return modelpolicy.Source{Path: personal}, nil
}
