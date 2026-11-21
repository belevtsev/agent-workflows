// Package advisers runs configured advisory providers and keeps their answers
// and the coordinator's decisions in portable task records.
package advisers

import (
	"errors"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/change"
)

const (
	FileName       = "kitsu-advisers.toml"
	DefaultTimeout = 5 * time.Minute
	MaxInput       = 1 << 20
	MaxAnswer      = 1 << 20
	MaxDiagnostic  = 64 << 10
)

var ErrNoBinding = errors.New("the task has no adviser binding")

// Definition is a provider selected by the user. Commands and environment
// values are never copied into the shared task binding.
type Definition struct {
	Provider  string        `json:"provider,omitempty"`
	BaseURL   string        `json:"base_url,omitempty"`
	APIKeyEnv string        `json:"api_key_env,omitempty"`
	Model     string        `json:"model"`
	Command   []string      `json:"command"`
	Timeout   time.Duration `json:"timeout_ns"`
	Source    string        `json:"source"`
}

type Config struct {
	Advisers    map[string]Definition `json:"advisers"`
	Access      map[string][]string   `json:"access"`
	Checkpoints []Checkpoint          `json:"checkpoints"`
}

type Checkpoint struct {
	ID       string   `json:"id" toml:"id"`
	Stage    string   `json:"stage" toml:"stage"`
	Role     string   `json:"role" toml:"role"`
	Advisers []string `json:"advisers" toml:"advisers"`
}

type Identity struct {
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model"`
	Fingerprint string `json:"fingerprint"`
}

type Binding struct {
	Version     int                 `json:"version"`
	ID          string              `json:"id"`
	Task        string              `json:"task"`
	Workflow    string              `json:"workflow"`
	CreatedAt   time.Time           `json:"created_at"`
	Advisers    map[string]Identity `json:"advisers"`
	Access      map[string][]string `json:"access"`
	Checkpoints []Checkpoint        `json:"checkpoints"`
}

type Choice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type Request struct {
	Version  int              `json:"version"`
	Kind     string           `json:"kind"`
	Question string           `json:"question"`
	Choices  []Choice         `json:"choices,omitempty"`
	Context  []string         `json:"context,omitempty"`
	Levels   []string         `json:"levels,omitempty"`
	Criteria *BooleanCriteria `json:"criteria,omitempty"`
}

type BooleanCriteria struct {
	True  string `json:"true,omitempty"`
	False string `json:"false,omitempty"`
}

// Probability stores identifiers as values, preserving arbitrary choice IDs.
type Probability struct {
	ID          string  `json:"id"`
	Probability float64 `json:"probability"`
}

type DecisionResult struct {
	Kind            string        `json:"kind"`
	Choice          string        `json:"choice,omitempty"`
	Score           *float64      `json:"score,omitempty"`
	ProbabilityTrue *float64      `json:"probability_true,omitempty"`
	Confidence      *float64      `json:"confidence,omitempty"`
	Probabilities   []Probability `json:"probabilities,omitempty"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Content uses the same artifact identities as independent reviews.
type Content = change.Content

type Execution struct {
	Decisions   map[string]*DecisionResult `json:"-"`
	Version     int                        `json:"version,omitempty"`
	Result      *DecisionResult            `json:"result,omitempty"`
	Usage       *Usage                     `json:"usage,omitempty"`
	Outcome     string                     `json:"outcome"`
	Answer      string                     `json:"answer"`
	Detail      string                     `json:"detail,omitempty"`
	ExitStatus  *int                       `json:"exit_status,omitempty"`
	StartedAt   time.Time                  `json:"started_at"`
	EndedAt     time.Time                  `json:"ended_at"`
	ActualModel string                     `json:"actual_model"`
	UsageKnown  bool                       `json:"usage_known"`
	Diagnostics string                     `json:"-"`
}

type Consultation struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Task       string    `json:"task"`
	BindingID  string    `json:"binding_id"`
	Role       string    `json:"role"`
	Stage      string    `json:"stage"`
	Adviser    string    `json:"adviser"`
	Identity   Identity  `json:"identity"`
	Checkpoint string    `json:"checkpoint,omitempty"`
	Content    Content   `json:"content"`
	Request    Request   `json:"request"`
	Prompt     string    `json:"prompt"`
	Execution  Execution `json:"execution"`
}

type Disposition struct {
	Version      int       `json:"version"`
	ID           string    `json:"id"`
	Consultation string    `json:"consultation"`
	Disposition  string    `json:"disposition"`
	Reason       string    `json:"reason"`
	Choice       string    `json:"choice,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Requirement struct {
	Checkpoint   string `json:"checkpoint"`
	Stage        string `json:"stage"`
	Role         string `json:"role"`
	Adviser      string `json:"adviser"`
	State        string `json:"state"`
	Consultation string `json:"consultation,omitempty"`
}

type Status struct {
	Task         string        `json:"task"`
	Workflow     string        `json:"workflow"`
	Ready        bool          `json:"ready"`
	Requirements []Requirement `json:"requirements"`
	Problems     []string      `json:"problems"`
}
