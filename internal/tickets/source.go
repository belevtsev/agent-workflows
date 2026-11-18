// Package tickets imports source material for bug delivery tasks.
package tickets

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/proc"
)

const maxTicketBytes = 512 << 10

var issuePath = regexp.MustCompile(`^/[A-Za-z0-9-]+/[A-Za-z0-9_.-]+/issues/[1-9][0-9]*$`)

// Location normalizes the identity without reading the ticket, so a task can reopen offline.
func Location(from string, supplied bool) (harness.TicketSource, error) {
	if supplied {
		return suppliedLocation(from)
	}
	if strings.Contains(from, "://") {
		u, err := url.Parse(from)
		if err != nil || u.Scheme != "https" || u.User != nil || u.RawPath != "" {
			return harness.TicketSource{}, fmt.Errorf("invalid ticket URL")
		}
		path := strings.TrimSuffix(u.Path, "/")
		host := strings.ToLower(u.Host)
		switch {
		case host == "github.com" && u.RawQuery == "" && u.Fragment == "" && issuePath.MatchString(path):
			return harness.TicketSource{Kind: "github", URL: "https://" + host + path}, nil
		case jiraCloudHost.MatchString(host) && jiraBrowsePath.MatchString(path):
			return harness.TicketSource{Kind: "jira", URL: "https://" + host + path}, nil
		default:
			return harness.TicketSource{}, fmt.Errorf("no built-in reader for this ticket URL; read it with your tracker and pass its Markdown with --stdin")
		}
	}
	path, err := filepath.Abs(from)
	if err != nil {
		return harness.TicketSource{}, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
	default:
		return harness.TicketSource{}, fmt.Errorf("--from requires a ticket URL or a .md or .markdown file")
	}
	name := filepath.ToSlash(path)
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	return harness.TicketSource{Kind: "markdown", URL: (&url.URL{Scheme: "file", Path: name}).String()}, nil
}

// A supplied source is a reference for the receipt. Kitsu never dereferences it.
func suppliedLocation(from string) (harness.TicketSource, error) {
	u, err := url.Parse(from)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mcp") {
		return harness.TicketSource{}, fmt.Errorf("--stdin requires a tracker URL or mcp://server/ticket-id source")
	}
	return harness.TicketSource{Kind: "supplied", URL: u.String()}, nil
}

// Read captures the source text and a digest for the immutable task request.
func Read(ctx context.Context, from string, source harness.TicketSource, input io.Reader) (harness.TicketSource, string, error) {
	var text string
	var err error
	switch source.Kind {
	case "github":
		source, text, err = readGitHubIssue(ctx, source)
	case "jira":
		source, text, err = readJiraCloudIssue(ctx, source)
	case "markdown", "supplied":
		if source.Kind == "markdown" {
			f, err := openMarkdown(from)
			if err != nil {
				return source, "", err
			}
			defer func() { _ = f.Close() }()
			input = f
			source.Title = filepath.Base(from)
		}
		if input == nil {
			return source, "", fmt.Errorf("missing ticket text on stdin")
		}
		b, err := io.ReadAll(io.LimitReader(input, maxTicketBytes+1))
		if err != nil {
			return source, "", err
		}
		text = string(b)
		if source.Kind == "supplied" {
			title, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
			source.Title = strings.TrimSpace(strings.TrimLeft(title, "#"))
			if source.Title == "" || len(source.Title) > 256 {
				source.Title = source.URL
			}
		}
	default:
		return source, "", fmt.Errorf("unsupported ticket source %q", source.Kind)
	}
	if err != nil {
		return source, "", err
	}
	if len(text) > maxTicketBytes || strings.TrimSpace(text) == "" || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return source, "", fmt.Errorf("ticket must contain nonempty UTF-8 text of at most %d bytes", maxTicketBytes)
	}
	source.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	return source, text, nil
}

func openMarkdown(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Markdown ticket must be a regular file")
	}
	return os.Open(path)
}

func readGitHubIssue(ctx context.Context, source harness.TicketSource) (harness.TicketSource, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := proc.Run(ctx, proc.Spec{Path: "gh", Args: []string{"issue", "view", source.URL, "--json", "url,title,body,updatedAt"}, Env: os.Environ(), StdoutLimit: 6*maxTicketBytes + 4096, StderrLimit: 4096})
	if result.Err != nil || result.Code != 0 || result.Overflow {
		return source, "", fmt.Errorf("read GitHub issue %s with gh (exit %d): %s %v", source.URL, result.Code, strings.TrimSpace(string(result.Stderr)), result.Err)
	}
	var issue struct{ URL, Title, Body, UpdatedAt string }
	if err := json.Unmarshal(result.Stdout, &issue); err != nil {
		return source, "", err
	}
	if !strings.EqualFold(issue.URL, source.URL) || strings.TrimSpace(issue.Title) == "" {
		return source, "", fmt.Errorf("GitHub did not return the requested issue %s", source.URL)
	}
	source.Title, source.UpdatedAt = issue.Title, issue.UpdatedAt
	return source, issue.Title + "\n\n" + issue.Body, nil
}
