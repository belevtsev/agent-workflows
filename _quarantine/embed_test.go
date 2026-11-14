package agentworkflows_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
)

// RT-1: the embedded default policy is the shipped policy.
func TestDefaultModelPolicyIsShippedPolicy(t *testing.T) {
	shipped, err := os.ReadFile(filepath.Join("skills", "task-orchestration", "model-policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(agentworkflows.DefaultModelPolicy, shipped) {
		t.Fatalf("DefaultModelPolicy (%d bytes) differs from skills/task-orchestration/model-policy.yaml (%d bytes)",
			len(agentworkflows.DefaultModelPolicy), len(shipped))
	}
	policy, err := modelpolicy.Parse(agentworkflows.DefaultModelPolicy)
	if err != nil {
		t.Fatalf("Parse(DefaultModelPolicy) error = %v", err)
	}
	if policy.Deadline != 30*time.Second {
		t.Errorf("Deadline = %v; want 30s", policy.Deadline)
	}
	want := map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}
	if !maps.Equal(policy.Providers, want) {
		t.Errorf("Providers = %v; want %v", policy.Providers, want)
	}
}
