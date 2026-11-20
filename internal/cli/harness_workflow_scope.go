package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func validateWorkflowPlan(root string, scope harness.Scope, plan *harness.WorkflowPlan) error {
	if plan == nil || len(plan.CodePaths)+len(plan.TestPaths) > 128 || len(plan.Shared) > 32 ||
		!slices.Contains([]string{"pass", "fail"}, plan.Baseline) {
		return failf("architecture needs a bounded plan with scoped paths and an expected baseline")
	}
	if scope.Entry == "tests" && (plan.Baseline != "pass" || len(plan.CodePaths) > 0) {
		return failf("test-only work requires a passing baseline and test scope")
	}
	if err := validateWorkflowPatterns(append(slices.Clone(plan.CodePaths), plan.TestPaths...)); err != nil {
		return err
	}
	return validateWorkflowRegions(root, scope, plan)
}

func validateWorkflowPatterns(patterns []string) error {
	for _, p := range patterns {
		if decl.ValidPattern(p) != nil || strings.ContainsAny(p, "\\\x00\r\n") || strings.Contains(p, ":") ||
			p == "**" || p == "*" || len(p) > 1024 || path.Clean(p) != p {
			return failf("architecture contains an invalid or unbounded write path %q", p)
		}
	}
	return nil
}

func validateWorkflowRegions(root string, scope harness.Scope, plan *harness.WorkflowPlan) error {
	seen := map[string]bool{}
	for _, region := range plan.Shared {
		if seen[region.Path] || !fs.ValidPath(region.Path) || !decl.MatchAny(plan.TestPaths, region.Path) ||
			(scope.Entry != "tests" && !decl.MatchAny(plan.CodePaths, region.Path)) ||
			region.First == "" || region.Last == "" || region.First == region.Last ||
			len(region.First)+len(region.Last) > 4096 || strings.ContainsAny(region.First+region.Last, "\r\n\x00") {
			return failf("architecture needs one unambiguous test region for %q", region.Path)
		}
		seen[region.Path] = true
	}
	_, err := workflowSharedFingerprints(root, plan.Shared)
	return err
}

func validateWorkflowAssertion(plan *harness.WorkflowPlan, assertion *harness.WorkflowAssertion) error {
	if assertion == nil || !validWorkflowArgv(assertion.Argv) ||
		strings.TrimSpace(assertion.Expect) == "" || len(assertion.Expect) > 4096 || len(assertion.TestID) > 1024 ||
		plan.Baseline == "fail" && strings.TrimSpace(assertion.TestID) == "" {
		return failf("test writer must supply a reviewed argv, expected behavior and a test ID for a red baseline")
	}
	return nil
}

func workflowPathAllowed(w *harness.Workflow, name string, after map[string]harness.WorkflowSharedFingerprint) bool {
	if roles.ProtectedWorkflowPath(name) || strings.HasPrefix(strings.ToLower(name), "openspec/") {
		return false
	}
	test := decl.MatchAny(w.Plan.TestPaths, name)
	projectTest := decl.MatchAny(w.TestPolicy, name)
	if current, shared := after[name]; shared {
		before := w.Active.Shared[name]
		if w.Active.Role == "quality-engineer" {
			return test && projectTest && before.Outside == current.Outside
		}
		return decl.MatchAny(w.Plan.CodePaths, name) && before.Inside == current.Inside
	}
	if w.Active.Role == "quality-engineer" {
		return test && projectTest
	}
	return decl.MatchAny(w.Plan.CodePaths, name) && !test && !decl.MatchAny(w.TestPolicy, name)
}

type sharedFile struct {
	prefix, body, suffix []byte
}

func workflowSharedFingerprints(rootName string, regions []harness.WorkflowTestRegion) (map[string]harness.WorkflowSharedFingerprint, error) {
	if len(regions) == 0 {
		return nil, nil
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	out := make(map[string]harness.WorkflowSharedFingerprint, len(regions))
	for _, region := range regions {
		file, err := readSharedFile(root, region)
		if err != nil {
			return nil, err
		}
		out[region.Path] = sharedFingerprint(file)
	}
	return out, nil
}

func readSharedFile(root *os.Root, region harness.WorkflowTestRegion) (sharedFile, error) {
	file, err := root.Open(region.Path)
	if err != nil {
		return sharedFile{}, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return sharedFile{}, failf("shared test file %q must be a bounded regular file", region.Path)
	}
	payload, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(payload) > 4<<20 {
		return sharedFile{}, failf("cannot read shared test file %q", region.Path)
	}
	return workflowRegion(payload, region)
}

func sharedFingerprint(file sharedFile) harness.WorkflowSharedFingerprint {
	return harness.WorkflowSharedFingerprint{
		Outside: fmt.Sprintf("%x/%x", sha256.Sum256(file.prefix), sha256.Sum256(file.suffix)),
		Inside:  fmt.Sprintf("%x", sha256.Sum256(file.body)),
	}
}

func workflowBoundary(data []byte, line string) (int, int, bool) {
	pos, first, end, count := 0, 0, 0, 0
	for remain := data; len(remain) != 0; {
		content, next, nl := bytes.Cut(remain, []byte("\n"))
		size := len(content)
		if nl {
			size++
		}
		if string(bytes.TrimSuffix(content, []byte("\r"))) == line {
			first, end, count = pos, pos+size, count+1
		}
		pos += size
		remain = next
	}
	return first, end, count == 1
}
