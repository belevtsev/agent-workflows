package publish

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"strconv"
)

// gh runs the GitHub CLI with exactly the argument vectors of contract
// section 8; value flags use the --flag=value form.
type gh struct{ tool }

const prFields = "--json=number,url,state,headRefName,headRefOid,baseRefName,title,body,isCrossRepository"

// storedPR is a PR as gh prints it; every field must be present.
type storedPR struct {
	Number            *int    `json:"number"`
	URL               *string `json:"url"`
	State             *string `json:"state"`
	HeadRefName       *string `json:"headRefName"`
	HeadRefOID        *string `json:"headRefOid"`
	BaseRefName       *string `json:"baseRefName"`
	Title             *string `json:"title"`
	Body              *string `json:"body"`
	IsCrossRepository *bool   `json:"isCrossRepository"`
}

func (s storedPR) parse() (pullRequest, error) {
	if s.Number == nil || s.URL == nil || s.State == nil || s.HeadRefName == nil || s.HeadRefOID == nil ||
		s.BaseRefName == nil || s.Title == nil || s.Body == nil || s.IsCrossRepository == nil {
		return pullRequest{}, errors.New("gh output lacks a field")
	}
	return pullRequest{Number: *s.Number, URL: *s.URL, State: *s.State, HeadRefName: *s.HeadRefName, HeadRefOID: *s.HeadRefOID,
		BaseRefName: *s.BaseRefName, Title: *s.Title, Body: *s.Body, IsCrossRepository: *s.IsCrossRepository}, nil
}

func (g gh) read(ctx context.Context, what string, value any, args ...string) *failure {
	got := g.run(ctx, nil, nil, args...)
	if !got.succeeded() || got.overflow {
		return readFailure(got, what)
	}
	if err := json.Unmarshal(got.stdout, value); err != nil {
		return failf("read-failed", "%s: gh output does not decode: %v", what, err)
	}
	return nil
}

// repoURLs returns the url and sshUrl gh reports for repo.
func (g gh) repoURLs(ctx context.Context, repo string) ([]string, *failure) {
	var stored struct {
		URL    *string `json:"url"`
		SSHURL *string `json:"sshUrl"`
	}
	if f := g.read(ctx, "reading "+repo, &stored, "repo", "view", repo, "--json=url,sshUrl"); f != nil {
		return nil, f
	}
	if stored.URL == nil || stored.SSHURL == nil {
		return nil, failf("read-failed", "reading %s: gh output lacks a field", repo)
	}
	return []string{*stored.URL, *stored.SSHURL}, nil
}

// list returns the open PRs gh lists for branch in repo.
func (g gh) list(ctx context.Context, repo, branch string) ([]pullRequest, *failure) {
	var stored []storedPR
	if f := g.read(ctx, "listing pull requests", &stored, "pr", "list", "--repo="+repo, "--head="+branch, "--state=open", "--limit=100", prFields); f != nil {
		return nil, f
	}
	prs := []pullRequest{}
	for _, s := range stored {
		pr, err := s.parse()
		if err != nil {
			return nil, failf("read-failed", "listing pull requests: %v", err)
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

// view reads one PR back.
func (g gh) view(ctx context.Context, repo string, number int) (pullRequest, *failure) {
	var stored storedPR
	if f := g.read(ctx, "reading the pull request", &stored, "pr", "view", strconv.Itoa(number), "--repo="+repo, prFields); f != nil {
		return pullRequest{}, f
	}
	pr, err := stored.parse()
	if err != nil {
		return pullRequest{}, failf("read-failed", "reading the pull request: %v", err)
	}
	return pr, nil
}

// create runs gh pr create with the body on stdin.
func (g gh) create(ctx context.Context, req Request) result {
	return g.run(ctx, req.body, nil, "pr", "create", "--repo="+req.repo, "--base="+req.base, "--head="+req.branch, "--title="+req.title, "--body-file=-")
}

// edit runs gh pr edit with the body on stdin.
func (g gh) edit(ctx context.Context, req Request, number int) result {
	return g.run(ctx, req.body, nil, "pr", "edit", strconv.Itoa(number), "--repo="+req.repo, "--base="+req.base, "--title="+req.title, "--body-file=-")
}
