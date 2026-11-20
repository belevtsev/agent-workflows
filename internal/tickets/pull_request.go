package tickets

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

const MaxPullRequestBytes = 256 << 10

var pullRequestPath = regexp.MustCompile(`^/[A-Za-z0-9-]+/[A-Za-z0-9_.-]*[A-Za-z0-9_-][A-Za-z0-9_.-]*/pull/[1-9][0-9]*$`)
var pullRequestHead = regexp.MustCompile(`^[0-9a-f]{40}$`)

// CanonicalPullRequestLink accepts a shareable GitHub PR URL with no query or fragment.
func CanonicalPullRequestLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") ||
		u.RawPath != "" || strings.ContainsAny(raw, "?#") {
		return ""
	}
	path := strings.TrimSuffix(u.Path, "/")
	if len(path) > 512 || !pullRequestPath.MatchString(path) {
		return ""
	}
	return "https://github.com" + path
}

func FailedCheckLocation(from, localCheck, job string) (harness.TicketSource, error) {
	link := CanonicalPullRequestLink(from)
	if link == "" || localCheck == "" {
		return harness.TicketSource{}, errors.New("supply a GitHub pull request URL with --from and an approved local check ID with --check")
	}
	if job != "" && !validCheckName(job) {
		return harness.TicketSource{}, errors.New("--job must be a nonempty check name of at most 256 bytes without control characters")
	}
	return harness.TicketSource{Kind: "github-pr-check", URL: link, Check: &harness.PullRequestCheck{Name: job, LocalCheck: localCheck}}, nil
}

func validCheckName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 256 && strings.IndexFunc(name, unicode.IsControl) < 0
}

// ReadFailedCheck records metadata only. The check command comes from the
// developer's approved declaration.
func ReadFailedCheck(source harness.TicketSource, input io.Reader) (harness.TicketSource, string, error) {
	data, err := io.ReadAll(io.LimitReader(input, MaxPullRequestBytes+1))
	if err != nil {
		return source, "", err
	}
	if len(data) > MaxPullRequestBytes {
		return source, "", errors.New("PR check metadata exceeds 256 KiB")
	}
	var pr struct {
		URL, State, HeadRefOid string
		StatusCheckRollup      []struct {
			TypeName                 string `json:"__typename"`
			Name, Status, Conclusion string
			Context, State           string
		}
	}
	if err := json.Unmarshal(data, &pr); err != nil {
		return source, "", errors.New("expected GitHub PR metadata with url, state, headRefOid and statusCheckRollup")
	}
	if !strings.EqualFold(pr.URL, source.URL) || pr.State != "OPEN" || !pullRequestHead.MatchString(pr.HeadRefOid) {
		return source, "", errors.New("check metadata must describe the requested open PR and its head commit")
	}
	var failed []string
	for _, check := range pr.StatusCheckRollup {
		var name string
		switch {
		case check.TypeName == "StatusContext" && (check.State == "FAILURE" || check.State == "ERROR"):
			name = check.Context
		case (check.TypeName == "" || check.TypeName == "CheckRun") && check.Status == "COMPLETED" && check.Conclusion == "FAILURE":
			name = check.Name
		}
		if validCheckName(name) && (source.Check.Name == "" || source.Check.Name == name) {
			failed = append(failed, name)
		}
	}
	switch len(failed) {
	case 0:
		return source, "", errors.New("no completed failing check matched the PR and job selection")
	case 1:
	default:
		return source, "", fmt.Errorf("multiple failing checks matched the PR. Select an unambiguous --job from %q", failed[:min(len(failed), 5)])
	}
	check := *source.Check
	check.Name, check.Head = failed[0], pr.HeadRefOid
	source.Check = &check
	source.Title = "Failed PR check: " + check.Name
	request := fmt.Sprintf("Repair the failure reproduced by local check %q.\n\nPR: <%s>\nFailed check: %q\nPR head: %s\n",
		check.LocalCheck, source.URL, check.Name, check.Head)
	source.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(request)))
	return source, request, nil
}
