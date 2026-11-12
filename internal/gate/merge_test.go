package gate_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

func TestMergeHeadsReadsGitsMergeHeadVariables(t *testing.T) {
	sha1, sha256, other := strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 40)
	environ := []string{
		"PATH=/usr/bin",
		"GITHEAD_" + sha1 + "=origin/main",
		"GITHEAD_" + sha256 + "=" + sha256,
		"GITHEAD_" + sha1 + "=again",
		"GITHEAD_" + strings.ToUpper(other) + "=upper",
		"GITHEAD_abc123=short",
		"GITHEAD_" + strings.Repeat("g", 40) + "=not hexadecimal",
		"XGITHEAD_" + other + "=prefixed",
		"GIT_REFLOG_ACTION=merge origin/main",
	}
	// Oracle: git merge names each commit it merges in as
	// GITHEAD_<object name>=<name as given>, the object name a full SHA-1 or
	// SHA-256 name in lower-case hexadecimal. Each such name counts once, in
	// the order found; any other variable, or a GITHEAD_ name that is not a
	// full lower-case object name, is not a merge head.
	if got, want := gate.MergeHeads(environ), []string{sha1, sha256}; !slices.Equal(got, want) {
		t.Fatalf("MergeHeads = %q, want %q", got, want)
	}
	if got := gate.MergeHeads(nil); len(got) != 0 {
		t.Fatalf("MergeHeads(nil) = %q, want none", got)
	}
}

func TestDecideGitHookAllowsOnlyAMergeOfRemoteTrackingCommits(t *testing.T) {
	repo := canonicalDir(t)
	remote, older, local, unknown := strings.Repeat("1", 40), strings.Repeat("2", 64), strings.Repeat("3", 40), strings.Repeat("4", 40)
	contains := func(head string) string {
		return "for-each-ref --count=1 --format=%(refname) --contains " + head + " refs/remotes/"
	}
	git := &fakeGit{}
	git.repository(repo)
	git.answer(repo, contains(remote), "refs/remotes/origin/main\n")
	git.answer(repo, contains(older), "refs/remotes/upstream/main\n")
	// No remote-tracking ref contains the local commit; git has no answer
	// for unknown, as for an object it cannot find.
	git.answer(repo, contains(local), "")
	gating := []taskflow.OpenRecord{record("task-1", "Add retries", "it has no passing cw task verify")}

	// Oracle (F4 review, design decision): with a record that gates, a merge
	// commit (pre-merge-commit) is allowed, with no decision and without
	// reading the task store, exactly when it names at least one merge head
	// and a remote-tracking ref (refs/remotes/) contains every one, which git
	// for-each-ref --contains answers; any other merge commit, and a commit,
	// is denied as a commit is. Each merge head is looked up by the full
	// object name only; a name that is not one is never passed to git.
	for _, row := range []struct {
		name    string
		heads   []string
		allow   bool
		lookups []string
	}{
		{"one merge head on a remote-tracking branch", []string{remote}, true, []string{remote}},
		{"an octopus merge of remote-tracking commits", []string{remote, older}, true, []string{remote, older}},
		{"a local merge head", []string{local}, false, []string{local}},
		{"a remote-tracking merge head and a local one", []string{remote, local}, false, []string{remote, local}},
		{"a merge head git cannot look up", []string{unknown}, false, []string{unknown}},
		{"no merge head", nil, false, nil},
		{"a merge head that is not an object name", []string{"origin/main"}, false, nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			git.calls = nil
			store := &fakeStore{open: map[string][]taskflow.OpenRecord{repo: gating}}
			got := gate.DecideGitHook(context.Background(), gate.GitHookInput{Dir: repo, MergeHeads: row.heads, Enforce: true, Prompt: true},
				gate.Deps{Git: git.run, Now: func() time.Time { return fixedNow }, Store: store})
			switch {
			case row.allow && (got != gate.Decision{} || len(store.calls) != 0):
				t.Fatalf("DecideGitHook = %+v after %d store reads, want no decision and no read", got, len(store.calls))
			case !row.allow && got.Permission != gate.Deny:
				t.Fatalf("DecideGitHook = %+v, want a deny", got)
			}
			var lookups []string
			for _, call := range git.calls {
				if rest, ok := strings.CutPrefix(call, repo+": for-each-ref "); ok {
					fields := strings.Fields(rest)
					if len(fields) != 5 || fields[2] != "--contains" || fields[4] != "refs/remotes/" {
						t.Fatalf("git call %q, want for-each-ref --contains HEAD refs/remotes/", call)
					}
					lookups = append(lookups, fields[3])
				}
			}
			if !slices.Equal(lookups, row.lookups) {
				t.Fatalf("for-each-ref looked up %q, want %q", lookups, row.lookups)
			}
		})
	}
}
