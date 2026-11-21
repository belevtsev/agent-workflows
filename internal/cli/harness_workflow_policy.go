package cli

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/verification"
	"github.com/belevtsev/kitsu/v3/internal/workflowrunner"
)

func checkWorkflowAuthority(client workflowrunner.Runner, task *harness.Task) error {
	w := task.Workflow
	if w.Authority == "" {
		return failf("this task predates supervised execution; inspect its record and open a new task with the original request and assertion")
	}
	if offer := client.Offer(); w.Authority != client.Authority() || w.Policy != workflowrunner.Digest(offer.Policy) || w.Profile != offer.Profile {
		return failf("trusted runner registration or reviewed policy changed; resume under the pinned profile or start a new task")
	}
	return nil
}

func validateWorkflowPolicy(p workflowrunner.Policy) error {
	if len(p.Checks) > 64 {
		return failf("reviewed runner policy exceeds 64 checks")
	}
	if err := validateWorkflowPatterns(append(slices.Clone(p.TestPaths), p.CodePaths...)); err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, check := range p.Checks {
		if !validHarnessID(check.ID) || ids[check.ID] || !validWorkflowArgv(check.Argv) {
			return failf("runner policy has an invalid or duplicate check %q", check.ID)
		}
		ids[check.ID] = true
		for name, digest := range check.Inputs {
			if !validWorkflowPath(name) || roles.HostConfigurationPath(name) || len(digest) != 64 {
				return failf("check %q has an invalid reviewed input", check.ID)
			}
			if _, err := hex.DecodeString(digest); err != nil {
				return failf("check %q has an invalid reviewed digest for %s", check.ID, name)
			}
		}
	}
	return nil
}

func validWorkflowArgv(argv []string) bool {
	if len(argv) == 0 || len(argv) > 128 || strings.TrimSpace(argv[0]) == "" {
		return false
	}
	for _, arg := range argv {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return false
		}
	}
	return true
}

func approvedWorkflowCheck(policy workflowrunner.Policy, assertion *harness.WorkflowAssertion) (string, error) {
	for _, check := range policy.Checks {
		if slices.Equal(check.Argv, assertion.Argv) && (assertion.TestID == "" || check.TestID == assertion.TestID) {
			return check.ID, nil
		}
	}
	return "", failf("assertion has no exact argv and test identifier in the trusted runner's reviewed policy")
}

func reviewedWorkflowChecks(ctx context.Context, s *session, task *harness.Task, candidate harness.Snapshot, kind string) ([]string, error) {
	var ids []string
	w, policy := task.Workflow, s.workflowRunner.Offer().Policy
	if kind == "baseline" {
		if w.Test == nil {
			return nil, failf("a baseline requires the test writer's reviewed assertion")
		}
		id, err := approvedWorkflowCheck(policy, w.Test)
		return []string{id}, err
	}
	for _, assertion := range []*harness.WorkflowAssertion{w.Intake, w.Test} {
		if assertion == nil {
			continue
		}
		id, err := approvedWorkflowCheck(policy, assertion)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, failf("verification requires an approved behavioral assertion")
	}
	declared, err := evidence.Declaration(ctx, s.repo, candidate.Tree)
	if errors.Is(err, evidence.ErrNoDeclaration) {
		return ids, nil
	}
	if err != nil {
		return nil, err
	}
	base := task.Start.Head
	if base == "" {
		base, err = s.repo.EmptyTree(ctx)
		if err != nil {
			return nil, err
		}
	}
	plan, err := verification.Build(ctx, s.repo, candidate.Tree, base)
	if err != nil {
		return nil, err
	}
	for _, id := range append(plan.PriorityIDs(), plan.AllIDs()...) {
		found := slices.ContainsFunc(policy.Checks, func(c workflowrunner.Check) bool {
			return c.ID == id && slices.ContainsFunc(declared.Checks, func(requiredCheck decl.Check) bool {
				return requiredCheck.ID == id && slices.Equal(requiredCheck.Run, c.Argv)
			})
		})
		if !found {
			return nil, failf("required repository check %q needs matching reviewed runner policy", id)
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func workflowCheckInputs(policy workflowrunner.Policy, ids []string, files []workflowrunner.File) error {
	for _, check := range policy.Checks {
		if !slices.Contains(ids, check.ID) {
			continue
		}
		for name, digest := range check.Inputs {
			idx := slices.IndexFunc(files, func(file workflowrunner.File) bool { return file.Path == name })
			if idx < 0 || workflowrunner.BytesDigest(files[idx].Data) != digest {
				return fmt.Errorf("reviewed check %s requires unchanged input %s", check.ID, name)
			}
		}
	}
	return nil
}
