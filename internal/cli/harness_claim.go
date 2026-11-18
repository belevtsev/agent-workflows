package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func claimHarness(root string, task *harness.Task, o harnessOptions, view harnessView) error {
	if !slices.Contains([]string{"intake", "plan", "review", "implement", "repair"}, view.Next) || view.Ticket == "" || o.ticket != view.Ticket || strings.TrimSpace(o.actor) == "" {
		return failf("claim requires the current action ticket and owner")
	}
	if task.Claim != nil {
		return failf("action already claimed by %s; recover that worker before starting another", task.Claim.Owner)
	}
	claim := &harness.Claim{Ticket: o.ticket, Owner: o.actor}
	if o.host != "" {
		if err := setSpecialist(root, task, claim, o.host, view); err != nil {
			return err
		}
	}
	if _, pause := task.Next(time.Now(), view.readiness()); pause != nil {
		return failf("reservation paused (%s); resume with task next %s", pause.Reason, task.ID)
	}
	if view.Next == "review" {
		claim.Candidate = &view.Candidate
		task.ReviewStarts = task.StartedReviews() + 1
	}
	task.Claim = claim
	return nil
}

func setSpecialist(root string, task *harness.Task, claim *harness.Claim, host string, view harnessView) error {
	var role string
	switch view.Next {
	case "review":
		role = "reviewer"
	case "implement", "repair":
		role = "developer"
	default:
		return usagef("--host applies to a review, implementation or repair reservation")
	}
	settings, err := roles.ResolveSpecialist(root, host, role)
	if err != nil {
		return failf("resolve %s: %v", role, err)
	}
	if role == "reviewer" {
		claim.Reviewer = settings
		return nil
	}
	stage := view.Next
	if stage == "implement" && task.Latest("implement") != nil {
		stage = "repair"
	}
	task.DeveloperStarts++
	label := fmt.Sprintf("kitsu-task: %s %s %d", task.ID, stage, task.DeveloperStarts)
	claim.Developer = &harness.DeveloperAssignment{
		SpecialistSettings: *settings, Scope: task.Scope, Start: view.Candidate,
		Owner: claim.Owner, Action: view.Next, LaunchLabel: label,
	}
	return nil
}

func linkHarnessWorker(task *harness.Task, o harnessOptions) error {
	claim := task.Claim
	if o.host != "" {
		return usagef("set --host when reserving the action before linking --worker")
	}
	if claim == nil || claim.Candidate == nil && claim.Developer == nil || claim.Ticket != o.ticket || claim.Owner != o.actor {
		return failf("linking a worker requires the saved reservation ticket and its owner")
	}
	if claim.Developer != nil {
		if o.worker == claim.Owner {
			return failf("delegation requires a developer worker distinct from the coordinator")
		}
		if claim.Worker != "" && claim.Worker != o.worker {
			return failf("delegated work belongs to another native worker; recover the linked session")
		}
	} else if err := checkHarnessActor(task, "review", o.worker); err != nil {
		return err
	}
	claim.Worker = o.worker
	return nil
}

func checkHarnessActor(task *harness.Task, action, actor string) error {
	claim := task.Claim
	if action == "review" {
		if claim == nil {
			return failf("claim the review before launching an independent reviewer")
		}
		if claim.Worker != "" && claim.Worker != actor {
			return failf("review belongs to another native worker; recover the linked session")
		}
		if claim.Owner == actor || task.UsedSession(actor) {
			return failf("review requires a fresh independent session")
		}
	} else if claim != nil && claim.Developer != nil {
		if claim.Worker == "" {
			return failf("developer launch is unresolved; recover and link the native worker before recording its result")
		}
		if claim.Worker != actor {
			return failf("delegated work belongs to another native worker; recover the linked session")
		}
	}
	return nil
}

// A reservation can report completed work after its deadline. The next
// observation still applies the deadline before dispatching further work.
func allowPausedReturn(task *harness.Task, view *harnessView) {
	pause, claim := view.Pause, task.Claim
	if pause == nil || claim == nil {
		return
	}
	if pause.Pending == "review" || pause.Reason == "deadline" && claim.Developer != nil && claim.Worker != "" && claim.Developer.Action == pause.Pending {
		view.Next = pause.Pending
		view.Ticket = actionTicket(task, view.Next, view.Candidate)
	}
}
