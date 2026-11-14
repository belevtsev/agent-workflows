package taskflow

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
)

// Profile is the flow a task runs under, fixed at cw task start like its
// packs: the stages it may assign, the author rounds each capped stage
// allows, the fix rounds allowed per finding or requirement, and the cost
// budget. The built-in profiles are docs/operations.md "Profiles"; their
// budgets are provisional until the evaluation runs measure them.
type Profile struct {
	Name   string   `json:"name"`
	Stages []string `json:"stages"`
	// Rounds caps the author rounds of a stage; a stage it does not name has
	// no cap.
	Rounds    map[string]int `json:"rounds"`
	FixRounds int            `json:"fix_rounds"`
	Budget    Budget         `json:"budget"`
}

// Budget is a task's cost limit in US dollars. At StopAt of MaxUSD the task
// stops taking new work. It holds no token cap: tokens count cache reads,
// which bill at a fraction of their price, so a token cap trips on cached
// prose, not on spend.
type Budget struct {
	MaxUSD float64 `json:"max_usd"`
	StopAt float64 `json:"stop_at,omitzero"`
}

// DefaultProfile is the profile of a plan that names none.
const DefaultProfile = "lite"

// defaultStopAt is the share of the budget at which a task stops taking new
// work.
const defaultStopAt = 0.8

// Stage names, in the order a flow runs them.
const (
	StageSpec           = "spec"
	StageSpecReview     = "spec-review"
	StageDesign         = "design"
	StageDesignReview   = "design-review"
	StageTests          = "tests"
	StageImplementation = "implementation"
	StageTestReview     = "test-review"
	StageCodeReview     = "code-review"
	StageAdversarial    = "adversarial"
)

// stageOrder lists every stage a profile may hold, in flow order.
var stageOrder = []string{StageSpec, StageSpecReview, StageDesign, StageDesignReview, StageTests, StageImplementation, StageTestReview, StageCodeReview, StageAdversarial}

// builtInProfiles is the table in docs/operations.md "Profiles". Each
// profile adds stages to the one before it.
var builtInProfiles = map[string]Profile{
	"lite": {
		Name:      "lite",
		Stages:    []string{StageTests, StageImplementation, StageTestReview, StageCodeReview},
		Rounds:    map[string]int{StageTests: 2},
		FixRounds: 2,
		Budget:    Budget{MaxUSD: 15, StopAt: defaultStopAt},
	},
	"standard": {
		Name:      "standard",
		Stages:    []string{StageDesign, StageDesignReview, StageTests, StageImplementation, StageTestReview, StageCodeReview},
		Rounds:    map[string]int{StageDesign: 2, StageTests: 2},
		FixRounds: 2,
		Budget:    Budget{MaxUSD: 40, StopAt: defaultStopAt},
	},
	"strict": {
		Name:      "strict",
		Stages:    []string{StageSpec, StageSpecReview, StageDesign, StageDesignReview, StageTests, StageImplementation, StageTestReview, StageCodeReview, StageAdversarial},
		Rounds:    map[string]int{StageSpec: 2, StageDesign: 2, StageTests: 2},
		FixRounds: 2,
		Budget:    Budget{MaxUSD: 100, StopAt: defaultStopAt},
	},
}

// BuiltInProfile returns a copy of the built-in profile name, or false when
// there is none.
func BuiltInProfile(name string) (Profile, bool) {
	profile, ok := builtInProfiles[name]
	if !ok {
		return Profile{}, false
	}
	profile.Stages = slices.Clone(profile.Stages)
	profile.Rounds = maps.Clone(profile.Rounds)
	return profile, true
}

// planProfileName is the profile a plan selects: its profile member, or
// DefaultProfile when it names none.
func planProfileName(plan Plan) string {
	if plan.Profile == "" {
		return DefaultProfile
	}
	return plan.Profile
}

// validatePlanProfile checks the plan's profile and budget members as stored:
// a built-in profile name, and a budget whose values are in range. Whether a
// budget lowers the profile's is resolveProfile's question, asked of new
// plans only, so that a later change to the built-in budgets keeps recorded
// tasks loadable.
func validatePlanProfile(plan Plan) error {
	if plan.Profile != "" {
		if _, ok := builtInProfiles[plan.Profile]; !ok {
			return fmt.Errorf("plan profile %q is not lite, standard or strict", plan.Profile)
		}
	}
	if plan.Budget != nil {
		if err := validateBudget(*plan.Budget, plan.Budget.StopAt == 0); err != nil {
			return fmt.Errorf("plan budget: %w", err)
		}
	}
	return nil
}

// validateBudget checks that max_usd is a positive amount and stop_at a share
// in (0, 1]; stop_at may be 0 only when optional.
func validateBudget(budget Budget, stopAtOptional bool) error {
	if math.IsNaN(budget.MaxUSD) || math.IsInf(budget.MaxUSD, 0) || budget.MaxUSD <= 0 {
		return errors.New("max_usd must be a positive amount")
	}
	if budget.StopAt == 0 && stopAtOptional {
		return nil
	}
	if math.IsNaN(budget.StopAt) || budget.StopAt <= 0 || budget.StopAt > 1 {
		return errors.New("stop_at must be above 0 and at most 1")
	}
	return nil
}

// resolveProfile returns the profile a new task records for plan: the
// built-in profile it names, or lite, with the plan's budget applied. A plan
// budget may only lower the profile's values; one above them is refused.
func resolveProfile(plan Plan) (*Profile, error) {
	if err := validatePlanProfile(plan); err != nil {
		return nil, err
	}
	profile, _ := BuiltInProfile(planProfileName(plan))
	if override := plan.Budget; override != nil {
		if override.MaxUSD > profile.Budget.MaxUSD {
			return nil, fmt.Errorf("plan budget max_usd %g exceeds profile %s's %g; a plan budget may only lower the profile's", override.MaxUSD, profile.Name, profile.Budget.MaxUSD)
		}
		if override.StopAt > profile.Budget.StopAt {
			return nil, fmt.Errorf("plan budget stop_at %g exceeds profile %s's %g; a plan budget may only lower the profile's", override.StopAt, profile.Name, profile.Budget.StopAt)
		}
		profile.Budget.MaxUSD = override.MaxUSD
		if override.StopAt != 0 {
			profile.Budget.StopAt = override.StopAt
		}
	}
	return &profile, nil
}

// validateStoredProfile checks a recorded profile against its plan. A record
// started before profiles existed has none, and its plan names neither a
// profile nor a budget. start writes the resolved name into the plan, so a
// recorded profile's name must equal the plan's profile member, never today's
// DefaultProfile. A recorded profile is judged by its shape and by the plan,
// not against today's built-in values, which may change between releases
// while the task keeps the ones it started with.
func validateStoredProfile(record Record) error {
	profile := record.Profile
	if profile == nil {
		if record.Plan.Profile != "" || record.Plan.Budget != nil {
			return errors.New("invalid stored profile: the plan names a profile or budget but the record has no profile")
		}
		return nil
	}
	if profile.Name != record.Plan.Profile {
		return errors.New("invalid stored profile: its name differs from the plan's profile")
	}
	if len(profile.Stages) == 0 || len(profile.Stages) > len(stageOrder) {
		return errors.New("invalid stored profile stages")
	}
	seen := map[string]bool{}
	for _, stage := range profile.Stages {
		if !slices.Contains(stageOrder, stage) || seen[stage] {
			return errors.New("invalid stored profile stages")
		}
		seen[stage] = true
	}
	for stage, n := range profile.Rounds {
		if !seen[stage] || n < 1 {
			return errors.New("invalid stored profile round caps")
		}
	}
	if profile.FixRounds < 1 {
		return errors.New("invalid stored profile fix rounds")
	}
	if err := validateBudget(profile.Budget, false); err != nil {
		return fmt.Errorf("invalid stored profile budget: %w", err)
	}
	if override := record.Plan.Budget; override != nil && (override.MaxUSD != profile.Budget.MaxUSD || override.StopAt != 0 && override.StopAt != profile.Budget.StopAt) {
		return errors.New("invalid stored profile budget: it differs from the plan's budget")
	}
	return nil
}
