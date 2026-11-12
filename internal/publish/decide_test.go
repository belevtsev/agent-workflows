//go:build !windows

package publish

import (
	"slices"
	"testing"
)

// errText is err's message, or "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// oracle: commitSet's contract: the authorized paths whose verified state
// differs from the head's, sorted, and [] (not nil, so the report prints [])
// when there are none.
func TestCommitSetKeepsChangedAuthorizedPathsSorted(t *testing.T) {
	file := func(mode, sum string) pathState { return pathState{present: true, mode: mode, sha256: sum} }
	head := map[string]pathState{"a.txt": file("100644", "1"), "b.txt": file("100644", "2"), "gone.txt": file("100644", "3"), "tool": file("100644", "4")}
	verified := map[string]pathState{"a.txt": file("100644", "1x"), "b.txt": file("100644", "2"), "new.txt": file("100644", "5"), "tool": file("100755", "4")}
	cases := []struct {
		name             string
		authorized, want []string
	}{
		{"none authorized", nil, []string{}},
		{"unchanged path left out", []string{"b.txt"}, []string{}},
		{"absent on both sides left out", []string{"never.txt"}, []string{}},
		{"content change", []string{"a.txt"}, []string{"a.txt"}},
		{"mode change", []string{"tool"}, []string{"tool"}},
		{"new path", []string{"new.txt"}, []string{"new.txt"}},
		{"deleted path", []string{"gone.txt"}, []string{"gone.txt"}},
		{"sorted, unchanged dropped", []string{"tool", "new.txt", "b.txt", "gone.txt", "a.txt"}, []string{"a.txt", "gone.txt", "new.txt", "tool"}},
	}
	for _, c := range cases {
		if got := commitSet(c.authorized, verified, head); got == nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: commitSet(%q) = %#v, want %q", c.name, c.authorized, got, c.want)
		}
	}
}

// oracle: cw's reading of Git's boolean rules as gitBool's comment states
// them (words in any case, empty is false, signed decimal or 0x integers with
// a k/m/g suffix are true when non-zero, anything else is not recognised).
// The rows pin cw's current parser. Two rows differ from git 2.56, which
// rejects 3g (it overflows Git's int) and reads a quoted " 1" as true; both
// predate this test and fail closed or fail in git itself.
func TestGitBoolFollowsGitBooleanRules(t *testing.T) {
	cases := []struct {
		value               string
		valueless, want, ok bool
	}{
		{"", true, true, true},
		{"true", false, true, true}, {"YES", false, true, true}, {"On", false, true, true},
		{"false", false, false, true}, {"No", false, false, true}, {"OFF", false, false, true}, {"", false, false, true},
		{"0", false, false, true}, {"-0", false, false, true}, {"0x0", false, false, true}, {"0k", false, false, true},
		{"1", false, true, true}, {"-1", false, true, true}, {"+2", false, true, true}, {"007", false, true, true},
		{"1k", false, true, true}, {"2M", false, true, true}, {"3g", false, true, true},
		{"0x10", false, true, true}, {"0XfF", false, true, true}, {"-0x1", false, true, true},
		{"maybe", false, false, false}, {"t", false, false, false}, {"1t", false, false, false}, {"1kk", false, false, false},
		{"k", false, false, false}, {"-", false, false, false}, {"--1", false, false, false}, {"+-1", false, false, false},
		{"0x", false, false, false}, {"0xg", false, false, false}, {"1.5", false, false, false}, {" 1", false, false, false},
		{"99999999999999999999", false, false, false},
	}
	for _, c := range cases {
		if got, ok := gitBool(c.value, c.valueless); got != c.want || ok != c.ok {
			t.Errorf("gitBool(%q, valueless=%v) = %v, %v; want %v, %v", c.value, c.valueless, got, ok, c.want, c.ok)
		}
	}
}

// oracle: the `git config -z --get-regexp` format: NUL-terminated entries of
// `key\nvalue` or a bare key for a valueless key; the last entry wins, and
// output not ending in NUL is an error.
func TestEffectiveConfigTakesTheLastEntry(t *testing.T) {
	const badOutput = "unexpected git config output"
	cases := []struct {
		name, output string
		want         configEntry
		wantErr      string
	}{
		{"one value", "core.autocrlf\ntrue\x00", configEntry{set: true, value: "true"}, ""},
		{"valueless", "core.autocrlf\x00", configEntry{set: true, valueless: true}, ""},
		{"empty value is not valueless", "core.autocrlf\n\x00", configEntry{set: true}, ""},
		{"last entry wins", "core.autocrlf\ntrue\x00core.autocrlf\ninput\x00", configEntry{set: true, value: "input"}, ""},
		{"valueless last entry wins", "core.autocrlf\nfalse\x00core.autocrlf\x00", configEntry{set: true, valueless: true}, ""},
		{"value with a newline", "k\nline one\nline two\x00", configEntry{set: true, value: "line one\nline two"}, ""},
		{"empty output", "", configEntry{}, badOutput},
		{"no terminating NUL", "core.autocrlf\ntrue", configEntry{}, badOutput},
		{"bytes after the last NUL", "core.autocrlf\ntrue\x00core", configEntry{}, badOutput},
	}
	for _, c := range cases {
		got, err := effectiveConfig([]byte(c.output))
		if errText(err) != c.wantErr || got != c.want {
			t.Errorf("%s: effectiveConfig = %+v, %q; want %+v, %q", c.name, got, errText(err), c.want, c.wantErr)
		}
	}
}

// oracle: autocrlf converts when its effective value is input (any case) or
// true; fileMode is off when its value is false; unset means neither; a value
// Git would not accept is an error that names its key (the user sees it as
// read-failed).
func TestAutocrlfAndFileModeDecisions(t *testing.T) {
	const autocrlfErr = "core.autocrlf has a value cw does not recognise"
	const fileModeErr = "core.fileMode has a value cw does not recognise"
	val := func(v string) configEntry { return configEntry{set: true, value: v} }
	cases := []struct {
		name        string
		entry       configEntry
		converts    bool
		convertsErr string
		off         bool
		offErr      string
	}{
		{"unset", configEntry{}, false, "", false, ""},
		{"valueless", configEntry{set: true, valueless: true}, true, "", false, ""},
		{"true", val("true"), true, "", false, ""},
		{"one", val("1"), true, "", false, ""},
		{"false", val("false"), false, "", true, ""},
		{"empty", val(""), false, "", true, ""},
		{"zero", val("0"), false, "", true, ""},
		{"input", val("input"), true, "", false, fileModeErr},
		{"INPUT", val("INPUT"), true, "", false, fileModeErr},
		{"unrecognised", val("sometimes"), false, autocrlfErr, false, fileModeErr},
	}
	for _, c := range cases {
		if got, err := autocrlfConverts(c.entry); got != c.converts || errText(err) != c.convertsErr {
			t.Errorf("%s: autocrlfConverts = %v, %q; want %v, %q", c.name, got, errText(err), c.converts, c.convertsErr)
		}
		if got, err := fileModeOff(c.entry); got != c.off || errText(err) != c.offErr {
			t.Errorf("%s: fileModeOff = %v, %q; want %v, %q", c.name, got, errText(err), c.off, c.offErr)
		}
	}
}

// oracle: R10.b as conversion's comment states it: filter, ident and
// working-tree-encoding always block; text, eol, crlf and autocrlf block only
// content with a CR byte; with fileMode off a mode change blocks; set means
// anything but unspecified and unset.
func TestConversionNamesWhyGitWouldNotStoreThePath(t *testing.T) {
	reg := pathState{present: true, mode: "100644", sha256: "v"}
	exe := pathState{present: true, mode: "100755", sha256: "v"}
	lf, crlf := []byte("one\ntwo\n"), []byte("one\r\ntwo\r\n")
	const crText = "it contains a CR byte and the text attribute is set"
	const modeChanges = "core.fileMode is off and its mode changes"
	cases := []struct {
		name              string
		attrs             map[string]string
		content           []byte
		verified, head    pathState
		autocrlf, modeOff bool
		want              string
	}{
		{"nothing applies", nil, crlf, reg, reg, false, false, ""},
		{"filter set", map[string]string{"filter": "pubfilter"}, lf, reg, reg, false, false, "the filter attribute is set"},
		{"ident set", map[string]string{"ident": "set"}, lf, reg, reg, false, false, "the ident attribute is set"},
		{"working-tree-encoding set", map[string]string{"working-tree-encoding": "ISO-8859-1"}, lf, reg, reg, false, false, "the working-tree-encoding attribute is set"},
		{"filter unspecified", map[string]string{"filter": "unspecified"}, lf, reg, reg, false, false, ""},
		{"filter unset", map[string]string{"filter": "unset"}, lf, reg, reg, false, false, ""},
		{"filter reported before CR rules", map[string]string{"filter": "x", "text": "set"}, crlf, reg, reg, true, false, "the filter attribute is set"},
		{"text without CR", map[string]string{"text": "set"}, lf, reg, reg, false, false, ""},
		{"text with CR", map[string]string{"text": "set"}, crlf, reg, reg, false, false, crText},
		{"text=auto with CR", map[string]string{"text": "auto"}, crlf, reg, reg, false, false, crText},
		{"text with a lone CR", map[string]string{"text": "set"}, []byte("a\rb"), reg, reg, false, false, crText},
		{"eol with CR", map[string]string{"eol": "crlf"}, crlf, reg, reg, false, false, "it contains a CR byte and the eol attribute is set"},
		{"crlf with CR", map[string]string{"crlf": "set"}, crlf, reg, reg, false, false, "it contains a CR byte and the crlf attribute is set"},
		{"text unset with CR", map[string]string{"text": "unset"}, crlf, reg, reg, false, false, ""},
		{"autocrlf with CR", nil, crlf, reg, reg, true, false, "it contains a CR byte and core.autocrlf converts line endings"},
		{"autocrlf without CR", nil, lf, reg, reg, true, false, ""},
		{"fileMode off, executable set", nil, lf, exe, reg, false, true, modeChanges},
		{"fileMode off, executable cleared", nil, lf, reg, exe, false, true, modeChanges},
		{"fileMode off, mode kept", nil, lf, exe, exe, false, true, ""},
		{"fileMode off, new executable", nil, lf, exe, pathState{}, false, true, modeChanges},
		{"fileMode off, new regular file", nil, lf, reg, pathState{}, false, true, ""},
		{"fileMode on, mode changes", nil, lf, exe, reg, false, false, ""},
	}
	for _, c := range cases {
		if got := conversion(c.attrs, c.content, c.verified, c.head, c.autocrlf, c.modeOff); got != c.want {
			t.Errorf("%s: conversion = %q, want %q", c.name, got, c.want)
		}
	}
}

// oracle: reduceURL's comment: URLs with a scheme Git serves itself give the
// lowercased host without user and port, [user@]host:path with the colon
// before any slash gives host and path, and anything else is a clean absolute
// local path.
func TestReduceURLSplitsHostAndPath(t *testing.T) {
	cases := []struct{ name, url, host, path string }{
		{"https with user and port", "https://User@GitHub.COM:443/Owner/Repo.git", "github.com", "/Owner/Repo.git"},
		{"http scheme", "http://host.example/r.git", "host.example", "/r.git"},
		{"ssh scheme", "ssh://git@host.example/srv/r.git", "host.example", "/srv/r.git"},
		{"git+ssh scheme", "git+ssh://git@host.example/srv/r.git", "host.example", "/srv/r.git"},
		{"ssh+git scheme", "ssh+git://host.example/srv/r.git", "host.example", "/srv/r.git"},
		{"git scheme", "git://host.example:9418/r.git", "host.example", "/r.git"},
		{"IPv6 host with port keeps its brackets", "ssh://git@[::1]:2222/r.git", "[::1]", "/r.git"},
		{"IPv6 host without port keeps its brackets", "ssh://[::1]/r.git", "[::1]", "/r.git"},
		{"no path", "https://host.example", "host.example", ""},
		{"file scheme", "file:///srv/r.git", "", "/srv/r.git"},
		{"scp form", "git@github.com:owner/repo.git", "github.com", "owner/repo.git"},
		{"scp form without user", "host:path/r.git", "host", "path/r.git"},
		{"scp form without a slash", "host:r.git", "host", "r.git"},
		{"scp form with absolute path", "host:/srv/r.git", "host", "/srv/r.git"},
		{"colon after a slash is local", "./dir:x/r.git", "", "/w/repo/dir:x/r.git"},
		{"leading colon is local", ":r.git", "", "/w/repo/:r.git"},
		{"absolute local is cleaned", "/srv/./r.git/", "", "/srv/r.git"},
		{"relative local", "../remote.git", "", "/w/remote.git"},
	}
	for _, c := range cases {
		if host, path, ok := reduceURL(c.url, "/w/repo"); host != c.host || path != c.path || !ok {
			t.Errorf("%s: reduceURL(%q) = %q, %q, %v; want %q, %q, true", c.name, c.url, host, path, ok, c.host, c.path)
		}
	}
}

// oracle: Git hands a URL to a remote helper when its leading letters,
// digits, +, - and . are followed by "::" (transport.c, transport_get), or
// when its scheme is none of ssh, git+ssh, ssh+git, git, file (served
// natively) or http, https (Git's curl helper), compared case-sensitively.
// reduceURL reduces none of them, since the helper decides where a push
// goes.
func TestReduceURLRefusesRemoteHelperURLs(t *testing.T) {
	for _, url := range []string{
		"pubhelper://github.com/o/r",
		"pubhelper::https://github.com/o/r",
		"pubhelper::file:///w/remote.git",
		"pubhelper::/w/remote.git",
		"ext::ssh -i key github.com %S o/r",
		"git-remote.v2+x::anything",
		"::/w/remote.git",
		"HTTPS://github.com/o/r",
		"Ssh://github.com/o/r",
		"ftp://github.com/o/r",
		"ftps://github.com/o/r",
		"git@host://github.com/o/r",
	} {
		if host, path, ok := reduceURL(url, "/w/repo"); ok {
			t.Errorf("reduceURL(%q) = %q, %q, true; want false", url, host, path)
		}
	}
}

// oracle: sameRepository's comment: equal hosts, and paths equal
// case-insensitively once leading and trailing slashes and one trailing .git
// are removed.
func TestSameRepositoryComparesReducedURLs(t *testing.T) {
	cases := []struct {
		name, a, b string
		want       bool
	}{
		{"identical", "https://github.com/o/r.git", "https://github.com/o/r.git", true},
		{".git suffix optional", "https://github.com/o/r.git", "https://github.com/o/r", true},
		{"trailing slash", "https://github.com/o/r", "https://github.com/o/r/", true},
		{"path in other case", "https://github.com/Owner/Repo", "https://github.com/owner/repo", true},
		{".GIT suffix", "/srv/r.GIT", "/srv/r", true},
		{"only one .git removed", "/srv/r.GIT.git", "/srv/r", false},
		{"https and scp form", "https://github.com/o/r.git", "git@github.com:o/r.git", true},
		{"ssh scheme and scp form", "ssh://git@github.com/o/r", "git@github.com:o/r.git", true},
		{"file scheme and local path", "file:///w/remote.git", "/w/remote.git", true},
		{"relative and absolute local path", "../remote.git", "/w/remote.git", true},
		{"other host", "https://github.com/o/r", "https://gitlab.com/o/r", false},
		{"other IPv6 host", "ssh://[::1]/o/r", "ssh://[::2]/o/r", false},
		{"other repository", "https://github.com/o/r", "https://github.com/o/s", false},
		{"host against local path", "ssh://host.example/w/remote.git", "/w/remote.git", false},
	}
	for _, c := range cases {
		if got := sameRepository(c.a, c.b, "/w/repo"); got != c.want {
			t.Errorf("%s: sameRepository(%q, %q) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}

// oracle: a URL Git hands to a remote helper (another scheme, any case but
// lower, or <transport>::<address>) names no repository, so it never matches
// the URL it imitates, nor itself.
func TestSameRepositoryNeverMatchesRemoteHelperURLs(t *testing.T) {
	cases := []struct{ name, a, b string }{
		{"unknown scheme", "pubhelper://github.com/o/r", "https://github.com/o/r"},
		{"transport before an https URL", "pubhelper::https://github.com/o/r", "https://github.com/o/r"},
		{"transport before a file URL", "pubhelper::file:///w/remote.git", "/w/remote.git"},
		{"transport before an scp form", "pubhelper::git@github.com:o/r.git", "git@github.com:o/r.git"},
		{"upper-case scheme", "HTTPS://github.com/o/r", "https://github.com/o/r"},
		{"fetch-only ftp", "ftp://github.com/o/r", "https://github.com/o/r"},
		{"helper URL against itself", "pubhelper://github.com/o/r", "pubhelper://github.com/o/r"},
	}
	for _, c := range cases {
		if sameRepository(c.a, c.b, "/w/repo") || sameRepository(c.b, c.a, "/w/repo") {
			t.Errorf("%s: sameRepository(%q, %q) = true, want false", c.name, c.a, c.b)
		}
	}
}

// oracle: only open, same-repository PRs whose head branch is the branch are
// kept, in gh's order.
func TestSameRepositoryOpenKeepsOpenPRsForTheBranch(t *testing.T) {
	prs := []pullRequest{
		{Number: 1, HeadRefName: "feature", State: "OPEN"},
		{Number: 2, HeadRefName: "other", State: "OPEN"},
		{Number: 3, HeadRefName: "feature", State: "CLOSED"},
		{Number: 4, HeadRefName: "feature", State: "MERGED"},
		{Number: 5, HeadRefName: "feature", State: "OPEN", IsCrossRepository: true},
		{Number: 6, HeadRefName: "feature", State: "OPEN"},
	}
	cases := []struct {
		branch string
		want   []int
	}{{"feature", []int{1, 6}}, {"other", []int{2}}, {"missing", []int{}}}
	for _, c := range cases {
		got := []int{}
		for _, pr := range sameRepositoryOpen(prs, c.branch) {
			got = append(got, pr.Number)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("sameRepositoryOpen(%q) = %v, want %v", c.branch, got, c.want)
		}
	}
}

const (
	testCommitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testCommitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// testPR is an open PR that carries testPRRequest's base, title and body
// (with LF line endings where the request has CRLF).
func testPR(number int, edit func(*pullRequest)) pullRequest {
	pr := pullRequest{Number: number, State: "OPEN", HeadRefName: "feature", HeadRefOID: testCommitA, BaseRefName: "main", Title: "Add the widget", Body: "Body text.\nLine two."}
	if edit != nil {
		edit(&pr)
	}
	return pr
}

func testPRRequest(mode string) Request {
	return Request{pr: mode, branch: "feature", base: "main", title: "Add the widget", body: []byte("Body text.\r\nLine two.\r\n\n")}
}

// oracle: R12.c and R12.d: create needs no open PR (or one equivalent PR,
// which is present); edit needs exactly one, which is present only when it is
// equivalent and already at the known head; body comparison treats CRLF as LF
// and ignores trailing whitespace.
func TestPlanPRAppliesTheCreateAndEditRules(t *testing.T) {
	crlfBody := testPR(7, func(pr *pullRequest) { pr.Body = "Body text.\r\nLine two.\r\n \t" })
	otherTitle := testPR(7, func(pr *pullRequest) { pr.Title = "Other" })
	otherBase := testPR(7, func(pr *pullRequest) { pr.BaseRefName = "develop" })
	otherBody := testPR(7, func(pr *pullRequest) { pr.Body = "Other body." })
	cases := []struct {
		name, mode, head string
		open             []pullRequest
		action, reason   string
		number           int
	}{
		{"create, none open", PRCreate, "", nil, "create", "", 0},
		{"create, one equivalent", PRCreate, "", []pullRequest{testPR(7, nil)}, "present", "", 7},
		{"create, one equivalent after body normalisation", PRCreate, testCommitB, []pullRequest{crlfBody}, "present", "", 7},
		{"create, one with another title", PRCreate, "", []pullRequest{otherTitle}, "", "pr-exists", 0},
		{"create, one with another base", PRCreate, "", []pullRequest{otherBase}, "", "pr-exists", 0},
		{"create, one with another body", PRCreate, "", []pullRequest{otherBody}, "", "pr-exists", 0},
		{"create, two equivalent", PRCreate, "", []pullRequest{testPR(7, nil), testPR(8, nil)}, "", "pr-exists", 0},
		{"edit, none open", PREdit, testCommitA, nil, "", "pr-missing", 0},
		{"edit, equivalent at head", PREdit, testCommitA, []pullRequest{testPR(7, nil)}, "present", "", 7},
		{"edit, equivalent at another head", PREdit, testCommitB, []pullRequest{testPR(7, nil)}, "edit", "", 7},
		{"edit, equivalent with head unknown", PREdit, "", []pullRequest{testPR(7, nil)}, "edit", "", 7},
		{"edit, another title at head", PREdit, testCommitA, []pullRequest{otherTitle}, "edit", "", 7},
		{"edit, two open", PREdit, testCommitA, []pullRequest{testPR(7, nil), testPR(8, nil)}, "", "pr-ambiguous", 0},
	}
	for _, c := range cases {
		plan, reason := planPR(c.open, testPRRequest(c.mode), c.head)
		if plan.action != c.action || reason != c.reason || plan.pr.Number != c.number {
			t.Errorf("%s: planPR = {%q PR %d}, %q; want {%q PR %d}, %q", c.name, plan.action, plan.pr.Number, reason, c.action, c.number, c.reason)
		}
	}
}

// oracle: readbackDiffers names every difference in the order state, base,
// title, body, head, and reports onlyHead when the head is the sole one.
func TestReadbackDiffersNamesEachDifference(t *testing.T) {
	cases := []struct {
		name     string
		pr       pullRequest
		want     string
		onlyHead bool
	}{
		{"matches", testPR(7, nil), "", false},
		{"matches after body normalisation", testPR(7, func(pr *pullRequest) { pr.Body = "Body text.\r\nLine two.\r\n" }), "", false},
		{"head only", testPR(7, func(pr *pullRequest) { pr.HeadRefOID = testCommitB }), "the pull request differs: head " + testCommitB, true},
		{"state", testPR(7, func(pr *pullRequest) { pr.State = "CLOSED" }), "the pull request differs: state CLOSED", false},
		{"base and title", testPR(7, func(pr *pullRequest) { pr.BaseRefName, pr.Title = "develop", "Other" }), "the pull request differs: base develop, title", false},
		{"body and head", testPR(7, func(pr *pullRequest) { pr.Body, pr.HeadRefOID = "Other body.", testCommitB }), "the pull request differs: body, head " + testCommitB, false},
		{"everything", pullRequest{State: "MERGED", BaseRefName: "develop", Title: "Other", Body: "x", HeadRefOID: testCommitB}, "the pull request differs: state MERGED, base develop, title, body, head " + testCommitB, false},
	}
	for _, c := range cases {
		if got, onlyHead := readbackDiffers(c.pr, testPRRequest(PREdit), testCommitA); got != c.want || onlyHead != c.onlyHead {
			t.Errorf("%s: readbackDiffers = %q, %v; want %q, %v", c.name, got, onlyHead, c.want, c.onlyHead)
		}
	}
}

// oracle: ToolEnvironment's comment: the nine repository-selecting variables
// (listed here independently of repositoryVariables) are removed, set replaces
// any earlier value of its name and comes last, and everything else is kept
// in order.
func TestToolEnvironmentDropsRepositoryVariables(t *testing.T) {
	repository := []string{"GIT_DIR=/x", "GIT_WORK_TREE=/x", "GIT_INDEX_FILE=/x", "GIT_OBJECT_DIRECTORY=/x",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/x", "GIT_COMMON_DIR=/x", "GIT_NAMESPACE=x", "GIT_CEILING_DIRECTORIES=/x",
		"GIT_DISCOVERY_ACROSS_FILESYSTEM=1"}
	cases := []struct {
		name          string
		environ, want []string
		set           string
	}{
		{"empty", nil, []string{"GIT_TERMINAL_PROMPT=0"}, "GIT_TERMINAL_PROMPT=0"},
		{"replaces an earlier value", []string{"PATH=/bin", "GIT_TERMINAL_PROMPT=1", "HOME=/h"}, []string{"PATH=/bin", "HOME=/h", "GIT_TERMINAL_PROMPT=0"}, "GIT_TERMINAL_PROMPT=0"},
		{"drops repository variables", append([]string{"PATH=/bin"}, append(repository, "GIT_AUTHOR_NAME=a")...), []string{"PATH=/bin", "GIT_AUTHOR_NAME=a", "GH_PROMPT_DISABLED=1"}, "GH_PROMPT_DISABLED=1"},
		{"keeps look-alike names", []string{"GIT_DIRX=1", "XGIT_DIR=2", "git_dir=3"}, []string{"GIT_DIRX=1", "XGIT_DIR=2", "git_dir=3", "X=1"}, "X=1"},
		{"drops a repository variable without a value", []string{"GIT_DIR", "PATH=/bin"}, []string{"PATH=/bin", "X=1"}, "X=1"},
	}
	for _, c := range cases {
		if got := ToolEnvironment(c.environ, c.set); !slices.Equal(got, c.want) {
			t.Errorf("%s: ToolEnvironment = %q, want %q", c.name, got, c.want)
		}
	}
}
