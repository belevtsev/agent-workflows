package gitx

import (
	"encoding/json"
	"testing"
)

func TestPublicationRequiresTheRequestedOpenPR(t *testing.T) {
	const link = "https://github.com/example/repo/pull/17"
	for _, tc := range []struct{ field, value string }{
		{}, {"url", link + "0"}, {"state", "CLOSED"}, {"headRefOid", "older"},
		{"headRepositoryOwner", "unrelated-owner"}, {"headRepository", "other-repo"}, {"headRefName", "other"}, {"baseRefName", "release"},
	} {
		t.Run("matches-"+tc.field, func(t *testing.T) {
			pr := map[string]any{"headRepositoryOwner": map[string]string{"login": "example"}, "headRepository": map[string]string{"name": "repo"}, "url": link, "state": "OPEN", "headRefOid": "verified", "headRefName": "topic", "baseRefName": "main"}
			if tc.field != "" {
				switch tc.field {
				case "headRepository":
					pr[tc.field] = map[string]string{"name": tc.value}
				case "headRepositoryOwner":
					pr[tc.field] = map[string]string{"login": tc.value}
				default:
					pr[tc.field] = tc.value
				}
			}
			data, err := json.Marshal(pr)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkHarnessPR(data, link, "topic", "main", "verified", []string{"example/repo"}); (err == nil) != (tc.field == "") {
				t.Fatalf("unexpected confirmation result for %v: %v", pr, err)
			}
		})
	}
}

func TestPullRequestSourceMatchesHostAndTransport(t *testing.T) {
	for _, tc := range []struct {
		remote, host string
		ok           bool
	}{
		{"https://enterprise:8443/owner/repo.git", "enterprise:8443", true},
		{"https://enterprise:8443/owner/repo", "enterprise", false},
		{"git@enterprise:owner/repo.git", "enterprise", true},
		{"ssh://git@enterprise:2222/owner/repo.git", "enterprise", true},
		{"https://enterprise:443/owner/repo", "enterprise:443", true},
		{"https://enterprise/owner/repo", "foreign", false},
		{"/some/local/directory", "enterprise", false},
	} {
		owner, repo, ok := pullRequestSource(tc.remote, "https://"+tc.host+"/base/repo/pull/1")
		if ok != tc.ok || ok && (owner != "owner" || repo != "repo") {
			t.Errorf("%s / %s: got %s/%s, matched %v", tc.remote, tc.host, owner, repo, ok)
		}
	}
}

func TestPullRequestIsInTheRepositoryOfTheRemote(t *testing.T) {
	for _, tc := range []struct {
		remote, link string
		in           bool
	}{
		{"git@github.com:Owner/Repo.git", "https://github.com/owner/repo/pull/7", true},
		{"https://github.com/owner/repo", "https://github.com/owner/repo/pull/7", true},
		{"https://github.com/owner/repo", "https://github.com/attacker/other/pull/1", false},
		{"https://github.com/owner/repo", "https://github.com/attacker/repo/pull/1", false},
		{"https://github.com/owner/repo", "https://enterprise/owner/repo/pull/7", false},
		{"https://github.com/owner/repo", "https://github.com/owner/repo/pull/7/files", false},
		{"/some/local/directory", "https://github.com/owner/repo/pull/7", false},
	} {
		if got := pullRequestIn(tc.remote, tc.link); got != tc.in {
			t.Errorf("%s holds %s: got %v, want %v", tc.remote, tc.link, got, tc.in)
		}
	}
}
