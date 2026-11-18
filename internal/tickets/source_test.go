package tickets

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGitHubTicketImport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX gh test executable")
	}
	dir := t.TempDir()
	const issue = "https://github.com/example/project/issues/42"
	script := "#!/bin/sh\ntest \"$#\" = 5 && test \"$1\" = issue && test \"$2\" = view && test \"$3\" = " + issue + " && test \"$4\" = --json && test \"$5\" = url,title,body,updatedAt || exit 2\nprintf '%s' '{\"url\":\"" + issue + "\",\"title\":\"Quoted input\",\"body\":\"Keep quotes.\\nSee example.\",\"updatedAt\":\"2026-01-01T00:00:00Z\"}'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	src, err := Location(issue+"/", false)
	if err != nil {
		t.Fatal(err)
	}
	src, text, err := Read(t.Context(), issue, src, nil)
	if err != nil || text != "Quoted input\n\nKeep quotes.\nSee example." || src.URL != issue || src.UpdatedAt == "" || src.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(text))) {
		t.Fatal(src, text, err)
	}
	if err := os.Remove(filepath.Join(dir, "gh")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(t.Context(), issue, src, nil); err == nil {
		t.Fatal("missing gh reported a successful import")
	}
}

func TestTicketSourceRejectsInvalidInputs(t *testing.T) {
	for _, value := range []string{"", "ticket.txt", "https://github.com/o/r/pull/1", "https://github.com/o/r/issues/0", "https://github.com@elsewhere.example/o/r/issues/4"} {
		if _, err := Location(value, false); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	p := filepath.Join(t.TempDir(), "issue.md")
	src, err := Location(p, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{[]byte(" "), {0xff}, bytes.Repeat([]byte("a"), maxTicketBytes+1)} {
		if err := os.WriteFile(p, content, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Read(t.Context(), p, src, nil); err == nil {
			t.Fatal("accepted invalid Markdown")
		}
	}
	if !strings.HasPrefix(src.URL, "file:") {
		t.Fatal(src)
	}
}

func TestSuppliedTicket(t *testing.T) {
	const ref = "https://issues.example.test/record?id=17"
	src, err := Location(ref, true)
	if err != nil || src.Kind != "supplied" || src.URL != ref {
		t.Fatal(src, err)
	}
	const body = "# Café exports\n\nReproduce with an empty table."
	got, text, err := Read(t.Context(), ref, src, strings.NewReader(body))
	if err != nil || text != body || got.Title != "Café exports" || got.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(body))) {
		t.Fatal(got, text, err)
	}
	for _, val := range []string{"", "   ", "bad\x00body", "\xff", strings.Repeat("x", maxTicketBytes+1)} {
		if _, _, err := Read(t.Context(), ref, src, strings.NewReader(val)); err == nil {
			t.Fatal("accepted invalid body")
		}
	}
	for _, val := range []string{"", "local.md", "mcp:///no-server", "https://user:secret@example.test/t", "javascript:alert(1)"} {
		if _, err := Location(val, true); err == nil {
			t.Errorf("accepted %q", val)
		}
	}
}
