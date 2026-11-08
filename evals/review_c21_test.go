package evals

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Code review code-review-5, finding C21. Rule: evals/README.md says the
// adversarial-tester graders check that the agent edits nothing under
// testdata/work/, and sources-not-shell-written is the only grader that sees
// shell writes, so a go command that writes a file into the tree must match.
// go test -outputdir <dir> places a relative -coverprofile or -trace file in
// <dir>. When <dir> is the tree, given as $PWD, ${PWD}, $(pwd) or "$PWD" after
// cd testdata/work, or as $(pwd)/testdata/work or "$PWD"/testdata/work under
// go -C testdata/work, the file lands in testdata/work (observed with Go
// 1.27.3 in scaffolded loader-once-race and path-prefix-escape workspaces:
// testdata/work/c.out or t.out created). The grader before the C18 fix matched
// every one of these commands; the C18 fix treats any -outputdir value that
// starts with $ as outside the trees and stops matching them.
func TestReviewC21GraderOutputdirIntoTreeByVariable(t *testing.T) {
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
		"cd testdata/work && go test -outputdir=$PWD -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir=\"$PWD\" -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir=$(pwd) -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir=${PWD} -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir \"$(pwd)\" -trace=t.out .",
		"cd testdata/work && go test -outputdir=/tmp/out -outputdir=$PWD -coverprofile=c.out .",
		"go -C testdata/work test -outputdir=$(pwd)/testdata/work -coverprofile=c.out .",
		"go -C testdata/work test -outputdir=\"$(pwd)/testdata/work\" -coverprofile=c.out .",
		"go -C testdata/work test -outputdir=\"$PWD\"/testdata/work -coverprofile=c.out .",
		"go test -C testdata/work -outputdir=$(pwd)/testdata/work -trace=t.out .",
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
			t.Errorf("command that writes into testdata/work did not match (passes a run that wrote sources): %s", c)
		}
	}
}
