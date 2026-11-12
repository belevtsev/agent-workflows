package gate

import (
	"math/rand"
	"strings"
	"testing"
)

// formPiece is one command form the generator combines, with what a shell
// does with it: whether it runs a git commit or push in the directory it
// starts in (every shell among bash, zsh and dash that runs it at all), and
// whether only a newline or the end may follow it.
type formPiece struct {
	text      string
	publishes bool
	endsLine  bool
}

// formPieces is the allowlist the generator draws from: forms that only
// mention git, forms that run it, and the forms the review of the
// shellwords gate found or that make its reading uncertain.
var formPieces = []formPiece{
	// Mentions only.
	{text: `echo "todo; git commit later"`},
	{text: `rg "foo|git commit" docs`},
	{text: `echo 'git push; x'`},
	{text: `printf '%s\n' "a && git push"`},
	{text: `jq -n '"git push"'`},
	{text: `git log --grep 'git push; x'`},
	{text: `echo "${X:-a; git push}"`},
	{text: "cat > /dev/null <<'EOF'\ngit push origin main\nEOF", endsLine: true},
	{text: "cat > /dev/null <<EOF\nrun git commit -m x\nEOF", endsLine: true},
	{text: "cat > /dev/null <<'END-OF-MSG'\ngit push\nEND-OF-MSG", endsLine: true},
	{text: "cat > /dev/null <<E\\OF\ngit push\nEOF", endsLine: true},
	{text: "cat > /dev/null <<EOF-X\n${X:-it's}\nEOF-X", endsLine: true},
	{text: "true # ; git push", endsLine: true},
	// Neither.
	{text: "true"},
	{text: "echo a\r#b"},
	{text: `echo "${X:-it's}"`},
	{text: "echo $((1 + 2))"},
	{text: "make build"},
	// Runs git commit or push.
	{text: "git push", publishes: true},
	{text: "git commit -m x", publishes: true},
	{text: `git commit -m "fix; git push"`, publishes: true},
	{text: `echo "$(git push)"`, publishes: true},
	{text: "echo `git commit -m x`", publishes: true},
	{text: "sh -c 'git push'", publishes: true},
	{text: `bash -c "git commit -m 'a b'"`, publishes: true},
	{text: `eval "git push"`, publishes: true},
	{text: "{ git push; }", publishes: true},
	{text: "((git push) )", publishes: true},
	{text: "echo $((git push) )", publishes: true},
	{text: "env -S 'git push'", publishes: true},
	{text: `echo "x; git push" | sh`, publishes: true},
	{text: "git rebase --exec 'true; git push' HEAD~1", publishes: true},
	{text: `y="$(case a in a) git push;; esac)"`, publishes: true},
	{text: "cat <<'EOF' | sh\ngit push\nEOF", publishes: true, endsLine: true},
	{text: "bash <<'EOF'\ngit push\nEOF", publishes: true, endsLine: true},
	{text: "eval \"$(cat <<'EOF'\ntrue; git push\nEOF\n)\"", publishes: true},
	{text: "echo \"$(cat <<EOF\nmsg\nEOF)\"; git push", publishes: true},
	{text: "cat > /dev/null <<EOF\nprice $((1+\nEOF\ngit push", publishes: true},
}

// generateForm combines pieces into a command line up to depth levels of
// wrappers and lists, and reports whether a shell runs a git commit or push
// in it. A list runs every member here: the gate reads `false && git push`
// as a push, so the oracle does too.
func generateForm(rng *rand.Rand, depth int) formPiece {
	if depth == 0 || rng.Intn(3) == 0 {
		return formPieces[rng.Intn(len(formPieces))]
	}
	inner := generateForm(rng, depth-1)
	end := ""
	if inner.endsLine {
		end = "\n"
	}
	wrap := func(text string) formPiece { return formPiece{text: text, publishes: inner.publishes} }
	switch rng.Intn(9) {
	case 0:
		return wrap("(" + inner.text + end + ")")
	case 1:
		return wrap("{ " + inner.text + end + "; }")
	case 2:
		return wrap(`echo "$( ` + inner.text + end + ` )"`) // "$((" would be arithmetic
	case 3:
		return wrap("x=$( " + inner.text + end + " )")
	case 4:
		return wrap("if true; then " + inner.text + end + "; fi")
	case 5:
		if strings.Contains(inner.text, "'") {
			return inner
		}
		return wrap("sh -c '" + inner.text + "'")
	case 6:
		return formPiece{text: inner.text + " # c ; git push", publishes: inner.publishes, endsLine: true}
	}
	next := generateForm(rng, depth-1)
	separator := []string{"; ", " && ", " || ", " | ", "\n"}[rng.Intn(5)]
	if inner.endsLine {
		separator = "\n"
	}
	return formPiece{text: inner.text + separator + next.text, publishes: inner.publishes || next.publishes, endsLine: next.endsLine}
}

// TestGateFailsClosed compares the gate with the flat split it replaced on
// generated command lines. Oracle: wherever the gate finds no commit or push
// and the flat split finds one, the shellwords reading is certain and the
// generator's oracle says no shell runs a git commit or push in the line.
// The flat split's misses that the gate shares, such as a case pattern
// inside $(...), are outside this property.
func TestGateFailsClosed(t *testing.T) {
	rng := rand.New(rand.NewSource(20261112))
	const cwd = "/gated"
	removed, uncertain := 0, 0
	for range 20000 {
		form := generateForm(rng, 3)
		found, certain := splitPublications(form.text, cwd)
		if !certain {
			uncertain++
		}
		gate, flat := gitPublications(form.text, cwd), flatPublications(form.text, cwd)
		if len(gate) > 0 || len(flat) == 0 {
			continue
		}
		removed++
		if !certain || form.publishes {
			t.Fatalf("gate allows %q, which the flat split denies: certain %v (found %v), runs a commit or push %v",
				form.text, certain, found, form.publishes)
		}
	}
	// Both sides of the property occur: the gate drops flat denies, and
	// falls back on uncertain readings.
	if removed == 0 || uncertain == 0 {
		t.Fatalf("generated %d removed denies and %d uncertain readings, want some of each", removed, uncertain)
	}
}
