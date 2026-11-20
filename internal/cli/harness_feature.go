package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/harness"
)

func readFeatureBrief(in io.Reader, path string) (*harness.FeatureBrief, error) {
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		in = file
	}
	if in == nil {
		return nil, usagef("feature brief input is missing")
	}
	data, err := io.ReadAll(io.LimitReader(in, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<10 || !utf8.Valid(data) {
		return nil, usagef("feature brief must be UTF-8 JSON up to 64 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var brief harness.FeatureBrief
	if err := dec.Decode(&brief); err != nil {
		return nil, usagef("feature brief: %v", err)
	}
	if dec.Decode(new(json.RawMessage)) != io.EOF {
		return nil, usagef("feature brief must contain one JSON object")
	}
	if err := brief.Validate(); err != nil {
		return nil, usagef("%v", err)
	}
	return &brief, nil
}

func featurePolicy(ctx context.Context, s *session, tree string, brief *harness.FeatureBrief) (*decl.File, error) {
	policy, err := evidence.Declaration(ctx, s.repo, tree)
	if errors.Is(err, evidence.ErrNoDeclaration) {
		policy, err = &decl.File{}, nil
	}
	if err != nil {
		return nil, err
	}
	for _, c := range brief.Criteria {
		if id, named := strings.CutPrefix(c.Check, "check:"); named {
			if _, found := policy.Check(id); !found {
				return nil, failf("feature criterion %q refers to undeclared check %q", c.ID, id)
			}
		}
	}
	return policy, nil
}

func requireFeatureApproval(ctx context.Context, s *session, task *harness.Task, tree string) error {
	policy, err := featurePolicy(ctx, s, tree, task.Feature)
	if err != nil || len(policy.Checks) == 0 {
		return err
	}
	return requireHarnessCheckApproval(s, task.RepositoryPolicy)
}

func (o *harnessOptions) resolveFeatureAssertion(ctx context.Context, s *session, task *harness.Task, c harness.Snapshot) error {
	brief := task.Feature
	if brief.Draft {
		return failf("accept a feature brief before verification")
	}
	behavior := brief.Behavior
	if behavior == nil {
		policy, err := featurePolicy(ctx, s, c.Tree, brief)
		if err != nil {
			return err
		}
		check, _ := policy.Check(brief.PrimaryCheck())
		behavior = &harness.FeatureBehavior{Argv: check.Run, Expect: "Feature check " + check.ID + " passes"}
	}
	if !o.retry && (!slices.Equal(o.argv, behavior.Argv) || o.expect != behavior.Expect) {
		return failf("use task test --retry to run the acceptance checks from the saved feature brief")
	}
	o.argv, o.expect = behavior.Argv, behavior.Expect
	return nil
}

func validateFeatureRecord(ctx context.Context, s *session, t *harness.Task, o harnessOptions, v harnessView) error {
	if o.feature == nil {
		if o.action == "plan" && t.Feature != nil {
			return usagef("record the reviewed feature brief with --feature-brief FILE")
		}
		return nil
	}
	if t.Bug != nil || t.Workflow != nil || v.Completion != nil || t.CommitAttempt != nil {
		return failf("save a feature brief on an open daily task before commit")
	}
	if o.feature.Draft {
		return nil
	}
	_, err := featurePolicy(ctx, s, v.Candidate.Tree, o.feature)
	return err
}

func mayReviseFeature(t *harness.Task, o harnessOptions, v harnessView) bool {
	return o.action == "plan" && o.feature != nil && t.Feature != nil && t.Claim == nil &&
		slices.Contains([]string{"implement", "review", "verify", "commit"}, v.Next)
}

func (v *harnessView) observeDailyGate(ctx context.Context, s *session, t *harness.Task) error {
	var err error
	v.Gate, v.Verified, err = harnessGate(ctx, s, v.Candidate.Tree, t.Proof)
	if err != nil || !v.Verified || t.Feature == nil {
		return err
	}
	v.Verified = t.Feature.Covers(t.Proof)
	if err := requireFeatureApproval(ctx, s, t, v.Candidate.Tree); err != nil {
		v.Verified, v.PolicyError = false, err.Error()
	}
	return nil
}
