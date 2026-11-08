package evals

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Code review code-review-3, finding C12. Rule: evals/README.md says the
// adversarial-tester graders check that the agent edits nothing under
// testdata/work/, and sources-not-shell-written already treats a go test
// that leaves <pkg>.test in the tree (-c without an outside -o) as a write.
// go test with -cpuprofile, -memprofile, -blockprofile or -mutexprofile also
// writes <pkg>.test into the package directory, wherever the profile goes
// (observed with Go 1.27.3: memo.test appears in testdata/work), so these
// commands must match too. Conversely a command that writes nothing must not
// match, since the grader (max: 0) then fails a correct run.
func TestReviewC12GraderGoTestBinaryWrites(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH")
	}
	fm := graderRead(t)
	script, err := filepath.Abs(filepath.Join("testdata", "adversarial-grader", "match.js"))
	if err != nil {
		t.Fatal(err)
	}
	// write is true for a command that leaves a file in testdata/work.
	samples := []struct {
		command string
		write   bool
	}{
		{"go -C testdata/work test -count=1 -cpuprofile=/tmp/cpu.out .", true},
		{"go -C testdata/work test -race -blockprofile=/tmp/block.out .", true},
		{"go -C testdata/work test -race -mutexprofile=../../artefacts/mutex.out .", true},
		{"cd testdata/work && go test -memprofile=/tmp/mem.out .", true},
		{"go test -C testdata/work -cpuprofile=/tmp/cpu.out .", true},
		// go build of a single non-main package (the case's only package, by
		// import path or by file) discards the result and writes nothing
		// (go help build; observed: testdata/work unchanged).
		{"go -C testdata/work build example.com/memo", false},
		{"go -C testdata/work build memo.go", false},
		{"cd testdata/work && go build example.com/memo", false},
	}
	var commands []string
	for _, s := range samples {
		commands = append(commands, s.command)
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
	if len(got) != len(samples) {
		t.Fatalf("node returned %d results for %d samples", len(got), len(samples))
	}
	for i, s := range samples {
		switch {
		case s.write && !got[i].Matched:
			t.Errorf("write of <pkg>.test into testdata/work not matched: %s", s.command)
		case !s.write && got[i].Matched:
			t.Errorf("command that writes nothing matched (fails a correct run): %s", s.command)
		}
	}
}
