package usagelog

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// recordingObserver writes each call as one line of text.
type recordingObserver struct{ calls *[]string }

func (o recordingObserver) Session(id string) { *o.calls = append(*o.calls, "session "+id) }
func (o recordingObserver) Agent(agent Agent, main bool) LineObserver {
	*o.calls = append(*o.calls, fmt.Sprintf("agent %s %v", agent.AgentID, main))
	return o
}
func (o recordingObserver) Assistant(_ []byte, key, model string, tokens Tokens, recorded, history bool) {
	*o.calls = append(*o.calls, fmt.Sprintf("assistant %s %s %d %v %v", key, model, tokens.TotalInputTokens, recorded, history))
}
func (o recordingObserver) User([]byte)       { *o.calls = append(*o.calls, "user") }
func (o recordingObserver) Attachment([]byte) { *o.calls = append(*o.calls, "attachment") }
func (o recordingObserver) System([]byte)     { *o.calls = append(*o.calls, "system") }

func TestImportObservedPassesClaudeLinesInOrder(t *testing.T) {
	// The lines are written by hand from the fields claudeHead and
	// claudeEntry read; none comes from a real session.
	request := func(session, requestID, model string, input, read int) string {
		return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"requestId":%q,"message":{"id":"m-%s","model":%q,"usage":`+
			`{"input_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d,"output_tokens":1}}}`, session, requestID, requestID, model, input, read)
	}
	write := func(path string, lines ...string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "s1.jsonl")
	write(main,
		`{"type":"attachment","sessionId":"s1","attachment":{"type":"instructions"},"rendered":[{"content":"rules"}]}`,
		`{"type":"user","sessionId":"s1","message":{"role":"user","content":"hi"}}`,
		request("s1", "r1", "m-opus", 10, 90),
		request("s1", "r1", "m-opus", 10, 90),
		request("s1", "rs", "<synthetic>", 1, 0),
		`{"type":"assistant","sessionId":"s1","requestId":"rn","message":{"id":"m-rn","model":"m-opus"}}`,
		request("s0", "old", "m-opus", 500, 0),
		`{"type":`,
		`{"type":"system","sessionId":"s1","subtype":"compact_boundary"}`,
		`{"type":"cost-state","sessionId":"s1","totalCostUSD":0.1,"modelUsage":{}}`,
	)
	write(filepath.Join(dir, "s1", "subagents", "agent-a1.jsonl"), request("s1", "q1", "m-sonnet", 20, 0))

	var calls []string
	report, err := ImportObserved(t.Context(), Options{ClaudeTranscript: main}, recordingObserver{&calls})
	if err != nil {
		t.Fatal(err)
	}
	// Oracle: the lines in file order, main first. A request with usable
	// usage carries its model and total input (r1: 10+90 = 100; q1: 20);
	// its repeat is not recorded; synthetic and usage-less lines pass no
	// model and zero tokens; the s0 line is another session's history.
	// Malformed and cost-state lines pass nothing.
	want := []string{
		"session s1", "agent main true",
		"attachment", "user",
		"assistant r1 m-opus 100 true false",
		"assistant r1 m-opus 100 false false",
		"assistant rs  0 false false",
		"assistant rn  0 false false",
		"assistant old  0 false true",
		"system",
		"agent agent-a1 false",
		"assistant q1 m-sonnet 20 true false",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("observer calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	// Oracle: observing changes nothing; the report is Import's.
	plain, err := Import(t.Context(), Options{ClaudeTranscript: main})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report, plain) {
		t.Errorf("ImportObserved report differs from Import:\n%+v\n%+v", report, plain)
	}
}
