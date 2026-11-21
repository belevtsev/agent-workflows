package gitx

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

// CommitTarget names the full ref a commit would advance. On a detached
// checkout Git updates HEAD itself. An unborn branch already has a target.
func (r *Repo) CommitTarget(ctx context.Context) (string, error) {
	ref, err := r.git(ctx, nil, "symbolic-ref", "--quiet", "HEAD")
	var gitErr *Error
	if errors.As(err, &gitErr) && gitErr.Code == 1 {
		return "HEAD", nil
	}
	return ref, err
}

// DeliveredAt checks that the expected destination resolves to the observed
// commit. Detached HEAD counts only while this checkout remains detached.
func (r *Repo) DeliveredAt(ctx context.Context, target, head string) (bool, error) {
	if target == "HEAD" {
		current, err := r.CommitTarget(ctx)
		return current == target, err
	}
	commit, found, err := r.CommitOf(ctx, target)
	return found && commit == head, err
}

type CommittedCandidate struct {
	Commit string
	Tree   string
}

// CommitStaged runs hooks against a copy of the verified index. If a hook
// changes the commit's tree, the real index can adopt that tree only while it
// still matches the original copy. A returned commit may need fresh checks.
func (r *Repo) CommitStaged(ctx context.Context, tmp, tree, head, mergeHeads, target, message string) (CommittedCandidate, error) {
	path, cleanup, err := r.copyIndex(tmp)
	if err != nil {
		return CommittedCandidate{}, err
	}
	defer cleanup()
	original, err := readIndexBytes(path)
	if err != nil {
		return CommittedCandidate{}, err
	}
	env := []string{"GIT_INDEX_FILE=" + path}
	if err := r.checkStagedCommit(ctx, env, tree, head, mergeHeads, target); err != nil {
		return CommittedCandidate{}, err
	}
	if _, err := r.git(ctx, env, "commit", "-m", message); err != nil {
		return CommittedCandidate{}, err
	}
	after, err := r.CommitTarget(ctx)
	if err != nil {
		return CommittedCandidate{}, fmt.Errorf("inspect commit target: %w", err)
	}
	if after != target {
		return CommittedCandidate{}, fmt.Errorf("checkout changed during commit from %s to %s; inspect both destinations before continuing", target, after)
	}
	commit, found, err := r.CommitOf(ctx, target)
	if err != nil {
		return CommittedCandidate{}, err
	}
	if !found || commit == head {
		return CommittedCandidate{}, fmt.Errorf("could not observe the new commit on %s", target)
	}
	actualTree, parents, err := r.CommitContents(ctx, commit)
	if err != nil {
		return CommittedCandidate{}, fmt.Errorf("inspect new commit: %w", err)
	}
	if parents != strings.TrimSpace(head+" "+mergeHeads) {
		return CommittedCandidate{}, fmt.Errorf("the commit was created but its parents changed; inspect HEAD before continuing")
	}
	result := CommittedCandidate{Commit: commit, Tree: actualTree}
	if actualTree == tree {
		return result, nil
	}
	err = r.reconcileCommitIndex(ctx, path, original, tree, target, result)
	return result, errors.Join(fmt.Errorf("the committed tree changed. Inspect %s and run task next", commit), err)
}

func (r *Repo) checkStagedCommit(ctx context.Context, env []string, tree, head, mergeHeads, target string) error {
	staged, err := r.git(ctx, env, "write-tree")
	if err != nil {
		return err
	}
	current, _, err := r.CommitOf(ctx, "HEAD")
	if err != nil {
		return err
	}
	merging, err := r.MergeParents(ctx)
	if err != nil {
		return err
	}
	before, err := r.CommitTarget(ctx)
	if err != nil {
		return err
	}
	if before != target {
		return fmt.Errorf("commit target changed from %s to %s; inspect the checkout and run task next", target, before)
	}
	if current != head || merging != mergeHeads || staged != tree {
		return fmt.Errorf("HEAD or staged candidate changed before commit")
	}
	return nil
}

func readIndexBytes(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	return data, err
}

func (r *Repo) reconcileCommitIndex(ctx context.Context, copyPath string, original []byte, before, target string, result CommittedCandidate) error {
	// Rebuild from the original index so a post-commit hook can never add
	// uncommitted entries to the replacement.
	env := []string{"GIT_INDEX_FILE=" + copyPath}
	if original == nil {
		if _, err := r.git(ctx, env, "read-tree", "-i", before); err != nil {
			return err
		}
	} else if err := os.WriteFile(copyPath, original, 0o600); err != nil {
		return err
	}
	if _, err := r.git(ctx, env, "read-tree", "-i", "-m", before, result.Tree); err != nil {
		return err
	}
	updated, err := r.git(ctx, env, "write-tree")
	if err != nil || updated != result.Tree {
		return errors.Join(err, errors.New("could not recreate the committed index"))
	}
	replacement, err := os.ReadFile(copyPath)
	if err != nil {
		return err
	}
	prepared, err := os.Stat(copyPath)
	if err != nil {
		return err
	}
	return r.replaceCommittedIndex(ctx, original, replacement, prepared.ModTime(), target, result.Commit)
}

func (r *Repo) replaceCommittedIndex(ctx context.Context, original, replacement []byte, modified time.Time, target, commit string) error {
	index := r.indexPath()
	lock := index + ".lock"
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return fmt.Errorf("could not lock the index, so staging was preserved: %w", err)
	}
	replaced := false
	defer func() {
		_ = f.Close()
		if !replaced {
			_ = os.Remove(lock)
		}
	}()
	info, err := os.Lstat(index)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if info != nil && !info.Mode().IsRegular() {
		return errors.New("the real index is not a regular file, so staging was preserved")
	}
	current, err := readIndexBytes(index)
	if err != nil {
		return err
	}
	if !bytes.Equal(original, current) {
		return errors.New("the real index changed during commit, so staging was preserved")
	}
	checkedOut, err := r.CommitTarget(ctx)
	if err != nil {
		return err
	}
	head, _, err := r.CommitOf(ctx, target)
	if err != nil || head != commit || checkedOut != target {
		return errors.Join(err, errors.New("the target moved after commit, so staging was preserved"))
	}
	if info != nil {
		if err := f.Chmod(info.Mode().Perm()); err != nil {
			return err
		}
	}
	if _, err := f.Write(replacement); err != nil {
		return err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	// Keep Git's index timestamp so it rechecks potentially changed worktree files.
	if err := os.Chtimes(lock, modified, modified); err != nil {
		return err
	}
	if err := os.Rename(lock, index); err != nil {
		return err
	}
	replaced = true
	return nil
}

// MergeParents returns the ordered extra parents of a pending merge in this
// worktree. An annotated tag in MERGE_HEAD names its peeled commit as parent.
func (r *Repo) MergeParents(ctx context.Context) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.GitDir, "MERGE_HEAD"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	heads := strings.Fields(string(data))
	if len(heads) == 0 {
		return "", fmt.Errorf("empty MERGE_HEAD")
	}
	for i, id := range heads {
		if _, err := hex.DecodeString(id); err != nil || len(id) != 40 && len(id) != 64 {
			return "", fmt.Errorf("invalid object ID in MERGE_HEAD")
		}
		commit, found, err := r.CommitOf(ctx, id)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf("missing merge parent %s", id)
		}
		heads[i] = commit
	}
	return strings.Join(heads, " "), nil
}

// CommitContents reads the tree and all parent IDs in Git order.
func (r *Repo) CommitContents(ctx context.Context, rev string) (string, string, error) {
	out, err := r.git(ctx, nil, "show", "-s", "--no-notes", "--no-show-signature", "--format=%T %P", rev, "--")
	tree, parents, _ := strings.Cut(out, " ")
	return tree, parents, err
}

// CommitOfTree finds the commit that put tree on parents, at tip or among its
// first-parent ancestors. It names a plain git commit of a verified candidate,
// also after later commits moved the branch on. It reads at most 100 commits.
func (r *Repo) CommitOfTree(ctx context.Context, tip, tree, parents string) (string, error) {
	out, err := r.git(ctx, nil, "log", "--first-parent", "-n", "100", "--no-notes", "--no-show-signature", "--format=%H %T %P", tip, "--")
	for _, line := range strings.Split(out, "\n") {
		if commit, rest, _ := strings.Cut(strings.TrimSpace(line), " "); rest == strings.TrimSpace(tree+" "+parents) {
			return commit, err
		}
	}
	return "", err
}

// ConfirmPublication checks the verified commit on every configured push destination.
// A requested PR must originate from one of those same destinations. It must be in one of
// their repositories too, or in that of the remote named before the base branch as REMOTE:BRANCH.
func (r *Repo) ConfirmPublication(ctx context.Context, remote, branch, head, base, link string) error {
	ref := "refs/heads/" + branch
	if _, err := r.git(ctx, nil, "check-ref-format", ref); err != nil {
		return err
	}
	destinations, err := r.git(ctx, nil, "remote", "get-url", "--push", "--all", "--", remote)
	if err != nil {
		return fmt.Errorf("unknown configured remote %q", remote)
	}
	var sources []string
	for destination := range strings.SplitSeq(destinations, "\n") {
		out, err := r.git(ctx, nil, "ls-remote", "--exit-code", "--heads", "--", destination, ref)
		if err != nil {
			return fmt.Errorf("could not confirm %s on every push destination of %s", ref, remote)
		}
		// Git patterns also match at slash boundaries. Compare complete ref lines.
		if !slices.Contains(strings.Split(out, "\n"), head+"\t"+ref) {
			return fmt.Errorf("remote branch %s/%s does not point at the verified commit on every push destination", remote, branch)
		}
		if owner, repo, ok := pullRequestSource(destination, link); ok {
			sources = append(sources, owner+"/"+repo)
		}
	}
	if base == "" {
		return nil
	}
	if len(sources) == 0 {
		return fmt.Errorf("cannot match pull request host and configured GitHub push remote %s", remote)
	}
	bases := destinations
	if target, name, named := strings.Cut(base, ":"); named {
		if bases, err = r.git(ctx, nil, "remote", "get-url", "--all", "--", target); err != nil {
			return fmt.Errorf("unknown configured remote %q", target)
		}
		base = name
	}
	if !slices.ContainsFunc(strings.Split(bases, "\n"), func(url string) bool { return pullRequestIn(url, link) }) {
		return fmt.Errorf("pull request is not in the pinned base repository; pin the remote of another one as REMOTE:BRANCH")
	}
	return r.confirmPullRequest(ctx, link, branch, base, head, sources)
}

// pullRequestIn says whether the pull request link is in the repository of a remote URL.
func pullRequestIn(url, link string) bool {
	owner, repo, ok := pullRequestSource(url, link)
	parts := strings.Split(link, "/") // https://HOST/OWNER/REPO/pull/NUMBER
	return ok && len(parts) == 7 && strings.EqualFold(owner+"/"+repo, parts[3]+"/"+parts[4])
}

// Accept common GitHub remote spellings without sending credentials to gh.
var gitHubRemote = regexp.MustCompile(`^(?:(https?|git|ssh)://(?:[^/@]+@)?|[^/@:]+@)([^/:]+)(?::([0-9]+))?[/:]([^/]+)/([^/]+)/?$`)

func (r *Repo) confirmPullRequest(ctx context.Context, link, branch, base, head string, sources []string) error {
	result := proc.Run(ctx, proc.Spec{
		Path: "gh", Args: []string{"pr", "view", link, "--json", "url,state,headRefOid,headRefName,baseRefName,headRepository,headRepositoryOwner"},
		Dir: r.Root, Env: os.Environ(), StdoutLimit: 16 << 10,
	})
	if result.Err != nil {
		return fmt.Errorf("inspect published pull request with gh: %w", result.Err)
	}
	if result.Stopped || result.Overflow || result.Cleanup != nil || result.Code != 0 {
		return fmt.Errorf("GitHub CLI did not return a complete pull request record")
	}
	return checkHarnessPR(result.Stdout, link, branch, base, head, sources)
}

func pullRequestSource(remote, link string) (string, string, bool) {
	match := gitHubRemote.FindStringSubmatch(remote)
	if match == nil {
		return "", "", false
	}
	host := match[2]
	// SSH transport ports are independent of the HTTPS web endpoint.
	if scheme, port := match[1], match[3]; port != "" && (scheme == "https" && port != "443" || scheme == "http" && port != "80") {
		host += ":" + port
	}
	web, _, _ := strings.Cut(strings.TrimPrefix(link, "https://"), "/")
	return match[4], strings.TrimSuffix(match[5], ".git"), strings.EqualFold(host, strings.TrimSuffix(web, ":443"))
}

func checkHarnessPR(data []byte, link, branch, base, head string, sources []string) error {
	var pr struct {
		URL, State, HeadRefOid, HeadRefName, BaseRefName string
		HeadRepository                                   struct{ Name string }
		HeadRepositoryOwner                              struct{ Login string }
	}
	if err := json.Unmarshal(data, &pr); err != nil {
		return err
	}
	source := pr.HeadRepositoryOwner.Login + "/" + pr.HeadRepository.Name
	matched := slices.ContainsFunc(sources, func(repo string) bool { return strings.EqualFold(repo, source) })
	if !matched || pr.URL != link || pr.State != "OPEN" || pr.HeadRefOid != head || pr.HeadRefName != branch || pr.BaseRefName != base {
		return fmt.Errorf("pull request differs from the requested open branch, base or verified commit")
	}
	return nil
}
