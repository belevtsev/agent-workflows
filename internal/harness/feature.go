package harness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// FeatureBrief records what the author intends to verify for a feature task.
type FeatureBrief struct {
	Version  int              `json:"version"`
	Goal     string           `json:"goal"`
	Scope    string           `json:"scope"`
	Draft    bool             `json:"draft,omitempty"`
	Criteria []Criterion      `json:"criteria"`
	Behavior *FeatureBehavior `json:"behavior,omitempty"`
}

type Criterion struct {
	ID       string `json:"id"`
	Expected string `json:"expected_behavior"`
	Check    string `json:"check"`
}

type FeatureBehavior struct {
	Expect string   `json:"expected_behavior"`
	Argv   []string `json:"argv"`
}

var criterionID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (b *FeatureBrief) Validate() error {
	if b.Version != 1 || strings.TrimSpace(b.Goal) == "" || strings.TrimSpace(b.Scope) == "" {
		return fmt.Errorf("feature brief needs version 1, a goal and scope")
	}
	if len(b.Criteria) > 32 || len(b.Criteria) == 0 && !b.Draft {
		return fmt.Errorf("an accepted feature brief needs 1–32 acceptance criteria")
	}
	seen := make(map[string]bool, len(b.Criteria))
	behavior := false
	for _, c := range b.Criteria {
		if !criterionID.MatchString(c.ID) || seen[c.ID] || strings.TrimSpace(c.Expected) == "" {
			return fmt.Errorf("criterion %q needs a unique ID and observable behavior", c.ID)
		}
		seen[c.ID] = true
		checkID, named := strings.CutPrefix(c.Check, "check:")
		switch {
		case c.Check == "behavior":
			behavior = true
		case named && criterionID.MatchString(checkID), b.Draft && c.Check == "":
		default:
			return fmt.Errorf("criterion %q needs behavior or check:<id>", c.ID)
		}
	}
	if b.Behavior == nil && (!behavior || b.Draft) {
		return nil
	}
	if !behavior || !validFeatureBehavior(b.Behavior) {
		return fmt.Errorf("supply an expected behavior and argv for the behavior criteria")
	}
	return nil
}

func validFeatureBehavior(b *FeatureBehavior) bool {
	if b == nil || strings.TrimSpace(b.Expect) == "" || len(b.Argv) == 0 || strings.TrimSpace(b.Argv[0]) == "" {
		return false
	}
	return !slices.ContainsFunc(b.Argv, func(arg string) bool { return strings.ContainsRune(arg, 0) })
}

func (b *FeatureBrief) Revision() string {
	if b == nil {
		return ""
	}
	data, _ := json.Marshal(b) // The brief contains only JSON primitives.
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// PrimaryCheck identifies the named check to run first when there is no
// separate behavioral assertion. The remaining checks still run normally.
func (b *FeatureBrief) PrimaryCheck() string {
	if b == nil || b.Behavior != nil || len(b.Criteria) == 0 {
		return ""
	}
	return strings.TrimPrefix(b.Criteria[0].Check, "check:")
}

func (c Criterion) Evidence(p *Proof) CheckRun {
	if c.Check == "behavior" {
		return CheckRun{ID: p.Run, Digest: p.LogDigest}
	}
	return p.GateRuns[strings.TrimPrefix(c.Check, "check:")]
}

func (b *FeatureBrief) Covers(p *Proof) bool {
	if b.Draft || b.Revision() != p.FeatureRevision {
		return false
	}
	return !slices.ContainsFunc(b.Criteria, func(c Criterion) bool { return c.Evidence(p).ID == "" })
}

func (t *Task) CurrentFeatureEvent(e *Event) bool {
	return e != nil && (t.Feature == nil || e.FeatureRevision == t.Feature.Revision())
}

func (t *Task) needsPlan() bool {
	return t.Scope.Entry == "plan" && t.Latest("plan") == nil ||
		t.Feature != nil && t.Feature.Draft && t.Scope.Until != "plan"
}

// StoryReview is advisory evidence for one feature intake revision.
type StoryReview struct {
	Version      int                    `json:"version"`
	Revision     string                 `json:"feature_revision"`
	Adviser      string                 `json:"adviser"`
	Model        string                 `json:"model,omitempty"`
	ActualModel  string                 `json:"actual_model,omitempty"`
	Status       string                 `json:"status"`
	Detail       string                 `json:"detail,omitempty"`
	Duration     time.Duration          `json:"duration_ns,omitempty"`
	Answers      map[string]StoryAnswer `json:"answers,omitempty"`
	InputTokens  *int64                 `json:"input_tokens,omitempty"`
	OutputTokens *int64                 `json:"output_tokens,omitempty"`
}

type StoryAnswer struct {
	Choice     string   `json:"choice"`
	Confidence *float64 `json:"confidence,omitempty"`
}
