package evals

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Code review code-review-6, finding C22, superseded by adversarial-evals
// revision 8 (G17), the owner's fail-closed decision. C22 asked that these
// commands not match: after cd testdata/work, $PWD/../../artefacts and
// $(pwd)/../../artefacts name the workspace's artefacts/ directory, go test
// writes c.out or t.out only there and testdata/ is unchanged (observed with
// Go 1.27.3 in scaffolded loader-once-race, frame-length-limit and
// path-prefix-escape workspaces). Revision 8 fails closed instead: an
// -outputdir value that starts with a shell variable and contains .. never
// counts as outside the trees, because such values can climb back into a tree
// (for example $PWD/../../artefacts/../testdata/work). So each command below
// is an accepted-cost command: it writes only into artefacts/, yet
// sources-not-shell-written (max: 0) must match it. The test fails when the
// fail-closed rule for $ -outputdir values with .. is lost.
func TestReviewC22GraderDollarArtefactsViaParent(t *testing.T) {
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
		"cd testdata/work && go test -outputdir=$PWD/../../artefacts -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir=$(pwd)/../../artefacts -coverprofile=c.out .",
		"cd testdata/work && go test -outputdir=\"$PWD/../../artefacts\" -trace=t.out .",
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
			t.Errorf("accepted-cost command not matched: it writes only into artefacts/, but the fail-closed rule for $ -outputdir values with .. (adversarial-evals revision 8, G17) must match it; that rule was lost: %s", c)
		}
	}
}
