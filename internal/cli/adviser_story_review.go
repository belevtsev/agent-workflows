package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

type storyApproval struct {
	Version int    `json:"version"`
	Adviser string `json:"adviser,omitempty"`
	Policy  string `json:"policy,omitempty"`
}

func storyApprovalFile(repo *gitx.Repo) (uxSnapshot, error) {
	return localTrustFile(repo.GitDir, ".story-review.json")
}

func storyDefinition(repo *gitx.Repo, name, checkPolicy string) (advisers.Definition, string, error) {
	settings, err := advisers.Load(repo.Root)
	if err != nil {
		return advisers.Definition{}, "", err
	}
	d := settings.Advisers[name]
	if d.Provider != "typesafe" || d.Model != advisers.StoryModel || !advisers.Allowed(settings.Access, roles.Coordinator, name) {
		return d, "", failf("select a TypeSafe %s adviser allowed for the coordinator", advisers.StoryModel)
	}
	pin := contextpack.Key([]any{1, name, advisers.Fingerprint(d), checkPolicy, allowedAdviserList(settings.Access, roles.Coordinator)})
	return d, pin, nil
}

func runStoryReviewSetting(enable bool) func(context.Context, Config, []string) error {
	return func(ctx context.Context, cfg Config, args []string) error {
		f := newFlags("advisers story-review")
		var name string
		if enable {
			f.StringVar(&name, "adviser", "", "")
		}
		p, err := parseArgs(f, args)
		if err != nil {
			return err
		}
		if len(p.pos) != 0 || p.dash || enable && name == "" {
			return usagef("use advisers story-review enable --adviser NAME or advisers story-review disable")
		}
		if !onboardingTerminal(cfg) {
			return usagef("change automatic story review in your own terminal")
		}
		s, err := open(ctx, cfg, true)
		if err != nil {
			return err
		}
		before, err := storyApprovalFile(s.repo)
		if err != nil {
			return err
		}
		approval := storyApproval{Version: 1, Adviser: name}
		if enable {
			approval.Policy, err = confirmStoryReview(ctx, s, name)
			if err != nil {
				return err
			}
		}
		data, _ := json.Marshal(approval)
		if err := uxSaveFile(ctx, before, before.path+".lock", nil, data); err != nil {
			return err
		}
		if enable {
			fmt.Fprintln(cfg.Out, "Story review enabled for new feature tasks in this checkout.")
		} else {
			fmt.Fprintln(cfg.Out, "Story review disabled for this checkout.")
		}
		return nil
	}
}

func confirmStoryReview(ctx context.Context, s *session, name string) (string, error) {
	_, checkPolicy, err := workingCheckPolicy(ctx, s.repo)
	if err != nil {
		return "", err
	}
	d, pin, err := storyDefinition(s.repo, name, checkPolicy)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(s.cfg.Out, "Review new feature stories in %q with %s (%s at %s).\n", s.repo.Root, name, d.Model, d.BaseURL)
	fmt.Fprintln(s.cfg.Out, "Each intake sends the saved request, goal, scope and acceptance text once. Type yes to enable.")
	if err := readCheckApproval(ctx, s.cfg.In); err != nil {
		return "", err
	}
	return pin, nil
}

// prepareStoryIntake is called only for a newly accepted feature intake.
func prepareStoryIntake(ctx context.Context, s *session, t *harness.Task, candidate harness.Snapshot) *advisers.NativeCall {
	saved, err := storyApprovalFile(s.repo)
	if err != nil || !saved.exists {
		return nil
	}
	var approval storyApproval
	if json.Unmarshal(saved.data, &approval) != nil || approval.Version != 1 || approval.Adviser == "" {
		return nil
	}
	review := &harness.StoryReview{Version: 1, Revision: t.Feature.Revision(), Adviser: approval.Adviser,
		Status: "unavailable", Detail: "adviser call did not complete"}
	t.StoryReview = review
	d, pin, err := storyDefinition(s.repo, approval.Adviser, t.RepositoryPolicy)
	if err != nil || pin != approval.Policy || checkHarnessPolicy(ctx, s, t, candidate.Tree) != nil {
		review.Detail = "approval changed; re-enable story review for future feature tasks"
		return nil
	}
	review.Model = d.Model
	story := advisers.StoryInput{Request: t.Request, Goal: t.Feature.Goal, Scope: t.Feature.Scope}
	for _, c := range t.Feature.Criteria {
		story.Criteria = append(story.Criteria, advisers.StoryCriterion{ID: c.ID, Expected: c.Expected})
	}
	call, err := advisers.PrepareStoryReview(d, story)
	if err != nil {
		review.Detail = err.Error()
	}
	return call
}

func completeStoryIntake(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o harnessOptions, call *advisers.NativeCall) error {
	if err := l.write(harnessFile(task.ID), task); err != nil {
		return err
	}
	// The saved intake consumes its one attempt even if the process stops here.
	l.close()
	var result advisers.Execution
	if s.cfg.NativeExecute == nil {
		result = call.Execute(ctx, nil)
	} else {
		result = s.cfg.NativeExecute(ctx, call)
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerWait)
	defer cancel()
	reopened, err := openStageLedgerWait(saveCtx, s, ledgerWait)
	if err != nil {
		return err
	}
	defer reopened.close()
	var saved harness.Task
	if err := reopened.read(harnessFile(task.ID), &saved); err != nil {
		return err
	}
	if !saved.Valid(task.ID) || saved.StoryReview == nil || saved.StoryReview.Revision != task.StoryReview.Revision {
		return failf("the intake changed during story review; resume with task next %s", task.ID)
	}
	review := saved.StoryReview
	review.Detail = result.Detail
	if result.Usage != nil {
		review.InputTokens, review.OutputTokens = &result.Usage.InputTokens, &result.Usage.OutputTokens
	}
	if result.Outcome == "success" && result.ActualModel != review.Model {
		review.Detail = "adviser reported a different model from the approved model"
	} else if result.Outcome == "success" {
		review.Status = "available"
		review.Answers = make(map[string]harness.StoryAnswer, len(result.Decisions))
		for id, answer := range result.Decisions {
			review.Answers[id] = harness.StoryAnswer{Choice: answer.Choice, Confidence: answer.Confidence}
		}
	}
	review.ActualModel, review.Duration = result.ActualModel, result.EndedAt.Sub(result.StartedAt)
	view, err := observeSavedHarness(saveCtx, s, reopened, &saved, o.json && !o.brief)
	if err != nil {
		return err
	}
	return printHarnessResult(s.cfg, "record", o, view)
}

type briefStoryReview struct {
	Status   string            `json:"status"`
	Revision string            `json:"feature_revision"`
	Gaps     map[string]string `json:"gaps,omitempty"`
	Detail   string            `json:"detail,omitempty"`
}

func (v harnessView) addBriefStoryReview(reply map[string]any) {
	review := v.StoryReview
	if review == nil || v.Feature == nil || review.Revision != v.Feature.Revision() || v.Last == nil || v.Last.Action != "intake" {
		return
	}
	summary := briefStoryReview{Status: review.Status, Revision: review.Revision}
	switch review.Status {
	case "available":
		summary.Gaps = make(map[string]string)
		for dimension, answer := range review.Answers {
			if answer.Choice == "gap" || answer.Choice == "uncertain" {
				summary.Gaps[dimension] = answer.Choice
			}
		}
	case "unavailable":
		summary.Detail = review.Detail
	}
	reply["story_review"] = summary
}
