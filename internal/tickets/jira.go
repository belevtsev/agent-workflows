package tickets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

var (
	jiraSite  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?\.atlassian\.net$`)
	jiraIssue = regexp.MustCompile(`^/browse/[A-Z][A-Z0-9_]*-[1-9][0-9]*$`)
)

func readJira(ctx context.Context, source harness.TicketSource) (harness.TicketSource, string, error) {
	site, key, ok := strings.Cut(source.URL, "/browse/")
	if !ok {
		return source, "", fmt.Errorf("invalid Jira issue URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, site+"/rest/api/2/issue/"+key+"?fields=summary,description,updated", nil)
	if err != nil {
		return source, "", err
	}
	if err := jiraAuth(req, site); err != nil {
		return source, "", err
	}
	req.Header.Set("Accept", "application/json")
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return source, "", fmt.Errorf("read Jira issue: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return source, "", fmt.Errorf("read Jira issue %s: HTTP %d; check the site, issue access and Jira credentials", key, resp.StatusCode)
	}
	const maxJSON = 6*maxTicketBytes + 4096
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxJSON+1))
	if err != nil {
		return source, "", err
	}
	if len(data) > maxJSON || !utf8.Valid(data) {
		return source, "", fmt.Errorf("oversized or invalid Jira response")
	}
	var issue struct {
		Key    string
		Fields struct{ Summary, Description, Updated string }
	}
	if err := json.Unmarshal(data, &issue); err != nil {
		return source, "", fmt.Errorf("read Jira issue JSON: %w", err)
	}
	if issue.Key != key || strings.TrimSpace(issue.Fields.Summary) == "" {
		return source, "", fmt.Errorf("Jira did not return the requested issue %s", key)
	}
	source.Title, source.UpdatedAt = issue.Fields.Summary, issue.Fields.Updated
	return source, issue.Fields.Summary + "\n\n" + issue.Fields.Description, nil
}

func jiraAuth(req *http.Request, site string) error {
	email, token := os.Getenv("KITSU_JIRA_EMAIL"), os.Getenv("KITSU_JIRA_API_TOKEN")
	if email == "" && token == "" {
		return nil
	}
	if email == "" || token == "" || !strings.EqualFold(strings.TrimSuffix(os.Getenv("KITSU_JIRA_SITE"), "/"), site) {
		return fmt.Errorf("set KITSU_JIRA_SITE to the issue's HTTPS origin and provide both KITSU_JIRA_EMAIL and KITSU_JIRA_API_TOKEN")
	}
	req.SetBasicAuth(email, token)
	return nil
}
