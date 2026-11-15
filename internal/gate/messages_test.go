package gate

import (
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/evidence"
	"github.com/belevtsev/gatecub/v3/internal/store"
)

// A list is printed as gatecub.toml holds it, and an absent list as "not set"
// (second challenge, finding U13).
func TestListText(t *testing.T) {
	cases := map[string][]string{
		"not set":            nil,
		`["sh", "unit.sh"]`:  {"sh", "unit.sh"},
		`["src/**"]`:         {"src/**"},
		`["a \"quoted\" b"]`: {`a "quoted" b`},
	}
	for want, items := range cases {
		if got := listText(items); got != want {
			t.Errorf("listText(%q) = %s, want %s", items, got, want)
		}
	}
}

// The line of a check whose last run decided nothing says how that run
// ended (second challenge, finding U2).
func TestDescribeNamesTheLastRunThatDecidedNothing(t *testing.T) {
	st := store.Open(t.TempDir())
	for outcome, detail := range map[string]string{
		store.SourceChanged: "the source changed during the run: out.txt",
		store.Interrupted:   "",
	} {
		c := evidence.Check{State: evidence.NotRun, Last: &store.Record{Outcome: outcome, Detail: detail}}
		want := "not run on this content (the last run ended " + outcome
		if detail != "" {
			want += ": " + detail
		}
		want += ")"
		if got := Describe(st, c); got != want {
			t.Errorf("Describe = %q, want %q", got, want)
		}
	}
	if got := Describe(st, evidence.Check{State: evidence.NotRun}); got != "not run on this content" {
		t.Errorf("Describe without a last run = %q", got)
	}
}

// A denial says to fix a failed check first only when a run decided it
// (second challenge, finding U3), and names how to set changes aside when
// the commit leaves files out (finding U4).
func TestDenialInstruction(t *testing.T) {
	const fixFirst = "gatecub: a check failed or timed out; first fix what its log shows.\n"
	st := store.Open(t.TempDir())
	code := 3
	failed := verdict(check("unit", evidence.Failed, &store.Record{Outcome: store.Failed, ExitStatus: &code, Log: "logs/r1.log"}), check("lint", evidence.NotRun, nil))
	timedOut := verdict(check("slow", evidence.TimedOut, &store.Record{Outcome: store.TimedOut, Log: "logs/r2.log"}))
	notRun := verdict(check("unit", evidence.NotRun, nil), check("lint", evidence.NotRun, nil))

	for name, r := range map[string]Ruling{
		"failed":    {Hook: "pre-commit", Verdict: failed},
		"timed out": {Hook: "pre-commit", Verdict: timedOut},
	} {
		text := r.Denial(st)
		fix, run := strings.Index(text, fixFirst), strings.LastIndex(text, "  gatecub run\n")
		if fix < 0 || run < fix {
			t.Errorf("%s: the denial does not hold %q before the command:\n%s", name, fixFirst, text)
		}
	}
	if text := (Ruling{Hook: "pre-commit", Verdict: notRun}).Denial(st); strings.Contains(text, "failed or timed out") {
		t.Errorf("a denial of checks that were only not run says that one failed:\n%s", text)
	}

	text := Ruling{Hook: "pre-commit", Verdict: notRun, Elsewhere: true, Differs: "The working tree holds other content than this commit."}.Denial(st)
	for _, want := range []string{"git add -A", "git stash push --keep-index --include-untracked", "  gatecub run\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("the denial of a commit that leaves files out does not hold %q:\n%s", want, text)
		}
	}
	if text := (Ruling{Hook: "pre-commit", Verdict: notRun}).Denial(st); !strings.HasSuffix(text, "gatecub: to produce the missing evidence, run:\n  gatecub run\n") || strings.Contains(text, "git stash") {
		t.Errorf("a denial whose content is the working tree's does not end with the plain instruction:\n%s", text)
	}
}
