package decl

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

const validFile = `version = 1

[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]
timeout = "5m"

[[check]]
id = "lint"
run = ["sh", "lint.sh"]
`

func TestParseValid(t *testing.T) {
	file, err := Parse([]byte(validFile))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []Check{
		{ID: "unit", Run: []string{"sh", "unit.sh"}, Paths: []string{"src/**", "unit.sh"}, Timeout: 5 * time.Minute, Line: 3},
		{ID: "lint", Run: []string{"sh", "lint.sh"}, Paths: nil, Timeout: DefaultTimeout, Line: 9},
	}
	if len(file.Checks) != len(want) {
		t.Fatalf("got %d checks, want %d", len(file.Checks), len(want))
	}
	for i, w := range want {
		g := file.Checks[i]
		if g.ID != w.ID || !slices.Equal(g.Run, w.Run) || !slices.Equal(g.Paths, w.Paths) || g.Timeout != w.Timeout || g.Line != w.Line {
			t.Errorf("check %d = %+v, want %+v", i, g, w)
		}
		if w.Paths == nil && g.Paths != nil {
			t.Errorf("check %s: Paths = %#v, want nil for a check without paths", g.ID, g.Paths)
		}
	}
	if c, ok := file.Check("lint"); !ok || c.ID != "lint" {
		t.Errorf("Check(lint) = %+v, %v", c, ok)
	}
	if _, ok := file.Check("nosuch"); ok {
		t.Error("Check(nosuch) found a check")
	}
}

func TestParseNoChecks(t *testing.T) {
	file, err := Parse([]byte("version = 1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(file.Checks) != 0 {
		t.Errorf("got %d checks, want none", len(file.Checks))
	}
}

// withCheck is a file whose only [[check]] header is at line 3, followed by
// body.
func withCheck(body string) string {
	return "version = 1\n\n[[check]]\n" + body
}

func TestParseRefusals(t *testing.T) {
	cases := []struct {
		name  string
		data  string
		line  int
		words []string
	}{
		{
			name:  "unknown key",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\"]\n\n\n\ntiemout = \"5m\"\n"),
			line:  9,
			words: []string{"tiemout"},
		},
		{
			name:  "unknown top-level key",
			data:  "version = 1\nname = \"x\"\n",
			line:  2,
			words: []string{"name"},
		},
		{
			name:  "duplicate id",
			data:  withCheck("id = \"unit\"\nrun = [\"a\"]\n\n[[check]]\nid = \"unit\"\nrun = [\"b\"]\n"),
			line:  7,
			words: []string{"unit", "3", "7"},
		},
		{
			name:  "run as a string",
			data:  withCheck("id = \"unit\"\nrun = \"go test ./...\"\n"),
			line:  5,
			words: []string{"`run`", "array of strings"},
		},
		{
			name:  "empty run",
			data:  withCheck("id = \"unit\"\nrun = []\n"),
			line:  5,
			words: []string{"`run`"},
		},
		{
			name:  "run with an empty program",
			data:  withCheck("id = \"unit\"\nrun = [\"\", \"x\"]\n"),
			line:  5,
			words: []string{"`run`"},
		},
		{
			name:  "run with a number",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\", 1]\n"),
			line:  5,
			words: []string{"`run`"},
		},
		{
			name:  "missing run",
			data:  withCheck("id = \"unit\"\n"),
			line:  3,
			words: []string{"`run`"},
		},
		{
			name:  "missing id",
			data:  withCheck("run = [\"sh\"]\n"),
			line:  3,
			words: []string{"`id`"},
		},
		{
			name:  "upper-case id",
			data:  withCheck("id = \"Unit\"\nrun = [\"sh\"]\n"),
			line:  4,
			words: []string{"`id`"},
		},
		{
			name:  "id that begins with a dash",
			data:  withCheck("id = \"-unit\"\nrun = [\"sh\"]\n"),
			line:  4,
			words: []string{"`id`"},
		},
		{
			name:  "id of 41 characters",
			data:  withCheck("id = \"" + strings.Repeat("a", 41) + "\"\nrun = [\"sh\"]\n"),
			line:  4,
			words: []string{"`id`"},
		},
		{
			name:  "id as a number",
			data:  withCheck("id = 7\nrun = [\"sh\"]\n"),
			line:  4,
			words: []string{"`id`"},
		},
		{
			name:  "version 2",
			data:  "# comment\nversion = 2\n",
			line:  2,
			words: []string{"`version`"},
		},
		{
			name:  "version as a string",
			data:  "version = \"1\"\n",
			line:  1,
			words: []string{"`version`"},
		},
		{
			name:  "missing version",
			data:  "[[check]]\nid = \"unit\"\nrun = [\"sh\"]\n",
			words: []string{"`version`"},
		},
		{
			name:  "timeout too short",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\"]\ntimeout = \"500ms\"\n"),
			line:  6,
			words: []string{"`timeout`"},
		},
		{
			name:  "timeout too long",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\"]\ntimeout = \"6h1s\"\n"),
			line:  6,
			words: []string{"`timeout`"},
		},
		{
			name:  "timeout as a number",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\"]\ntimeout = 60\n"),
			line:  6,
			words: []string{"`timeout`"},
		},
		{
			name:  "absolute pattern",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\"]\npaths = [\"/src/**\"]\n"),
			line:  6,
			words: []string{"/src/**"},
		},
		{
			name:  "empty pattern",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\"]\npaths = [\"\"]\n"),
			line:  6,
			words: []string{"empty"},
		},
		{
			name:  "empty paths",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\"]\npaths = []\n"),
			line:  6,
			words: []string{"`paths`"},
		},
		{
			name:  "paths as a string",
			data:  withCheck("id = \"unit\"\nrun = [\"sh\"]\npaths = \"src/**\"\n"),
			line:  6,
			words: []string{"`paths`"},
		},
		{
			name:  "invalid TOML",
			data:  "version = 1\n\n[[check]\nid = \"unit\"\n",
			line:  3,
			words: []string{"TOML"},
		},
		{
			name:  "invalid TOML value",
			data:  "version = 1\nx = \n",
			line:  2,
			words: []string{"TOML"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, err := Parse([]byte(tc.data))
			if err == nil {
				t.Fatalf("Parse accepted the file: %+v", file)
			}
			var declErr *Error
			if !errors.As(err, &declErr) {
				t.Fatalf("error %T %v is not a *decl.Error", err, err)
			}
			text := err.Error()
			if tc.line > 0 {
				prefix := fmt.Sprintf("%s:%d:", FileName, tc.line)
				if !strings.Contains(text, prefix) {
					t.Errorf("error %q does not name %s", text, prefix)
				}
			}
			for _, w := range tc.words {
				if !strings.Contains(text, w) {
					t.Errorf("error %q does not hold %q", text, w)
				}
			}
		})
	}
}

func TestParseReportsEveryProblem(t *testing.T) {
	data := withCheck("id = \"Bad\"\nrun = \"sh\"\ntimeout = \"1d\"\n")
	_, err := Parse([]byte(data))
	var declErr *Error
	if !errors.As(err, &declErr) {
		t.Fatalf("error %v is not a *decl.Error", err)
	}
	if len(declErr.Problems) != 3 {
		t.Errorf("got %d problems, want 3 (id, run, timeout): %v", len(declErr.Problems), err)
	}
}

func TestErrorWithoutLine(t *testing.T) {
	err := &Error{Problems: []Problem{{Text: "a"}, {Line: 4, Text: "b"}}}
	want := FileName + ": a\n" + FileName + ":4: b"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestParseTimeout(t *testing.T) {
	cases := []struct {
		text string
		want time.Duration
		ok   bool
	}{
		{"1s", time.Second, true},
		{"90s", 90 * time.Second, true},
		{"5m", 5 * time.Minute, true},
		{"1h30m", 90 * time.Minute, true},
		{"6h", 6 * time.Hour, true},
		{"999ms", 0, false},
		{"0s", 0, false},
		{"-5m", 0, false},
		{"6h0m1s", 0, false},
		{"7h", 0, false},
		{"1d", 0, false},
		{"5", 0, false},
		{"", 0, false},
		{"five minutes", 0, false},
	}
	for _, tc := range cases {
		got, err := ParseTimeout(tc.text)
		if tc.ok && (err != nil || got != tc.want) {
			t.Errorf("ParseTimeout(%q) = %v, %v; want %v", tc.text, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("ParseTimeout(%q) = %v, want an error", tc.text, got)
		}
	}
}

func TestAppend(t *testing.T) {
	cases := []struct {
		name string
		old  string
	}{
		{"ends with a newline", validFile},
		{"no final newline", strings.TrimSuffix(validFile, "\n")},
		{"only the version", "version = 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Append([]byte(tc.old), "build", []string{"make", "-j4", "build"}, []string{"src/**"}, "5m")
			if err != nil {
				t.Fatalf("Append: %v", err)
			}
			if !bytes.HasPrefix(out, []byte(tc.old)) {
				t.Fatalf("the result does not begin with the old bytes:\n%s", out)
			}
			file, err := Parse(out)
			if err != nil {
				t.Fatalf("the result does not parse: %v", err)
			}
			last := file.Checks[len(file.Checks)-1]
			if last.ID != "build" || !slices.Equal(last.Run, []string{"make", "-j4", "build"}) ||
				!slices.Equal(last.Paths, []string{"src/**"}) || last.Timeout != 5*time.Minute {
				t.Errorf("appended check = %+v", last)
			}
		})
	}
}

func TestAppendDefaults(t *testing.T) {
	out, err := Append([]byte("version = 1\n"), "lint", []string{"true"}, nil, "")
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if bytes.Contains(out, []byte("paths")) || bytes.Contains(out, []byte("timeout")) {
		t.Errorf("Append wrote a key that was not given:\n%s", out)
	}
	file, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if c := file.Checks[0]; c.Paths != nil || c.Timeout != DefaultTimeout {
		t.Errorf("check = %+v", c)
	}
}

func TestAppendQuotesArguments(t *testing.T) {
	run := []string{"sh", "-c", "echo \"$HOME\" \\ && false\n\tx\r\x01\x7f"}
	out, err := Append([]byte("version = 1\n"), "quote", run, nil, "")
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	file, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, out)
	}
	if got := file.Checks[0].Run; !slices.Equal(got, run) {
		t.Errorf("run = %q, want %q", got, run)
	}
}

func TestAppendRefuses(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		run     []string
		paths   []string
		timeout string
		word    string
	}{
		{"duplicate id", "unit", []string{"true"}, nil, "", "unit"},
		{"bad id", "Build", []string{"true"}, nil, "", "`id`"},
		{"no program", "build", nil, nil, "", "`run`"},
		{"bad timeout", "build", []string{"true"}, nil, "7h", "`timeout`"},
		{"bad pattern", "build", []string{"true"}, []string{"/abs"}, "", "/abs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := []byte(validFile)
			kept := bytes.Clone(old)
			out, err := Append(old, tc.id, tc.run, tc.paths, tc.timeout)
			if err == nil {
				t.Fatalf("Append accepted it:\n%s", out)
			}
			var declErr *Error
			if !errors.As(err, &declErr) {
				t.Errorf("error %T is not a *decl.Error", err)
			}
			if !strings.Contains(err.Error(), tc.word) {
				t.Errorf("error %q does not hold %q", err, tc.word)
			}
			if out != nil {
				t.Errorf("Append returned bytes with its error")
			}
			if !bytes.Equal(old, kept) {
				t.Errorf("Append changed the old bytes")
			}
		})
	}
}

func TestAppendToInvalidFile(t *testing.T) {
	if _, err := Append([]byte("version = 2\n"), "build", []string{"true"}, nil, ""); err == nil {
		t.Error("Append accepted a file whose version is 2")
	}
}
