package tokens_test

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// claudeCount reads a generated Claude Code record by the rules of the
// design: lines with the same message.id are one answer, counted once with
// the largest count of each kind; <synthetic> answers are not counted. It
// returns the counts and the first line of the first user line.
func claudeCount(t *testing.T, path string) (counts, string) {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // a file this test generated
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	answers := map[string]counts{}
	label := ""
	seenUser := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				ID      string          `json:"id"`
				Model   string          `json:"model"`
				Content json.RawMessage `json:"content"`
				Usage   struct {
					Input      int `json:"input_tokens"`
					CacheWrite int `json:"cache_creation_input_tokens"`
					CacheRead  int `json:"cache_read_input_tokens"`
					Output     int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		switch {
		case line.Type == "user" && !seenUser:
			seenUser = true
			label = firstLine(t, line.Message.Content)
		case line.Type == "assistant" && line.Message.Model != "<synthetic>":
			a, u := answers[line.Message.ID], line.Message.Usage
			a.Input, a.CacheWrite = max(a.Input, u.Input), max(a.CacheWrite, u.CacheWrite)
			a.CacheRead, a.Output = max(a.CacheRead, u.CacheRead), max(a.Output, u.Output)
			answers[line.Message.ID] = a
		}
	}
	var sum counts
	for _, a := range answers {
		sum = counts{sum.Input + a.Input, sum.CacheWrite + a.CacheWrite, sum.CacheRead + a.CacheRead, sum.Output + a.Output}
	}
	return sum, label
}

func firstLine(t *testing.T, content json.RawMessage) string {
	t.Helper()
	var text string
	if err := json.Unmarshal(content, &text); err != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(content, &parts); err != nil {
			t.Fatal(err)
		}
		for _, p := range parts {
			if p.Text != "" {
				text = p.Text
				break
			}
		}
	}
	first, _, _ := strings.Cut(text, "\n")
	return first
}

// TestTestDataOfS1AddsUpToTheTable checks the generated S1 against the table
// of the specification's fixture, so that a failing scenario points at kitsu and
// not at the data.
func TestTestDataOfS1AddsUpToTheTable(t *testing.T) {
	t.Parallel()
	s := buildS1(t, variant{})
	got, label := claudeCount(t, s.Paths["session"])
	if got.total() != 40000 || label != "Run the task add-greeting on the full path." {
		t.Errorf("session: %d tokens, label %q", got.total(), label)
	}
	byLabel := map[string]int{}
	total := got.total()
	for _, st := range s1Starts() {
		c, l := claudeCount(t, s.Paths[st.ID])
		if l != st.Label {
			t.Errorf("%s: first line %q, want %q", st.ID, l, st.Label)
		}
		byLabel[l] += c.total()
		total += c.total()
		if st.Label == "kitsu-task: add-greeting code 1" && c != (counts{20000, 30000, 140000, 10000}) {
			t.Errorf("code 1: %+v", c)
		}
	}
	want := map[string]int{
		"kitsu-task: add-greeting lookup 1": 10000, "kitsu-task: add-greeting spec 1": 60000,
		"kitsu-task: add-greeting spec-review 1": 30000, "kitsu-task: add-greeting design 1": 80000,
		"kitsu-task: add-greeting design-review 1": 30000, "kitsu-task: add-greeting tests 1": 90000,
		"kitsu-task: add-greeting code 1": 200000, "kitsu-task: add-greeting review 1": 50000,
		"kitsu-task: add-greeting code 2": 40000, "kitsu-task: add-greeting review 2": 20000,
		"kitsu-task: add-greeting commit 1": 5000,
	}
	for l, n := range want {
		if byLabel[l] != n {
			t.Errorf("%s: %d tokens, want %d", l, byLabel[l], n)
		}
	}
	if total != 655000 {
		t.Errorf("S1 holds %d tokens, want 655,000", total)
	}
	if len(s.Notices) != len(s1Starts())+1 {
		t.Errorf("%d notices", len(s.Notices))
	}
}

// TestTestDataVariants checks the changes the scenarios make to S1.
func TestTestDataVariants(t *testing.T) {
	t.Parallel()
	all := variant{NoGitLabel: true, EmptyArchitect: true, SecondTask: true}
	s := buildS1(t, all)
	for _, st := range all.starts() {
		switch st.Role {
		case "architect":
			if info, err := os.Stat(s.Paths[st.ID]); err != nil || info.Size() != 0 {
				t.Errorf("the architect record is not an empty file: %v", err)
			}
		case "git":
			if _, l := claudeCount(t, s.Paths[st.ID]); strings.HasPrefix(l, "kitsu-task:") {
				t.Errorf("the git brief still has its label: %q", l)
			}
		}
		if st.Label == "kitsu-task: fix-typo code 1" {
			if c, _ := claudeCount(t, s.Paths[st.ID]); c.total() != 15000 {
				t.Errorf("fix-typo: %d tokens", c.total())
			}
		}
	}
}
