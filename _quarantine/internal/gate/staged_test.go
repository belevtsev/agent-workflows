package gate

import (
	"slices"
	"strings"
	"testing"
)

// TestCommitStagedReadsWhatTheCommitRecords reads command lines as the
// plugin hook does. Oracle, from git commit's documentation: a plain commit
// records the index, -a the working tree of every indexed path, and
// pathspecs, -i or -o the named paths from the working tree; a commit after
// a command that may change the index (anything but cd, the read-only
// trusted commands and git push) cannot be judged by the index as it stands.
func TestCommitStagedReadsWhatTheCommitRecords(t *testing.T) {
	const cwd = "/repo"
	for _, form := range []struct {
		command string
		want    []stagedRule
	}{
		{"git commit -m x", []stagedRule{stagedIndex}},
		{"git commit -qm 'a b'", []stagedRule{stagedIndex}},
		{"git commit --message x --author 'A <a@b>' --amend", []stagedRule{stagedIndex}},
		{"git commit -F msg.txt -S", []stagedRule{stagedIndex}},
		{"git commit -am x", []stagedRule{stagedRemovals}},
		{"git commit --all -m x", []stagedRule{stagedRemovals}},
		{"git commit -m x b.txt", []stagedRule{stagedUnknown}},
		{"git commit -m x -- b.txt", []stagedRule{stagedUnknown}},
		{"git commit -i -m x b.txt", []stagedRule{stagedUnknown}},
		{"git commit --only -m x b.txt", []stagedRule{stagedUnknown}},
		{"git commit --pathspec-from-file=list -m x", []stagedRule{stagedUnknown}},
		// -u and -S take only an attached value: never -o, -a or the next word.
		{"git commit -uno -m x", []stagedRule{stagedIndex}},
		{"git commit -unormal -m x", []stagedRule{stagedIndex}},
		{"git commit -uall -m x", []stagedRule{stagedIndex}},
		{"git commit -Sabc -m x", []stagedRule{stagedIndex}},
		{"git commit -S -m x", []stagedRule{stagedIndex}},
		{"git commit -quno -m x", []stagedRule{stagedIndex}},
		{"git commit -auno -m x", []stagedRule{stagedRemovals}},
		{"git commit -qSkey -am x", []stagedRule{stagedRemovals}},
		// A command that writes a file or runs a program may write the index.
		{"cat saved.idx > .git/index && git commit -m x", []stagedRule{stagedUnknown}},
		{"printf x > .git/index; git commit -m x", []stagedRule{stagedUnknown}},
		{"rg --pre prog x . && git commit -m x", []stagedRule{stagedUnknown}},
		{"echo hi > /dev/null 2>&1 && git commit -m x", []stagedRule{stagedIndex}},
		{"git commit -q -F - <<EOF\n$(git add a.txt; echo msg)\nEOF", []stagedRule{stagedUnknown}},
		// Another index, git directory or work tree.
		{"GIT_INDEX_FILE=alt git commit -m x", []stagedRule{stagedUnknown}},
		{"env GIT_DIR=x git commit -m x", []stagedRule{stagedUnknown}},
		{"GIT_WORK_TREE=. git commit -m x", []stagedRule{stagedUnknown}},
		{"git --git-dir=.git commit -m x", []stagedRule{stagedUnknown}},
		{"git --work-tree . commit -m x", []stagedRule{stagedUnknown}},
		{"cd /repo && echo hi | cat && git push && git commit -m x", []stagedRule{stagedIndex}},
		{"git add -A && git commit -m x", []stagedRule{stagedUnknown}},
		{"git commit -m a; git commit -m b", []stagedRule{stagedIndex, stagedUnknown}},
		{"sh -c 'git add . && git commit -m x'", []stagedRule{stagedUnknown}},
		{"cp a b && git commit -m x", []stagedRule{stagedUnknown}},
	} {
		var got []stagedRule
		for _, found := range gitPublications(form.command, cwd, "") {
			if !found.push && !slices.Contains(got, found.staged) {
				got = append(got, found.staged)
			}
		}
		if !slices.Equal(got, form.want) {
			t.Errorf("%q: commits read as %v, want %v", form.command, got, form.want)
		}
	}
}

// TestCommitsAllReadsAttachedOptionValues: -u and -S take only an attached
// value, so -uall and -Sabc do not commit all; oracle, git commit's options.
func TestCommitsAllReadsAttachedOptionValues(t *testing.T) {
	for arguments, want := range map[string]bool{"-uall -m x": false, "-Sabc -m x": false, "-uno": false, "-qSkey -am x": true, "-S -a": true} {
		if got := commitsAll(strings.Fields(arguments)); got != want {
			t.Errorf("commitsAll(%s) = %v, want %v", arguments, got, want)
		}
	}
}
