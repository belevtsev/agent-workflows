package projectctx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// MergeQuery names what InspectMerge resolves and compares.
type MergeQuery struct {
	Commit       string // revision naming the merge commit
	BaseRef      string // revision naming the base
	Head         string // full commit ID of the current HEAD
	BaselineHead string // full commit ID of the task's baseline HEAD, or ""
}

// MergeFacts are what Git reports about Commit.
type MergeFacts struct {
	Commit         string   // full ID Commit resolved to; "" when it names no commit
	BaseCommit     string   // full ID BaseRef resolved to; "" when it names no commit
	Parents        []string // Commit's parents in order; nil when Commit is "", empty for a root commit
	FromHead       bool     // Commit is Head or an ancestor of it
	FromBaseline   bool     // Commit is BaselineHead or an ancestor of it
	FromBase       bool     // Commit is BaseCommit or an ancestor of it
	ParentFromBase bool     // some parent after the first is BaseCommit or an ancestor of it
	Changed        []string // files Commit changed relative to its first parent, relative to the repository root (Snapshot.Root); nil when Commit has no parent
}

// InspectMerge runs read-only, bounded Git probes in the work tree root. It
// refuses a shallow repository with an error.
func InspectMerge(ctx context.Context, root string, query MergeQuery) (MergeFacts, error) {
	if !fullCommitID(query.Head) {
		return MergeFacts{}, errors.New("HEAD must be a full commit ID")
	}
	if query.BaselineHead != "" && !fullCommitID(query.BaselineHead) {
		return MergeFacts{}, errors.New("baseline HEAD must be empty or a full commit ID")
	}
	git := mergeGit{ctx: ctx, root: root}
	shallow, err := git.output("rev-parse", "--is-shallow-repository")
	if err != nil {
		return MergeFacts{}, err
	}
	switch strings.TrimSpace(shallow) {
	case "false":
	case "true":
		return MergeFacts{}, errors.New("repository is shallow, so Git cannot answer merge ancestry; run git fetch --unshallow and retry")
	default:
		return MergeFacts{}, errors.New("Git did not report whether the repository is shallow")
	}
	head, err := git.resolve(query.Head)
	if err != nil {
		return MergeFacts{}, err
	}
	if head != query.Head {
		return MergeFacts{}, fmt.Errorf("HEAD %s does not name a commit in the repository", query.Head)
	}
	var facts MergeFacts
	if facts.Commit, err = git.resolve(query.Commit); err != nil {
		return MergeFacts{}, err
	}
	if facts.BaseCommit, err = git.resolve(query.BaseRef); err != nil {
		return MergeFacts{}, err
	}
	if facts.Commit == "" {
		return facts, nil
	}
	if facts.Parents, err = git.parents(facts.Commit); err != nil {
		return MergeFacts{}, err
	}
	if facts.FromHead, err = git.isAncestor(facts.Commit, query.Head); err != nil {
		return MergeFacts{}, err
	}
	if query.BaselineHead != "" {
		if facts.FromBaseline, err = git.isAncestor(facts.Commit, query.BaselineHead); err != nil {
			return MergeFacts{}, err
		}
	}
	if facts.BaseCommit != "" {
		if facts.FromBase, err = git.isAncestor(facts.Commit, facts.BaseCommit); err != nil {
			return MergeFacts{}, err
		}
		for _, parent := range facts.Parents[min(1, len(facts.Parents)):] {
			if facts.ParentFromBase, err = git.isAncestor(parent, facts.BaseCommit); err != nil {
				return MergeFacts{}, err
			}
			if facts.ParentFromBase {
				break
			}
		}
	}
	if len(facts.Parents) > 0 {
		if facts.Changed, err = git.changed(facts.Parents[0], facts.Commit); err != nil {
			return MergeFacts{}, err
		}
	}
	return facts, nil
}

// fullCommitID reports whether value is a full SHA-1 or SHA-256 object ID in
// lowercase hexadecimal.
func fullCommitID(value string) bool {
	return (len(value) == 40 || len(value) == 64) && !strings.ContainsFunc(value, func(r rune) bool { return !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') })
}

// revisionShape reports whether value may be passed to Git as a revision:
// 1 to 256 bytes, not starting with -, without whitespace or control
// characters.
func revisionShape(value string) bool {
	return value != "" && len(value) <= 256 && !strings.HasPrefix(value, "-") && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r < 0x20 || r == 0x7f })
}

// mergeGit runs Git probes in one work tree. Unlike capture's probes it
// reports Git's exit status, and each caller decides which statuses are
// facts; every other status is an error.
type mergeGit struct {
	ctx  context.Context
	root string
}

// run returns Git's output and exit status. It returns an error, never a
// status, when Git could not run, did not finish cleanly (timeout, signal,
// the caller's cancellation) or wrote more than maxGitOutput.
func (git mergeGit) run(args ...string) (string, int, error) {
	probe := strings.Join(args, " ")
	probeCtx, cancel := context.WithTimeout(git.ctx, gitProbeBudget)
	defer cancel()
	// Replace refs and grafts can rewrite parentage, so neither is read.
	environment := append(minimalEnvironment(os.Getenv), "GIT_NO_REPLACE_OBJECTS=1", "GIT_GRAFT_FILE="+filepath.Join(os.DevNull, "cw-no-grafts"))
	gitArgs := []string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-C", git.root}
	result, runErr := executeBounded(probeCtx, Command{Executable: "git", Args: append(gitArgs, args...), Dir: git.root, Env: environment, WaitDelay: gitProbeWaitDelay}, maxGitOutput)
	if runErr != nil {
		if git.ctx.Err() != nil {
			return "", 0, fmt.Errorf("Git inspection stopped by the caller (git %s): %w", probe, git.ctx.Err())
		}
		if probeCtx.Err() != nil {
			return "", 0, fmt.Errorf("Git inspection timed out after %s (git %s)", gitProbeBudget, probe)
		}
		exit, isExit := errors.AsType[*exec.ExitError](runErr)
		if !isExit || !exit.Exited() {
			return "", 0, fmt.Errorf("Git inspection did not finish cleanly (git %s): %w", probe, runErr)
		}
		return string(result.Stdout), exit.ExitCode(), nil
	}
	if result.Truncated {
		return "", 0, fmt.Errorf("Git output exceeded the inspection output limit (git %s)", probe)
	}
	return string(result.Stdout), 0, nil
}

// output runs a probe whose only fact is success.
func (git mergeGit) output(args ...string) (string, error) {
	out, status, err := git.run(args...)
	if err == nil && status != 0 {
		err = fmt.Errorf("git %s exited with status %d", strings.Join(args, " "), status)
	}
	return out, err
}

// resolve returns the full ID of the commit revision names, or "" when it
// names none: a revision of an invalid shape, or Git's exit status 1.
func (git mergeGit) resolve(revision string) (string, error) {
	if !revisionShape(revision) {
		return "", nil
	}
	args := []string{"rev-parse", "--verify", "--quiet", revision + "^{commit}"}
	out, status, err := git.run(args...)
	switch {
	case err != nil:
		return "", err
	case status == 1:
		return "", nil
	case status != 0:
		return "", fmt.Errorf("git %s exited with status %d", strings.Join(args, " "), status)
	}
	id := strings.TrimSpace(out)
	if !fullCommitID(id) {
		return "", fmt.Errorf("git %s did not print a full commit ID", strings.Join(args, " "))
	}
	return id, nil
}

// parents returns commit's parents in order, empty for a root commit.
func (git mergeGit) parents(commit string) ([]string, error) {
	out, err := git.output("rev-list", "--parents", "--max-count=1", commit)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 || fields[0] != commit || slices.ContainsFunc(fields, func(id string) bool { return !fullCommitID(id) }) {
		return nil, fmt.Errorf("git rev-list did not list the parents of %s", commit)
	}
	return append([]string{}, fields[1:]...), nil
}

// isAncestor reports whether ancestor is descendant or an ancestor of it.
func (git mergeGit) isAncestor(ancestor, descendant string) (bool, error) {
	args := []string{"merge-base", "--is-ancestor", ancestor, descendant}
	_, status, err := git.run(args...)
	switch {
	case err != nil:
		return false, err
	case status == 0:
		return true, nil
	case status == 1:
		return false, nil
	}
	return false, fmt.Errorf("git %s exited with status %d", strings.Join(args, " "), status)
}

// changed returns the files commit changed relative to parent, spelled as the
// manifest spells them, sorted and listed once; renames count as a deletion
// and an addition.
func (git mergeGit) changed(parent, commit string) ([]string, error) {
	out, err := git.output("diff-tree", "-r", "-z", "--name-only", "--no-renames", parent, commit)
	if err != nil {
		return nil, err
	}
	changed := []string{}
	for name := range strings.SplitSeq(out, "\x00") {
		if name != "" {
			changed = append(changed, manifestPath(name))
		}
	}
	slices.Sort(changed)
	return slices.Compact(changed), nil
}
