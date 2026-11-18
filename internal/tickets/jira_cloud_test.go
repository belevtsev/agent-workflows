package tickets

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

func TestReadJiraCloudIssue(t *testing.T) {
	const body = `{"key":"APP-42","fields":{"summary":"Broken output","description":"Step 1\nStep 2","updated":"today"}}`
	for _, tc := range []struct {
		name, body string
		status     int
		wrongSite  bool
		want       bool
	}{
		{"import", body, 200, false, true},
		{"permission denied", "", 403, false, false},
		{"unbound credentials", body, 200, true, false},
		{"other issue", strings.Replace(body, "APP-42", "APP-43", 1), 200, false, false},
		{"missing summary", "{}", 200, false, false},
		{"redirect refused", "", 302, false, false},
		{"body too large", strings.Replace(body, "Step 1", strings.Repeat("x", maxTicketBytes+1), 1), 200, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.wrongSite {
					t.Error("sent request despite tenant mismatch")
				}
				if r.Method != "GET" || r.URL.RequestURI() != "/rest/api/2/issue/APP-42?fields=summary,description,updated" {
					t.Error(r.Method, r.URL)
				}
				if user, pass, ok := r.BasicAuth(); !ok || user != "user" || pass != "token" {
					t.Error("missing credentials")
				}
				if tc.status == 302 {
					w.Header().Set("Location", "/unexpected")
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			site := srv.URL
			if tc.wrongSite {
				site = "https://different.atlassian.net"
			}
			t.Setenv("KITSU_JIRA_SITE", site)
			t.Setenv("KITSU_JIRA_EMAIL", "user")
			t.Setenv("KITSU_JIRA_API_TOKEN", "token")
			src := harness.TicketSource{URL: srv.URL + "/browse/APP-42", Kind: "jira"}
			got, text, err := Read(t.Context(), src.URL, src, nil)
			if tc.want && (err != nil || text != "Broken output\n\nStep 1\nStep 2" || got.UpdatedAt != "today" || got.SHA256 == "") {
				t.Fatal(got, text, err)
			}
			if !tc.want && err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}
func TestJiraCloudIssueURL(t *testing.T) {
	const target = "https://acme.atlassian.net/browse/APP-42"
	src, err := Location(target+"/?from=board#detail", false)
	if err != nil || src.Kind != "jira" || src.URL != target {
		t.Fatal(src, err)
	}
	for _, v := range []string{"http://acme.atlassian.net/browse/X-1", "https://acme.atlassian.net.evil.test/browse/X-1", "https://user@acme.atlassian.net/browse/X-1", target + "/comment", target + ":80"} {
		if _, err := Location(v, false); err == nil {
			t.Error(v)
		}
	}
}
