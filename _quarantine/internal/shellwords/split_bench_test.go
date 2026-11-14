package shellwords

import (
	"strconv"
	"strings"
	"testing"
)

// BenchmarkSplit times Split on commands an agent really runs
// (the realistic cases, which must not get slower) and on deeply nested
// commands, whose cost must stay linear in their length.
func BenchmarkSplit(b *testing.B) {
	var file strings.Builder
	for i := range 2000 {
		file.WriteString("line " + strconv.Itoa(i) + `: echo "$HOME" | tr a-z A-Z  # (not a <<marker)` + "\n")
	}
	cases := []struct{ name, command string }{
		{"git_commit_heredoc", "git add -A && git commit -q -F - <<'EOF'\nfix: keep the commit gate shut while a task is open\n\n" +
			"The gate now matches every git form (git -C dir commit, git commit -am).\n\n" +
			"Co-Authored-By: Claude <noreply@anthropic.com>\nEOF\ngit log --oneline -1"},
		{"cat_2000_lines", "cat > notes.md <<'EOF'\n" + file.String() + "EOF\ncw task verify --task \"$TASK\" --summary"},
		{"jq_substitution", `cw task assign --task "$TASK" --input "$(jq -n --arg id review-1 --arg goal "$(cat goal.txt)" ` +
			`'{id: $id, role: "review", goal: $goal, owned_paths: ["internal/usagelog"], check_ids: (["unit"] | map(.))}')" --summary`},
		{"quoted_markers_500KB", "echo '" + strings.Repeat("a<<b ", 100_000) + "'\ncw task start --repo r"},
	}
	for _, n := range []int{5_000, 20_000, 80_000} {
		cases = append(cases,
			struct{ name, command string }{"deep_lines_" + strconv.Itoa(n/1000) + "k", strings.Repeat("( ", n) + strings.Repeat("\n", n)},
			struct{ name, command string }{"deep_markers_" + strconv.Itoa(n/1000) + "k", strings.Repeat("( ", n) + strings.Repeat("a<<b ", n)},
		)
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.SetBytes(int64(len(c.command)))
			for b.Loop() {
				Split(c.command)
			}
		})
	}
}
