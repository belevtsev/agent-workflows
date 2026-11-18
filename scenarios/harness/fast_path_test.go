package harness_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestStagedVerifiedCommitInFourCalls(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Git("checkout", "-qb", "delivery")
	base := r.Git("rev-parse", "HEAD")
	r.Write("answer.txt", "42\n")
	r.Write("build/personal.txt", "developer draft\n")
	r.Git("add", "answer.txt")
	staged := r.Git("write-tree")

	f := flow{t: t, repo: r, id: "fast-path"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify the staged answer and commit it if it passes")
	f.record("intake", "coordinator", "The staged answer must equal 42")
	f.want("verify")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--cached", "-e", "^42$", "--", "answer.txt")
	f.want("commit")
	if !f.state.Verified || f.state.Proof == nil || f.state.Proof.Outcome != "passed" || f.state.Ticket == "" {
		t.Fatalf("missing commit authorization from verification: %+v", f.state)
	}
	proof := f.state.Proof.Run
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver the verified answer", "--include", "answer.txt")
	f.want("done")
	if f.state.Delivery != "committed" || !f.state.Verified || f.state.Proof == nil || f.state.Proof.Run != proof || f.state.Ticket != "" {
		t.Fatalf("commit did not return the final verified delivery: %+v", f.state)
	}

	head := r.Git("rev-parse", "HEAD")
	if r.Git("rev-list", "--parents", "-1", head) != head+" "+base || r.Git("rev-parse", "HEAD^{tree}") != staged ||
		r.Git("diff", "--name-only", base, head) != "answer.txt" || r.Git("status", "--porcelain") != "" ||
		r.Read("build/personal.txt") != "developer draft\n" {
		t.Fatal("commit diverged from the staged candidate or changed unrelated work")
	}
	for _, id := range []string{"task-fast-path", "unit", "lint"} {
		files := r.RecordFiles(id)
		if len(files) != 1 {
			t.Fatalf("%s has %d runs, want one", id, len(files))
		}
		var record struct {
			Tree, Outcome string
		}
		data, err := os.ReadFile(files[0])
		if err != nil || json.Unmarshal(data, &record) != nil || record.Tree != staged || record.Outcome != "passed" {
			t.Fatalf("%s record does not prove the delivered tree: %v\n%s", id, err, data)
		}
	}
}
