package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
)

func runConsult(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("consult")
	task, role := flags.String("task", "", ""), flags.String("role", "", "")
	stage, adviser := flags.String("stage", "", ""), flags.String("adviser", "", "")
	request, checkpoint := flags.String("request", "", ""), flags.String("checkpoint", "", "")
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if !validConsultArguments(p, *task, *role, *stage, *adviser, *request) {
		return usagef("usage: kitsu consult --task <task> --role <role> --stage <stage> --adviser <name> --request <file|-> [--checkpoint <id>] [--json]")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	b, err := advisers.LoadBinding(s.repo.Root, *task)
	if err != nil {
		return failf("%v", err)
	}
	settings, err := advisers.Load(s.repo.Root)
	if err != nil {
		return failf("%v", err)
	}
	d, ok := settings.Advisers[*adviser]
	identity, bound := b.Advisers[*adviser]
	if !ok || !bound || identity.Model != d.Model || identity.Fingerprint != advisers.Fingerprint(d) {
		return failf("adviser %q is missing or differs from the task binding; configure it and explicitly bind --refresh", *adviser)
	}
	if !advisers.Allowed(b.Access, *role, *adviser) {
		return failf("%s may not consult adviser %s", *role, *adviser)
	}
	r, err := adviserRequest(cfg, *request)
	if err != nil {
		return usagef("%v", err)
	}
	content, _, err := s.adviserContent(ctx, *task, false)
	if err != nil {
		return failf("%v", err)
	}
	prompt, err := advisers.Render(s.repo.Root, r)
	if err != nil {
		return failf("%v", err)
	}
	prompt = fmt.Sprintf("Task: %s\nRequester: %s\nStage: %s\n\n%s", *task, *role, *stage, prompt)
	if len(prompt) > advisers.MaxInput {
		return usagef("the rendered consultation exceeds 1 MiB")
	}
	c := advisers.Consultation{Task: *task, BindingID: b.ID, Role: *role, Stage: *stage, Adviser: *adviser,
		Identity: identity, Checkpoint: *checkpoint, Content: content, Request: r, Prompt: prompt}
	return s.executeConsultation(ctx, c, d, *asJSON)
}

func validConsultArguments(p parsed, task, role, stage, adviser, request string) bool {
	return len(p.pos) == 0 && !p.dash && taskName.MatchString(task) && advisers.ValidRole(role) &&
		stage != "" && adviser != "" && request != ""
}

func (s *session) executeConsultation(ctx context.Context, c advisers.Consultation, d advisers.Definition, asJSON bool) error {
	before, _, err := s.adviserContent(ctx, c.Task, false)
	if err != nil {
		return failf("verify consultation before execution: %v", err)
	}
	if before != c.Content {
		return failf("the consulted content changed before execution; rerun consult with current context")
	}
	tmp, err := os.MkdirTemp("", "kitsu-adviser-answer-")
	if err != nil {
		return failf("prepare adviser answer file: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	saved, err := advisers.Begin(s.repo.Root, c)
	if err != nil {
		return failf("record consultation before execution: %v", err)
	}
	saved.Execution = advisers.Execute(ctx, s.repo.Root, d, saved.Prompt, filepath.Join(tmp, "answer.txt"))
	if saved.Execution.Outcome == "success" {
		after, _, err := s.adviserContent(ctx, c.Task, false)
		if err != nil || after != c.Content {
			saved.Execution.Outcome = "failed"
			saved.Execution.Detail = "the consulted content changed or could not be verified after execution"
		}
	}
	if err := advisers.Finish(s.repo.Root, *saved); err != nil {
		return failf("consultation %s ran but its result could not be saved: %v", saved.ID, err)
	}
	if asJSON {
		if err := printJSON(s.cfg, saved); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(s.cfg.Out, saved.Execution.Answer)
		fmt.Fprintf(s.cfg.Out, "Consultation %s: %s (%s)\n", saved.ID, saved.Execution.Outcome, time.Since(saved.Execution.StartedAt).Round(time.Millisecond))
	}
	if saved.Execution.Outcome != "success" {
		return failf("consultation %s: %s", saved.ID, saved.Execution.Detail)
	}
	return nil
}
