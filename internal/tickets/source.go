// Package tickets imports source material for bug delivery tasks.
package tickets

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

const maxTicketBytes = 512 << 10

// Location normalizes the identity without reading the ticket, so a task can reopen offline.
func Location(from string, supplied bool) (harness.TicketSource, error) {
	if supplied || strings.Contains(from, "://") {
		u, err := url.Parse(from)
		if err != nil || u.Hostname() == "" || u.User != nil {
			return harness.TicketSource{}, fmt.Errorf("use a ticket URL without embedded credentials")
		}
		if supplied && (u.Scheme == "mcp" || u.Scheme == "plugin" || isWebLink(u)) {
			return harness.TicketSource{Kind: "supplied", URL: u.String()}, nil
		}
		if !isWebLink(u) {
			return harness.TicketSource{}, fmt.Errorf("supply an HTTP(S) URL, or pass MCP or plugin ticket text with --stdin")
		}
		return harness.TicketSource{Kind: "url", URL: u.String()}, nil
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

func isWebLink(u *url.URL) bool { return u.Scheme == "https" || u.Scheme == "http" }

var githubIssuePath = regexp.MustCompile(`^/[A-Za-z0-9-]+/[A-Za-z0-9_.-]*[A-Za-z0-9_-][A-Za-z0-9_.-]*/issues/[1-9][0-9]*$`)

// CanonicalIssueLink recognizes stable issue paths for shareable exports.
// Other ticket sources can contain private data and stay in the local record.
func CanonicalIssueLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") {
		return ""
	}
	path := strings.TrimSuffix(u.Path, "/")
	if len(path) > 512 || !githubIssuePath.MatchString(path) {
		return ""
	}
	return "https://github.com" + path
}

// Read captures the source text and a digest for the immutable task request.
func Read(ctx context.Context, from string, source harness.TicketSource, input io.Reader) (harness.TicketSource, string, error) {
	var text string
	var err error
	switch source.Kind {
	case "url":
		text, err = readPublicLink(ctx, source.URL)
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
	default:
		return source, "", fmt.Errorf("unsupported ticket source %q", source.Kind)
	}
	if err != nil {
		return source, "", err
	}
	if len(text) > maxTicketBytes || strings.TrimSpace(text) == "" || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return source, "", fmt.Errorf("ticket must contain nonempty UTF-8 text of at most %d bytes", maxTicketBytes)
	}
	if source.Title == "" {
		title, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
		source.Title = strings.TrimSpace(strings.TrimLeft(title, "#"))
		if source.Title == "" || len(source.Title) > 256 {
			source.Title = source.URL
		}
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

// SameSource also recognizes normalized links in older provider-specific snapshots.
func SameSource(request, saved harness.TicketSource) bool {
	if request.Kind == saved.Kind {
		return request.URL == saved.URL
	}
	if request.Kind != "url" || saved.Kind == "markdown" || saved.Kind == "supplied" {
		return false
	}
	u, err := url.Parse(request.URL)
	if err != nil {
		return false
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u.String() == saved.URL
}
