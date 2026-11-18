// Package decl reads and writes kitsu.toml, the file that declares the checks
// of a repository.
package decl

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// FileName is the declaration at the root of a tree.
const FileName = "kitsu.toml"

// The bounds and the default of a check's timeout.
const (
	MinTimeout     = time.Second
	MaxTimeout     = 6 * time.Hour
	DefaultTimeout = 10 * time.Minute
)

// Check is one declared check.
type Check struct {
	ID      string
	Run     []string
	Paths   []string // nil when the check depends on every path
	When    []string // optional change triggers, independent of evidence dependencies
	Expect  string
	Verify  bool
	Timeout time.Duration
	Line    int // the line of its [[check]] header, 0 when unknown
}

// File is a valid declaration.
type File struct {
	RequireReview bool
	Checks        []Check
}

// Check finds a declared check by its id.
func (f *File) Check(id string) (Check, bool) {
	for _, c := range f.Checks {
		if c.ID == id {
			return c, true
		}
	}
	return Check{}, false
}

// Problem is one broken rule of a declaration.
type Problem struct {
	Line int // 0 when the line is unknown
	Text string
}

// Error lists every broken rule found in a declaration.
type Error struct {
	Problems []Problem
}

func (e *Error) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		if p.Line > 0 {
			lines[i] = fmt.Sprintf("%s:%d: %s", FileName, p.Line, p.Text)
		} else {
			lines[i] = fmt.Sprintf("%s: %s", FileName, p.Text)
		}
	}
	return strings.Join(lines, "\n")
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// deviceName matches the ids that Windows reads as a device, not as the name
// of a folder. They are refused on every system, so that one declaration is
// valid everywhere.
var deviceName = regexp.MustCompile(`^(con|prn|aux|nul|com[0-9]|lpt[0-9])$`)

// Plain returns text as it is when it holds no control character, and
// quoted otherwise, so that a name a repository holds can add neither a line
// nor a terminal command to what kitsu prints.
func Plain(text string) string {
	for _, r := range text {
		if r < 0x20 || r >= 0x7f && r < 0xa0 || r == utf8.RuneError {
			return strconv.Quote(text)
		}
	}
	return text
}

// raw is the file as TOML gives it. The fields of a check are decoded
// loosely, so that each broken rule gets a sentence of its own.
type raw struct {
	RequireReview bool       `toml:"require_review"`
	Version       any        `toml:"version"`
	Check         []rawCheck `toml:"check"`
}

type rawCheck struct {
	ID      any `toml:"id"`
	Run     any `toml:"run"`
	Paths   any `toml:"paths"`
	When    any `toml:"when"`
	Expect  any `toml:"expect"`
	Verify  any `toml:"verify"`
	Timeout any `toml:"timeout"`
}

// Parse reads a declaration. Every error it returns is an *Error.
func Parse(data []byte) (*File, error) {
	var r raw
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return nil, tomlError(err)
	}
	var problems []Problem
	if v, ok := r.Version.(int64); !ok || v != 1 {
		problems = append(problems, Problem{Line: keyLine(data, 1, 0, "version"), Text: "`version` must be 1"})
	}
	headers := headerLines(data)
	if len(headers) != len(r.Check) {
		headers = make([]int, len(r.Check)) // written in another form: lines unknown
	}
	file := &File{RequireReview: r.RequireReview}
	seen := map[string]int{}
	for i, rc := range r.Check {
		end := 0
		if i+1 < len(headers) {
			end = headers[i+1]
		}
		check, found := rc.check(data, headers[i], end)
		problems = append(problems, found...)
		if first, dup := seen[check.ID]; dup && check.ID != "" {
			problems = append(problems, Problem{Line: check.Line, Text: fmt.Sprintf("the id `%s` is declared twice, at lines %d and %d", check.ID, first, check.Line)})
		} else if check.ID != "" {
			seen[check.ID] = check.Line
		}
		file.Checks = append(file.Checks, check)
	}
	if len(problems) > 0 {
		return nil, &Error{Problems: problems}
	}
	return file, nil
}

// check validates one [[check]] whose header is at line start; the next
// header is at line end, or 0.
func (rc rawCheck) check(data []byte, start, end int) (Check, []Problem) {
	check := Check{Line: start, Timeout: DefaultTimeout}
	var problems []Problem
	fail := func(key, format string, args ...any) {
		problems = append(problems, Problem{Line: keyLine(data, start, end, key), Text: fmt.Sprintf(format, args...)})
	}
	switch id, ok := rc.ID.(string); {
	case rc.ID == nil:
		fail("", "a check needs an `id`")
	case !ok || !idPattern.MatchString(id):
		fail("id", "`id` must match [a-z0-9][a-z0-9_-]{0,39}")
	case deviceName.MatchString(id):
		fail("id", "`id` must not be `%s`, which Windows reads as the name of a device", id)
	default:
		check.ID = id
	}
	run, ok := stringList(rc.Run)
	switch {
	case rc.Run == nil:
		fail("", "a check needs `run`, an array of the program and its arguments")
	case !ok || len(run) == 0 || run[0] == "":
		fail("run", "`run` must be an array of strings that begins with a program, such as [\"go\", \"test\", \"./...\"]")
	default:
		check.Run = run
	}
	if rc.Paths != nil {
		paths, ok := stringList(rc.Paths)
		if !ok || len(paths) == 0 {
			fail("paths", "`paths` must be an array of one or more patterns")
		}
		for _, p := range paths {
			if err := ValidPattern(p); err != nil {
				fail("paths", "%v", err)
			}
		}
		check.Paths = paths
	}
	if rc.Timeout != nil {
		text, _ := rc.Timeout.(string)
		d, err := ParseTimeout(text)
		if err != nil {
			fail("timeout", "%v", err)
		}
		check.Timeout = d
	}
	rc.metadata(&check, fail)
	return check, problems
}

func (rc rawCheck) metadata(check *Check, fail func(string, string, ...any)) {
	if rc.When != nil {
		patterns, ok := stringList(rc.When)
		if !ok || len(patterns) == 0 {
			fail("when", "`when` must be an array of one or more patterns")
		}
		for _, pattern := range patterns {
			if err := ValidPattern(pattern); err != nil {
				fail("when", "`when`: %v", err)
			}
		}
		check.When = patterns
	}
	if rc.Expect != nil {
		text, ok := rc.Expect.(string)
		if !ok || strings.TrimSpace(text) == "" {
			fail("expect", "`expect` must be nonempty text describing the expected behavior")
		}
		check.Expect = text
	}
	if rc.Verify != nil {
		value, ok := rc.Verify.(bool)
		if !ok {
			fail("verify", "`verify` must be true or false")
		}
		check.Verify = value
	}
	if check.Verify && strings.TrimSpace(check.Expect) == "" && rc.Expect == nil {
		fail("verify", "`verify = true` needs nonempty `expect` text")
	}
}

// ParseTimeout reads a timeout such as "90s", "5m" or "1h30m".
func ParseTimeout(text string) (time.Duration, error) {
	d, err := time.ParseDuration(text)
	if err != nil || d < MinTimeout || d > MaxTimeout {
		return 0, errors.New("`timeout` must be a duration from 1s to 6h, such as \"5m\"")
	}
	return d, nil
}

func stringList(v any) ([]string, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// tomlError turns a decoding error into problems with lines.
func tomlError(err error) error {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		var problems []Problem
		for _, e := range strict.Errors {
			line, _ := e.Position()
			key := e.Key()
			problems = append(problems, Problem{Line: line, Text: fmt.Sprintf("unknown key `%s`", Plain(key[len(key)-1]))})
		}
		return &Error{Problems: problems}
	}
	var decode *toml.DecodeError
	if errors.As(err, &decode) {
		line, _ := decode.Position()
		return &Error{Problems: []Problem{{Line: line, Text: "not valid TOML: " + Plain(decode.Error())}}}
	}
	return &Error{Problems: []Problem{{Text: "not valid TOML: " + Plain(err.Error())}}}
}

var headerPattern = regexp.MustCompile(`^\s*\[\[\s*check\s*\]\]\s*(#.*)?$`)

// headerLines are the lines that hold a [[check]] header.
func headerLines(data []byte) []int {
	var lines []int
	for i, line := range strings.Split(string(data), "\n") {
		if headerPattern.MatchString(strings.TrimRight(line, "\r")) {
			lines = append(lines, i+1)
		}
	}
	return lines
}

// keyLine is the line that sets key between the lines start and end, and
// start when there is none. end 0 means the end of the file.
func keyLine(data []byte, start, end int, key string) int {
	if key == "" || start == 0 {
		return start
	}
	for i, line := range strings.Split(string(data), "\n") {
		n := i + 1
		if n < start || (end > 0 && n >= end) {
			continue
		}
		name, _, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && strings.TrimSpace(name) == key {
			return n
		}
	}
	return start
}

// Append returns old with one more check at its end. No earlier byte
// changes. timeout is written as given; empty leaves the default.
func Append(old []byte, id string, run, paths []string, timeout string, metadata ...Metadata) ([]byte, error) {
	if len(metadata) > 1 {
		return nil, &Error{Problems: []Problem{{Text: "only one check metadata value may be appended"}}}
	}
	var b bytes.Buffer
	b.Write(old)
	if len(old) > 0 && !bytes.HasSuffix(old, []byte("\n")) {
		b.WriteByte('\n')
	}
	b.WriteString("\n[[check]]\n")
	fmt.Fprintf(&b, "id = %s\n", quote(id))
	fmt.Fprintf(&b, "run = %s\n", QuoteList(run))
	if len(paths) > 0 {
		fmt.Fprintf(&b, "paths = %s\n", QuoteList(paths))
	}
	if timeout != "" {
		fmt.Fprintf(&b, "timeout = %s\n", quote(timeout))
	}
	if len(metadata) == 1 {
		m := metadata[0]
		if m.When != nil {
			fmt.Fprintf(&b, "when = %s\n", QuoteList(m.When))
		}
		if m.Expect != "" {
			fmt.Fprintf(&b, "expect = %s\n", quote(m.Expect))
		}
		if m.Verify {
			b.WriteString("verify = true\n")
		}
	}
	if _, err := Parse(b.Bytes()); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Metadata optionally describes when a check verifies an expected behavior.
// It does not change the check's run identity or evidence dependencies.
type Metadata struct {
	When   []string
	Expect string
	Verify bool
}

// QuoteList writes items as a TOML array of strings.
func QuoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = quote(item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// quote writes s as a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
