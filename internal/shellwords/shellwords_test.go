package shellwords

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line string
		want [][]string
	}{
		{`git commit -m "a b"`, [][]string{{"git", "commit", "-m", "a b"}}},
		{`echo 'x;y' "q\"r" s\ t ''`, [][]string{{"echo", "x;y", `q"r`, "s t", ""}}},
		{"a;b&&c||d|e&f\ng", [][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}, {"g"}}},
		{"(cd x) && $(y z) `w`", [][]string{{"cd", "x"}, {"$"}, {"y", "z"}, {"w"}}},
		{`echo "$(git log)"`, [][]string{{"$"}, {"git", "log"}, {"echo", "$(git log)"}}},
		{"", nil},
	}
	for _, c := range cases {
		got, ok := Split(c.line)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Split(%q) = %q, %v; want %q, true", c.line, got, ok, c.want)
		}
	}
	for _, line := range []string{`echo 'x`, `echo "x`, `git commit -m "a`} {
		if _, ok := Split(line); ok {
			t.Errorf("Split(%q) reads an unclosed quote", line)
		}
	}
}

func TestSkipsHooks(t *testing.T) {
	t.Parallel()
	// want is a part of the phrase, or "" when the line does not skip.
	cases := []struct{ line, want string }{
		// Spellings that the text rule missed.
		{"git commit -n -m x", "-n"},
		{"git commit -anm x", "-n"},
		{"git -c core.hooksPath=/dev/null commit -m x", "core.hooksPath"},
		{"git -c CORE.HOOKSPATH=/dev/null commit -m x", "core.hooksPath"},
		{"git --config-env=core.hooksPath=X commit -m x", "core.hooksPath"},
		{"git config core.hooksPath /dev/null", "git config"},
		{"git config core.hookspath .husky", "git config"},
		{"git config --global --unset core.hooksPath", "git config"},
		{"git commit --no-verif -m x", "the option --no-verif"},
		{"git commit --no-veri -m x", "the option --no-veri"},
		// Lines that the text rule flagged wrongly.
		{`git commit -m "--no-verify is blocked"`, ""},
		{`echo "--no-verify"`, ""},
		{"grep -- --no-verify docs/hooks.md", ""},
		{"git commit -m --no-verify", ""},
		{"git commit --message --no-verify", ""},
		{"git commit --no-ver -m x", ""},
		// Other subcommands and options.
		{"git push --no-verify", "git push the option --no-verify"},
		{"git merge --no-verify x", "git merge"},
		{"git push -n", ""},
		{"git merge -n x", ""},
		{"git log -n 5", ""},
		{`git commit -m "fix -n handling"`, ""},
		{"git commit -- -n", ""},
		{"git commit -- --no-verify", ""},
		{"git commit -mn", ""},
		{"git commit -uno -m x", ""},
		{"git commit -C HEAD -n", "-n"},
		{"git config --get core.hooksPath", ""},
		{"git config core.hooksPath", ""},
		{"git config get core.hooksPath", ""},
		{"git config --list", ""},
		{"git status", ""},
		{"go test ./...", ""},
		// Where the git call stands in the line.
		{"cd x && git commit -n -m y", "-n"},
		{"sh -c 'git commit -n -m y'", "-n"},
		{`bash -lc "git commit --no-verify"`, "--no-verify"},
		{`sh -c "sh -c 'git commit -n'"`, "-n"},
		{"FOO=1 git commit -n", "-n"},
		{"GIT_CONFIG_PARAMETERS=\"'core.hooksPath=/dev/null'\" git commit -m x", "environment"},
		{"env FOO=1 git commit -n", "-n"},
		{"sudo -u bob nice -n 5 git commit -n", "-n"},
		{"/usr/bin/git commit -n", "-n"},
		{`"C:\\Program Files\\Git\\bin\\git.exe" commit -n`, "-n"},
		{"git -C sub commit -n", "-n"},
		{"git --git-dir .git --no-pager commit -n", "-n"},
		{"echo ok; git push --no-verify origin main", "--no-verify"},
		{`echo "$(git commit -n -m x)"`, "-n"},
		{"sh -c", ""},
		// Wrappers that run the next words as a command (S4).
		{"timeout 5 git commit -n", "-n"},
		{"exec git commit --no-verify", "--no-verify"},
		{"nohup git push --no-verify", "--no-verify"},
		{`eval "git commit -n -m x"`, "-n"},
		{"eval git commit -n", "-n"},
		{"timeout 5 git status", ""},
		{"eval echo hi", ""},
		// A call of cw that turns the gate off (S4).
		{"cw uninstall", "runs `cw uninstall`"},
		{"/usr/local/bin/cw uninstall", "runs `/usr/local/bin/cw uninstall`"},
		{"sudo cw uninstall", "runs `cw uninstall`"},
		{"cw init --observe", "runs `cw init --observe`"},
		{"cw init -observe", "runs `cw init -observe`"},
		{"cw init --observe=true", "runs `cw init --observe=true`"},
		{"cw init", ""},
		{"cw run", ""},
		{"cw status", ""},
		{"echo cw uninstall", ""},
		// Text that cannot be split falls back to the text rule.
		{`git commit --no-verify -m "unclosed`, "holds --no-verify"},
		{`echo "unclosed`, ""},
	}
	for _, c := range cases {
		got := SkipsHooks(c.line)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("SkipsHooks(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}
