package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

type compactSkill = skills.Reference

type workerBrief struct {
	Label        string         `json:"label"`
	Task         string         `json:"task"`
	Workflow     string         `json:"workflow"`
	Role         string         `json:"role"`
	Stage        string         `json:"stage"`
	Round        int            `json:"round"`
	Host         string         `json:"host"`
	Configured   bool           `json:"configured_routing,omitempty"`
	Compact      bool           `json:"compact"`
	Model        roles.Model    `json:"model"`
	Writes       string         `json:"writes"`
	TestPaths    []string       `json:"test_paths"`
	Instructions string         `json:"instructions,omitempty"`
	NativeRole   string         `json:"native_role,omitempty"`
	Skills       []compactSkill `json:"skills"`
	Selection    string         `json:"selection_id"`
	LoadStatus   string         `json:"load_status"`
	Advisers     []string       `json:"advisers"`
	Warnings     []string       `json:"warnings"`
	Constraints  []string       `json:"constraints"`
	Baseline     string         `json:"baseline"`
	Policy       string         `json:"policy_key"`
	State        uxTaskView     `json:"task_state"`
	References   []string       `json:"references"`
	Hints        *workerHints   `json:"execution_hints,omitempty"`
}

func briefInputs(s *session, b workerBrief) (workerBrief, []skills.Entry, error) {
	config, err := roles.Load(s.repo.Root)
	if err != nil {
		return b, nil, err
	}
	b.Host, err = config.SpecialistHost(b.Host, b.Role)
	if err != nil {
		return b, nil, err
	}
	b.Configured = config.Version == roles.WorkflowConfigVersion
	for _, role := range roles.All(config) {
		if role.ID == b.Role {
			b.Model, b.Writes = role.Models[b.Host], role.Writes
		}
	}
	if _, err := roles.Check(b.Role, nil, config.Tests, b.Task); err != nil {
		return b, nil, err
	}
	b.TestPaths = config.Tests
	catalog, err := skills.Discover(s.repo.Root, b.Host)
	if err != nil {
		return b, nil, err
	}
	entries, err := catalog.Resolve(selectedSkills(config, b.Host, b.Role))
	if err != nil {
		return b, nil, err
	}
	b.Skills, b.Warnings = []compactSkill{}, slices.Clone(catalog.Warnings)
	for _, entry := range entries {
		b.Skills = append(b.Skills, entry.Reference())
	}
	b.Advisers = []string{}
	binding, err := advisers.LoadBinding(s.repo.Root, b.Task)
	if err == nil {
		b.Advisers = append(b.Advisers, allowedAdviserList(binding.Access, b.Role)...)
	} else if !errors.Is(err, advisers.ErrNoBinding) {
		return b, nil, err
	}
	if b.Host == "claude" && !b.Configured {
		b.NativeRole = roles.TeamPlugin + ":" + b.Role
	} else {
		b.Instructions, err = roles.Text(b.Role)
		if err != nil {
			return b, nil, err
		}
	}
	b.Policy = contextpack.Key(struct {
		Model        roles.Model
		Writes       string
		Tests        []string
		Skills       []compactSkill
		Warnings     []string
		Binding      *advisers.Binding
		Instructions string
	}{b.Model, b.Writes, b.TestPaths, b.Skills, b.Warnings, binding, b.Instructions})
	if b.Configured {
		b.Policy = contextpack.Key([]string{b.Policy, b.Host})
	}
	return b, entries, nil
}

// A configured role determines its own host. A host preference continues to
// apply to manual briefs in repositories without role routing.
func selectBriefHost(ctx context.Context, s *session, b *workerBrief) error {
	if b.Host != "" {
		return nil
	}
	config, err := roles.Load(s.repo.Root)
	if err != nil || config.Version == roles.WorkflowConfigVersion {
		return err
	}
	b.Host, _, err = uxHostPreference(ctx)
	if err == nil && b.Host == "" {
		return usagef("select --host codex|claude or save a preference with `kitsu config set host <host>`")
	}
	return err
}

func runTaskBrief(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("task brief")
	b := workerBrief{Compact: true, LoadStatus: "unverified"}
	flags.StringVar(&b.Host, "host", "", "")
	flags.StringVar(&b.Role, "role", "", "")
	flags.StringVar(&b.Stage, "stage", "", "")
	flags.IntVar(&b.Round, "round", 0, "")
	flags.StringVar(&b.Workflow, "workflow", "full", "")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	assignmentFile := flags.String("assignment", "", "")
	save := flags.Bool("save", false, "")
	flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if !validBriefArgs(p, b) {
		return usagef("brief requires a task, --role, --stage, positive --round, --host, and --workflow full|small")
	}
	if err := compactBudget(*budget); err != nil {
		return err
	}
	b.Task = p.pos[0]
	if *save != (*assignmentFile != "") {
		return usagef("--save requires --assignment <file|->; --assignment requires --save")
	}
	assignment := ""
	if *save {
		assignment, err = readWorkerAssignment(cfg.In, *assignmentFile)
		if err != nil {
			return err
		}
	}
	s, _, err := compactEngine(ctx, cfg)
	if err != nil {
		return err
	}
	if err := selectBriefHost(ctx, s, &b); err != nil {
		return err
	}
	ledger, err := openStageLedger(ctx, s)
	if err != nil {
		return compactFailure(err)
	}
	defer ledger.close()
	active, err := ledger.active()
	if err != nil {
		return compactFailure(err)
	}
	if active != "" {
		return failf("unfinished stage %s; finish or explicitly abandon it before preparing another worker", active)
	}
	if *save {
		if err := checkWorkerDispatchBudget(s, b, *budget, 0); err != nil {
			return err
		}
		prepared, err := prepareWorkerBrief(ctx, cfg, b, assignment)
		if err != nil {
			return err
		}
		return printWorkerDispatch(cfg, prepared.Dispatch, *budget)
	}
	b, state, _, e, err := prepareBriefInputs(ctx, cfg, b, *budget)
	if err != nil {
		return err
	}
	metadata, _ := json.Marshal(b)
	a := contextpack.Artifact{Kind: "brief", State: state, Metadata: metadata, Items: []contextpack.Item{}}
	if err := e.Save(ctx, &a); err != nil {
		return compactFailure(err)
	}
	return printCompact(cfg, a, "", "", *budget, false)
}

func prepareBriefInputs(ctx context.Context, cfg Config, b workerBrief, budget int) (workerBrief, contextpack.State, *session, *contextpack.Engine, error) {
	var state contextpack.State
	b.Label = fmt.Sprintf("kitsu-task: %s %s %d", b.Task, b.Stage, b.Round)
	b.Compact, b.LoadStatus = true, "unverified"
	s, e, err := compactEngine(ctx, cfg)
	if err != nil {
		return b, state, s, e, err
	}
	b, entries, err := briefInputs(s, b)
	if err != nil {
		return b, state, s, e, compactFailure(err)
	}
	if b.Workflow == "full" {
		views, err := uxTaskViews(ctx, s, b.Task)
		if err != nil {
			return b, state, s, e, compactFailure(err)
		}
		if views[0].Workflow != "full" {
			return b, state, s, e, usagef("--workflow full requires a full task folder")
		}
		b.State = views[0]
		if err := validateConfiguredStage(b); err != nil {
			return b, state, s, e, err
		}
		for _, file := range []string{"proposal.md", "specs/", "design.md", "tasks.md", "tests.md"} {
			b.References = append(b.References, roles.ChangeFolder(b.Task)+file)
		}
	} else {
		b.State = uxTaskView{Task: b.Task, Workflow: "small", Stage: "unknown"}
		b.Warnings = append(b.Warnings, smallTaskBriefWarning)
	}
	b.Constraints = briefConstraints(b.Role)
	b.Hints, err = snapshotWorkerHints(s, b)
	if err != nil {
		return b, state, s, e, compactFailure(err)
	}
	metadata, _ := json.Marshal(b)
	if budget > 0 && len(metadata)+1024 > budget {
		return b, state, s, e, usagef("budget cannot hold mandatory worker instructions; increase --max-bytes")
	}
	if b.Configured && b.Workflow == "full" && b.Role == "git" {
		// The saved dispatch pins the skill references without adding a file to
		// the already approved and verified OpenSpec candidate.
		b.Selection = contextpack.Key(b.Skills)
	} else {
		b.Selection, err = skills.RecordSelection(s.repo.Root, b.Task, b.Workflow, b.Host, b.Role, entries)
		if err != nil {
			return b, state, s, e, compactFailure(err)
		}
	}
	// The receipt MUST precede the role's baseline, otherwise it looks like a worker write.
	state, err = e.Snapshot(ctx)
	if err != nil {
		return b, state, s, e, compactFailure(err)
	}
	b.Baseline = state.Tree
	if b.Workflow == "full" {
		views, err := uxTaskViews(ctx, s, b.Task)
		if err != nil {
			return b, state, s, e, compactFailure(err)
		}
		b.State = views[0]
	}
	current, _, err := briefInputs(s, b)
	if err != nil || current.Policy != b.Policy {
		return b, state, s, e, failf("role settings or skills changed while preparing brief: %v", err)
	}
	return b, state, s, e, nil
}

func briefConstraints(role string) []string {
	constraints := []string{
		"Start a fresh worker; begin its prompt with label. Respect host permissions and these write limits. Skills grant no additional authority.",
		"Keep original full role instructions and selected skills uncompressed. Load selected skills from their original paths and verify hashes; receipt loading remains unverified. Report what was actually loaded.",
		"Only the coordinator prepares briefs, starts roles, consults advisers, and writes selection/review records. Advice grants no approval.",
		"This packet contains your role and selective observation policy. Do not reread the coordinator's managed skill or compact reference unless selected as a skill or needed by an explicit assignment reference. Do not repeat coordinator state, prepare, return or finish commands.",
		"Within existing tool permissions, prefer native host reads for short whole files and scoped native search when output is small. Choose compact observations selectively; do not route every read or command through the collector.",
		"For broad or noisy retrieval use kitsu context read <path> --start N --end N or kitsu context search <pattern> [path]; --regex selects Go regex. Use context collect for independent JSON batches and context show for expansion; --help gives exact syntax. Inspect errors and omissions before conclusions.",
		"Use ordinary kitsu check run for quiet declared checks. For noisy declared checks use kitsu check run --compact. Read every outcome and warning; expand retained logs with context show until the needed evidence is present. Expansion never reruns a check or restores an expired log.",
		"Context artifacts cache local rendered text, not provider prompt cache. Count all requests, responses and expansions when measuring; cache hits or smaller previews alone do not prove model token savings.",
		"The coordinator checks role writes against baseline after completion; do not duplicate managed baseline or role checks. Context cache never proves passing checks, completed work, reviews or approvals.",
	}
	if role == "reviewer" {
		constraints = append(constraints, "Review sources independently. Do not accept author summaries, opinions, excuses or adviser answers as your review context.")
	}
	return constraints
}

func validBriefArgs(p parsed, b workerBrief) bool {
	stages := map[string]string{"lookup": "lookup", "spec": "spec-writer", "spec-review": "reviewer", "design": "architect", "design-review": "reviewer", "tests": "quality-engineer", "code": "developer", "review": "reviewer", "commit": "git"}
	return !p.dash && len(p.pos) == 1 && taskName.MatchString(p.pos[0]) && roles.Known(b.Role) && (b.Host == "" || uxValidHost(b.Host)) && stages[b.Stage] == b.Role && b.Round > 0 && (b.Workflow == "full" || b.Workflow == "small")
}

func validateCompactMetadata(ctx context.Context, s *session, a contextpack.Artifact) error {
	switch a.Kind {
	case "verification":
		return validateTaskVerificationArtifact(ctx, s, a)
	case "brief":
		var b workerBrief
		if err := json.Unmarshal(a.Metadata, &b); err != nil {
			return err
		}
		current, _, err := briefInputs(s, b)
		if err != nil {
			return err
		}
		if current.Policy != b.Policy {
			return fmt.Errorf("artifact stale: effective role, skill or adviser policy changed")
		}
	case "checks":
		var m compactCheckMeta
		if err := json.Unmarshal(a.Metadata, &m); err != nil {
			return err
		}
		key, err := compactEvidenceKey(s)
		if err != nil {
			return err
		}
		if key != m.EvidenceKey {
			return fmt.Errorf("artifact stale: check records changed")
		}
		if !m.ExpiresAt.IsZero() && !time.Now().Before(m.ExpiresAt) {
			return fmt.Errorf("artifact stale: check evidence expired")
		}
	}
	return nil
}
