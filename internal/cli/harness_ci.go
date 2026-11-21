package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/tickets"
)

func importFailedCheck(ctx context.Context, s *session, o harnessOptions) (harness.Snapshot, *harness.BugDelivery, string, error) {
	start, err := harnessSnapshot(ctx, s)
	if err != nil {
		return start, nil, "", err
	}
	if start.Head == "" || start.Tree != start.Index || start.MergeHeads != "" {
		return start, nil, "", failf("check out the PR head on a branch in a clean worktree before starting CI repair")
	}
	tree, _, err := s.repo.CommitContents(ctx, start.Head)
	if err != nil {
		return start, nil, "", err
	}
	if tree != start.Tree {
		return start, nil, "", failf("check out the PR head on a branch in a clean worktree before starting CI repair")
	}
	_, approval, err := approvedCICheck(ctx, s, tree, o.source.Check.LocalCheck)
	if err != nil {
		return start, nil, "", err
	}
	var input io.Reader
	if o.stdin {
		input = s.cfg.In
		if input == nil {
			return start, nil, "", usagef("--stdin requires pull request metadata")
		}
	} else {
		data, err := s.repo.PullRequestChecks(ctx, o.source.URL, tickets.MaxPullRequestBytes)
		if err != nil {
			return start, nil, "", err
		}
		input = bytes.NewReader(data)
	}
	source, request, err := tickets.ReadFailedCheck(o.source, input)
	if err != nil {
		return start, nil, "", err
	}
	if source.Check.Head != start.Head {
		return start, nil, "", failf("PR head is %s; check it out in a clean worktree before starting CI repair", source.Check.Head)
	}
	source.Check.ApprovalSHA256 = approval
	return start, &harness.BugDelivery{Source: source}, request, nil
}

func approvedCICheck(ctx context.Context, s *session, tree, id string) (decl.Check, string, error) {
	data, fingerprint, err := checkDeclaration(ctx, s.repo, tree)
	if err != nil {
		return decl.Check{}, "", err
	}
	if fingerprint != undeclaredPolicy {
		policy, err := decl.Parse(data)
		if err != nil {
			return decl.Check{}, "", err
		}
		if len(policy.Checks) > 0 {
			approval, err := trustFile(s.repo, "")
			if err != nil || string(approval.data) != fingerprint+"\n" {
				return decl.Check{}, "", failf("review and approve the local declaration with kitsu check trust in your terminal")
			}
		}
		if check, found := policy.Check(id); found {
			return check, "", nil
		}
	}
	return readApprovedLocalCI(s.repo, id)
}

func (o *harnessOptions) resolveCIAssertion(ctx context.Context, s *session, task *harness.Task, candidate harness.Snapshot) (decl.Check, error) {
	if task.NeedsReproduction() && task.Start != candidate {
		return decl.Check{}, failf("restore the original PR candidate before reproducing its check; use a new task for a different head")
	}
	source := task.Bug.Source.Check
	check, approval, err := approvedCICheck(ctx, s, task.Start.Head, source.LocalCheck)
	if err != nil {
		return check, err
	}
	if approval != source.ApprovalSHA256 {
		return decl.Check{}, failf("the approved local check changed. Restore the original approval or start a new CI repair")
	}
	expect := check.Expect
	if expect == "" {
		expect = "repository check " + check.ID + " passes"
	}
	if !o.retry && (o.expect != expect || !slices.Equal(o.argv, check.Run)) {
		return decl.Check{}, failf("CI repair keeps the original local check; use task test with --retry")
	}
	o.expect, o.argv = expect, check.Run
	return check, nil
}

func finishCIRepair(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o harnessOptions, view harnessView, full bool) error {
	if view.Next == "done" {
		return printHarness(s.cfg, o, view)
	}
	if err := checkBugReproduction(s, task.Bug.Reproduction); err != nil {
		return err
	}
	view, err := verifyCIRepair(ctx, s, l, task, o, view)
	if err != nil {
		return err
	}
	if view.Next == "commit" {
		// Reuse a completed verification's ticket after an interruption. The
		// current view has already checked its evidence, review and branch.
		proof := task.Proof
		if view.Candidate == proof.Candidate && o.ticket != "" && (o.ticket == proof.Ticket || o.ticket == proof.CompletionTicket) {
			o.ticket = view.Ticket
		}
		if err := applyHarness(ctx, s, l, task, o, "commit", view); err != nil {
			return err
		}
		view, err = observeSavedHarness(ctx, s, l, task, false)
		if err != nil {
			return err
		}
	}
	if view.Next != "receipt" {
		return errors.Join(printHarness(s.cfg, o, view), exit(ExitNotVerified))
	}
	if err := saveBugReceipt(ctx, s, l, task, view); err != nil {
		view.Verified = false
		view.Delivery = "committed with receipt pending"
		return errors.Join(err, printHarness(s.cfg, o, view))
	}
	view, err = observeHarness(ctx, s, task, full)
	if err != nil {
		return err
	}
	return printHarness(s.cfg, o, view)
}

func verifyCIRepair(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o harnessOptions, view harnessView) (harnessView, error) {
	if view.Next != "verify" {
		return view, nil
	}
	if strings.TrimSpace(o.note) == "" || o.ticket == "" {
		return view, failf("delivery needs the current action ticket and a commit message")
	}
	if view.Candidate.Index != view.Candidate.Tree {
		return view, failf("stage the intended repair before running task commit")
	}
	action := "test"
	o.retry = true
	if recheckStagedCommit(task, o, view) {
		action = "commit"
	}
	if err := applyHarness(ctx, s, l, task, o, action, view); err != nil {
		return view, err
	}
	return observeSavedHarness(ctx, s, l, task, false)
}
