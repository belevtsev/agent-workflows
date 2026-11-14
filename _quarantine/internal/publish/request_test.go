package publish

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testHead = "0123456789abcdef0123456789abcdef01234567"

var testMessage = []byte("Subject\n\nBody.\n")

func strPtr(s string) *string { return &s }

// usageInput is a minimal valid Input without a pull request, changed by edit.
func usageInput(edit func(*Input)) Input {
	in := Input{Task: "/t/task", Message: testMessage, ExpectHead: testHead, Remote: "origin", Branch: "feature"}
	if edit != nil {
		edit(&in)
	}
	return in
}

// prInput is usageInput with --pr mode and the four flags it needs.
func prInput(mode string, edit func(*Input)) Input {
	return usageInput(func(in *Input) {
		in.PR, in.Repo, in.Base, in.Title = strPtr(mode), strPtr("octo/widget"), strPtr("main"), strPtr("Add the widget")
		in.Body, in.BodyGiven = []byte("Body text.\n"), true
		if edit != nil {
			edit(in)
		}
	})
}

// oracle: the R1 usage rules; each rejected input breaks one rule and the
// error is that rule's exact message, naming the flag (the message is the
// usage error's kind).
func TestParseRequestRejectsUsageErrors(t *testing.T) {
	type tc struct {
		name string
		in   Input
		want string
	}
	cases := []tc{
		{"no --task", usageInput(func(in *Input) { in.Task = "" }), "--task DIR is required"},
		{"no --message-file", usageInput(func(in *Input) { in.Message = nil }), "--message-file FILE is required"},
		{"no --expect-head", usageInput(func(in *Input) { in.ExpectHead = "" }), "--expect-head SHA is required"},
		{"no --push", usageInput(func(in *Input) { in.Remote = "" }), "--push REMOTE is required"},
		{"no --branch", usageInput(func(in *Input) { in.Branch = "" }), "--branch BRANCH is required"},
		{"--pr draft", prInput("draft", nil), `--pr must be none, create or edit, not "draft"`},
		{"create without --repo", prInput(PRCreate, func(in *Input) { in.Repo = nil }), "--pr create needs --repo, --base, --title and --body-file"},
		{"create without --base", prInput(PRCreate, func(in *Input) { in.Base = nil }), "--pr create needs --repo, --base, --title and --body-file"},
		{"create without --title", prInput(PRCreate, func(in *Input) { in.Title = nil }), "--pr create needs --repo, --base, --title and --body-file"},
		{"create without --body-file", prInput(PRCreate, func(in *Input) { in.BodyGiven = false }), "--pr create needs --repo, --base, --title and --body-file"},
		{"edit without --repo", prInput(PREdit, func(in *Input) { in.Repo = nil }), "--pr edit needs --repo, --base, --title and --body-file"},
		{"none with --repo", prInput(PRNone, nil), "--repo applies only with --pr create or --pr edit"},
		{"default none with --title", usageInput(func(in *Input) { in.Title = strPtr("t") }), "--title applies only with --pr create or --pr edit"},
		{"default none with --base", usageInput(func(in *Input) { in.Base = strPtr("main") }), "--base applies only with --pr create or --pr edit"},
		{"default none with --body-file", usageInput(func(in *Input) { in.BodyGiven = true }), "--body-file applies only with --pr create or --pr edit"},
		{"base invalid", prInput(PRCreate, func(in *Input) { in.Base = strPtr("ma..in") }), `--base "ma..in" is not a valid branch name`},
		{"base equals branch", prInput(PRCreate, func(in *Input) { in.Base = strPtr("feature") }), "--base must differ from --branch"},
		{"title empty", prInput(PRCreate, func(in *Input) { in.Title = strPtr("") }), "--title must not be empty"},
		{"title invalid UTF-8", prInput(PRCreate, func(in *Input) { in.Title = strPtr("bad \xff") }), "--title is not valid UTF-8"},
		{"title newline", prInput(PRCreate, func(in *Input) { in.Title = strPtr("two\nlines") }), "--title must be one line"},
		{"title CR", prInput(PRCreate, func(in *Input) { in.Title = strPtr("two\rlines") }), "--title must be one line"},
		{"title 257", prInput(PRCreate, func(in *Input) { in.Title = strPtr(strings.Repeat("t", 257)) }), "--title exceeds 256 characters"},
		{"title leading space", prInput(PRCreate, func(in *Input) { in.Title = strPtr(" lead") }), "--title must not start or end with whitespace"},
		{"title trailing tab", prInput(PRCreate, func(in *Input) { in.Title = strPtr("trail\t") }), "--title must not start or end with whitespace"},
		{"title trailing no-break space", prInput(PRCreate, func(in *Input) { in.Title = strPtr("trail ") }), "--title must not start or end with whitespace"},
		{"body invalid UTF-8", prInput(PRCreate, func(in *Input) { in.Body = []byte("body \xc3\x28\n") }), "--body-file is not valid UTF-8"},
		{"message empty", usageInput(func(in *Input) { in.Message = []byte{} }), "--message-file must end with a newline"},
		{"message without newline", usageInput(func(in *Input) { in.Message = []byte("subject") }), "--message-file must end with a newline"},
		{"message invalid UTF-8", usageInput(func(in *Input) { in.Message = []byte("subject \xff\n") }), "--message-file is not valid UTF-8"},
		{"message NUL", usageInput(func(in *Input) { in.Message = []byte("subject\x00\n") }), "--message-file contains NUL"},
		{"message blank first line", usageInput(func(in *Input) { in.Message = []byte("\nbody\n") }), "--message-file must start with a non-blank subject line"},
		{"message whitespace first line", usageInput(func(in *Input) { in.Message = []byte(" \t\r\nbody\n") }), "--message-file must start with a non-blank subject line"},
		{"path empty", usageInput(func(in *Input) { in.Paths = []string{""} }), "--path must not be empty"},
		{"path NUL", usageInput(func(in *Input) { in.Paths = []string{"a\x00b"} }), "--path contains NUL"},
		{"path absolute", usageInput(func(in *Input) { in.Paths = []string{"/r/a.txt"} }), `--path "/r/a.txt" must be relative to the worktree`},
		{"path dotdot", usageInput(func(in *Input) { in.Paths = []string{".."} }), `--path ".." must not contain a .. segment`},
		{"path leading dotdot", usageInput(func(in *Input) { in.Paths = []string{"../r/a.txt"} }), `--path "../r/a.txt" must not contain a .. segment`},
		{"path inner dotdot", usageInput(func(in *Input) { in.Paths = []string{"sub/../a.txt"} }), `--path "sub/../a.txt" must not contain a .. segment`},
		{"path twice", usageInput(func(in *Input) { in.Paths = []string{"a.txt", "b.txt", "a.txt"} }), `--path "a.txt" is given twice`},
		{"repo empty owner", prInput(PRCreate, func(in *Input) { in.Repo = strPtr("/widget") }), `--repo "/widget" must be OWNER/NAME`},
	}
	for _, repo := range []string{"octo", "octo/widget/x", "-octo/widget", "octo/-widget", "oc to/widget", "octo/", "octo/wid:get", "octo/wid@get"} {
		cases = append(cases, tc{"repo " + repo, prInput(PRCreate, func(in *Input) { in.Repo = strPtr(repo) }), fmt.Sprintf("--repo %q must be OWNER/NAME", repo)})
	}
	for _, head := range []string{"abc123", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("a", 65), strings.ToUpper(testHead), strings.Repeat("g", 40)} {
		cases = append(cases, tc{"expect-head " + head, usageInput(func(in *Input) { in.ExpectHead = head }), "--expect-head must be a full lowercase commit ID (40 or 64 hexadecimal characters)"})
	}
	for _, remote := range []string{"ABSENT", "none", "", "abc", strings.ToUpper(testHead)} {
		cases = append(cases, tc{"expect-remote " + remote, usageInput(func(in *Input) { in.ExpectRemote = strPtr(remote) }), "--expect-remote must be absent or a full lowercase commit ID (40 or 64 hexadecimal characters)"})
	}
	for _, name := range []string{"-feature", "fea..ture", "fea ture", "feature.lock", "x.lock/y", "feature/.x", ".hidden", "@", "feature/", "feature.",
		"/feature", "fea//ture", "fea@{ture", "fea~1", "fea^", "fea:ture", "fea?", "fea*", "fea[ture", `fea\ture`, "fea\x01ture", "fea\x7fture", "fea\tture"} {
		cases = append(cases, tc{"branch " + name, usageInput(func(in *Input) { in.Branch = name }), fmt.Sprintf("--branch %q is not a valid branch name", name)})
	}
	for _, name := range []string{"-origin", "ori gin", ".."} {
		cases = append(cases, tc{"push " + name, usageInput(func(in *Input) { in.Remote = name }), fmt.Sprintf("--push %q is not a valid remote name", name)})
	}
	for _, timeout := range []string{"0s", "999ms", "2h0m1s", "3h", "-1s", "10", "soon", ""} {
		cases = append(cases, tc{"timeout " + timeout, usageInput(func(in *Input) { in.Timeout = strPtr(timeout) }), "--timeout must be a duration from 1s to 2h"})
	}
	for _, c := range cases {
		req, err := ParseRequest(c.in)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: ParseRequest error %v, want %q", c.name, err, c.want)
		}
		if !reflect.DeepEqual(req, Request{}) {
			t.Errorf("%s: ParseRequest returned %+v with its error, want the zero Request", c.name, req)
		}
	}
}

// oracle: the R1 boundary values that pass usage, and the Request each one
// yields: defaults (--pr none, 10m timeout, no paths, no --expect-remote), the
// PR fields copied only with --pr create or edit, and an absent body read as
// empty, not nil.
func TestParseRequestAcceptsBoundaryValues(t *testing.T) {
	want := func(edit func(*Request)) Request {
		r := Request{task: "/t/task", message: testMessage, expectHead: testHead, remote: "origin", branch: "feature", pr: PRNone, timeout: 10 * time.Minute}
		if edit != nil {
			edit(&r)
		}
		return r
	}
	withPR := func(mode string, edit func(*Request)) Request {
		return want(func(r *Request) {
			r.pr, r.repo, r.base, r.title, r.body = mode, "octo/widget", "main", "Add the widget", []byte("Body text.\n")
			if edit != nil {
				edit(r)
			}
		})
	}
	long, wide := strings.Repeat("t", 256), strings.Repeat("é", 256)
	sha256Head := strings.Repeat("a", 64)
	cases := []struct {
		name string
		in   Input
		want Request
	}{
		{"defaults", usageInput(nil), want(nil)},
		{"explicit none", usageInput(func(in *Input) { in.PR = strPtr(PRNone) }), want(nil)},
		{"create", prInput(PRCreate, nil), withPR(PRCreate, nil)},
		{"edit", prInput(PREdit, nil), withPR(PREdit, nil)},
		{"body empty", prInput(PRCreate, func(in *Input) { in.Body = nil }), withPR(PRCreate, func(r *Request) { r.body = []byte{} })},
		{"title 256 characters", prInput(PRCreate, func(in *Input) { in.Title = &long }), withPR(PRCreate, func(r *Request) { r.title = long })},
		{"title 256 two-byte characters", prInput(PRCreate, func(in *Input) { in.Title = &wide }), withPR(PRCreate, func(r *Request) { r.title = wide })},
		{"title starting with --", prInput(PRCreate, func(in *Input) { in.Title = strPtr("--draft") }), withPR(PRCreate, func(r *Request) { r.title = "--draft" })},
		{"title inner tab", prInput(PRCreate, func(in *Input) { in.Title = strPtr("a\tb") }), withPR(PRCreate, func(r *Request) { r.title = "a\tb" })},
		{"repo with dots and underscores", prInput(PRCreate, func(in *Input) { in.Repo = strPtr("o.c_t-o/w.i_d-g3t") }), withPR(PRCreate, func(r *Request) { r.repo = "o.c_t-o/w.i_d-g3t" })},
		{"base with slash", prInput(PRCreate, func(in *Input) { in.Base = strPtr("release/1.x") }), withPR(PRCreate, func(r *Request) { r.base = "release/1.x" })},
		{"expect-head 64 hex", usageInput(func(in *Input) { in.ExpectHead = sha256Head }), want(func(r *Request) { r.expectHead = sha256Head })},
		{"expect-remote absent", usageInput(func(in *Input) { in.ExpectRemote = strPtr("absent") }), want(func(r *Request) { r.expectRemote = "absent" })},
		{"expect-remote 64 hex", usageInput(func(in *Input) { in.ExpectRemote = &sha256Head }), want(func(r *Request) { r.expectRemote = sha256Head })},
		{"branch with slash", usageInput(func(in *Input) { in.Branch = "team/feature-1" }), want(func(r *Request) { r.branch = "team/feature-1" })},
		{"branch non-ASCII", usageInput(func(in *Input) { in.Branch = "fé" }), want(func(r *Request) { r.branch = "fé" })},
		{"message with CRLF subject", usageInput(func(in *Input) { in.Message = []byte("Subject\r\n") }), want(func(r *Request) { r.message = []byte("Subject\r\n") })},
		{"paths", usageInput(func(in *Input) { in.Paths = []string{"a..txt", "./b.txt", "sub/c.txt"} }), want(func(r *Request) { r.paths = []string{"a..txt", "./b.txt", "sub/c.txt"} })},
		{"timeout 1s", usageInput(func(in *Input) { in.Timeout = strPtr("1s") }), want(func(r *Request) { r.timeout = time.Second })},
		{"timeout 1m30s", usageInput(func(in *Input) { in.Timeout = strPtr("1m30s") }), want(func(r *Request) { r.timeout = 90 * time.Second })},
		{"timeout 2h", usageInput(func(in *Input) { in.Timeout = strPtr("2h") }), want(func(r *Request) { r.timeout = 2 * time.Hour })},
	}
	for _, c := range cases {
		got, err := ParseRequest(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ParseRequest = %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
}

// oracle: the Request owns its paths: changing the caller's slice afterwards
// does not change the request.
func TestParseRequestCopiesPaths(t *testing.T) {
	paths := []string{"a.txt"}
	req, err := ParseRequest(usageInput(func(in *Input) { in.Paths = paths }))
	if err != nil {
		t.Fatal(err)
	}
	paths[0] = "b.txt"
	if !reflect.DeepEqual(req.paths, []string{"a.txt"}) {
		t.Errorf("request paths = %q after the input changed, want [a.txt]", req.paths)
	}
}
