package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/harness"
)

const storyFixture = `{"version":1,"goal":"Export books","scope":"Keep the default format","criteria":[{"id":"empty","expected_behavior":"Empty JSON output is []","check":"behavior"}],"behavior":{"expected_behavior":"No entries are exported","argv":["private-command-argv"]}}`

func TestConfiguredStoryReviewIntake(t *testing.T) {
	repo, root := guardRepo(t, "")
	t.Chdir(root)
	t.Setenv("KITSU_TEST_STORY_KEY", "fixture-story-secret")
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("APPDATA", config)
	t.Setenv("HOME", config)

	f := &nativeCLIFixture{t: t, root: root}
	f.status.Store(http.StatusOK)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		f.calls.Add(1)
		if req.URL.Path != "/v1/systemone" || req.Header.Get("Authorization") != "Bearer fixture-story-secret" {
			t.Error("wrong request path or authorization")
		}
		body, err := io.ReadAll(req.Body)
		var batch struct {
			Model     string                     `json:"model"`
			State     string                     `json:"state"`
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err != nil || json.Unmarshal(body, &batch) != nil || batch.Model != advisers.StoryModel ||
			len(batch.Questions) != 6 || !strings.Contains(batch.State, "Keep the default format") ||
			strings.Contains(string(body), "private-command-argv") || strings.Contains(string(body), "fixture-story-secret") {
			t.Error("unexpected story context or question count")
		}
		// A second invocation can read the ledger while the adviser is running.
		var view bytes.Buffer
		if err := Execute(t.Context(), []string{"task", "next", "disabled", "--brief"}, Config{Out: &view, Err: &view}); err != nil {
			t.Errorf("story review held the ledger lock: %v", err)
		}
		if status := f.status.Load(); status != http.StatusOK {
			w.WriteHeader(int(status))
			return
		}
		choice := "clear"
		if strings.Contains(batch.State, "Which output?") {
			choice = "gap"
		}
		answers := map[string]any{}
		for _, id := range []string{"goal_actor", "scope", "behavior", "testability", "failure_cases", "compatibility"} {
			if len(batch.Questions[id]) == 0 {
				t.Errorf("missing dimension %s", id)
			}
			probs := map[string]float64{"clear": 0.03, "gap": 0.03, "uncertain": 0.03, "not_applicable": 0.03}
			probs[choice] = 0.91
			answers[id] = map[string]any{"type": "choice", "choice": choice, "probabilities": probs}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": advisers.StoryModel, "answers": answers,
			"usage": map[string]int{"input_tokens": 37, "output_tokens": 4}})
	}))
	t.Cleanup(server.Close)
	f.client = server.Client()
	f.configuration = fmt.Sprintf("version = 2\n[advisers.jev]\nprovider = 'typesafe'\nbase_url = %q\napi_key_env = 'KITSU_TEST_STORY_KEY'\n[access]\ncoordinator = ['jev']\n", server.URL+"/v1")
	f.write(advisers.FileName, f.configuration)

	if help := f.run("", true, "advisers", "story-review", "enable", "--help"); !strings.Contains(help, "story-review enable --adviser NAME") {
		t.Fatalf("missing story review help: %s", help)
	}
	for _, args := range [][]string{{"enable", "--adviser", "jev"}, {"disable"}} {
		command := append([]string{"advisers", "story-review"}, args...)
		err := Execute(t.Context(), command, Config{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard})
		if err == nil || !strings.Contains(err.Error(), "in your own terminal") {
			t.Fatalf("%v did not reach terminal approval: %v", command, err)
		}
	}
	for _, command := range []string{"kitsu advisers story-review enable --adviser jev", "kitsu adviser story-review disable"} {
		why := guardCheckTrust(t.Context(), repo, command)
		if !strings.Contains(why, "own terminal") {
			t.Fatalf("guard allowed %q", command)
		}
	}

	first := startStoryFixture(f, "disabled", "Clear story")
	if got := f.run(storyFixture, true, first...); strings.Contains(got, "story_review") || f.calls.Load() != 0 {
		t.Fatalf("story review was enabled by configuration alone: %s", got)
	}
	_, policy, err := workingCheckPolicy(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	_, pin, err := storyDefinition(repo, "jev", policy)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := storyApprovalFile(repo)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(storyApproval{Version: 1, Adviser: "jev", Policy: pin})
	if err := os.MkdirAll(filepath.Dir(snapshot.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if reason := editReason(t.Context(), repo, root, []string{snapshot.path}); reason != checkTrustEditReason {
		t.Fatalf("story approval is unprotected: %q", reason)
	}

	for i, c := range []struct{ id, story, choice string }{
		{"clear-story", "Export books with JSON", "clear"},
		{"gap-story", "Which output?", "gap"},
	} {
		args := startStoryFixture(f, c.id, c.story)
		var view struct {
			Review *harness.StoryReview `json:"story_review"`
			Next   string               `json:"next"`
		}
		for range 2 {
			output := f.run(storyFixture, true, args...)
			err := json.Unmarshal([]byte(output), &view)
			if err != nil || view.Next != "implement" || view.Review == nil || view.Review.Status != "available" ||
				len(view.Review.Answers) != 6 || view.Review.Answers["scope"].Choice != c.choice ||
				view.Review.Answers["scope"].Confidence != nil || view.Review.InputTokens == nil ||
				*view.Review.InputTokens != 37 || f.calls.Load() != int32(i+1) {
				t.Fatalf("unexpected intake: %s (%v)", output, err)
			}
		}
	}
	f.status.Store(http.StatusServiceUnavailable)
	args := startStoryFixture(f, "outage", "Export books")
	for range 2 {
		output := f.run(storyFixture, true, args...)
		if !strings.Contains(output, "\"unavailable\"") || !strings.Contains(output, "\"implement\"") || f.calls.Load() != 3 {
			t.Fatalf("outage blocked intake or retried: %s", output)
		}
	}

	args = startStoryFixture(f, "lost-result", "Export books")
	loseStoryResult(t, args)
	if output := f.run(storyFixture, true, args...); !strings.Contains(output, "did not complete") || f.calls.Load() != 3 {
		t.Fatalf("an interrupted attempt relaunched: %s", output)
	}

	f.write(advisers.FileName, strings.Replace(f.configuration, "coordinator = ['jev']", "coordinator = []", 1))
	output := f.run(storyFixture, true, startStoryFixture(f, "policy-change", "Export books")...)
	if !strings.Contains(output, "re-enable story review") || f.calls.Load() != 3 {
		t.Fatalf("changed approval sent a story: %s", output)
	}
}

func startStoryFixture(f *nativeCLIFixture, id, request string) []string {
	f.t.Helper()
	out := f.run("", true, "task", "open", id, "--entry", "implement", "--until", "verify", "--request", request, "--brief")
	var view struct{ Ticket string }
	if err := json.Unmarshal([]byte(out), &view); err != nil || view.Ticket == "" {
		f.t.Fatalf("invalid intake ticket: %s, %v", out, err)
	}
	return []string{"task", "record", id, "intake", "--ticket", view.Ticket, "--actor", "host", "--note", "Story intake", "--feature-brief", "-", "--brief"}
}

func loseStoryResult(t *testing.T, args []string) {
	t.Helper()
	marker := new(int)
	defer func() {
		if recover() != marker {
			t.Fatal("expected the intake to reserve a request")
		}
	}()
	_ = Execute(t.Context(), args, Config{In: strings.NewReader(storyFixture), Out: io.Discard, Err: io.Discard,
		NativeExecute: func(context.Context, *advisers.NativeCall) advisers.Execution { panic(marker) }})
}
