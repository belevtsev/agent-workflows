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

func checkPublicationReceipt(t *testing.T, r *kitsutest.Repo, task, outcome, target, pr string) time.Time {
	t.Helper()
	result := r.Kitsu("task", "next", task, "--brief")
	var brief struct {
		Last struct {
			Outcome, Target, Commit string
			PR                      string    `json:"pull_request"`
			ObservedAt              time.Time `json:"observed_at"`
		} `json:"last_publication"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &brief); err != nil || result.Code != 0 || brief.Last.Outcome != outcome || brief.Last.Target != target || brief.Last.PR != pr || brief.Last.Commit != r.Git("rev-parse", "HEAD") || brief.Last.ObservedAt.IsZero() {
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
	checkPublicationReceipt(t, r, f.id, "confirmed", "mirrors/topic", "")
	// Mutate a remote outside Kitsu, just as a teammate could after delivery.
	r.Git("--git-dir", second, "update-ref", "refs/heads/archive/refs/heads/topic", r.Git("rev-parse", "HEAD"))
	r.Git("--git-dir", second, "update-ref", "-d", "refs/heads/topic")
	f.call(0, "next")
	f.want("done") // ordinary resume reads the last observation locally
	failed := r.Kitsu("task", "confirm", f.id, "--brief", "--ticket", firstTicket)
	var recovery struct{ Next, Ticket string }
	if err := json.Unmarshal([]byte(failed.Stdout), &recovery); failed.Code != 1 || err != nil || recovery.Next != "publish" || recovery.Ticket == "" {
		t.Fatal("recheck failure lost its resume ticket", failed.Output(), err)
	}
	checkPublicationReceipt(t, r, f.id, "unconfirmed", "mirrors/topic", "")
	f.call(0, "next")
	f.want("publish")
	if f.state.Ticket != recovery.Ticket || !f.state.Verified || f.state.Proof.Run != firstRun {
		t.Fatal("missing remote branch invalidated local verification", f.state)
	}
	f.call(1, "confirm", "--ticket", f.state.Ticket)
	r.Git("push", second, "HEAD:refs/heads/topic")
	newTicket := f.state.Ticket
	f.call(0, "confirm", "--ticket", newTicket)
	f.want("done")
	confirmed := checkPublicationReceipt(t, r, f.id, "confirmed", "mirrors/topic", "")
	// The old ticket is stale; rechecking the saved destination needs no lookup.
	f.call(1, "confirm", "--ticket", firstTicket)
	f.call(0, "confirm")
	f.want("done")
	if !checkPublicationReceipt(t, r, f.id, "confirmed", "mirrors/topic", "").After(confirmed) {
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
	const url = "https://git.example.invalid/maintainer/qs/pull/17"
	setPR := func(state, owner string) {
		t.Helper()
		data, err := json.Marshal(map[string]any{"url": url, "state": state, "headRefOid": head, "headRefName": "topic", "baseRefName": "main", "headRepository": map[string]string{"name": "qs"}, "headRepositoryOwner": map[string]string{"login": owner}})
		if err != nil {
			t.Fatal(err)
		}
		r.Write(".git/pr.json", string(data))
	}
	f := flow{t: t, repo: r, id: "fork-pr"}
	f.call(0, "open", "--request", "Confirm the topic PR into main", "--entry", "verify", "--until", "deliver", "--publish-to", "team/topic", "--pr-base", "main")
	f.record("intake", "host", "Confirm the existing patch and PR")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "unit passes", "--", "sh", "unit.sh")
	setPR("OPEN", "other-fork")
	f.call(1, "confirm", "--ticket", f.state.Ticket, "--pr", url)
	setPR("OPEN", "contributor")
	f.call(0, "confirm", "--ticket", f.state.Ticket, "--pr", url)
	f.want("done")
	checkPublicationReceipt(t, r, f.id, "confirmed", "team/topic", url)
	setPR("CLOSED", "contributor")
	f.call(1, "confirm")
	f.call(0, "next")
	f.want("publish")
	setPR("OPEN", "contributor")
	f.call(0, "confirm", "--ticket", f.state.Ticket, "--pr", url)
	f.want("done")
}
