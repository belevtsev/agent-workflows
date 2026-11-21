package harness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func checkPublicationReceipt(t *testing.T, r *kitsutest.Repo, task, outcome, target, pr, commit string) time.Time {
	t.Helper()
	result := r.Kitsu("task", "next", task, "--brief")
	var brief struct {
		Last struct {
			Outcome, Target, Commit string
			PR                      string    `json:"pull_request"`
			ObservedAt              time.Time `json:"observed_at"`
		} `json:"last_publication"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &brief); err != nil || result.Code != 0 || brief.Last.Outcome != outcome || brief.Last.Target != target || brief.Last.PR != pr || brief.Last.Commit != commit || brief.Last.ObservedAt.IsZero() {
		t.Fatal("incomplete publication receipt", result.Output(), err)
	}
	return brief.Last.ObservedAt
}

func TestReconfirmPublishedBranchAfterOneDestinationChanges(t *testing.T) {
	r := kitsutest.Demo(t)
	first, second := t.TempDir(), t.TempDir()
	r.Git("init", "--bare", first)
	r.Git("init", "--bare", second)
	r.Git("remote", "add", "mirrors", first)
	r.Git("config", "--add", "remote.mirrors.pushurl", first)
	r.Git("config", "--add", "remote.mirrors.pushurl", second)
	r.Git("push", "mirrors", "HEAD:refs/heads/topic")
	f := flow{t: t, repo: r, id: "ship-mirrors"}
	f.call(0, "open", "--request", "Confirm the published topic branch", "--entry", "verify", "--until", "deliver", "--publish-to", "mirrors/topic")
	f.record("intake", "host", "Verify the existing commit and both push destinations")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "candidate passes unit", "--", "sh", "unit.sh")
	f.want("publish")
	firstTicket, firstRun := f.state.Ticket, f.state.Proof.Run
	f.call(0, "confirm", "--ticket", firstTicket)
	f.want("done")
	head, completed := r.Git("rev-parse", "HEAD"), f.state.Completion
	checkPublicationReceipt(t, r, f.id, "confirmed", "mirrors/topic", "", head)
	r.Git("commit", "--allow-empty", "-qm", "Later local work")
	// Mutate a remote outside Kitsu, just as a teammate could after delivery.
	r.Git("--git-dir", second, "update-ref", "refs/heads/archive/refs/heads/topic", head)
	r.Git("--git-dir", second, "update-ref", "-d", "refs/heads/topic")
	f.call(0, "next")
	f.want("done") // ordinary resume reads the last observation locally
	failed := r.Kitsu("task", "confirm", f.id, "--brief", "--ticket", firstTicket)
	var recovery struct{ Next, Ticket string }
	if err := json.Unmarshal([]byte(failed.Stdout), &recovery); failed.Code != 1 || err != nil || recovery.Next != "done" || recovery.Ticket != "" {
		t.Fatal("recheck failure reopened the delivery", failed.Output(), err)
	}
	checkPublicationReceipt(t, r, f.id, "unconfirmed", "mirrors/topic", "", head)
	f.call(0, "next")
	f.want("done")
	if f.state.Ticket != "" || !f.state.Verified || f.state.Proof.Run != firstRun ||
		completed == nil || f.state.Completion == nil || *f.state.Completion != *completed {
		t.Fatal("missing remote branch changed the completed delivery", f.state)
	}
	f.call(1, "confirm", "--ticket", "unrelated")
	r.Git("push", second, head+":refs/heads/topic")
	f.call(0, "confirm")
	f.want("done")
	confirmed := checkPublicationReceipt(t, r, f.id, "confirmed", "mirrors/topic", "", head)
	f.call(0, "confirm")
	f.want("done")
	if !checkPublicationReceipt(t, r, f.id, "confirmed", "mirrors/topic", "", head).After(confirmed) {
		t.Fatal("live check did not refresh the observation time")
	}
	shown := r.Kitsu("task", "show", f.id, "--json")
	var state struct{ Events []struct{ Action string } }
	if err := json.Unmarshal([]byte(shown.Stdout), &state); shown.Code != 0 || err != nil {
		t.Fatal(shown.Output(), err)
	}
	counts := map[string]int{}
	for _, e := range state.Events {
		counts[e.Action]++
	}
	if counts["publish"] != 2 || counts["publication_unconfirmed"] != 1 || counts["test"] != 1 {
		t.Fatalf("unexpected history: %v", counts)
	}
}

func TestPullRequestOnSecondPushDestinationCanBeRechecked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a local POSIX gh and SSH transport fixture")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\ntest \"$1 $2\" = 'pr view' || exit 1\ncat .git/pr.json\n"), 0755); err != nil {
		t.Fatal(err)
	}
	r := kitsutest.Demo(t)
	mirror := t.TempDir()
	r.Git("init", "--bare", mirror)
	r.Git("push", mirror, "HEAD:refs/heads/topic")
	r.Git("remote", "add", "team", mirror)
	r.Git("config", "--add", "remote.team.pushurl", mirror)
	r.Git("config", "--add", "remote.team.pushurl", "git@git.example.invalid:contributor/qs.git")
	r.Write(".git/ssh", "exec git upload-pack \"$(cat .git/fixture-mirror)\"\n")
	r.Write(".git/fixture-mirror", mirror)
	r.Git("config", "core.sshCommand", "sh .git/ssh")
	r.Git("config", "ssh.variant", "ssh")
	head := r.Git("rev-parse", "HEAD")
	r.Git("remote", "add", "upstream", "https://git.example.invalid/maintainer/qs.git")
	const url = "https://git.example.invalid/maintainer/qs/pull/17"
	setPR := func(url, state, owner string) {
		t.Helper()
		data, err := json.Marshal(map[string]any{"url": url, "state": state, "headRefOid": head, "headRefName": "topic", "baseRefName": "main", "headRepository": map[string]string{"name": "qs"}, "headRepositoryOwner": map[string]string{"login": owner}})
		if err != nil {
			t.Fatal(err)
		}
		r.Write(".git/pr.json", string(data))
	}
	f := flow{t: t, repo: r, id: "fork-pr"}
	f.call(0, "open", "--request", "Confirm the topic PR into main", "--entry", "verify", "--until", "deliver", "--publish-to", "team/topic", "--pr-base", "upstream:main")
	f.record("intake", "host", "Confirm the existing patch and PR")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "unit passes", "--", "sh", "unit.sh")
	// A pull request with the same head in a repository that is not the pinned base confirms nothing.
	const foreign = "https://git.example.invalid/attacker/other/pull/1"
	setPR(foreign, "OPEN", "contributor")
	f.call(1, "confirm", "--ticket", f.state.Ticket, "--pr", foreign)
	setPR(url, "OPEN", "other-fork")
	f.call(1, "confirm", "--ticket", f.state.Ticket, "--pr", url)
	setPR(url, "OPEN", "contributor")
	f.call(0, "confirm", "--ticket", f.state.Ticket, "--pr", url)
	f.want("done")
	checkPublicationReceipt(t, r, f.id, "confirmed", "team/topic", url, head)
	setPR(url, "CLOSED", "contributor")
	f.call(1, "confirm")
	f.call(0, "next")
	f.want("done")
	setPR(url, "OPEN", "contributor")
	f.call(0, "confirm")
	f.want("done")

	// Without a named remote the base repository is the push remote's own.
	f = flow{t: t, repo: r, id: "own-pr"}
	f.call(0, "open", "--request", "Confirm the topic PR into main", "--entry", "verify", "--until", "deliver", "--publish-to", "team/topic", "--pr-base", "main")
	f.record("intake", "host", "Confirm the existing patch and PR")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "unit passes", "--", "sh", "unit.sh")
	f.call(1, "confirm", "--ticket", f.state.Ticket, "--pr", url)
	const own = "https://git.example.invalid/contributor/qs/pull/3"
	setPR(own, "OPEN", "contributor")
	f.call(0, "confirm", "--ticket", f.state.Ticket, "--pr", own)
	f.want("done")
}
