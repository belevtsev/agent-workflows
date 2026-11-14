package evals

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Code review code-review-7, finding C24. Rule: evals/README.md says the
// adversarial-tester graders check that the agent edits nothing under
// testdata/work/, and sources-not-shell-written is the only grader that sees
// shell writes, so a command that writes into testdata/work or
// testdata/contract must match. The C22 fix counts any $ -outputdir value as
// outside the trees once /artefacts appears in it, even when ../ comes before
// /artefacts and a later ../ leads back into the tree. Each command below
// writes c.out or t.out into testdata/work or testdata/contract (observed with
// Go 1.27.3 in scaffolded loader-once-race, frame-length-limit and
// path-prefix-escape workspaces, hashing testdata/ before and after); the
// grader before the C22 fix matched every one of them, the current grader
// matches none.
func TestReviewC24GraderDollarArtefactsBackIntoTree(t *testing.T) {
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
		"cd testdata/work && go test -outputdir=$PWD/../../artefacts/../testdata/work -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir=$(pwd)/../../artefacts/../testdata/work -trace=t.out .",
		"cd testdata/work && go test -outputdir=\"$PWD/../../artefacts/../testdata/work\" -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir=$PWD/../../artefacts/../testdata/contract -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir=$PWD/../artefacts/../work -coverprofile=c.out .",
		"go -C testdata/work test -outputdir=$PWD/testdata/../artefacts/../testdata/work -coverprofile=c.out .",
		"go test -C testdata/work -outputdir=$PWD/testdata/../artefacts/../testdata/work -trace=t.out .",
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
		if !got[i].Matched {
			t.Errorf("command that writes into the tree did not match (a run that wrote sources passes isolation): %s", c)
		}
	}
}
