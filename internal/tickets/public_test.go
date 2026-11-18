package tickets

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

func TestPublicTicket(t *testing.T) {
	const body = "# Broken export\nPreserve empty columns."
	for _, tc := range []struct {
		name, media, body, want string
		status                  int
	}{
		{"markdown", "text/markdown; charset=UTF-8", body, body, 200},
		{"plain", "text/plain", body, body, 200},
		{"html", "text/html", "<nav>Home</nav><main><h1>Broken export</h1><p>Keep <code>empty</code> columns.</p><script>noise</script></main>", "Broken export\n\nKeep empty columns.", 200},
		{"heading after navigation", "text/html", "<main><span>Project</span><h1>Bug 42</h1><p>Broken export</p></main>", "Bug 42\nProject\nBroken export", 200},
		{"unreadable", "text/html", "<body><script>loadTicket()</script></body>", "", 200},
		{"unavailable", "text/plain", body, "", 403},
		{"binary", "application/octet-stream", body, "", 200},
		{"charset", "text/plain; charset=windows-1252", body, "", 200},
		{"too large", "text/plain", strings.Repeat("x", maxTicketBytes+1), "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("unexpected non-anonymous GET")
				}
				if r.URL.Path == "/ticket" {
					http.Redirect(w, r, "/export", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", tc.media)
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			ref := srv.URL + "/ticket"
			src, err := Location(ref, false)
			if err != nil {
				t.Fatal(err)
			}
			got, text, err := Read(t.Context(), ref, src, nil)
			if tc.want == "" {
				if err == nil {
					t.Fatal("accepted unsupported content")
				}
				return
			}
			if err != nil || text != tc.want || got.URL != ref || got.Kind != "url" || got.SHA256 == "" {
				t.Fatalf("got %v, %q, %v", got, text, err)
			}
		})
	}
}

func TestOlderTicketKeepsItsSource(t *testing.T) {
	req, err := Location("https://EXAMPLE.test/t/42/?from=board", false)
	if err != nil {
		t.Fatal(err)
	}
	old := harness.TicketSource{Kind: "legacy-reader", URL: "https://example.test/t/42"}
	if !SameSource(req, old) {
		t.Fatal("cannot reopen saved ticket")
	}
	old.URL = "https://example.test/t/24"
	if SameSource(req, old) {
		t.Fatal("different ticket was allowed")
	}
}

func TestPluginTicket(t *testing.T) {
	ref := "plugin://support/INC-42"
	src, err := Location(ref, true)
	if err != nil {
		t.Fatal(err)
	}
	got, text, err := Read(t.Context(), ref, src, strings.NewReader("# Timeout\nDetails"))
	if err != nil || got.Kind != "supplied" || got.URL != ref || text != "# Timeout\nDetails" {
		t.Fatal(got, text, err)
	}
	if _, err := Location(ref, false); err == nil {
		t.Fatal("plugin source must come from the host")
	}
}
