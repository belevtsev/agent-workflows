package publish

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Decisions without I/O: path states and the commit set, Git's boolean and
// conversion rules, URL reduction, pull-request equivalence and selection, and
// the environment the external programs run with.

// pathState is a path's mode and content SHA-256, or its absence.
type pathState struct {
	present bool
	mode    string
	sha256  string
}

// commitSet returns, sorted, the members of authorized whose verified state
// differs from their state in the expected head's tree.
func commitSet(authorized []string, verified, head map[string]pathState) []string {
	set := []string{}
	for _, p := range authorized {
		if verified[p] != head[p] {
			set = append(set, p)
		}
	}
	slices.Sort(set)
	return set
}

// gitBool applies Git's boolean rules to one configuration value: a
// valueless key is true; true, yes, on are true and false, no, off and the
// empty value false, in any case; an integer (decimal or 0x hexadecimal,
// optionally signed, with an optional k, m or g suffix) is true when non-zero.
func gitBool(value string, valueless bool) (bool, bool) {
	if valueless {
		return true, true
	}
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true, true
	case "false", "no", "off", "":
		return false, true
	}
	number := value
	if n := len(number); n > 0 && strings.ContainsRune("kKmMgG", rune(number[n-1])) {
		number = number[:n-1]
	}
	digits := strings.TrimLeft(number, "+-")
	if len(number)-len(digits) > 1 || digits == "" {
		return false, false
	}
	base := 10
	if len(digits) > 2 && (digits[:2] == "0x" || digits[:2] == "0X") {
		digits, base = digits[2:], 16
	}
	parsed, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		return false, false
	}
	return parsed != 0, true
}

// configEntry is the effective value of one configuration key.
type configEntry struct {
	set       bool
	value     string
	valueless bool
}

// effectiveConfig parses `git config -z --get-regexp` output: entries end
// with NUL, each `key\nvalue` or `key` alone for a valueless key; the last
// one wins.
func effectiveConfig(output []byte) (configEntry, error) {
	entries := bytes.Split(output, []byte{0})
	if len(entries) < 2 || len(entries[len(entries)-1]) != 0 {
		return configEntry{}, errors.New("unexpected git config output")
	}
	last := entries[len(entries)-2]
	_, value, hasValue := bytes.Cut(last, []byte("\n"))
	return configEntry{set: true, value: string(value), valueless: !hasValue}, nil
}

// autocrlfConverts reports whether core.autocrlf converts line endings: its
// effective value is input (any case) or true.
func autocrlfConverts(entry configEntry) (bool, error) {
	if !entry.set {
		return false, nil
	}
	if !entry.valueless && strings.EqualFold(entry.value, "input") {
		return true, nil
	}
	value, ok := gitBool(entry.value, entry.valueless)
	if !ok {
		return false, errors.New("core.autocrlf has a value cw does not recognise")
	}
	return value, nil
}

// fileModeOff reports whether core.fileMode is off: its effective value is
// false.
func fileModeOff(entry configEntry) (bool, error) {
	if !entry.set {
		return false, nil
	}
	value, ok := gitBool(entry.value, entry.valueless)
	if !ok {
		return false, errors.New("core.fileMode has a value cw does not recognise")
	}
	return !value, nil
}

// attributeSet reports whether a check-attr value is set to anything.
func attributeSet(info string) bool { return info != "" && info != "unspecified" && info != "unset" }

// conversion says why Git would not store a path's verified bytes or mode,
// or returns "" when it would. attrs holds the check-attr values of filter,
// ident, working-tree-encoding, text, eol and crlf.
func conversion(attrs map[string]string, content []byte, verified, head pathState, autocrlf, modeOff bool) string {
	for _, name := range []string{"filter", "ident", "working-tree-encoding"} {
		if attributeSet(attrs[name]) {
			return "the " + name + " attribute is set"
		}
	}
	if bytes.IndexByte(content, '\r') >= 0 {
		for _, name := range []string{"text", "eol", "crlf"} {
			if attributeSet(attrs[name]) {
				return "it contains a CR byte and the " + name + " attribute is set"
			}
		}
		if autocrlf {
			return "it contains a CR byte and core.autocrlf converts line endings"
		}
	}
	if modeOff && (head.present && head.mode != verified.mode || !head.present && verified.mode == "100755") {
		return "core.fileMode is off and its mode changes"
	}
	return ""
}

// gitSchemes are the URL schemes Git serves itself: ssh (with its deprecated
// git+ssh and ssh+git spellings), git and file natively, https and http
// through the curl helper Git ships. Git hands a URL with any other scheme,
// in any other case, to the remote helper the scheme names.
var gitSchemes = []string{"https", "http", "ssh", "git+ssh", "ssh+git", "git", "file"}

// helperTransport reports whether Git reads url as <transport>::<address> and
// runs that remote helper: url starts with a letter or digit, then letters,
// digits, +, - or ., and "::" follows them; as in Git, the run may be empty.
func helperTransport(url string) bool {
	i := 0
	for i < len(url) && (isAlnum(url[i]) || i > 0 && strings.IndexByte("+-.", url[i]) >= 0) {
		i++
	}
	return strings.HasPrefix(url[i:], "::")
}

// isAlnum reports whether c is an ASCII letter or digit.
func isAlnum(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}

// reduceURL reduces a remote URL to a host and a path: URLs with a scheme in
// gitSchemes give their lowercased host without user and port,
// [user@]host:path (colon before any /) gives host and path as scpForm
// splits it, and anything else is a local path made absolute against
// worktree. The third result is false for a URL Git hands to a remote helper
// (another scheme, or <transport>::<address>): the helper chooses where the
// push goes, so the URL's host and path name nothing. It is also false for an
// scp form in which Git finds no path.
func reduceURL(url, worktree string) (string, string, bool) {
	if helperTransport(url) {
		return "", "", false
	}
	if scheme, rest, found := strings.Cut(url, "://"); found && scheme != "" && !strings.Contains(scheme, "/") {
		if !slices.Contains(gitSchemes, scheme) {
			return "", "", false
		}
		authority, path := rest, ""
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			authority, path = rest[:i], rest[i:]
		}
		if i := strings.LastIndexByte(authority, '@'); i >= 0 {
			authority = authority[i+1:]
		}
		host := authority
		if strings.HasPrefix(host, "[") {
			if end := strings.IndexByte(host, ']'); end >= 0 {
				host = host[:end+1]
			}
		} else if i := strings.LastIndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		return strings.ToLower(host), path, true
	}
	if colon := strings.IndexByte(url, ':'); colon > 0 {
		if slash := strings.IndexByte(url, '/'); slash < 0 || colon < slash {
			return scpForm(url)
		}
	}
	local := url
	if !filepath.IsAbs(local) {
		local = filepath.Join(worktree, local)
	}
	return "", filepath.Clean(local), true
}

// scpForm splits [user@]host:path as Git's parse_connect_url does (connect.c).
// The host may be in brackets after an optional user@ (host_end: from the
// first "@[", else from a leading "["); the brackets run to the first "]",
// and the path starts after the first ":" that follows them, so a colon
// inside the brackets belongs to the host. Without brackets the path starts
// after the first ":". The host drops its user and, inside brackets, a
// trailing :port (get_port: the first ":" followed by a number below 65536
// and nothing else); a bracketed host that still holds a ":" is an IPv6
// address and keeps its brackets, as an ssh:// URL's does. The third result
// is false when no ":" follows the brackets: Git finds no path there.
func scpForm(url string) (string, string, bool) {
	start, end := 0, 0
	if i := strings.Index(url, "@["); i >= 0 {
		start = i + 1
	}
	if url[start] == '[' {
		if i := strings.IndexByte(url[start:], ']'); i >= 0 {
			end = start + i
		}
	}
	separator := strings.IndexByte(url[end:], ':')
	if separator < 0 {
		return "", "", false
	}
	separator += end
	host, path := url[:separator], url[separator+1:]
	if end == 0 {
		if i := strings.LastIndexByte(host, '@'); i >= 0 {
			host = host[i+1:]
		}
		return host, path, true
	}
	host = url[start+1 : end]
	if i := strings.LastIndexByte(host, '@'); i >= 0 {
		host = host[i+1:]
	}
	if i := strings.IndexByte(host, ':'); i >= 0 {
		if _, err := strconv.ParseUint(host[i+1:], 10, 16); err == nil {
			host = host[:i]
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host, path, true
}

// sameRepository reports whether two remote URLs name the same repository:
// both reduce, with equal hosts, and paths equal case-insensitively once
// leading and trailing slashes and one trailing .git (in any case) are
// removed. A URL served by a remote helper names no repository, not even
// itself. R11.g and R12.a both compare through it.
func sameRepository(a, b, worktree string) bool {
	hostA, pathA, okA := reduceURL(a, worktree)
	hostB, pathB, okB := reduceURL(b, worktree)
	return okA && okB && hostA == hostB && repositoryPath(pathA) == repositoryPath(pathB)
}

// repositoryPath is a reduced path in the form sameRepository compares:
// lowercased, without leading and trailing slashes and one trailing .git.
func repositoryPath(path string) string {
	path = strings.Trim(strings.ToLower(path), "/")
	path = strings.TrimSuffix(path, ".git")
	return strings.Trim(path, "/")
}

// pullRequest is one PR as gh reports it.
type pullRequest struct {
	Number            int
	URL               string
	State             string
	HeadRefName       string
	HeadRefOID        string
	BaseRefName       string
	Title             string
	Body              string
	IsCrossRepository bool
}

// normalizeBody turns CRLF into LF and removes trailing spaces, tabs, CR and
// LF.
func normalizeBody(body string) string {
	return strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), " \t\r\n")
}

// sameRepositoryOpen keeps the same-repository open PRs whose head branch is
// branch.
func sameRepositoryOpen(prs []pullRequest, branch string) []pullRequest {
	kept := []pullRequest{}
	for _, pr := range prs {
		if pr.HeadRefName == branch && pr.State == "OPEN" && !pr.IsCrossRepository {
			kept = append(kept, pr)
		}
	}
	return kept
}

// equivalent reports whether pr already carries the request's base, title
// and body.
func equivalent(pr pullRequest, req Request) bool {
	return pr.BaseRefName == req.base && pr.Title == req.title && normalizeBody(pr.Body) == normalizeBody(string(req.body))
}

// prPlan is what the PR step will do.
type prPlan struct {
	// action is create, edit or present (nothing to change).
	action string
	pr     pullRequest
}

// planPR applies R12.c and R12.d to the same-repository open PRs for the
// branch; head is the publication commit when known, else "".
func planPR(open []pullRequest, req Request, head string) (prPlan, string) {
	switch req.pr {
	case PRCreate:
		switch {
		case len(open) == 0:
			return prPlan{action: "create"}, ""
		case len(open) == 1 && equivalent(open[0], req):
			return prPlan{action: "present", pr: open[0]}, ""
		}
		return prPlan{}, "pr-exists"
	default:
		switch len(open) {
		case 0:
			return prPlan{}, "pr-missing"
		case 1:
			if equivalent(open[0], req) && head != "" && open[0].HeadRefOID == head {
				return prPlan{action: "present", pr: open[0]}, ""
			}
			return prPlan{action: "edit", pr: open[0]}, ""
		}
		return prPlan{}, "pr-ambiguous"
	}
}

// readbackDiffers says how a read-back PR differs from the request and head,
// and whether the head commit is the only difference.
func readbackDiffers(pr pullRequest, req Request, head string) (string, bool) {
	var differs []string
	if pr.State != "OPEN" {
		differs = append(differs, "state "+pr.State)
	}
	if pr.BaseRefName != req.base {
		differs = append(differs, "base "+pr.BaseRefName)
	}
	if pr.Title != req.title {
		differs = append(differs, "title")
	}
	if normalizeBody(pr.Body) != normalizeBody(string(req.body)) {
		differs = append(differs, "body")
	}
	onlyHead := len(differs) == 0
	if pr.HeadRefOID != head {
		differs = append(differs, "head "+pr.HeadRefOID)
	}
	if len(differs) == 0 {
		return "", false
	}
	return "the pull request differs: " + strings.Join(differs, ", "), onlyHead
}

// repositoryVariables select a repository other than the worktree's own;
// they are removed from every git and gh environment.
var repositoryVariables = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES",
	"GIT_DISCOVERY_ACROSS_FILESYSTEM"}

// ToolEnvironment is environ without the repository-selecting variables and
// with set (NAME=value) replacing any earlier value of NAME. cw gate runs git
// with it too, so git finds the repository it names rather than one the
// caller's environment selects.
func ToolEnvironment(environ []string, set string) []string {
	name, _, _ := strings.Cut(set, "=")
	kept := []string{}
	for _, variable := range environ {
		key, _, _ := strings.Cut(variable, "=")
		if key == name || slices.Contains(repositoryVariables, key) {
			continue
		}
		kept = append(kept, variable)
	}
	return append(kept, set)
}
