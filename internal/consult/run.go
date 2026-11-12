// Package consult collects independent adviser judgments for a prepared Choice
// request, then validates the coordinator's evidence-based decision. It never
// decides which judgment is correct, and it records nothing about credentials.
//
// Advisers participate by configuration: a task-bound request uses the adviser
// binding in state.task_context (or the identical bindings in
// state.task_contexts), once Options.TaskRecords confirms that task records
// back those contexts; any other request uses the policy's enabled providers.
// A participating adviser is attempted only when its credential is set and,
// for a request without a task binding, Options.Disabled does not list it;
// with none attempted, no network call is made.
package consult

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
)

// Options selects one consultation or offline adjudication.
type Options struct {
	Request, Adjudicate, Decision, Output string
	// Policy is the model policy a request runs under: a file, or the shipped
	// document embedded in cw. Adjudication reads none.
	Policy modelpolicy.Source
	DryRun bool
	// Getenv resolves the credential variables; nil reads none.
	Getenv func(string) string
	// Disabled, when set, loads the provider IDs the user withholds: those
	// the preferences disable and, unless the policy was chosen with --policy,
	// those the user has not opted in to. It is called only for a request
	// without a task binding, which treats each as having no credential: it
	// is recorded as not_configured and never contacted. Its error fails the
	// run before any output is written. A task-bound request keeps its
	// binding and never calls it.
	Disabled func() ([]string, error)
	// TaskRecords, when set, checks a task-bound request against the task
	// records: it receives each task context of the request's state, as
	// compact JSON in request order, and fails unless a task record backs
	// every one. It is called only for a task-bound request, before anything
	// is written or sent, and its error fails the run as invalid input.
	// Unset, the contexts' binding is taken as given.
	TaskRecords func(contexts [][]byte) error
	Stdout      io.Writer
	Stderr      io.Writer
	// Unset outside package tests, which keeps the endpoints and budget fixed.
	collect  collector
	deadline time.Duration
}

// Check reports flag combinations that no consultation can run with.
func (o Options) Check() error {
	if (o.Request == "") == (o.Adjudicate == "") {
		return errors.New("exactly one of --request or --adjudicate is required")
	}
	if o.Output == "" {
		return errors.New("--output is required")
	}
	if (o.Adjudicate != "") != (o.Decision != "") || o.Adjudicate != "" && o.DryRun {
		return errors.New("--decision requires --adjudicate; --dry-run is only for --request")
	}
	return nil
}

// plan is one validated request, ready to preview or send.
type plan struct {
	shared *object
	// prepared holds every participating adviser in fixed order.
	prepared *object
	batch    batch
	// bound is set when the participation set is a task's adviser binding.
	bound bool
}

func readJSON(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read valid JSON input")
	}
	value, err := parseJSON(data)
	if err != nil {
		return nil, errors.New("cannot read valid JSON input")
	}
	return value, nil
}

func (o Options) plan() (*plan, error) {
	selected, err := loadPolicy(o.Policy)
	if err != nil {
		return nil, err
	}
	value, err := readJSON(o.Request)
	if err != nil {
		return nil, err
	}
	shared, err := sharedRequest(value)
	if err != nil {
		return nil, err
	}
	participants, bound, err := taskBinding(shared.get("state"))
	if err != nil {
		return nil, err
	}
	if bound && o.TaskRecords != nil {
		contexts, err := taskContexts(shared.get("state"))
		if err != nil {
			return nil, err
		}
		if err := o.TaskRecords(contexts); err != nil {
			return nil, err
		}
	}
	var disabled []string
	if !bound {
		participants = selected.providers
		if o.Disabled != nil {
			if disabled, err = o.Disabled(); err != nil {
				return nil, err
			}
		}
	}
	prepared := newObject()
	credentials := map[string]string{}
	for _, name := range providerOrder {
		model, participating := participants[name]
		if !participating {
			continue
		}
		value, err := prepareProvider(name, shared, model)
		if err != nil {
			return nil, err
		}
		prepared.set(name, value)
		if slices.Contains(disabled, name) {
			credentials[name] = ""
			continue
		}
		credentials[name] = credential(o.Getenv, name)
	}
	deadline := selected.deadline
	if o.deadline > 0 {
		deadline = o.deadline
	}
	return &plan{shared: shared, prepared: prepared, bound: bound, batch: batch{credentials: credentials, deadline: deadline}}, nil
}

// unconfigured lists the participating advisers without a credential.
func (p *plan) unconfigured() []string {
	var names []string
	for _, name := range p.prepared.names {
		if p.batch.credentials[name] == "" {
			names = append(names, name)
		}
	}
	return names
}

func (p *plan) preview() (*object, error) {
	previews := newObject()
	for _, name := range p.prepared.names {
		status := "dry_run"
		if p.batch.credentials[name] == "" {
			status = "not_configured"
		}
		previews.set(name, newObject("requested_model", p.prepared.get(name).(*object).get("model"), "status", status))
	}
	digest, err := fingerprint(p.shared)
	if err != nil {
		return nil, err
	}
	return newObject("version", number(recordVersion), "status", "dry_run", "request", p.shared, "request_sha256", digest,
		"providers", previews, "final_decision", pendingDecision()), nil
}

// reserve claims a private output before network access; earlier evidence is
// never replaced.
func reserve(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.New("cannot create a new output file")
	}
	return file, nil
}

// writeRecord writes the record to the reserved output and closes it. A failed
// Close is a failed write: the data may not have reached the file.
func writeRecord(output io.WriteCloser, record *object) error {
	data, err := recordFormat.marshal(record)
	if err == nil {
		_, err = output.Write(append(data, '\n'))
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Outcomes of Run. The cw command maps each to its exit status.
const (
	// Complete is success, no_advisers_configured, dry_run or a resolved
	// decision.
	Complete = 0
	// Invalid is invalid input or an existing output path. It also
	// accompanies every error Run returns.
	Invalid = 1
	// Incomplete is partial or unavailable coverage, an unresolved decision, or
	// a task-bound request whose bound adviser is not configured. The record
	// is still written.
	Incomplete = 2
)

// Run executes one consultation or adjudication and returns its outcome:
// Complete, Invalid or Incomplete. The error is only for cancellation or a
// failed write of the reserved record.
func Run(ctx context.Context, o Options) (int, error) {
	var record *object
	var request *plan
	var err error
	if o.Adjudicate != "" {
		var consultation, decision any
		if consultation, err = readJSON(o.Adjudicate); err == nil {
			if decision, err = readJSON(o.Decision); err == nil {
				record, err = adjudicate(consultation, decision)
			}
		}
	} else {
		request, err = o.plan()
	}
	var output *os.File
	if err == nil {
		output, err = reserve(o.Output)
	}
	if err != nil {
		fmt.Fprintln(o.Stderr, "Decision consultation validation failed: "+err.Error())
		return Invalid, nil
	}
	if request != nil {
		if record, err = o.execute(ctx, request); err != nil {
			_ = output.Close() // the run failed; the reserved file stays empty
			return Invalid, err
		}
	}
	if err := writeRecord(output, record); err != nil {
		return Invalid, err
	}
	if o.Adjudicate != "" {
		status := record.get("final_decision").(*object).get("status").(string)
		fmt.Fprintln(o.Stdout, "Coordinator decision "+status)
		if status != "resolved" {
			return Incomplete, nil
		}
		return Complete, nil
	}
	status := record.get("status").(string)
	fmt.Fprintln(o.Stdout, "Decision consultation "+status+"; Coordinator decision pending")
	code := Complete
	if status != "success" && status != "no_advisers_configured" && status != "dry_run" {
		code = Incomplete
	}
	if request.bound {
		for _, name := range request.unconfigured() {
			fmt.Fprintf(o.Stderr, "Adviser %s is bound to this task but not configured; set %s and consult again.\n", name, credentialNames[name])
			code = Incomplete
		}
	}
	return code, nil
}

// execute previews the plan, or sends it once to each configured adviser. The
// collector is never called when no participating adviser is configured.
func (o Options) execute(ctx context.Context, request *plan) (*object, error) {
	if o.DryRun {
		return request.preview()
	}
	raw, err := gather(ctx, o.collect, request.prepared, request.batch)
	if err != nil {
		return nil, err
	}
	return makeRecord(request.shared, request.prepared, raw)
}

// gather passes only the configured advisers in prepared, with only their
// credentials, to one collection, and records every other participant as
// not_configured. It never calls collect when no adviser is configured. A nil
// collect uses the fixed transport.
func gather(ctx context.Context, collect collector, prepared *object, options batch) (map[string]any, error) {
	raw := map[string]any{}
	active := newObject()
	credentials := map[string]string{}
	for _, name := range prepared.names {
		value := prepared.get(name).(*object)
		if key := options.credentials[name]; key != "" {
			active.set(name, value)
			credentials[name] = key
		} else {
			raw[name] = notConfigured(value)
		}
	}
	if active.size() == 0 {
		return raw, nil
	}
	if collect == nil {
		collect = newTransport().collect
	}
	collected, err := collect(ctx, active, batch{credentials: credentials, deadline: options.deadline})
	if err != nil {
		return nil, err
	}
	for _, name := range active.names {
		raw[name] = collected[name]
	}
	return raw, nil
}
