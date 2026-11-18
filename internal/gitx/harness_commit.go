package gitx

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

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

// CommitStaged uses a private copy of the verified index. A concurrent staging
// operation cannot put unrelated content in this commit. Git runs its normal
// commit hooks against that index and the user's real index stays intact.
func (r *Repo) CommitStaged(ctx context.Context, tmp, tree, head, mergeHeads, target, message string) error {
	path, cleanup, err := r.copyIndex(tmp)
	if err != nil {
		return err
	}
	defer cleanup()
	env := []string{"GIT_INDEX_FILE=" + path}
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
	if _, err := r.git(ctx, env, "commit", "-m", message); err != nil {
		return err
	}
	after, err := r.CommitTarget(ctx)
	if err != nil {
		return fmt.Errorf("inspect commit target: %w", err)
	}
	if after != target {
		return fmt.Errorf("checkout changed during commit from %s to %s; inspect both destinations before continuing", target, after)
	}
	actualTree, parents, err := r.CommitContents(ctx, target)
	if err != nil {
		return fmt.Errorf("inspect new commit: %w", err)
	}
	if actualTree != tree || parents != strings.TrimSpace(head+" "+mergeHeads) {
		return fmt.Errorf("the commit was created but its tree or parents changed; inspect HEAD and run task next before continuing")
	}
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

// ConfirmPublication checks the verified commit on every configured push destination.
// A requested PR must originate from one of those same destinations.
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
	return r.confirmPullRequest(ctx, link, branch, base, head, sources)
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
