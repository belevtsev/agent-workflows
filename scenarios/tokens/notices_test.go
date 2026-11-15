package tokens_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

// Requirement: Notices as the host gives them, scenario: A record that grows
// after its notice. The host writes its record while it gives the notice, so
// the record of the first developer start lacks its last answer when its
// notice is given, and holds it at the next notice.
func TestARecordThatGrowsAfterItsNotice(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	s := buildS1(t, variant{})
	path := s.Paths["a1f0008"]
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var early []string
	for _, line := range strings.SplitAfter(string(whole), "\n") {
		if !strings.Contains(line, `"msg_a1f0008_04"`) {
			early = append(early, line)
		}
	}
	for i, n := range s.Notices {
		if strings.Contains(n, `"agent_id":"a1f0008"`) {
			writeFile(t, path, strings.Join(early, ""))
		}
		if r := recordNotice(t, repo, s.Host, n); r.Code != 0 {
			t.Fatalf("notice %d: exit status %d\n%s", i+1, r.Code, r.Output())
		}
		writeFile(t, path, string(whole))
	}
	out := tokensJSON(t, repo, "--task", "add-greeting")
	wantNumber(t, out, "total", 655000)
	wantNumber(t, find(t, entries(t, out, "stages"), map[string]any{"stage": "code", "round": 1}), "total", 200000)
}

// Requirement: Notices as the host gives them, scenario: An internal agent of
// the host. Claude Code ends its own agents with a notice whose agent_type is
// empty.
func TestAnInternalAgentOfTheHost(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	s := buildS1(t, variant{})
	record(t, repo, s)
	path := filepath.Join(filepath.Dir(s.Paths["a1f0001"]), "agent-aside01.jsonl")
	claudeAgentRecord(t, path, s1Session, start{ID: "aside01", Sentence: "Suggest the next prompt.", Model: haiku, Tokens: split(3000), Answers: 1}, false)
	n := notice(map[string]any{
		"session_id": s1Session, "transcript_path": s.Paths["session"], "cwd": "/tmp/demo",
		"hook_event_name": "SubagentStop", "agent_id": "aside01", "agent_type": "", "agent_transcript_path": path,
	})
	if r := recordNotice(t, repo, s.Host, n); r.Code != 0 {
		t.Fatalf("exit status %d\n%s", r.Code, r.Output())
	}
	out := tokensJSON(t, repo)
	if got := entries(t, out, "unlabelled"); len(got) != 0 {
		t.Errorf("unlabelled is %s, want nothing", show(got))
	}
	if _, err := os.Stat(filepath.Join(repo.StoreDir(), "tokens", s1Session, "agent-aside01.json")); err == nil {
		t.Errorf("the internal agent was stored as a start")
	}
}

// Requirement: Notices as the host gives them, scenario: The directory of the
// notice. The hook runs in a repository without kitsu.toml, and the notice's
// cwd is demo.
func TestTheDirectoryOfTheNotice(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	other := kitsutest.NewRepo(t)
	s := buildS1(t, variant{})
	for i, n := range s.Notices {
		if r := recordNoticeIn(t, other, repo.Dir, s.Host, n); r.Code != 0 {
			t.Fatalf("notice %d: exit status %d\n%s", i+1, r.Code, r.Output())
		}
	}
	wantNumber(t, tokensJSON(t, repo, "--task", "add-greeting"), "total", 655000)
	if _, err := os.Lstat(other.StoreDir()); err == nil {
		t.Errorf("%s was created", other.StoreDir())
	}
}

// The token report reads a task name from a label as the commands of kitsu read
// it: a name kitsu change status refuses is no label, and the other way round.
func TestTaskNamesAgreeWithTheCommands(t *testing.T) {
	t.Parallel()
	repo := kitsutest.NewRepo(t)
	for _, name := range []string{"a", "add-greeting", "a1-b2-c3", "9", "A", "a--b", "a-", "a_b", "a.b", "ä", "add greeting"} {
		r := repo.Kitsu("change", "status", name)
		refused := r.Code == 2 && strings.Contains(r.Stderr, "is not the name of a task")
		if _, ok := tokens.ParseLabel("kitsu-task: " + name + " code 1"); ok == refused {
			t.Errorf("%q: kitsu change status refuses it %v (exit %d), a label with it is read %v", name, refused, r.Code, ok)
		}
	}
}
