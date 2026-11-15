// Package roles holds the seven roles of the workflow: what each does, the
// model it runs on, what it may write, and its text.
package roles

import (
	"errors"
	"fmt"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

// Hosts are the agent hosts a role has a model on.
var Hosts = []string{"claude", "codex"}

// Model is the model and the effort a role runs with on one host.
type Model struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// Role is one of the seven roles.
type Role struct {
	ID     string
	Job    string // one line
	Writes string // what it may change, for a reader
	Models map[string]Model
	// Tools are the tools its Claude Code file allows. None of the roles
	// has the tool that starts another role.
	Tools []string
}

var (
	substantive  = map[string]Model{"claude": {"opus", "high"}, "codex": {"gpt-6.1-sol", "high"}}
	testWriting  = map[string]Model{"claude": {"sonnet", "high"}, "codex": {"gpt-6.1-sol", "high"}}
	implementing = map[string]Model{"claude": {"sonnet", "high"}, "codex": {"gpt-6.1-terra", "high"}}
	// deciding is for the design and for its review: a review never runs at
	// a lower effort than the work it reviews.
	deciding = map[string]Model{"claude": {"opus", "max"}, "codex": {"gpt-6.1-sol", "max"}}
	writing  = []string{"Read", "Grep", "Glob", "Edit", "Write"}
	building = []string{"Read", "Grep", "Glob", "Edit", "Write", "Bash"}
)

// defaults lists the roles in the order a task meets them.
var defaults = []Role{
	{
		ID: "lookup", Job: "Finds files, facts and existing specifications, and reports where they are.",
		Writes: "nothing",
		Models: map[string]Model{"claude": {"haiku", "medium"}, "codex": {"gpt-6-luna", "medium"}},
		Tools:  []string{"Read", "Grep", "Glob", "WebFetch", "WebSearch"},
	},
	{
		ID: "spec-writer", Job: "Writes the requirements and their scenarios from the request and its sources.",
		Writes: "proposal.md and specs/ of the task's change folder",
		Models: substantive, Tools: writing,
	},
	{
		ID: "architect", Job: "Designs the building blocks, their dependencies and the tasks.",
		Writes: "design.md and tasks.md of the task's change folder",
		Models: deciding, Tools: writing,
	},
	{
		ID: "quality-engineer", Job: "Writes the tests of the scenarios before the code exists.",
		Writes: "test paths, and tests.md of the task's change folder",
		Models: testWriting, Tools: building,
	},
	{
		ID: "developer", Job: "Writes the code that makes the tests pass, within the design.",
		Writes: "outside kitsu.toml, kitsu-roles.toml, kitsu-advisers.toml and openspec/; tests: add only",
		Models: implementing, Tools: building,
	},
	{
		ID: "reviewer", Job: "Reviews a specification, a design or code that another role wrote, and reports findings.",
		Writes: "nothing",
		Models: deciding, Tools: []string{"Read", "Grep", "Glob", "Bash"},
	},
	{
		ID: "git", Job: "Runs the checks and makes the commit through the gate.",
		Writes: "nothing",
		Models: map[string]Model{"claude": {"haiku", "low"}, "codex": {"gpt-6-luna", "low"}},
		Tools:  []string{"Read", "Grep", "Glob", "Bash"},
	},
}

// IDs lists the ids of the seven roles.
func IDs() []string {
	ids := make([]string, len(defaults))
	for i, r := range defaults {
		ids[i] = r.ID
	}
	return ids
}

// Known says whether id names a role.
func Known(id string) bool {
	for _, r := range defaults {
		if r.ID == id {
			return true
		}
	}
	return false
}

// All lists the roles with the models the configuration gives them. A nil
// configuration gives the defaults.
func All(cfg *Config) []Role {
	out := make([]Role, len(defaults))
	for i, r := range defaults {
		models := map[string]Model{}
		for _, host := range Hosts {
			m := r.Models[host]
			if cfg != nil {
				set := cfg.Models[host][r.ID]
				if set.Model != "" {
					m.Model = set.Model
				}
				if set.Effort != "" {
					m.Effort = set.Effort
				}
			}
			models[host] = m
		}
		r.Models = models
		out[i] = r
	}
	return out
}

// Reasons a limit cannot be checked.
var (
	ErrNeedsTests = errors.New(FileName + " must name the test paths under [paths], as tests = [...]: the limits of `developer` and `quality-engineer` depend on them")
	ErrNeedsTask  = errors.New("this role writes into the change folder of a task; name the task with --task")
)

// Violation is a path a role changed outside its limit.
type Violation struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Why  string `json:"why"`
}

// ChangeFolder is the folder of a task, with a final slash.
func ChangeFolder(task string) string { return "openspec/changes/" + task + "/" }

// Check compares what changed with what the role may change. tests are the
// test path patterns of kitsu-roles.toml; task may be empty.
func Check(role string, changes []gitx.Change, tests []string, task string) ([]Violation, error) {
	allowed, err := limit(role, tests, task)
	if err != nil {
		return nil, err
	}
	return check(changes, allowed), nil
}

func check(changes []gitx.Change, allowed func(gitx.Change) string) []Violation {
	var out []Violation
	for _, change := range changes {
		if why := allowed(change); why != "" {
			out = append(out, Violation{Path: change.Path, Kind: change.Kind, Why: why})
		}
	}
	return out
}

// CheckFinalization checks only writes to the current specifications of
// capabilities present in the task's starting tree. Entries must come from
// that tree, so a role cannot expand its scope by adding a capability.
func CheckFinalization(changes []gitx.Change, task string, entries []string) ([]Violation, error) {
	allowed := map[string]bool{}
	prefix := ChangeFolder(task) + "specs/"
	for _, entry := range entries {
		rest, found := strings.CutPrefix(entry, prefix)
		capability, file, _ := strings.Cut(rest, "/")
		if found && capability != "" && file == "spec.md" {
			allowed["openspec/specs/"+capability+"/spec.md"] = true
		}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%s has no capability specification in the starting tree", prefix)
	}
	return check(changes, func(c gitx.Change) string {
		if allowed[c.Path] {
			return ""
		}
		return "`spec-writer` finalization may change only openspec/specs/<capability>/spec.md for capabilities of the task in the starting tree"
	}), nil
}

// limit gives the rule of one role: a function that returns why a change is
// outside the limit, or nothing.
func limit(role string, tests []string, task string) (func(gitx.Change) string, error) {
	folder := ChangeFolder(task)
	within := func(ok bool, what string) string {
		if ok {
			return ""
		}
		return fmt.Sprintf("`%s` may change only %s", role, what)
	}
	switch role {
	case "lookup", "reviewer", "git":
		return func(gitx.Change) string { return fmt.Sprintf("`%s` may change nothing", role) }, nil
	case "spec-writer":
		if task == "" {
			return nil, ErrNeedsTask
		}
		return func(c gitx.Change) string {
			return within(c.Path == folder+"proposal.md" || strings.HasPrefix(c.Path, folder+"specs/"), folder+"proposal.md and "+folder+"specs/")
		}, nil
	case "architect":
		if task == "" {
			return nil, ErrNeedsTask
		}
		return func(c gitx.Change) string {
			return within(c.Path == folder+"design.md" || c.Path == folder+"tasks.md", folder+"design.md and "+folder+"tasks.md")
		}, nil
	case "quality-engineer":
		if tests == nil {
			return nil, ErrNeedsTests
		}
		return func(c gitx.Change) string {
			record := task != "" && c.Path == folder+"tests.md"
			return within(record || decl.MatchAny(tests, c.Path), "the test paths and tests.md of the task's change folder")
		}, nil
	case "developer":
		if tests == nil {
			return nil, ErrNeedsTests
		}
		return func(c gitx.Change) string { return developerLimit(c, tests) }, nil
	}
	return nil, fmt.Errorf("unknown role %q; the roles are %s", role, strings.Join(IDs(), ", "))
}

func developerLimit(c gitx.Change, tests []string) string {
	switch {
	case c.Path == decl.FileName, c.Path == FileName, c.Path == "kitsu-advisers.toml", strings.HasPrefix(c.Path, "openspec/"):
		return "`developer` may not change kitsu.toml, kitsu-roles.toml, kitsu-advisers.toml or anything under openspec/"
	case decl.MatchAny(tests, c.Path) && c.Kind != "added":
		return "`developer` may add a test and may not change or remove one"
	}
	return ""
}
