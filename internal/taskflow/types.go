// Package taskflow records source-bound task evidence.
package taskflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Version is the task record version this cw writes; it reads versions
// MinVersion to Version. Once a release has written version N, a new member
// or a new value of a validated member (a change a cw reading N could not
// load, by decode or by validation) makes the next release write N+1
// (docs/development.md, "Task record format"). Every new revision is written
// with Version, so a record started by an older cw takes this version at its
// next revision.
const (
	Version    = 5
	MinVersion = 3
)

const (
	MaxInputBytes = 8 << 20
	MaxLogBytes   = 128 << 10
)

// TestRulesV1 is the test rule set cw task start records (R1).
const TestRulesV1 = "go.test-evidence/v1"

var identifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// Plan is supplied explicitly by the coordinator. Repository instructions are
// never parsed into executable commands.
type Plan struct {
	Title         string        `json:"title"`
	Kind          string        `json:"kind"`
	CoordinatorID string        `json:"coordinator_id,omitempty"`
	Requirements  []Requirement `json:"requirements"`
	Checks        []Check       `json:"checks"`
	// Checkpoints is nil for records written before the field existed and for
	// plans that omit it; both mean adviser checkpoints are required.
	Checkpoints *CheckpointPolicy `json:"checkpoints,omitempty"`
}

// CheckpointPolicy lets a substantial plan declare, with a reason, that the
// before and final adviser checkpoints are not required.
type CheckpointPolicy struct {
	Required bool   `json:"required"`
	Reason   string `json:"reason,omitempty"`
}

// UnmarshalJSON requires an explicit required member: an object that omits it
// would otherwise decode as not required and silently skip the checkpoints.
func (p *CheckpointPolicy) UnmarshalJSON(data []byte) error {
	var raw struct {
		Required *bool  `json:"required"`
		Reason   string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(data, &raw, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if raw.Required == nil {
		return errors.New("plan checkpoints needs an explicit required member")
	}
	*p = CheckpointPolicy{Required: *raw.Required, Reason: raw.Reason}
	return nil
}

// checkpointsRequired reports whether the plan requires adviser checkpoints.
func checkpointsRequired(plan Plan) bool {
	return plan.Checkpoints == nil || plan.Checkpoints.Required
}

type Requirement struct {
	ID                string   `json:"id"`
	Description       string   `json:"description"`
	CheckIDs          []string `json:"check_ids"`
	AllowNonExecution bool     `json:"allow_non_execution"`
}

type Check struct {
	ID             string   `json:"id"`
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	// EnvPassthrough names variables of cw's environment that the check
	// receives although checkEnvironment would drop them, such as
	// DB_PASSWORD. It holds names only, never values
	// (validateEnvPassthrough).
	EnvPassthrough []string `json:"env_passthrough,omitempty"`
}

// SourceRef hashes the complete file; the line range identifies the relevant
// excerpt. Both the file hash and the existence of every line are checked.
type SourceRef struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	SHA256    string `json:"sha256"`
}

type Assessment struct {
	ID                 string      `json:"id"`
	Status             string      `json:"status"`
	Evidence           []SourceRef `json:"evidence"`
	CheckIDs           []string    `json:"check_ids"`
	NonExecutionReason string      `json:"non_execution_reason"`
}

type Decision struct {
	Status             string      `json:"status"`
	Reason             string      `json:"reason"`
	Evidence           []SourceRef `json:"evidence"`
	CheckIDs           []string    `json:"check_ids"`
	NonExecutionReason string      `json:"non_execution_reason"`
}

type AssessmentInput struct {
	Requirements []Assessment `json:"requirements"`
	Decision     *Decision    `json:"decision,omitempty"`
}

type BoundAssessment struct {
	Assessment Assessment          `json:"assessment"`
	Snapshot   projectctx.Snapshot `json:"snapshot"`
	CheckRuns  []string            `json:"check_runs"`
}

type BoundDecision struct {
	Decision  Decision            `json:"decision"`
	Snapshot  projectctx.Snapshot `json:"snapshot"`
	CheckRuns []string            `json:"check_runs"`
}

// Context is the exact object placed in request.state.task_context, or as one
// member of request.state.task_contexts for a shared multi-project batch.
// Advisers is the task's immutable adviser binding (provider -> pinned model,
// possibly empty), so cw consult asks exactly the advisers bound at start.
type Context struct {
	TaskID         string              `json:"task_id"`
	Phase          string              `json:"phase"`
	Snapshot       projectctx.Snapshot `json:"snapshot"`
	EvidenceSHA256 string              `json:"evidence_sha256"`
	Advisers       map[string]string   `json:"advisers"`
}

// sameContext compares every emitted context field, including the binding.
func sameContext(a, b Context) bool {
	return a.TaskID == b.TaskID && a.Phase == b.Phase && a.Snapshot == b.Snapshot && a.EvidenceSHA256 == b.EvidenceSHA256 && maps.Equal(a.Advisers, b.Advisers)
}

type CheckRun struct {
	ID           string               `json:"id"`
	CheckID      string               `json:"check_id"`
	Status       string               `json:"status"`
	Argv         []string             `json:"argv"`
	StartedAt    time.Time            `json:"started_at"`
	FinishedAt   *time.Time           `json:"finished_at,omitempty"`
	Before       projectctx.Snapshot  `json:"before"`
	After        *projectctx.Snapshot `json:"after,omitempty"`
	ExitCode     *int                 `json:"exit_code,omitempty"`
	Log          string               `json:"log"`
	LogSHA256    string               `json:"log_sha256"`
	OutputSHA256 string               `json:"output_sha256"`
	OutputBytes  int64                `json:"output_bytes"`
	Truncated    bool                 `json:"truncated"`
	Failure      string               `json:"failure,omitempty"`
	// GOFLAGS is the GOFLAGS that cw's environment gave the check, before
	// checkSpec pinned GoEnv's into it, with compiler and linker flag values
	// redacted (langpack.RedactGOFLAGS); runs recorded before it existed omit
	// it. The Go evidence rule reads GoEnv's instead; rerun also compares it
	// with a failed run recorded without GoEnv. Argv, unlike GOFLAGS, is
	// stored verbatim.
	GOFLAGS string `json:"goflags,omitempty"`
	// TestResults names the private file in the task directory holding the
	// per-test results read from the run's complete output; TestResultsSHA256
	// is the hash of its bytes. Both are empty when the output held no test
	// event.
	TestResults       string `json:"test_results,omitempty"`
	TestResultsSHA256 string `json:"test_results_sha256,omitempty"`
	// Tool is the verified go command that ran a check whose program is go
	// (checkSpec). It is absent for other programs, for a run refused with
	// failure untrusted_toolchain, and for runs recorded before cw verified go.
	Tool *langpack.GoTool `json:"tool,omitempty"`
	// GoEnv is what Tool reported with go env, under the check's environment
	// and in its directory, just before the check ran (checkSpec): the GOFLAGS
	// it applied, redacted, and its target and host platforms. The Go evidence
	// rule credits only a run that has it (evidenceRun). It is absent when Tool
	// is, and for runs recorded before cw read it (record version 4).
	GoEnv *langpack.GoEnv `json:"go_env,omitempty"`
}

type Attachment struct {
	Phase            string            `json:"phase"`
	SourcePath       string            `json:"source_path"`
	File             string            `json:"file"`
	SHA256           string            `json:"sha256"`
	Context          Context           `json:"context"`
	DecisionStatus   string            `json:"decision_status"`
	ProviderStatuses map[string]string `json:"provider_statuses"`
}

type Verification struct {
	TaskID          string              `json:"task_id"`
	Complete        bool                `json:"complete"`
	CurrentSnapshot projectctx.Snapshot `json:"current_snapshot"`
	// SourceSHA256 is SourceDigest of the verified source: the content identity
	// a publication gate compares, unaffected by later staging or commits.
	// Records verified before it existed omit it.
	SourceSHA256 string    `json:"source_sha256,omitempty"`
	Problems     []string  `json:"problems"`
	Limitations  []string  `json:"limitations"`
	CheckedAt    time.Time `json:"checked_at"`
	// ProblemFindings and LimitationFindings are Problems and Limitations
	// with their codes, in the same order. Verify sets them; the JSON output
	// and the record carry only the text, so a loaded record has none.
	ProblemFindings    []Finding `json:"-"`
	LimitationFindings []Finding `json:"-"`
}

type Record struct {
	Version          int       `json:"version"`
	ID               string    `json:"id"`
	Directory        string    `json:"directory"`
	Revision         int       `json:"revision"`
	EvidenceRevision int       `json:"evidence_revision"`
	PreviousSHA256   string    `json:"previous_sha256"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Plan             Plan      `json:"plan"`
	// Amendments lists, in order, the checks added after start (amend.go).
	Amendments []Amendment         `json:"amendments,omitempty"`
	Baseline   projectctx.Snapshot `json:"baseline"`
	// BaselineManifest lists every eligible file at start. BaselineManifest,
	// Packs and TestRules are required: Load refuses a record without one
	// (requireStartMembers), so code that reads a loaded record may rely on
	// them.
	BaselineManifest *ManifestRef `json:"baseline_manifest,omitempty"`
	// Packs is the pack set fixed at start.
	Packs *PackSet `json:"packs,omitempty"`
	// TestRules names the test rule set fixed at start: TestRulesV1.
	TestRules         string                     `json:"test_rules,omitempty"`
	Advisers          map[string]string          `json:"advisers"`
	Contexts          map[string]Context         `json:"contexts"`
	Checks            []CheckRun                 `json:"checks"`
	Assessments       map[string]BoundAssessment `json:"assessments"`
	Decision          *BoundDecision             `json:"decision,omitempty"`
	Consultations     map[string]Attachment      `json:"consultations"`
	Verification      *Verification              `json:"verification,omitempty"`
	Assignments       []WorkerAssignment         `json:"assignments,omitempty"`
	WorkerResults     []BoundWorkerResult        `json:"worker_results,omitempty"`
	WorkerAcceptances []BoundWorkerAcceptance    `json:"worker_acceptances,omitempty"`
	// Abandonment, once set, is the record's terminal revision (abandon.go).
	Abandonment *Abandonment `json:"abandonment,omitempty"`
}

// Decode rejects unknown members, duplicate members, trailing JSON and partial
// input. It is intentionally independent of installer codecs.
func Decode(reader io.Reader, value any) error {
	data, err := io.ReadAll(io.LimitReader(reader, MaxInputBytes+1))
	if err != nil {
		return err
	}
	if len(data) > MaxInputBytes {
		return errors.New("task JSON exceeds the 8 MiB limit")
	}
	if err := json.Unmarshal(data, value, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("invalid task JSON: %w", err)
	}
	return nil
}

func encode(value any) ([]byte, error)            { return json.Marshal(value, json.Deterministic(true)) }
func digest(data []byte) string                   { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
func nonblank(value string) bool                  { return strings.TrimSpace(value) != "" }
func phaseValid(phase string) bool                { return phase == "before" || phase == "final" }
func equalSnapshot(a, b projectctx.Snapshot) bool { return a == b && a.Fingerprint != "" }
func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func uniqueIDs(ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		if !identifier.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func ValidatePlan(plan *Plan) error {
	for i := range plan.Checks {
		if plan.Checks[i].TimeoutSeconds == 0 {
			plan.Checks[i].TimeoutSeconds = 300
		}
	}
	if err := validateRecordedPlan(*plan); err != nil {
		return err
	}
	// A substantial plan must be able to execute something: completion of a
	// changed substantial task needs a passed run (see executionProblems).
	// Applied only to new plans so that recorded tasks still load.
	if plan.Kind == "substantial" && !slices.ContainsFunc(plan.Requirements, func(r Requirement) bool { return len(r.CheckIDs) > 0 }) {
		return errors.New("a substantial plan needs at least one check bound to a requirement")
	}
	for _, check := range plan.Checks {
		if err := validateCommand(check.Argv); err != nil {
			return fmt.Errorf("check %s: %w", check.ID, err)
		}
		if err := validateEnvPassthrough(check.EnvPassthrough); err != nil {
			return fmt.Errorf("check %s: %w", check.ID, err)
		}
	}
	return nil
}

// validateRecordedPlan checks immutable declarations without applying defaults
// or asking whether today's manager would admit their commands for execution.
func validateRecordedPlan(plan Plan) error {
	if plan.CoordinatorID != "" && !validAgentID(plan.CoordinatorID) {
		return errors.New("coordinator_id must be a bounded nonblank identity declaration")
	}
	if !nonblank(plan.Title) || len(plan.Title) > 4096 || (plan.Kind != "small" && plan.Kind != "substantial") {
		return errors.New("plan needs a title and kind small or substantial")
	}
	if plan.Checkpoints != nil {
		if plan.Kind != "substantial" {
			return errors.New("plan checkpoints apply only to substantial tasks")
		}
		if (!plan.Checkpoints.Required || plan.Checkpoints.Reason != "") && !boundedText(plan.Checkpoints.Reason) {
			return errors.New("plan checkpoints reason must be bounded and nonblank, and required false needs one")
		}
	}
	if len(plan.Requirements) == 0 || len(plan.Requirements) > 256 || len(plan.Checks) > 256 {
		return errors.New("plan needs 1..256 requirements and at most 256 checks")
	}
	checks := map[string]bool{}
	for _, check := range plan.Checks {
		if !identifier.MatchString(check.ID) || checks[check.ID] {
			return errors.New("check IDs must be distinct identifiers")
		}
		checks[check.ID] = true
		if check.TimeoutSeconds < 1 || check.TimeoutSeconds > 3600 {
			return errors.New("check timeout must be 1..3600 seconds")
		}
		if err := validateCommandShape(check.Argv); err != nil {
			return fmt.Errorf("check %s: %w", check.ID, err)
		}
		if err := validateEnvPassthroughShape(check.EnvPassthrough); err != nil {
			return fmt.Errorf("check %s: %w", check.ID, err)
		}
	}
	seen := map[string]bool{}
	for _, requirement := range plan.Requirements {
		if !identifier.MatchString(requirement.ID) || seen[requirement.ID] || !nonblank(requirement.Description) || len(requirement.Description) > 16384 {
			return errors.New("requirements need distinct IDs and descriptions")
		}
		seen[requirement.ID] = true
		if !uniqueIDs(requirement.CheckIDs) {
			return errors.New("requirement check IDs must be distinct identifiers")
		}
		for _, id := range requirement.CheckIDs {
			if !checks[id] {
				return fmt.Errorf("requirement %s references unknown check %s", requirement.ID, id)
			}
		}
		if len(requirement.CheckIDs) == 0 && !requirement.AllowNonExecution {
			return fmt.Errorf("requirement %s needs a planned check or explicit non-execution permission", requirement.ID)
		}
	}
	return nil
}

func decodeBytes(data []byte, value any) error { return Decode(bytes.NewReader(data), value) }
