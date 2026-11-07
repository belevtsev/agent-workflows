package agentworkflows_test

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentworkflows "github.com/belevtsev/agent-workflows"
	"github.com/belevtsev/agent-workflows/internal/modelpolicy"
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

// RT-2: the carved-out runtime packages do not depend on internal/workflow.
func TestRuntimePackagesDoNotImportWorkflow(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	cmd := exec.Command(goTool, "list", "-deps",
		"./internal/taskflow", "./internal/consult", "./internal/projectctx",
		"./internal/credentials", "./internal/modelpolicy", "./internal/userconfig")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list -deps failed: %v\n%s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("go list -deps printed no packages")
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.Contains(line, "internal/workflow") {
			t.Errorf("dependency %q is internal/workflow", line)
		}
	}
}
