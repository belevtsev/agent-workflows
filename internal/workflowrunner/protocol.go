// Package workflowrunner connects configured Kitsu tasks to an operator-run
// service. The service owns isolation and signs receipts outside the job.
package workflowrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

const (
	Version    = 1
	MaxBundle  = 16 << 20
	MaxFiles   = 8192
	maxMessage = 48 << 20
)

// Check is a reviewed command. Inputs name its trusted executable files.
type Check struct {
	ID     string            `json:"id"`
	Argv   []string          `json:"argv"`
	Inputs map[string]string `json:"inputs,omitempty"`
	TestID string            `json:"test_id,omitempty"`
}

type Policy struct {
	Declaration string   `json:"declaration"`
	CodePaths   []string `json:"code_paths"`
	TestPaths   []string `json:"test_paths"`
	Checks      []Check  `json:"checks"`
}

type Capabilities struct {
	Platform   string   `json:"platform"`
	Profile    string   `json:"profile"`
	Policy     Policy   `json:"policy"`
	NativeTest []string `json:"native_tested"`
}

// File carries exact bytes for a regular path or an explicit deletion.
type File = gitx.WorkflowFile

// Job's binding includes the saved intake and exact candidate. Starting an
// attempt twice with the same ID retrieves its result or its current state.
type Skill struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type Job struct {
	Intake   string          `json:"intake"`
	Task     string          `json:"task"`
	Attempt  string          `json:"attempt"`
	Binding  string          `json:"binding"`
	Policy   string          `json:"policy"`
	Profile  string          `json:"profile"`
	Kind     string          `json:"kind"`
	Deadline time.Time       `json:"deadline"`
	Files    []File          `json:"files"`
	Checks   []string        `json:"checks,omitempty"`
	Host     string          `json:"host,omitempty"`
	Role     string          `json:"role,omitempty"`
	Model    string          `json:"model,omitempty"`
	Effort   string          `json:"effort,omitempty"`
	Skills   []Skill         `json:"skills,omitempty"`
	ReadOnly bool            `json:"read_only,omitempty"`
	Prompt   string          `json:"prompt,omitempty"`
	Schema   json.RawMessage `json:"schema,omitempty"`
}

// CheckResult is produced by the runner's approved reporter. Infrastructure
// errors have a different outcome from an assertion failure.
type CheckResult struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	TestID  string `json:"test_id,omitempty"`
	Exit    *int   `json:"exit,omitempty"`
	Log     []byte `json:"log,omitempty"`
}

type Receipt struct {
	Request      string           `json:"request"`
	Project      string           `json:"project"`
	Platform     string           `json:"platform"`
	Profile      string           `json:"profile"`
	Policy       string           `json:"policy"`
	Attempt      string           `json:"attempt,omitempty"`
	Binding      string           `json:"binding,omitempty"`
	Job          string           `json:"job,omitempty"`
	Status       string           `json:"status"`
	Stopped      bool             `json:"stopped,omitempty"`
	Release      bool             `json:"release_authorized,omitempty"`
	AgentType    string           `json:"agent_type,omitempty"`
	Usage        map[string]int64 `json:"usage,omitempty"`
	Session      string           `json:"session,omitempty"`
	Role         string           `json:"role,omitempty"`
	Host         string           `json:"host,omitempty"`
	Model        string           `json:"model,omitempty"`
	Effort       string           `json:"effort,omitempty"`
	Result       json.RawMessage  `json:"result,omitempty"`
	Changes      []File           `json:"changes,omitempty"`
	Checks       []CheckResult    `json:"checks,omitempty"`
	Capabilities *Capabilities    `json:"capabilities,omitempty"`
}

type Request struct {
	Version int    `json:"version"`
	Nonce   string `json:"nonce"`
	Project string `json:"project"`
	Action  string `json:"action"`
	Attempt string `json:"attempt,omitempty"`
	Binding string `json:"binding,omitempty"`
	Job     *Job   `json:"job,omitempty"`
}

type Signed struct {
	Receipt   []byte `json:"receipt"`
	Signature []byte `json:"signature"`
}

func Digest(v any) string {
	data, _ := json.Marshal(v)
	return BytesDigest(data)
}

func BytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
