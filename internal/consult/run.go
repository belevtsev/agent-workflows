// Package consult collects independent adviser judgments for a prepared Choice
// request, then validates the coordinator's evidence-based decision. It never
// decides which judgment is correct, and it records nothing about credentials.
package consult

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/belevtsev/agent-workflows/internal/workflow"
)

// Options selects one consultation or offline adjudication.
type Options struct {
	Request, Adjudicate, Decision, Output, Policy string
	DryRun                                        bool
	// UseOpenAI is nil unless --with-openai or --without-openai was supplied.
	UseOpenAI *bool
	Getenv    func(string) string
	Stdout    io.Writer
	Stderr    io.Writer
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
	if (o.Adjudicate != "") != (o.Decision != "") || o.Adjudicate != "" && (o.DryRun || o.UseOpenAI != nil) {
		return errors.New("--decision requires --adjudicate; --dry-run and OpenAI flags are only for --request")
	}
	return nil
}

// policy is the validated adviser portion of a model policy.
type policy struct {
	model        string
	deadline     time.Duration
	providers    map[string]string
	optional     map[string]string
	hasProviders bool
}

func modelMap(value any) map[string]string {
	models := map[string]string{}
	configured, _ := value.(workflow.Object)
	for name, model := range configured {
		models[name], _ = model.(string)
	}
	return models
}

func loadPolicy(path string) (policy, error) {
	value, err := workflow.ValidateModelPolicy(path)
	if err != nil {
		return policy{}, err
	}
	consultation := value["consultation"].(workflow.Object)
	_, hasProviders := consultation["providers"]
	return policy{
		model:        consultation["model"].(string),
		deadline:     time.Duration(consultation["deadline_seconds"].(int)) * time.Second,
		providers:    modelMap(consultation["providers"]),
		optional:     modelMap(consultation["optional_providers"]),
		hasProviders: hasProviders,
	}, nil
}

// prepareAll prepares every declared adviser in the record's fixed order.
func (p policy) prepareAll(shared *object) (*object, error) {
	prepared := newObject()
	for _, name := range providerOrder {
		model, required := p.providers[name]
		if !required {
			model, required = p.optional[name]
		}
		if !required {
			continue
		}
		value, err := prepareProvider(name, shared, model)
		if err != nil {
			return nil, err
		}
		prepared.set(name, value)
	}
	return prepared, nil
}

func (p policy) requiredProviders() []string {
	var names []string
	for _, name := range providerOrder {
		if _, required := p.providers[name]; required {
			names = append(names, name)
		}
	}
	return names
}

// plan is one validated request, ready to preview or send.
type plan struct {
	policy   policy
	version  int
	shared   *object
	prepared *object
	batch    batch
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
	if !selected.hasProviders {
		return nil, errors.New("policy does not configure adviser consultation")
	}
	_, requiredOpenAI := selected.providers[openAIProvider]
	if requiredOpenAI && o.UseOpenAI != nil && !*o.UseOpenAI {
		return nil, errors.New("required OpenAI adviser cannot be disabled")
	}
	if o.UseOpenAI != nil && !requiredOpenAI && len(selected.optional) == 0 {
		return nil, errors.New("policy does not configure the OpenAI adviser")
	}
	version := 1
	if requiredOpenAI {
		version = 3
	} else if len(selected.optional) > 0 {
		version = 2
	}
	value, err := readJSON(o.Request)
	if err != nil {
		return nil, err
	}
	shared, err := sharedRequest(value)
	if err != nil {
		return nil, err
	}
	prepared, err := selected.prepareAll(shared)
	if err != nil {
		return nil, err
	}
	credentials := map[string]string{}
	for _, name := range prepared.names {
		credentials[name] = o.Getenv(credentialNames[name])
	}
	disabledProviders := map[string]string{}
	if prepared.has(openAIProvider) && !requiredOpenAI {
		if o.UseOpenAI != nil && !*o.UseOpenAI {
			disabledProviders[openAIProvider] = "excluded"
		} else if o.UseOpenAI == nil && credentials[openAIProvider] == "" {
			disabledProviders[openAIProvider] = "missing_credential"
		}
	}
	deadline := selected.deadline
	if o.deadline > 0 {
		deadline = o.deadline
	}
	return &plan{policy: selected, version: version, shared: shared, prepared: prepared, batch: batch{
		credentials: credentials, deadline: deadline, disabled: disabledProviders, required: selected.requiredProviders(),
	}}, nil
}

func (p *plan) preview() (*object, error) {
	previews := newObject()
	for _, name := range p.prepared.names {
		previews.set(name, newObject("requested_model", p.prepared.get(name).(*object).get("model")))
	}
	if p.prepared.has(openAIProvider) {
		preview := previews.get(openAIProvider).(*object)
		preview.set("status", "dry_run")
		if reason, excluded := p.batch.disabled[openAIProvider]; excluded {
			preview.set("status", "disabled")
			preview.set("reason", reason)
		}
	}
	digest, err := fingerprint(p.shared)
	if err != nil {
		return nil, err
	}
	return newObject("version", number(p.version), "status", "dry_run", "request", p.shared, "request_sha256", digest,
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

func writeRecord(output *os.File, record *object) error {
	data, err := recordFormat.marshal(record)
	if err != nil {
		return err
	}
	_, err = output.Write(append(data, '\n'))
	return err
}

// Run executes one consultation or adjudication. A validation failure or an
// incomplete result is a nonzero status with its output already reported; the
// error is only for cancellation or a failed write of the reserved record.
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
		return 1, nil
	}
	defer output.Close()
	if request != nil {
		if record, err = o.execute(ctx, request); err != nil {
			return 1, err
		}
	}
	if err := writeRecord(output, record); err != nil {
		return 1, err
	}
	if o.Adjudicate != "" {
		status := record.get("final_decision").(*object).get("status").(string)
		fmt.Fprintln(o.Stdout, "Coordinator decision "+status)
		if status != "resolved" {
			return 2, nil
		}
		return 0, nil
	}
	status := record.get("status").(string)
	fmt.Fprintln(o.Stdout, "Decision consultation "+status+"; Coordinator decision pending")
	if status != "success" && status != "dry_run" {
		return 2, nil
	}
	return 0, nil
}

func (o Options) execute(ctx context.Context, request *plan) (*object, error) {
	if o.DryRun {
		return request.preview()
	}
	collect := o.collect
	if collect == nil {
		collect = newTransport().collect
	}
	raw, err := collect(ctx, request.prepared, request.batch)
	if err != nil {
		return nil, err
	}
	return makeRecord(request.shared, request.prepared, raw, request.version)
}

// LegacyOptions selects the Jev-only helper retained for existing integrations.
type LegacyOptions struct {
	Request, Output, Policy string
	DryRun                  bool
	Getenv                  func(string) string
	Stdout, Stderr          io.Writer
	transport               *transport
	deadline                time.Duration
}

// RunLegacy consults Jev alone, using the policy's consultation.model field. Its
// result is not three-provider coverage. Exit statuses match Run.
func RunLegacy(ctx context.Context, o LegacyOptions) (int, error) {
	var prepared *object
	selected, err := loadPolicy(o.Policy)
	if err == nil {
		var value any
		if value, err = readJSON(o.Request); err == nil {
			prepared, err = prepareRequest(value, selected.model)
		}
	}
	var output *os.File
	if err == nil {
		output, err = reserve(o.Output)
	}
	if err != nil {
		fmt.Fprintln(o.Stderr, "Consultation validation failed: "+err.Error())
		return 1, nil
	}
	defer output.Close()
	var record *object
	status := 0
	if o.DryRun {
		record = newObject("status", "dry_run", "request", prepared, "requested_model", prepared.get("model"), "duration_seconds", 0.0)
	} else {
		sender := newTransport()
		if o.transport != nil {
			sender = *o.transport
		}
		deadline := selected.deadline
		if o.deadline > 0 {
			deadline = o.deadline
		}
		bounded, cancel := context.WithTimeout(ctx, deadline)
		defer cancel()
		record, status = sender.consultPinned(bounded, jevProvider, prepared, o.Getenv(credentialNames[jevProvider]))
		if err := ctx.Err(); err != nil {
			return 1, err
		}
	}
	if err := writeRecord(output, record); err != nil {
		return 1, err
	}
	fmt.Fprintln(o.Stdout, "Consultation "+record.get("status").(string))
	return status, nil
}
