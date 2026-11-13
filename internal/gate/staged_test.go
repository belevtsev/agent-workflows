package gate

import (
	"slices"
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
