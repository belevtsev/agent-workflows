package publish

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	reconcileBound = 30 * time.Second
	prPollReads    = 5
	prPollPause    = 2 * time.Second
)

// Deps are the run's dependencies on its surroundings.
type Deps struct {
	// LookPath finds an executable on cw's PATH.
	LookPath func(file string) (string, error)
	// Environ is cw's own environment.
	Environ []string
	// HookStore returns the task store cw hook pre-publish reads.
	HookStore func() (string, error)
}

// run is one cw publish run.
type run struct {
	req  Request
	deps Deps
	rep  Report

	task     *taskflow.PublicationTask
	record   taskflow.Record
	worktree string
	git      git
	gh       gh
	locks    gitLocks

	verified map[string]pathState // verified state by manifest spelling
	expected map[string]pathState // state in --expect-head's tree
	raw      map[string]string    // manifest spelling to on-disk path
	files    map[string]projectctx.FileState
	newPaths []string
	head     string
	atHead   bool // HEAD is this request's commit
	pushURL  string
	plan     prPlan

	runID   string
	started bool
}

// Run carries out the request and returns its report. It never panics on
// external state and always releases the task's .publish-lock it took.
func Run(ctx context.Context, req Request, deps Deps) Report {
	r := &run{req: req, deps: deps, rep: newReport(req)}
	f := r.execute(ctx)
	r.conclude(f)
	if r.task != nil {
		if err := r.task.Close(); err != nil {
			r.rep.Detail = strings.TrimPrefix(r.rep.Detail+"; .publish-lock could not be removed: "+err.Error(), "; ")
		}
	}
	return r.rep
}

func newReport(req Request) Report {
	pr := "skipped"
	if req.pr == PRNone {
		pr = "not-requested"
	}
	return Report{
		Paths: []string{}, CommittedPaths: []string{}, IntentToAdd: []string{}, LocksLeft: []string{}, OpenRecords: []OpenRecord{},
		ExpectHead: req.expectHead, Branch: req.branch, Remote: req.remote, ExpectRemote: req.expectRemote,
		Steps:           taskflow.PublicationSteps{Commit: taskflow.CommitStep{Status: "skipped"}, Push: taskflow.PushStep{Status: "skipped"}, PR: taskflow.PRStep{Status: pr}},
		PreviousOutcome: "none",
	}
}

// execute runs the precedence list of contract section 5 and then the
// steps; it returns the first failure, or nil when published.
func (r *run) execute(ctx context.Context) *failure {
	gitPath, err := r.deps.LookPath("git")
	if err != nil {
		return failf("git-missing", "git is not on PATH")
	}
	task, err := taskflow.OpenPublicationTask(r.req.task)
	if err != nil {
		return failf("task-unreadable", "open the task directory: %v", err)
	}
	r.rep.Task = task.Directory()
	if err := task.Lock(); err != nil {
		task.Close()
		if errors.Is(err, taskflow.ErrPublicationLocked) {
			return failf("locked", "%v", err)
		}
		return failf("task-unreadable", "lock the task directory: %v", err)
	}
	r.task = task
	if r.record, err = task.Load(); err != nil {
		return failf("task-unreadable", "load the task record: %v", err)
	}
	r.rep.TaskID, r.worktree = r.record.ID, r.record.Baseline.Root
	r.rep.Worktree = r.worktree
	log, err := task.ReadPublications(r.record.ID)
	if err != nil {
		return failf("record-invalid", "publication log: %v", err)
	}
	r.rep.PreviousOutcome = log.PreviousOutcome()
	if f := gate(r.record); f != nil {
		return f
	}
	source, err := task.Source(ctx, r.record)
	switch {
	case err != nil && interrupted(ctx):
		return failf("cancelled", "interrupted while capturing the source")
	case errors.Is(err, taskflow.ErrSourceCapture):
		return failf("capture-failed", "%v", err)
	case err != nil:
		return failf("task-unreadable", "%v", err)
	}
	r.rep.SourceSHA256 = source.Digest
	if source.Digest != r.record.Verification.SourceSHA256 {
		return failf("source-changed", "the source changed after the last passing cw task verify")
	}
	r.git = git{tool{path: gitPath, dir: r.worktree, env: toolEnvironment(r.deps.Environ, "GIT_TERMINAL_PROMPT=0"), timeout: r.req.timeout}}
	if f := r.repository(ctx); f != nil {
		return f
	}
	if f := r.openRecords(ctx); f != nil {
		return f
	}
	if f := r.authorize(source); f != nil {
		return f
	}
	if f := r.preconditions(ctx); f != nil {
		return f
	}
	if f := r.start(); f != nil {
		return f
	}
	if f := r.commitStep(ctx); f != nil {
		return f
	}
	if f := r.pushStep(ctx); f != nil {
		return f
	}
	return r.prStep(ctx)
}

// gate applies R6.a, R6.e and R6.b to a loaded record.
func gate(record taskflow.Record) *failure {
	verification := record.Verification
	switch {
	case record.Abandonment != nil:
		return failf("task-abandoned", "the task was abandoned")
	case verification == nil || !verification.Complete:
		return failf("task-not-verified", "the task has no passing cw task verify")
	case verification.SourceSHA256 == "":
		return failf("task-not-verified", "the task's verification recorded no source digest; verify it again")
	}
	return nil
}

// reread applies R6.f at a step re-read.
func (r *run) reread() *failure {
	record, err := r.task.Load()
	if err != nil {
		return failf("task-unreadable", "reload the task record: %v", err)
	}
	if f := gate(record); f != nil {
		return f
	}
	if record.Verification.SourceSHA256 != r.rep.SourceSHA256 {
		return failf("source-changed", "the task was verified again on other source")
	}
	r.record = record
	return nil
}

// repository applies R9.a: the worktree must be a Git worktree root.
func (r *run) repository(ctx context.Context) *failure {
	toplevel, f := r.git.toplevel(ctx)
	if f != nil {
		return f
	}
	if toplevel != "" {
		if resolved, err := filepath.EvalSymlinks(toplevel); err == nil && resolved == r.worktree {
			return nil
		}
	}
	return failf("not-a-repository", "%s is not the root of a Git worktree", r.worktree)
}

// openRecords applies R7: no other open record may gate the repository.
func (r *run) openRecords(ctx context.Context) *failure {
	repo, err := taskflow.ResolveRepository(ctx, r.worktree, r.git.output)
	if err != nil {
		if interrupted(ctx) {
			return failf("cancelled", "interrupted while resolving the repository")
		}
		return failf("read-failed", "resolve the repository: %v", err)
	}
	hook, err := r.deps.HookStore()
	if err != nil {
		return failf("read-failed", "resolve the hook task store: %v", err)
	}
	var stores []string
	for _, store := range []string{filepath.Dir(r.task.Directory()), hook} {
		if canonical := canonicalStore(store); !slices.Contains(stores, canonical) {
			stores = append(stores, canonical)
		}
	}
	now := time.Now()
	seen := map[string]bool{r.record.ID: true}
	for _, store := range stores {
		open, err := taskflow.FindOpen(ctx, store, repo, now, taskflow.SourceDigest)
		if err != nil {
			if interrupted(ctx) {
				return failf("cancelled", "interrupted while reading task records")
			}
			return failf("read-failed", "read the task store %s: %v", store, err)
		}
		for _, record := range open {
			if !record.Gates() || seen[record.Record.ID] {
				continue
			}
			seen[record.Record.ID] = true
			r.rep.OpenRecords = append(r.rep.OpenRecords, OpenRecord{TaskID: record.Record.ID, Title: record.Record.Plan.Title, Directory: record.Record.Directory, Problem: record.Problem})
		}
	}
	if interrupted(ctx) {
		return failf("cancelled", "interrupted while reading task records")
	}
	if len(r.rep.OpenRecords) > 0 {
		return failf("open-record", "another open task record gates this repository; verify or abandon it first")
	}
	return nil
}

func canonicalStore(store string) string {
	if abs, err := filepath.Abs(store); err == nil {
		store = abs
	}
	if resolved, err := filepath.EvalSymlinks(store); err == nil {
		return resolved
	}
	return filepath.Clean(store)
}

// authorize applies R8 and fixes the authorized set A.
func (r *run) authorize(source taskflow.PublicationSource) *failure {
	if len(source.Changed) == 0 {
		return failf("nothing-to-publish", "the task changed no file since cw task start")
	}
	changed := map[string]bool{}
	var set []string
	for _, file := range source.Changed {
		changed[file.Path] = true
		set = append(set, file.Path)
	}
	if r.req.paths != nil {
		for _, p := range r.req.paths {
			if !changed[p] {
				return failf("path-not-verified", "%s is not in the task's verified change set", p)
			}
		}
		set = slices.Clone(r.req.paths)
	}
	slices.Sort(set)
	r.rep.Paths = set
	r.files = map[string]projectctx.FileState{}
	r.verified = map[string]pathState{}
	r.raw = map[string]string{}
	for _, file := range source.Files {
		r.files[file.Path] = file
	}
	for _, p := range set {
		r.raw[p] = projectctx.ManifestRawPath(p)
		if file, ok := r.files[p]; ok {
			r.verified[p] = pathState{present: true, mode: file.Mode, sha256: file.SHA256}
		}
	}
	return nil
}

func (r *run) rawPaths(paths []string) []string {
	raw := make([]string, 0, len(paths))
	for _, p := range paths {
		raw = append(raw, r.raw[p])
	}
	return raw
}

// preconditions applies R9.b to R12 in the order of section 5.
func (r *run) preconditions(ctx context.Context) *failure {
	branch, f := r.git.branch(ctx)
	if f != nil {
		return f
	}
	if branch != "refs/heads/"+r.req.branch {
		return failf("branch-mismatch", "HEAD is not on branch %s", r.req.branch)
	}
	exists, f := r.git.commitExists(ctx, r.req.expectHead)
	if f != nil {
		return f
	}
	if !exists {
		return failf("head-moved", "--expect-head %s names no commit", r.req.expectHead)
	}
	if r.head, f = r.git.head(ctx); f != nil {
		return f
	}
	if f := r.commitSet(ctx); f != nil {
		return f
	}
	if r.head == "" {
		return failf("head-moved", "HEAD names no commit")
	}
	if r.head != r.req.expectHead {
		ok, f := r.requestCommit(ctx, r.head)
		if f != nil {
			return f
		}
		if !ok {
			return failf("head-moved", "HEAD %s is neither --expect-head nor this request's commit", r.head)
		}
		r.atHead = true
	} else {
		if f := r.indexScope(ctx); f != nil {
			return f
		}
		if f := r.conversions(ctx); f != nil {
			return f
		}
	}
	gitDir, f := r.git.directory(ctx, "--git-dir")
	if f != nil {
		return f
	}
	commonDir, f := r.git.directory(ctx, "--git-common-dir")
	if f != nil {
		return f
	}
	r.locks = gitLocks{gitDir: gitDir, commonDir: commonDir, remote: r.req.remote, branch: r.req.branch}
	if f := r.lockFree(ctx); f != nil {
		return f
	}
	remotes, f := r.git.remotes(ctx)
	if f != nil {
		return f
	}
	if !slices.Contains(remotes, r.req.remote) {
		return failf("remote-unknown", "git remote lists no remote %s", r.req.remote)
	}
	if f := r.oneRepository(ctx); f != nil {
		return f
	}
	remote, f := r.git.remoteRef(ctx, r.req.remote, r.req.branch)
	if f != nil {
		return f
	}
	r.rep.Steps.Push.RemoteRef = remote
	publication := ""
	switch {
	case r.atHead:
		publication = r.head
	case len(r.rep.CommittedPaths) == 0:
		publication = r.req.expectHead
	}
	if publication == "" || remote != publication {
		if f := r.remoteAdvance(ctx, remote, r.req.expectHead); f != nil {
			return f
		}
	}
	if r.req.pr == PRNone {
		return nil
	}
	ghPath, err := r.deps.LookPath("gh")
	if err != nil {
		return failf("gh-missing", "gh is not on PATH")
	}
	r.gh = gh{tool{path: ghPath, dir: r.worktree, env: toolEnvironment(r.deps.Environ, "GH_PROMPT_DISABLED=1"), timeout: r.req.timeout}}
	urls, f := r.gh.repoURLs(ctx, r.req.repo)
	if f != nil {
		return f
	}
	if !slices.ContainsFunc(urls, func(url string) bool { return sameRepository(r.pushURL, url, r.worktree) }) {
		return failf("repo-mismatch", "the push URL %s of %s does not name %s (%s)", r.pushURL, r.req.remote, r.req.repo, strings.Join(urls, ", "))
	}
	prs, f := r.gh.list(ctx, r.req.repo, r.req.branch)
	if f != nil {
		return f
	}
	plan, reason := planPR(sameRepositoryOpen(prs, r.req.branch), r.req, "")
	if reason != "" {
		return failf(reason, "%s", prRefusal(reason, r.req.branch))
	}
	r.plan = plan
	return nil
}

func prRefusal(reason, branch string) string {
	switch reason {
	case "pr-exists":
		return "an open pull request for " + branch + " exists and differs from the request; use --pr edit"
	case "pr-missing":
		return "no open pull request for " + branch + " exists; use --pr create"
	}
	return "several open pull requests for " + branch + " exist"
}

// commitSet computes D from the expected head's tree.
func (r *run) commitSet(ctx context.Context) *failure {
	entries, f := r.git.tree(ctx, r.req.expectHead, r.rawPaths(r.rep.Paths))
	if f != nil {
		return f
	}
	var oids []string
	for _, entry := range entries {
		if entry.kind == "blob" {
			oids = append(oids, entry.oid)
		}
	}
	hashes, f := r.git.blobHashes(ctx, oids)
	if f != nil {
		return f
	}
	r.expected = map[string]pathState{}
	for _, p := range r.rep.Paths {
		if entry, ok := entries[r.raw[p]]; ok {
			r.expected[p] = pathState{present: true, mode: entry.mode, sha256: hashes[entry.oid]}
		}
	}
	r.rep.CommittedPaths = commitSet(r.rep.Paths, r.verified, r.expected)
	return nil
}

// requestCommit reports whether commit is this request's commit: one parent,
// --expect-head, the message file's bytes, and a tree that differs from
// --expect-head's exactly at D, with the verified modes and bytes.
func (r *run) requestCommit(ctx context.Context, commit string) (bool, *failure) {
	parents, message, f := r.git.commitObject(ctx, commit)
	if f != nil {
		return false, f
	}
	if len(parents) != 1 || parents[0] != r.req.expectHead || !bytes.Equal(message, r.req.message) {
		return false, nil
	}
	changes, f := r.git.diff(ctx, r.req.expectHead, commit)
	if f != nil {
		return false, f
	}
	if len(changes) != len(r.rep.CommittedPaths) {
		return false, nil
	}
	var oids []string
	for _, p := range r.rep.CommittedPaths {
		change, ok := changes[r.raw[p]]
		if !ok || change.deleted == r.verified[p].present {
			return false, nil
		}
		if !change.deleted {
			if change.newMode != r.verified[p].mode {
				return false, nil
			}
			oids = append(oids, change.newOID)
		}
	}
	hashes, f := r.git.blobHashes(ctx, oids)
	if f != nil {
		return false, f
	}
	for _, p := range r.rep.CommittedPaths {
		if change := changes[r.raw[p]]; !change.deleted && hashes[change.newOID] != r.verified[p].sha256 {
			return false, nil
		}
	}
	return true, nil
}

// indexScope applies R10.a and finds the new paths of D.
func (r *run) indexScope(ctx context.Context) *failure {
	committed := r.rep.CommittedPaths
	index, f := r.git.index(ctx, r.rawPaths(committed))
	if f != nil {
		return f
	}
	var oids []string
	for _, entries := range index {
		for _, entry := range entries {
			oids = append(oids, entry.oid)
		}
	}
	hashes, f := r.git.blobHashes(ctx, oids)
	if f != nil {
		return f
	}
	r.newPaths = nil
	for _, p := range committed {
		entries := index[r.raw[p]]
		verified, expected := r.verified[p], r.expected[p]
		if len(entries) == 0 {
			if verified.present {
				r.newPaths = append(r.newPaths, p)
			}
			if expected.present && verified.present {
				return failf("hunk-scope", "%s is staged for removal but its verified state keeps it", p)
			}
			continue
		}
		entry := entries[0]
		if len(entries) > 1 || entry.stage != "0" {
			return failf("hunk-scope", "%s is unmerged in the index", p)
		}
		// An intent-to-add entry, by git's own flag, counts as not staged for a
		// path absent from --expect-head. For a path present there it records a
		// staged removal, whatever its empty blob, so it is judged as an absent
		// entry. A genuinely staged empty blob is judged by its content like
		// any other.
		staged := pathState{present: true, mode: entry.mode, sha256: hashes[entry.oid]}
		notStaged := entry.intentToAdd && !expected.present
		if entry.intentToAdd && expected.present {
			staged = pathState{}
		}
		if staged != expected && staged != verified && !notStaged {
			return failf("hunk-scope", "%s is staged with content that is neither --expect-head's nor the verified file's; cw publishes whole files only", p)
		}
	}
	return nil
}

// conversions applies R10.b.
func (r *run) conversions(ctx context.Context) *failure {
	var present []string
	for _, p := range r.rep.CommittedPaths {
		if r.verified[p].present {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return nil
	}
	autocrlfEntry, f := r.git.config(ctx, `^core\.autocrlf$`)
	if f != nil {
		return f
	}
	fileModeEntry, f := r.git.config(ctx, `^core\.filemode$`)
	if f != nil {
		return f
	}
	autocrlf, err := autocrlfConverts(autocrlfEntry)
	if err != nil {
		return failf("read-failed", "%v", err)
	}
	modeOff, err := fileModeOff(fileModeEntry)
	if err != nil {
		return failf("read-failed", "%v", err)
	}
	attrs, f := r.git.attributes(ctx, r.rawPaths(present))
	if f != nil {
		return f
	}
	root, err := os.OpenRoot(r.worktree)
	if err != nil {
		return failf("read-failed", "open the worktree: %v", err)
	}
	defer root.Close()
	for _, p := range present {
		content, err := readVerified(root, r.raw[p], r.verified[p].mode)
		if err != nil {
			return failf("read-failed", "read %s: %v", p, err)
		}
		if why := conversion(attrs[r.raw[p]], content, r.verified[p], r.expected[p], autocrlf, modeOff); why != "" {
			return failf("content-conversion", "git would not store the verified %s: %s", p, why)
		}
	}
	return nil
}

// readVerified returns a file's bytes, or a symbolic link's target text.
func readVerified(root *os.Root, raw, mode string) ([]byte, error) {
	if mode == "120000" {
		target, err := root.Readlink(raw)
		return []byte(target), err
	}
	return root.ReadFile(raw)
}

// lockFree applies R10.c.
func (r *run) lockFree(ctx context.Context) *failure {
	locks, f := r.locks.listing(ctx)
	if f != nil {
		return f
	}
	if len(locks) > 0 {
		r.rep.LocksLeft = locks
		return failf("git-locked", "Git lock files exist: %s; remove one only after checking that no git process runs in this repository", strings.Join(locks, ", "))
	}
	return nil
}

// oneRepository applies R11.g and keeps the push URL.
func (r *run) oneRepository(ctx context.Context) *failure {
	fetch, f := r.git.remoteURLs(ctx, r.req.remote, false)
	if f != nil {
		return f
	}
	push, f := r.git.remoteURLs(ctx, r.req.remote, true)
	if f != nil {
		return f
	}
	if len(fetch) != 1 || len(push) != 1 || !sameRepository(fetch[0], push[0], r.worktree) {
		return failf("remote-split", "%s must fetch from and push to one repository: fetch %s, push %s", r.req.remote, strings.Join(fetch, " "), strings.Join(push, " "))
	}
	r.pushURL = push[0]
	return nil
}

// remoteAdvance applies R11.c to R11.e: the remote branch must be as
// expected and the push must fast-forward it to a descendant of base.
func (r *run) remoteAdvance(ctx context.Context, remote, base string) *failure {
	if r.req.expectRemote != "" {
		if remote != r.req.expectRemote {
			return failf("remote-moved", "%s of %s is %s, not --expect-remote %s", r.req.branch, r.req.remote, remote, r.req.expectRemote)
		}
		if r.req.expectRemote == "absent" {
			return nil
		}
	} else if remote == "absent" {
		return nil
	}
	exists, f := r.git.commitExists(ctx, remote)
	if f != nil {
		return f
	}
	ancestor := false
	if exists {
		if ancestor, f = r.git.isAncestor(ctx, remote, base); f != nil {
			return f
		}
	}
	if !ancestor {
		return failf("not-fast-forward", "%s of %s (%s) is not %s or one of its ancestors", r.req.branch, r.req.remote, remote, base)
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// start writes the started entry.
func (r *run) start() *failure {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return failf("record-write-failed", "generate a run ID: %v", err)
	}
	r.runID = hex.EncodeToString(id[:])
	request := &taskflow.PublicationRequest{
		SourceSHA256: r.rep.SourceSHA256, Paths: r.rep.Paths, CommittedPaths: r.rep.CommittedPaths,
		MessageSHA256: sha256Hex(r.req.message), ExpectHead: r.req.expectHead, Branch: r.req.branch,
		Remote: r.req.remote, ExpectRemote: r.req.expectRemote,
		PR: taskflow.PublicationPR{Mode: r.req.pr, Repo: r.req.repo, Base: r.req.base, Title: r.req.title},
	}
	if r.req.pr != PRNone {
		request.PR.BodySHA256 = sha256Hex(r.req.body)
	}
	if _, err := r.task.Append(taskflow.PublicationEntry{TaskID: r.record.ID, RunID: r.runID, Kind: "started", At: time.Now().UTC(), Request: request}); err != nil {
		return failf("record-write-failed", "write the started publication entry: %v", err)
	}
	r.started = true
	return nil
}

// reconciling returns the context for reads after a state-changing command:
// unaffected by interruption and bounded by min(--timeout, 30 s).
func (r *run) reconciling(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), min(r.req.timeout, reconcileBound))
}

// stepFailure sets a step's status for a failure at its re-read: refused for
// a refusal or denial, failed for a definite failure, skipped when cancelled.
func stepFailure(f *failure) string {
	switch reasonStatus[f.reason] {
	case StatusDenied, StatusRefused:
		return "refused"
	case StatusCancelled:
		return "skipped"
	}
	return "failed"
}

// commitStep is R13 and R14.
func (r *run) commitStep(ctx context.Context) *failure {
	step := &r.rep.Steps.Commit
	refuse := func(f *failure) *failure {
		step.Status, step.Detail = stepFailure(f), f.detail
		return f
	}
	if f := r.reread(); f != nil {
		return refuse(f)
	}
	current, err := taskflow.SourceDigest(ctx, r.record.Baseline.SelectedPath)
	switch {
	case err != nil && interrupted(ctx):
		return refuse(failf("cancelled", "interrupted while capturing the source"))
	case err != nil:
		return refuse(failf("capture-failed", "capture the current source: %v", err))
	case current != r.rep.SourceSHA256:
		return refuse(failf("source-changed", "the source changed during cw publish"))
	}
	head, f := r.git.head(ctx)
	if f != nil {
		return refuse(f)
	}
	if head != r.head {
		return refuse(failf("head-moved", "HEAD moved to %s during cw publish", head))
	}
	if f := r.lockFree(ctx); f != nil {
		return refuse(f)
	}
	switch {
	case r.atHead:
		step.Status, step.Commit = "already-present", r.head
		return nil
	case len(r.rep.CommittedPaths) == 0:
		step.Status, step.Commit = "not-needed", r.req.expectHead
		return nil
	}
	if interrupted(ctx) {
		return refuse(failf("cancelled", "interrupted before the commit"))
	}
	if len(r.newPaths) > 0 {
		r.rep.IntentToAdd = slices.Sorted(slices.Values(r.newPaths))
		got := r.git.run(ctx, nil, nil, append([]string{"--literal-pathspecs", "add", "--intent-to-add", "--"}, r.rawPaths(r.rep.IntentToAdd)...)...)
		if !got.succeeded() {
			return r.commitFailed(ctx, got, "git add --intent-to-add")
		}
	}
	args := append([]string{"--literal-pathspecs", "commit", "--only", "--cleanup=verbatim", "--file", "-", "--"}, r.rawPaths(r.rep.CommittedPaths)...)
	got := r.git.run(ctx, r.req.message, nil, args...)
	if !got.succeeded() {
		return r.commitFailed(ctx, got, "git commit")
	}
	check, cancel := r.reconciling(ctx)
	defer cancel()
	head, f = r.git.head(check)
	if f != nil || head == "" {
		step.Status = "unknown"
		return failf("commit-unknown", "git commit succeeded but HEAD cannot be read")
	}
	ok, f := r.requestCommit(check, head)
	if f != nil {
		step.Status, step.Commit = "unknown", head
		return failf("commit-unknown", "git commit succeeded but its commit cannot be read: %s", f.detail)
	}
	if !ok {
		step.Status, step.Commit = "mismatch", head
		return failf("commit-mismatch", "commit %s is not the requested commit (a hook or a conversion changed its content or message); it was not amended, reset or pushed", head)
	}
	step.Status, step.Commit = "done", head
	return nil
}

// commitFailed classifies a git add --intent-to-add or git commit that did
// not succeed (R14.a to R14.c, R18.c).
func (r *run) commitFailed(ctx context.Context, got result, what string) *failure {
	step := &r.rep.Steps.Commit
	check, cancel := r.reconciling(ctx)
	defer cancel()
	locks, lockFailure := r.locks.listing(check)
	// The observed HEAD goes into every unknown outcome ("" when unreadable).
	head, headFailure := r.git.head(check)
	switch {
	case lockFailure != nil:
		step.Status, step.Commit = "unknown", head
		return failf("commit-unknown", "%s did not succeed (%s) and the lock files cannot be listed", what, got.why())
	case len(locks) > 0:
		r.rep.LocksLeft = locks
		step.Status, step.Commit = "unknown", head
		return failf("commit-unknown", "%s did not succeed (%s) and left Git lock files: %s", what, got.why(), strings.Join(locks, ", "))
	case headFailure != nil:
		step.Status = "unknown"
		return failf("commit-unknown", "%s did not succeed (%s) and HEAD cannot be read", what, got.why())
	case head == r.req.expectHead:
		step.Status, step.Detail = "failed", what+": "+got.why()
		if got.cancelled {
			return failf("cancelled", "interrupted during %s; HEAD is unchanged", what)
		}
		return failf("commit-failed", "%s did not succeed: %s", what, got.why())
	}
	step.Status, step.Commit = "unknown", head
	return failf("commit-unknown", "%s did not succeed (%s) but HEAD moved to %s", what, got.why(), head)
}

// pushStep is R15.
func (r *run) pushStep(ctx context.Context) *failure {
	step := &r.rep.Steps.Push
	commit := r.rep.Steps.Commit.Commit
	refuse := func(f *failure) *failure {
		step.Status, step.Detail = stepFailure(f), f.detail
		return f
	}
	if interrupted(ctx) {
		return refuse(failf("cancelled", "interrupted before the push"))
	}
	if f := r.reread(); f != nil {
		return refuse(f)
	}
	if f := r.lockFree(ctx); f != nil {
		return refuse(f)
	}
	if f := r.oneRepository(ctx); f != nil {
		return refuse(f)
	}
	remote, f := r.git.remoteRef(ctx, r.req.remote, r.req.branch)
	if f != nil {
		return refuse(f)
	}
	step.RemoteRef = remote
	if remote == commit {
		step.Status = "already-present"
		return nil
	}
	if f := r.remoteAdvance(ctx, remote, commit); f != nil {
		return refuse(f)
	}
	if interrupted(ctx) {
		return refuse(failf("cancelled", "interrupted before the push"))
	}
	got := r.git.run(ctx, nil, nil, "push", "--no-follow-tags", "--end-of-options", r.req.remote, commit+":refs/heads/"+r.req.branch)
	check, cancel := r.reconciling(ctx)
	defer cancel()
	if !got.succeeded() {
		step.Status, step.RemoteRef = "unknown", ""
		if observed, f := r.git.remoteRef(check, r.req.remote, r.req.branch); f == nil {
			step.RemoteRef = observed
		}
		r.locksAfter(check)
		return failf("push-unknown", "git push did not confirm its outcome: %s", got.why())
	}
	observed, f := r.git.remoteRef(check, r.req.remote, r.req.branch)
	if f != nil {
		step.Status, step.RemoteRef = "unknown", ""
		r.locksAfter(check)
		return failf("push-unknown", "git push succeeded but the remote branch cannot be read: %s", f.detail)
	}
	step.RemoteRef = observed
	if observed != commit {
		step.Status = "mismatch"
		return failf("push-mismatch", "after the push %s of %s is %s, not %s", r.req.branch, r.req.remote, observed, commit)
	}
	step.Status = "done"
	return nil
}

// locksAfter records the lock listing taken after an unknown push; a listing
// that cannot be taken leaves locks_left empty and the outcome unknown.
func (r *run) locksAfter(ctx context.Context) {
	if locks, f := r.locks.listing(ctx); f == nil {
		r.rep.LocksLeft = locks
	}
}

// prStep is R16.
func (r *run) prStep(ctx context.Context) *failure {
	if r.req.pr == PRNone {
		return nil
	}
	step := &r.rep.Steps.PR
	commit := r.rep.Steps.Commit.Commit
	refuse := func(f *failure) *failure {
		step.Status, step.Detail = stepFailure(f), f.detail
		return f
	}
	if interrupted(ctx) {
		return refuse(failf("cancelled", "interrupted before the pull request step"))
	}
	if f := r.reread(); f != nil {
		return refuse(f)
	}
	prs, f := r.gh.list(ctx, r.req.repo, r.req.branch)
	if f != nil {
		return refuse(f)
	}
	plan, reason := planPR(sameRepositoryOpen(prs, r.req.branch), r.req, commit)
	if reason != "" {
		return refuse(failf(reason, "%s", prRefusal(reason, r.req.branch)))
	}
	if plan.action == "present" {
		return r.readBack(ctx, plan.pr.Number, commit, "already-present", false)
	}
	if interrupted(ctx) {
		return refuse(failf("cancelled", "interrupted before the pull request step"))
	}
	var got result
	if plan.action == "create" {
		got = r.gh.create(ctx, r.req)
	} else {
		got = r.gh.edit(ctx, r.req, plan.pr.Number)
	}
	check, cancel := r.reconciling(ctx)
	defer cancel()
	if !got.succeeded() {
		r.prUnknown(check, plan.pr.Number)
		return failf("pr-unknown", "gh pr %s did not confirm its outcome: %s", plan.action, got.why())
	}
	number := plan.pr.Number
	if plan.action == "create" {
		found := false
		for read := 0; read < prPollReads && !found; read++ {
			if read > 0 && !pause(check, prPollPause) {
				break
			}
			prs, f := r.gh.list(check, r.req.repo, r.req.branch)
			if f != nil {
				continue
			}
			if open := sameRepositoryOpen(prs, r.req.branch); len(open) == 1 {
				number, found = open[0].Number, true
			}
		}
		if !found {
			r.prUnknown(check, 0)
			return failf("pr-unknown", "gh pr create succeeded but the pull request cannot be located")
		}
	}
	return r.readBack(check, number, commit, "done", true)
}

// readBack is R16.c. changed says whether this run created or edited the PR,
// so a failed read is an unknown outcome rather than a failed read.
func (r *run) readBack(ctx context.Context, number int, commit, status string, changed bool) *failure {
	step := &r.rep.Steps.PR
	for read := 1; ; read++ {
		pr, f := r.gh.view(ctx, r.req.repo, number)
		if f != nil {
			if !changed {
				step.Status, step.Detail = stepFailure(f), f.detail
				return f
			}
			r.prUnknown(ctx, number)
			return failf("pr-unknown", "the pull request cannot be read back: %s", f.detail)
		}
		step.Number, step.URL, step.HeadRefOID, step.Base, step.State = pr.Number, pr.URL, pr.HeadRefOID, pr.BaseRefName, pr.State
		differs, onlyHead := readbackDiffers(pr, r.req, commit)
		switch {
		case differs == "":
			step.Status = status
			return nil
		case !onlyHead || read == prPollReads:
			step.Status, step.Detail = "mismatch", differs
			return failf("pr-mismatch", "%s", differs)
		case pause(ctx, prPollPause):
			continue
		case !changed:
			step.Status = "skipped"
			return failf("cancelled", "interrupted while reading the pull request back")
		}
		r.prUnknown(ctx, number)
		return failf("pr-unknown", "the pull request's head commit was not confirmed in time")
	}
}

// prUnknown records an unknown PR outcome with what a read-only list finds.
func (r *run) prUnknown(ctx context.Context, number int) {
	step := &r.rep.Steps.PR
	*step = taskflow.PRStep{Status: "unknown"}
	prs, f := r.gh.list(ctx, r.req.repo, r.req.branch)
	if f != nil {
		return
	}
	for _, pr := range sameRepositoryOpen(prs, r.req.branch) {
		if number == 0 || pr.Number == number {
			step.Number, step.URL, step.HeadRefOID, step.Base, step.State = pr.Number, pr.URL, pr.HeadRefOID, pr.BaseRefName, pr.State
			return
		}
	}
}

// pause waits d, or reports false when ctx ends first.
func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// conclude fills status, reason and detail and writes the finished entry.
func (r *run) conclude(f *failure) {
	if f == nil {
		r.rep.Status, r.rep.Reason, r.rep.Detail = StatusPublished, "", ""
	} else {
		r.rep.Status, r.rep.Reason, r.rep.Detail = reasonStatus[f.reason], f.reason, oneLine(f.detail)
	}
	r.rep.Steps.Commit.Detail = oneLine(r.rep.Steps.Commit.Detail)
	r.rep.Steps.Push.Detail = oneLine(r.rep.Steps.Push.Detail)
	r.rep.Steps.PR.Detail = oneLine(r.rep.Steps.PR.Detail)
	if !r.started {
		return
	}
	code := r.rep.ExitCode()
	steps := r.rep.Steps
	path, err := r.task.Append(taskflow.PublicationEntry{TaskID: r.record.ID, RunID: r.runID, Kind: "finished", At: time.Now().UTC(),
		Status: r.rep.Status, Reason: r.rep.Reason, ExitCode: &code, Steps: &steps})
	if err != nil {
		detail := "write the finished publication entry: " + err.Error()
		if r.rep.Status == StatusPublished {
			r.rep.Status, r.rep.Reason, r.rep.Detail = StatusFailed, "record-write-failed", detail
		} else {
			r.rep.Detail = oneLine(r.rep.Detail + "; " + detail)
		}
		return
	}
	r.rep.Record = path
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }
