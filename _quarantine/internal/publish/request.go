// Package publish carries out `cw publish`: it commits a verified task's
// files, pushes the commit and creates or edits a pull request, only after the
// task's verification passed on the current source and no other open task
// record gates the repository. It authorizes nothing: the coordinator that
// runs it carries the user's authorization.
package publish

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// ReadLimit is the largest message or body file cw publish accepts.
	ReadLimit      = 65536
	maxTitleRunes  = 256
	defaultTimeout = 10 * time.Minute
	minTimeout     = time.Second
	maxTimeout     = 2 * time.Hour
)

// PR modes.
const (
	PRNone   = "none"
	PRCreate = "create"
	PREdit   = "edit"
)

// Input is the command line as given, with the message and body files
// already read. A nil pointer is a flag that was not given.
type Input struct {
	Task         string
	Message      []byte
	ExpectHead   string
	Remote       string
	Branch       string
	Paths        []string
	ExpectRemote *string
	PR           *string
	Repo         *string
	Base         *string
	Title        *string
	// Body is the body file's bytes when BodyGiven.
	Body      []byte
	BodyGiven bool
	Timeout   *string
}

// Request is a validated cw publish request. Build it with ParseRequest.
type Request struct {
	task         string
	message      []byte
	expectHead   string
	remote       string
	branch       string
	paths        []string // nil when no --path was given
	expectRemote string   // "" when not given, "absent", or a commit
	pr           string
	repo         string
	base         string
	title        string
	body         []byte
	timeout      time.Duration
}

// ParseRequest applies the usage rules of R1 that need no file system
// access; any failure is a usage error.
func ParseRequest(in Input) (Request, error) {
	req := Request{task: in.Task, message: in.Message, expectHead: in.ExpectHead, remote: in.Remote, branch: in.Branch, pr: PRNone, timeout: defaultTimeout}
	if in.Task == "" {
		return Request{}, errors.New("--task DIR is required")
	}
	if in.Message == nil {
		return Request{}, errors.New("--message-file FILE is required")
	}
	if in.ExpectHead == "" {
		return Request{}, errors.New("--expect-head SHA is required")
	}
	if in.Remote == "" {
		return Request{}, errors.New("--push REMOTE is required")
	}
	if in.Branch == "" {
		return Request{}, errors.New("--branch BRANCH is required")
	}
	if in.PR != nil {
		switch *in.PR {
		case PRNone, PRCreate, PREdit:
			req.pr = *in.PR
		default:
			return Request{}, fmt.Errorf("--pr must be none, create or edit, not %q", *in.PR)
		}
	}
	prFlags := map[string]bool{"--repo": in.Repo != nil, "--base": in.Base != nil, "--title": in.Title != nil, "--body-file": in.BodyGiven}
	for _, name := range []string{"--repo", "--base", "--title", "--body-file"} {
		if req.pr == PRNone && prFlags[name] {
			return Request{}, fmt.Errorf("%s applies only with --pr create or --pr edit", name)
		}
		if req.pr != PRNone && !prFlags[name] {
			return Request{}, fmt.Errorf("--pr %s needs --repo, --base, --title and --body-file", req.pr)
		}
	}
	if !commitID(in.ExpectHead) {
		return Request{}, errors.New("--expect-head must be a full lowercase commit ID (40 or 64 hexadecimal characters)")
	}
	if in.ExpectRemote != nil {
		if *in.ExpectRemote != "absent" && !commitID(*in.ExpectRemote) {
			return Request{}, errors.New("--expect-remote must be absent or a full lowercase commit ID (40 or 64 hexadecimal characters)")
		}
		req.expectRemote = *in.ExpectRemote
	}
	if !validName(in.Branch) {
		return Request{}, fmt.Errorf("--branch %q is not a valid branch name", in.Branch)
	}
	if !validName(in.Remote) {
		return Request{}, fmt.Errorf("--push %q is not a valid remote name", in.Remote)
	}
	if req.pr != PRNone {
		req.repo, req.base, req.title, req.body = *in.Repo, *in.Base, *in.Title, in.Body
		if !validName(req.base) {
			return Request{}, fmt.Errorf("--base %q is not a valid branch name", req.base)
		}
		if req.base == in.Branch {
			return Request{}, errors.New("--base must differ from --branch")
		}
		if !validRepo(req.repo) {
			return Request{}, fmt.Errorf("--repo %q must be OWNER/NAME", req.repo)
		}
		if err := validTitle(req.title); err != nil {
			return Request{}, err
		}
		if !utf8.Valid(req.body) {
			return Request{}, errors.New("--body-file is not valid UTF-8")
		}
		if req.body == nil {
			req.body = []byte{}
		}
	}
	if err := validMessage(in.Message); err != nil {
		return Request{}, err
	}
	if in.Paths != nil {
		seen := map[string]bool{}
		for _, p := range in.Paths {
			if err := validPath(p); err != nil {
				return Request{}, err
			}
			if seen[p] {
				return Request{}, fmt.Errorf("--path %q is given twice", p)
			}
			seen[p] = true
		}
		req.paths = slices.Clone(in.Paths)
	}
	if in.Timeout != nil {
		timeout, err := time.ParseDuration(*in.Timeout)
		if err != nil || timeout < minTimeout || timeout > maxTimeout {
			return Request{}, errors.New("--timeout must be a duration from 1s to 2h")
		}
		req.timeout = timeout
	}
	return req, nil
}

func commitID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// validName applies the name rules for --branch, --push and --base: not
// empty, no leading - or /, no trailing / or ., none of //, .., @{, ASCII
// control characters, space, ~ ^ : ? * [ \, not @, and no slash-separated
// component that starts with . or ends with .lock.
func validName(value string) bool {
	if value == "" || value == "@" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "/") ||
		strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, bad := range []string{"//", "..", "@{"} {
		if strings.Contains(value, bad) {
			return false
		}
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 0x20 || c == 0x7f || strings.IndexByte(" ~^:?*[\\", c) >= 0 {
			return false
		}
	}
	for _, component := range strings.Split(value, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func validRepo(value string) bool {
	owner, name, ok := strings.Cut(value, "/")
	if !ok || strings.Contains(name, "/") {
		return false
	}
	for _, part := range []string{owner, name} {
		if part == "" || part[0] == '-' {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				return false
			}
		}
	}
	return true
}

func validTitle(title string) error {
	switch {
	case title == "":
		return errors.New("--title must not be empty")
	case !utf8.ValidString(title):
		return errors.New("--title is not valid UTF-8")
	case strings.ContainsAny(title, "\r\n"):
		return errors.New("--title must be one line")
	case utf8.RuneCountInString(title) > maxTitleRunes:
		return errors.New("--title exceeds 256 characters")
	}
	first, _ := utf8.DecodeRuneInString(title)
	last, _ := utf8.DecodeLastRuneInString(title)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return errors.New("--title must not start or end with whitespace")
	}
	return nil
}

func validMessage(message []byte) error {
	switch {
	case !utf8.Valid(message):
		return errors.New("--message-file is not valid UTF-8")
	case bytes.IndexByte(message, 0) >= 0:
		return errors.New("--message-file contains NUL")
	case !bytes.HasSuffix(message, []byte("\n")):
		return errors.New("--message-file must end with a newline")
	}
	first, _, _ := bytes.Cut(message, []byte("\n"))
	if len(bytes.TrimSpace(first)) == 0 {
		return errors.New("--message-file must start with a non-blank subject line")
	}
	return nil
}

func validPath(p string) error {
	switch {
	case p == "":
		return errors.New("--path must not be empty")
	case strings.IndexByte(p, 0) >= 0:
		return errors.New("--path contains NUL")
	case path.IsAbs(p) || strings.HasPrefix(p, "/"):
		return fmt.Errorf("--path %q must be relative to the worktree", p)
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return fmt.Errorf("--path %q must not contain a .. segment", p)
		}
	}
	return nil
}
