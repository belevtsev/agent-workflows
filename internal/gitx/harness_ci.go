package gitx

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

// PullRequestChecks reads status metadata for a validated GitHub PR link.
func (r *Repo) PullRequestChecks(ctx context.Context, link string, limit int) ([]byte, error) {
	const fields = `{url,state,headRefOid,statusCheckRollup:[(.statusCheckRollup // [])[] | {__typename,name,status,conclusion,context,state}]}`
	result := proc.Run(ctx, proc.Spec{
		Path: "gh", Args: []string{"pr", "view", link, "--json", "url,state,headRefOid,statusCheckRollup", "--jq", fields},
		Dir: r.Root, Env: append(os.Environ(), "GH_PROMPT_DISABLED=1"), Timeout: 15 * time.Second, StdoutLimit: limit,
	})
	if result.Err != nil || result.Stopped || result.Overflow || result.Cleanup != nil || result.Code != 0 {
		return nil, errors.New("could not read PR check metadata with gh; check gh authentication or supply the metadata with --stdin")
	}
	return result.Stdout, nil
}
