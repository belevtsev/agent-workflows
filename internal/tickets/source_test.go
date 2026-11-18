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
	src, err := Location(issue + "/")
	if err != nil {
		t.Fatal(err)
	}
	src, text, err := Read(t.Context(), issue, src)
	if err != nil || text != "Quoted input\n\nKeep quotes.\nSee example." || src.URL != issue || src.UpdatedAt == "" || src.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(text))) {
		t.Fatal(src, text, err)
	}
	if err := os.Remove(filepath.Join(dir, "gh")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(t.Context(), issue, src); err == nil {
		t.Fatal("missing gh reported a successful import")
	}
}

func TestTicketSourceRejectsInvalidInputs(t *testing.T) {
	for _, value := range []string{"", "ticket.txt", "https://github.com/o/r/pull/1", "https://github.com/o/r/issues/0", "https://github.com@elsewhere.example/o/r/issues/4"} {
		if _, err := Location(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	p := filepath.Join(t.TempDir(), "issue.md")
	src, err := Location(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{[]byte(" "), {0xff}, bytes.Repeat([]byte("a"), maxTicketBytes+1)} {
		if err := os.WriteFile(p, content, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Read(t.Context(), p, src); err == nil {
			t.Fatal("accepted invalid Markdown")
		}
	}
	if !strings.HasPrefix(src.URL, "file:") {
		t.Fatal(src)
	}
}
