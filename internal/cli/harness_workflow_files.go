package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/workflowrunner"
)

func validWorkflowPath(name string) bool {
	if name == "." || !fs.ValidPath(name) || !filepath.IsLocal(filepath.FromSlash(name)) || strings.ContainsAny(name, ":\\\x00\r\n") {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if strings.TrimRight(part, " .") != part {
			return false
		}
	}
	return true
}

func workflowFiles(ctx context.Context, s *session, task *harness.Task, candidate harness.Snapshot, staged bool) ([]workflowrunner.File, error) {
	root, err := os.OpenRoot(s.repo.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	var files []workflowrunner.File
	var objects []string
	seen := map[string]bool{}
	var size int
	var captureErr error
	err = s.repo.Entries(ctx, candidate.Tree, func(entry []byte, name string) {
		if captureErr != nil || roles.HostConfigurationPath(name) {
			return
		}
		header, _, _ := bytes.Cut(entry, []byte{'\t'})
		fields := strings.Fields(string(header))
		if len(fields) != 3 {
			captureErr = failf("invalid Git tree entry for %q", name)
			return
		}
		mode := fields[0]
		if !validWorkflowPath(name) || seen[strings.ToLower(name)] || mode != "100644" && mode != "100755" {
			captureErr = failf("runner requires portable regular files; cannot materialize %q", name)
			return
		}
		seen[strings.ToLower(name)] = true
		if len(files) >= workflowrunner.MaxFiles {
			captureErr = failf("runner snapshot exceeds 8192 files")
			return
		}
		file := workflowrunner.File{Path: name, Mode: mode}
		if staged {
			objects = append(objects, fields[2])
		} else {
			file.Data, captureErr = readWorkflowFile(root, name, workflowrunner.MaxBundle-size)
			size += len(file.Data)
		}
		files = append(files, file)
	})
	if err := errors.Join(err, captureErr); err != nil {
		return nil, err
	}
	if staged {
		blobs, err := s.repo.ReadWorkflowBlobs(ctx, objects, workflowrunner.MaxBundle)
		if err != nil {
			return nil, err
		}
		for i, data := range blobs {
			files[i].Data = data
		}
	}
	after, err := harnessCandidate(ctx, s, task)
	if err != nil || after != candidate {
		return nil, errors.Join(err, failf("candidate changed while collecting isolated runner inputs"))
	}
	return files, nil
}

func readWorkflowFile(root *os.Root, name string, remaining int) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(remaining) {
		return nil, failf("runner cannot read a bounded regular file at %q", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, failf("runner input changed while opening %q", name)
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(remaining)+1))
	after, statErr := root.Lstat(name)
	if err != nil || statErr != nil || !os.SameFile(info, after) || !after.ModTime().Equal(info.ModTime()) || int64(len(data)) != info.Size() {
		return nil, failf("runner input changed while reading %q", name)
	}
	return data, nil
}

func workflowFilesKey(files []workflowrunner.File) string {
	if len(files) == 0 {
		return workflowrunner.Digest([]workflowrunner.File(nil))
	}
	files = slices.Clone(files)
	slices.SortFunc(files, func(a, b workflowrunner.File) int { return strings.Compare(a.Path, b.Path) })
	return workflowrunner.Digest(files)
}

func workflowChanges(before, changes []workflowrunner.File) []workflowrunner.File {
	byPath := make(map[string]workflowrunner.File, len(before))
	for _, file := range before {
		byPath[file.Path] = file
	}
	for _, file := range changes {
		if file.Mode == "" {
			delete(byPath, file.Path)
		} else {
			byPath[file.Path] = file
		}
	}
	after := make([]workflowrunner.File, 0, len(byPath))
	for _, file := range byPath {
		after = append(after, file)
	}
	return after
}

func applyWorkflowFiles(ctx context.Context, s *session, l *stageLedger, task *harness.Task, changed []workflowrunner.File) (harness.Snapshot, error) {
	a := task.Workflow.Active
	current, err := harnessCandidate(ctx, s, task)
	if err != nil {
		return current, err
	}
	if current != a.Candidate && (a.Import == "" || !current.SameBase(a.Candidate) || current.Index != a.Candidate.Index) {
		return current, failf("checkout changed since the runner job started; restore the reserved candidate")
	}
	if len(changed) == 0 {
		return current, nil
	}
	files, err := workflowFiles(ctx, s, task, current, false)
	if err != nil {
		return current, err
	}
	if a.Import != "" && a.Import == workflowFilesKey(files) {
		return current, nil
	}
	if current != a.Candidate {
		return current, failf("interrupted import does not match the reserved candidate or its returned files")
	}
	proposed := workflowChanges(files, changed)
	if !workflowBundleFits(proposed) {
		return current, completeWorkflowFailure(l, task, "returned files exceed the runner bundle limit")
	}
	before, err := validateWorkflowChanges(ctx, s, task, files, changed)
	if err != nil {
		return current, completeWorkflowFailure(l, task, err.Error())
	}
	a.Import = workflowFilesKey(proposed)
	if err := l.write(harnessFile(task.ID), task); err != nil {
		return current, err
	}
	if err := s.repo.ApplyWorkflowFiles(ctx, before, changed); err != nil {
		return current, err
	}
	after, err := harnessCandidate(ctx, s, task)
	if err != nil || !after.SameBase(current) || after.Index != current.Index {
		return after, errors.Join(err, failf("Git parents or staging changed during runner import"))
	}
	imported, err := workflowFiles(ctx, s, task, after, false)
	if err != nil || workflowFilesKey(imported) != a.Import {
		return after, errors.Join(err, failf("imported worktree differs from the approved runner files; inspect preserved edits"))
	}
	return after, nil
}

func workflowBundleFits(files []workflowrunner.File) bool {
	size := 0
	for _, f := range files {
		size += len(f.Data)
	}
	return len(files) <= workflowrunner.MaxFiles && size <= workflowrunner.MaxBundle
}

func validateWorkflowChanges(ctx context.Context, s *session, task *harness.Task, files, changes []workflowrunner.File) ([]workflowrunner.File, error) {
	w, a := task.Workflow, task.Workflow.Active
	if a.Role != "quality-engineer" && a.Role != "developer" {
		return nil, failf("%s returned writes for a read-only job", a.Role)
	}
	hooks, _, err := s.repo.HooksDir(ctx)
	if err != nil {
		return nil, err
	}
	hookPath, relErr := filepath.Rel(s.repo.Root, hooks)
	hookPath = filepath.ToSlash(hookPath)
	localHooks := relErr == nil && filepath.IsLocal(hookPath)
	known := make(map[string]workflowrunner.File, len(files))
	for _, file := range files {
		known[strings.ToLower(file.Path)] = file
	}
	protected := reviewedWorkflowInputs(s.workflowRunner.Offer().Policy)
	touched := map[string]bool{}
	var before []workflowrunner.File
	for _, f := range changes {
		key := strings.ToLower(f.Path)
		if !validWorkflowChange(f) || touched[key] {
			return nil, failf("runner returned invalid or protected path %q", f.Path)
		}
		touched[key] = true
		if localHooks && pathWithinWorkflowHooks(f.Path, hookPath) {
			return nil, failf("runner changed the configured Git hook path %q", f.Path)
		}
		old, existed := known[key]
		if existed && old.Path != f.Path || !existed && f.Mode == "" {
			return nil, failf("runner returned an incompatible path %q", f.Path)
		}
		if err := checkWorkflowNewPath(ctx, s, f.Path, existed); err != nil {
			return nil, err
		}
		if protected[f.Path] {
			return nil, failf("runner changed reviewed check input %q", f.Path)
		}
		if err := validateWorkflowFileScope(s, w, old, f); err != nil {
			return nil, err
		}
		if existed {
			before = append(before, old)
		}
	}
	return before, nil
}

func checkWorkflowNewPath(ctx context.Context, s *session, name string, existed bool) error {
	if existed {
		return nil
	}
	ignored, err := s.repo.WorkflowPathIgnored(ctx, name)
	if err != nil || ignored {
		return errors.Join(err, failf("runner cannot introduce ignored path %q", name))
	}
	return nil
}

func reviewedWorkflowInputs(policy workflowrunner.Policy) map[string]bool {
	protected := map[string]bool{}
	for _, check := range policy.Checks {
		for name := range check.Inputs {
			protected[name] = true
		}
	}
	return protected
}

func pathWithinWorkflowHooks(name, hookPath string) bool {
	return hookPath == "." || name == hookPath || strings.HasPrefix(name, hookPath+"/")
}

func validWorkflowChange(file workflowrunner.File) bool {
	if !validWorkflowPath(file.Path) || roles.ProtectedWorkflowPath(file.Path) {
		return false
	}
	switch file.Mode {
	case "100644", "100755":
		return true
	case "":
		return len(file.Data) == 0
	}
	return false
}

func workflowRegion(data []byte, r harness.WorkflowTestRegion) (sharedFile, error) {
	first, _, startOK := workflowBoundary(data, r.First)
	_, last, endOK := workflowBoundary(data, r.Last)
	if !startOK || !endOK || first >= last {
		return sharedFile{}, failf("shared test boundaries in %q are missing, reordered, or ambiguous", r.Path)
	}
	return sharedFile{prefix: data[:first], body: data[first:last], suffix: data[last:]}, nil
}

func validateWorkflowFileScope(s *session, w *harness.Workflow, old, file workflowrunner.File) error {
	name := file.Path
	policy := s.workflowRunner.Offer().Policy
	if w.Active.Role == "developer" && !decl.MatchAny(policy.CodePaths, name) {
		return failf("developer changed %q outside the runner's reviewed source paths", name)
	}
	shared := map[string]harness.WorkflowSharedFingerprint{}
	for _, region := range w.Plan.Shared {
		if name != region.Path {
			continue
		}
		if old.Mode != file.Mode {
			return failf("cannot change the mode or remove shared test file %q", name)
		}
		result, err := workflowRegion(file.Data, region)
		if err != nil {
			return err
		}
		shared[name] = sharedFingerprint(result)
	}
	if !workflowPathAllowed(w, name, shared) {
		return failf("%s changed %q outside its reviewed write scope", w.Active.Role, name)
	}
	return nil
}
