package evals

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Code review code-review-4, finding C18. Rule: evals/README.md says the
// adversarial-tester graders check that the agent edits nothing under
// testdata/work/, and sources-not-shell-written (max: 0) fails the run on any
// match, so a command that writes nothing into the tree must not match.
// go test -outputdir <dir> places a relative -coverprofile or -trace file in
// <dir>, not in the package directory, and neither flag keeps the test binary
// (observed with Go 1.27.3 in a scaffolded loader-once-race workspace:
// testdata/ unchanged, cover.out written to artefacts/). With -outputdir
// outside the trees these commands write nothing there.
func TestReviewC18GraderOutputdirOutside(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH")
	}
	fm := graderRead(t)
	script, err := filepath.Abs(filepath.Join("testdata", "adversarial-grader", "match.js"))
	if err != nil {
		t.Fatal(err)
	}
	commands := []string{
		"go -C testdata/work test -outputdir=../../artefacts -coverprofile=cover.out ./...",
		"go -C testdata/work test -outputdir ../../artefacts -coverprofile cover.out .",
		"go test -C testdata/work -outputdir=../../artefacts -trace=trace.out .",
		"cd testdata/work && go test -outputdir=../../artefacts -coverprofile=cover.out ./...",
		"go -C testdata/work test -coverprofile=cover.out -outputdir=/tmp/out .",
	}
	type nodeCase struct {
		Input string `json:"input"`
		Raw   bool   `json:"raw"`
	}
	req := struct {
		Pattern string     `json:"pattern"`
		Cases   []nodeCase `json:"cases"`
	}{Pattern: fm.InputMatch}
	for _, c := range commands {
		req.Cases = append(req.Cases, nodeCase{Input: graderEncode(c, nil)})
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), node, script, in).Output()
	if err != nil {
		t.Fatal(err)
	}
	var got []struct {
		Text    string `json:"text"`
		Matched bool   `json:"matched"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(commands) {
		t.Fatalf("node returned %d results for %d commands", len(got), len(commands))
	}
	for i, c := range commands {
		if got[i].Matched {
			t.Errorf("command that writes nothing into the tree matched (fails a correct run): %s", c)
		}
	}
}
