package taskflow

import (
	"slices"
	"strings"
	"testing"
)

// Verify's input-copy rule (R6) reads only the stored copies, through a fake
// Artifacts here: no task directory, no repository. Oracle: a copy that is
// missing or no longer hashes to its input's sha256 is a problem, listed in
// assignment record order and then input order; a copy that still matches is
// none, whatever became of its original, which the rule never reads (the
// originals below do not exist). Verify reports these problems first among
// its trace entries; l3_trace_test.go covers that through Verify.
func TestVerifyChecksStoredInputCopies(t *testing.T) {
	input := func(name, content string) InputRef {
		return InputRef{Path: "/originals/" + name, SHA256: digest([]byte(content))}
	}
	copyName := func(assignment string, n int) string {
		return "input-" + assignment + "-" + strings.Repeat(string(rune('0'+n)), 32)
	}
	a, z, m, c, d := input("a.md", "a\n"), input("z.md", "z\n"), input("m.md", "m\n"), input("c.md", "c\n"), input("d.md", "d\n")
	// Record order (zeta, then alpha) and input order (z before m) both
	// differ from sorted order, so a sorted report fails.
	record := Record{Assignments: []WorkerAssignment{
		{Brief: WorkerBrief{ID: "zeta", Inputs: []InputRef{a, z, m, c}}, InputFiles: []string{copyName("zeta", 1), copyName("zeta", 2), copyName("zeta", 3), copyName("zeta", 4)}},
		{Brief: WorkerBrief{ID: "alpha", Inputs: []InputRef{d}}, InputFiles: []string{copyName("alpha", 1)}},
	}}
	artifacts := fakeArtifacts{
		copyName("zeta", 1): []byte("a\n"),        // matches a
		copyName("zeta", 2): []byte("tampered\n"), // differs from z's sha256
		copyName("zeta", 4): []byte("c\n"),        // matches c
		// The copies of m (zeta 3) and d (alpha 1) are missing.
	}
	suffix := ": stored copy is missing or differs from its sha256"
	want := []string{
		"worker assignment zeta input " + z.Path + suffix,
		"worker assignment zeta input " + m.Path + suffix,
		"worker assignment alpha input " + d.Path + suffix,
	}
	if got := inputCopyProblems(artifacts, record); !slices.Equal(got, want) {
		t.Errorf("input copy problems %q, want exactly %q in this order: assignment record order, then input order (R6)", got, want)
	}
}
