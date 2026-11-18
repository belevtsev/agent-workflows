package tickets

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTicketSourceRejectsInvalidInputs(t *testing.T) {
	for _, value := range []string{"", "ticket.txt", "ftp://tracker.example/tickets/1", "https:///tickets/1", "https://user@tracker.example/tickets/4"} {
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
