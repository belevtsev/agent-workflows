package tickets

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

func readPublicLink(ctx context.Context, link string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/markdown, text/plain, text/html;q=0.9")
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || r.URL.User != nil || !isWebLink(r.URL) || (via[len(via)-1].URL.Scheme == "https" && r.URL.Scheme != "https") {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("read public ticket: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("public ticket returned HTTP %d; use a connected tracker with --stdin for private tickets", resp.StatusCode)
	}
	media, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return "", fmt.Errorf("public ticket needs a text Content-Type; use --stdin for other formats")
	}
	switch media {
	case "text/markdown", "text/plain", "text/html":
	default:
		return "", fmt.Errorf("unsupported public ticket type %q; supply Markdown with --stdin", media)
	}
	if enc := strings.ToLower(params["charset"]); enc != "" && enc != "utf-8" && enc != "us-ascii" {
		return "", fmt.Errorf("public ticket charset %q requires conversion to UTF-8", enc)
	}
	// A rendered page includes markup and scripts in addition to the ticket text.
	limit := int64(maxTicketBytes)
	if media == "text/html" {
		limit *= 8
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(b)) > limit || !utf8.Valid(b) {
		return "", fmt.Errorf("oversized or invalid UTF-8 public ticket")
	}
	if media == "text/html" {
		return renderedTicket(b)
	}
	return string(b), nil
}

// Use the document's main content or article. Ignore navigation and executable content.
func renderedTicket(b []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	for _, tag := range []string{"main", "article"} {
		for n := range doc.Descendants() {
			if n.Type != html.ElementNode || n.Data != tag {
				continue
			}
			var text strings.Builder
			// Navigation labels can precede the heading in a rendered page.
			for heading := range n.Descendants() {
				if heading.Type == html.ElementNode && heading.Data == "h1" {
					writeVisibleText(&text, heading)
					heading.Parent.RemoveChild(heading)
					break
				}
			}
			writeVisibleText(&text, n)
			if body := strings.TrimSpace(text.String()); body != "" {
				return body, nil
			}
		}
	}
	return "", fmt.Errorf("page has no readable main or article; use a ticket export or a connected tracker with --stdin")
}

func writeVisibleText(out *strings.Builder, n *html.Node) {
	if n.Type == html.TextNode {
		out.WriteString(n.Data)
		return
	}
	if n.Type != html.ElementNode {
		return
	}
	switch n.Data {
	case "script", "style", "noscript", "template", "nav", "form", "button", "svg", "iframe":
		return
	}
	for _, a := range n.Attr {
		if a.Key == "hidden" || (a.Key == "aria-hidden" && a.Val == "true") {
			return
		}
	}
	switch n.Data {
	case "p", "div", "section", "li", "br", "pre", "h1", "h2", "h3", "h4", "h5", "h6", "tr":
		out.WriteByte('\n')
		defer out.WriteByte('\n')
	}
	for child := range n.ChildNodes() {
		writeVisibleText(out, child)
	}
}
