package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// syntheticTaskContext matches a task context that holds only an adviser
// binding, as the consult tests write one.
var syntheticTaskContext = regexp.MustCompile(`\{"advisers":(\{[^{}]*\})\}`)

// backedTaskContext starts a task in store bound to advisers and returns its
// before context as cw task context prints it.
func backedTaskContext(t *testing.T, store string, advisers map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("verified source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var plan taskflow.Plan
	if err := json.Unmarshal([]byte(bindingSmallPlan), &plan); err != nil {
		t.Fatal(err)
	}
	record, err := taskflow.Env{}.StartWithPacks(t.Context(), repo, store, plan, advisers, taskflow.SelectionExplicit, nil)
	if err != nil {
		t.Fatalf("start a task bound to %v: %v", advisers, err)
	}
	data, err := json.Marshal(record.Contexts["before"])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// backTaskContexts replaces each task context in state that holds only an
// adviser binding with the before context of a new task in store bound to
// those advisers, so a task record backs it. The rest of state, lone
// surrogate escapes included, is kept byte for byte.
func backTaskContexts(t *testing.T, store, state string) string {
	t.Helper()
	return syntheticTaskContext.ReplaceAllStringFunc(state, func(match string) string {
		var context struct {
			Advisers map[string]string `json:"advisers"`
		}
		if err := json.Unmarshal([]byte(match), &context); err != nil {
			t.Fatalf("task context %s: %v", match, err)
		}
		return backedTaskContext(t, store, context.Advisers)
	})
}

// backedConsultEnvironment returns state with its task contexts backed by
// records in a new store, and the environment of values plus CW_TASK_STORE
// naming that store.
func backedConsultEnvironment(t *testing.T, state string, values map[string]string) (string, func(string) string) {
	t.Helper()
	store := adviserBindingPrivateDir(t)
	environment := map[string]string{"CW_TASK_STORE": store}
	for name, value := range values {
		environment[name] = value
	}
	return backTaskContexts(t, store, state), fixtureEnvironment(environment)
}

// editContext returns context, a JSON object, after edit changes its decoded
// members.
func editContext(t *testing.T, context string, edit func(map[string]any)) string {
	t.Helper()
	var members map[string]any
	if err := json.Unmarshal([]byte(context), &members); err != nil {
		t.Fatal(err)
	}
	edit(members)
	data, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// oracle: cw consult asks a task's advisers only when the task store the hook
// reads ($CW_TASK_STORE here) holds the request's task context: the context's
// task names a record there that is not abandoned, and that record holds the
// same context (task, phase, snapshot, evidence and adviser binding), the
// check cw task consult applies when it attaches the record. Any other
// task-bound request is invalid input: exit 1, no record written and no
// adviser previewed; a context whose record is not in the store names the
// CW_TASK_STORE fix for a --store task. The
// OpenAI key is set and nothing opts in to OpenAI, so a context that names
// OpenAI is the only way the dry run would preview it.
func TestTaskBoundConsultNeedsTheTaskRecordToHoldItsContext(t *testing.T) {
	store, otherStore := adviserBindingPrivateDir(t), adviserBindingPrivateDir(t)
	unbound := backedTaskContext(t, store, map[string]string{})
	bound := backedTaskContext(t, store, map[string]string{"openai_decisions": "gpt-6-luna"})
	abandoned := backedTaskContext(t, store, map[string]string{"openai_decisions": "gpt-6-luna"})
	var abandonedTask struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(abandoned), &abandonedTask); err != nil {
		t.Fatal(err)
	}
	if _, err := (taskflow.Env{}).Abandon(filepath.Join(store, abandonedTask.TaskID), "superseded"); err != nil {
		t.Fatal(err)
	}
	const synthetic = `{"advisers":{"openai_decisions":"gpt-6-luna"}}`
	// says lists the fragments stderr holds, in order, separated by "…".
	refused := []struct{ name, store, state, says string }{
		{"context without a task", store, `{"task_context":` + synthetic + `}`, "names no task that cw task start created"},
		{"binding the record does not hold", store, `{"task_context":` + editContext(t, unbound, func(c map[string]any) {
			c["advisers"] = map[string]string{"openai_decisions": "gpt-6-luna"}
		}) + `}`, "differs from its task record"},
		{"snapshot the record does not hold", store, `{"task_context":` + editContext(t, bound, func(c map[string]any) {
			c["snapshot"].(map[string]any)["fingerprint"] = strings.Repeat("0", 64)
		}) + `}`, "differs from its task record"},
		{"phase the record has not prepared", store, `{"task_context":` + editContext(t, bound, func(c map[string]any) {
			c["phase"] = "final"
		}) + `}`, `has no prepared "final" context`},
		{"member the record lacks", store, `{"task_context":` + editContext(t, bound, func(c map[string]any) {
			c["note"] = "plain"
		}) + `}`, "differs from its task record"},
		{"member with a lone surrogate the record lacks", store, `{"task_context":` + strings.TrimSuffix(bound, "}") + `,"note":"\ud800"}}`, "is not the JSON that 'cw task context' prints"},
		{"task id that is a path", store, `{"task_context":` + editContext(t, bound, func(c map[string]any) {
			c["task_id"] = "../" + filepath.Base(store)
		}) + `}`, "names no task that cw task start created"},
		{"task in another store", otherStore, `{"task_context":` + bound + `}`,
			"no task record in the task store " + otherStore + " backs task context …; a task started with --store DIR needs CW_TASK_STORE=DIR"},
		{"abandoned task", store, `{"task_context":` + abandoned + `}`, "task " + abandonedTask.TaskID + " was abandoned; start a new task"},
		{"batch with one context without a task", store, `{"task_contexts":[` + bound + `,` + synthetic + `]}`, "names no task that cw task start created"},
	}
	consultIn := func(t *testing.T, store, state string) (consultOutcome, string) {
		t.Helper()
		directory := t.TempDir()
		policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, nil)
		request := writeFixtureFile(t, directory, "request.json", fixtureRequest(state))
		output := filepath.Join(directory, "preview.json")
		environment := fixtureCredentials("subq_snap", "openai_decisions")
		environment["CW_TASK_STORE"] = store
		return executeConsult(t, fixtureEnvironment(environment), "--request", request, "--output", output, "--policy", policy, "--dry-run", "--config-dir", adviserBindingPrivateDir(t)), output
	}

	t.Run("control: the record holds the context", func(t *testing.T) {
		outcome, output := consultIn(t, store, `{"task_context":`+bound+`}`)
		adviserBindingRequireStatuses(t, adviserBindingPreview(t, outcome, output), map[string]string{"openai_decisions": "dry_run"})
	})
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			outcome, output := consultIn(t, c.store, c.state)
			if ExitCode(outcome.err) != 1 {
				t.Fatalf("consult = exit %d, want 1: %s", ExitCode(outcome.err), outcome.text())
			}
			_, rest, said := strings.Cut(outcome.stderr, "Decision consultation validation failed")
			for fragment := range strings.SplitSeq(c.says, "…") {
				if !said {
					break
				}
				_, rest, said = strings.Cut(rest, fragment)
			}
			if !said {
				t.Fatalf("stderr %q does not say %q", outcome.stderr, c.says)
			}
			requireNotWritten(t, output)
		})
	}
}
