package tickets

import (
	"fmt"
	"strings"
	"testing"
)

func TestFailedCheckMetadataRequiresMatchingCompletedFailure(t *testing.T) {
	const link = "https://github.com/example/project/pull/17"
	const failed = `{"name":"unit","status":"COMPLETED","conclusion":"FAILURE","command":"remote-command"}`
	const external = `{"__typename":"StatusContext","context":"ci/verify","state":"FAILURE","targetUrl":"https://ci.example.net/private-run"}`
	for _, tc := range []struct {
		name, job, checks, want string
	}{
		{name: "completed failure", checks: failed, want: "unit"},
		{name: "selected failure", job: "unit", checks: `{"name":"lint","status":"COMPLETED","conclusion":"FAILURE"},` + failed, want: "unit"},
		{name: "external failure", checks: external, want: "ci/verify"},
		{name: "external error", checks: strings.Replace(external, "FAILURE", "ERROR", 1), want: "ci/verify"},
		{name: "external pending", checks: strings.Replace(external, "FAILURE", "PENDING", 1)},
		{name: "external success", checks: strings.Replace(external, "FAILURE", "SUCCESS", 1)},
		{name: "mixed failures", checks: failed + "," + external},
		{name: "selected external failure", job: "ci/verify", checks: failed + "," + external, want: "ci/verify"},
		{name: "unknown check type", checks: `{"__typename":"Other","name":"unit","status":"COMPLETED","conclusion":"FAILURE"}`},
		{name: "ambiguous", checks: failed + "," + failed},
		{name: "pending", checks: strings.Replace(failed, "COMPLETED", "IN_PROGRESS", 1)},
		{name: "passing", checks: strings.Replace(failed, "FAILURE", "SUCCESS", 1)},
		{name: "wrong job", job: "missing", checks: failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := FailedCheckLocation(link, "test", tc.job)
			if err != nil {
				t.Fatal(err)
			}
			head := strings.Repeat("a", 40)
			body := fmt.Sprintf(`{"url":%q,"state":"OPEN","headRefOid":%q,"statusCheckRollup":[%s]}`, link, head, tc.checks)
			got, request, err := ReadFailedCheck(source, strings.NewReader(body))
			if tc.want == "" {
				if err == nil {
					t.Fatalf("unqualified check accepted: %+v", got)
				}
				return
			}
			if err != nil || got.Check.Name != tc.want || got.Check.Head != head || got.Check.LocalCheck != "test" ||
				len(got.SHA256) != 64 || strings.Contains(request, "remote-command") || strings.Contains(request, "private-run") {
				t.Fatalf("incorrect local source: %+v (%v)", got, err)
			}
		})
	}
}

func TestFailedCheckLocationRequiresStablePRLink(t *testing.T) {
	for _, link := range []string{
		"https://github.com/example/project/pull/17?access_token=secret",
		"https://github.com/example/project/pull/17#log",
		"https://github.com@example.org/example/project/pull/17",
		"https://github.com/example/project/actions/runs/17",
		"https://github.com/example/project/pull/%31",
	} {
		if _, err := FailedCheckLocation(link, "test", ""); err == nil {
			t.Errorf("accepted unstable PR link %q", link)
		}
	}
}
