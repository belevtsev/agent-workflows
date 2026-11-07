package evals

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// jsRegex is a JavaScript pattern compiled with Go's regexp. approximate is
// true when the Go form cannot decide matches exactly: lookarounds become
// plain groups, backreferences empty groups, a lone surrogate a class that
// never matches, and a repeat count above RE2's limit of 1000 an open repeat,
// so the syntax is still checked but matches are not.
//
// Otherwise matches are exact for the text the validator matches, JSON tool
// input that is ASCII or BMP text, apart from these known differences:
//   - Go matches by code point where JavaScript without u matches by UTF-16
//     unit, so ., negated classes and repeats differ on astral characters.
//   - JavaScript's extra line terminators (U+2028, U+2029 and \r for . and
//     for ^ and $ with m) are not modelled.
//   - With flag i and without u, JavaScript does not fold the Kelvin sign,
//     the long s and the Angstrom sign onto k, s and å as Go does.
//
// Valid JavaScript that RE2 cannot express is rejected with an error naming
// the pattern, never passed silently: v-mode set notation, \p{...} binary
// properties, long category names, script aliases and Script_Extensions, and
// repeats nested beyond 1000.
type jsRegex struct {
	*regexp.Regexp
	approximate bool
}

// jsSyntax describes how a JavaScript pattern is read: Unicode mode (flag u or
// v) is strict, otherwise Annex B's legacy forms apply; v also changes the
// class grammar. names and groups come from a first pass, because JavaScript
// allows forward references.
type jsSyntax struct {
	unicode, vMode bool
	names          map[string]bool
	groups         int
}

// jsRepeat matches a {n}, {n,} or {n,m} quantifier; maxRE2Repeat is RE2's
// largest repeat count.
var jsRepeat = regexp.MustCompile(`^\{([0-9]+)(,([0-9]*))?\}`)

const maxRE2Repeat = 1000

// compileJS checks that pattern is a valid JavaScript regex as the eval runner
// uses it, and compiles it for Go with flags i, m and s applied. An invalid
// pattern returns an error containing "does not compile"; an inline flag group
// such as (?i), which the runner rejects, returns one containing "inline flag".
func compileJS(pattern, flags string) (*jsRegex, error) {
	syntax := jsSyntax{unicode: strings.ContainsAny(flags, "uv"), vMode: strings.Contains(flags, "v")}
	syntax.names, syntax.groups = scanJSGroups(pattern)
	var out strings.Builder
	approximate := false
	inClass := false
	var ranges classRanges
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch {
		case ch == '\\':
			goForm, width, approx, err := syntax.escape(pattern[i:], inClass)
			if err == nil && inClass {
				err = ranges.item(syntax, syntax.classEscape(pattern[i:]))
			}
			if err != nil {
				return nil, fmt.Errorf("does not compile: %w", err)
			}
			out.WriteString(goForm)
			approximate = approximate || approx
			i += width - 1
		case inClass:
			if ch == ']' {
				inClass = false
			} else if syntax.vMode && strings.IndexByte("()[{}/|", ch) >= 0 {
				// v-mode classes reserve these and allow nested set notation,
				// which RE2 cannot express.
				return nil, fmt.Errorf("does not compile: unescaped %q in a v-mode character class", ch)
			} else if syntax.vMode && ch == '&' && strings.HasPrefix(pattern[i:], "&&") {
				return nil, errors.New("does not compile: set intersection (&&) in a v-mode class is not supported")
			} else if syntax.vMode && strings.IndexByte("!#$%*+,.:;<=>?@^`~", ch) >= 0 && i+1 < len(pattern) && pattern[i+1] == ch {
				return nil, fmt.Errorf("does not compile: %c%c is reserved in a v-mode character class", ch, ch)
			}
			if ch == '-' {
				goForm, err := ranges.dash(syntax, pattern[i+1:])
				if err != nil {
					return nil, fmt.Errorf("does not compile: %w", err)
				}
				out.WriteString(goForm)
				continue
			}
			if ch != ']' {
				if err := ranges.item(syntax, false); err != nil {
					return nil, fmt.Errorf("does not compile: %w", err)
				}
			}
			if ch == '[' {
				// A literal [ in JavaScript; RE2 would read [: as a POSIX class.
				out.WriteString(`\[`)
				continue
			}
			out.WriteByte(ch)
		case strings.HasPrefix(pattern[i:], "[^]"):
			// Any character, newline included; RE2 would read ] as a member.
			out.WriteString(`[\x{0}-\x{10FFFF}]`)
			i += len("[^]") - 1
		case strings.HasPrefix(pattern[i:], "[]"):
			// The empty class, which never matches.
			out.WriteString(`[^\x{0}-\x{10FFFF}]`)
			i += len("[]") - 1
		case ch == '[':
			inClass = true
			ranges = classRanges{}
			out.WriteByte(ch)
			if strings.HasPrefix(pattern[i+1:], "^") {
				out.WriteByte('^')
				i++
			}
		case ch == '(' && strings.HasPrefix(pattern[i:], "(?"):
			rest := pattern[i+2:]
			switch {
			case strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, "!"):
				out.WriteString("(?:")
				approximate = true
				i += 2
			case strings.HasPrefix(rest, "<=") || strings.HasPrefix(rest, "<!"):
				out.WriteString("(?:")
				approximate = true
				i += 3
			case strings.HasPrefix(rest, ":"):
				out.WriteString("(?:")
				i += 2
			case strings.HasPrefix(rest, "<"):
				// A named group, which RE2 also spells (?<name>...).
				out.WriteString("(?<")
				i += 2
			case rest != "" && (rest[0] >= 'a' && rest[0] <= 'z' || rest[0] >= 'A' && rest[0] <= 'Z' || rest[0] == '-'):
				return nil, errors.New("inline flag groups such as (?i) are not supported by the eval runner; use the grader's flags key")
			default:
				return nil, errors.New("does not compile: invalid group after (?")
			}
		case ch == '{':
			m := jsRepeat.FindStringSubmatch(pattern[i:])
			if m == nil {
				if syntax.unicode {
					return nil, errors.New("does not compile: a lone { must be escaped in Unicode mode")
				}
				out.WriteByte(ch) // Annex B: a brace that is not a quantifier
				continue
			}
			low, _ := strconv.Atoi(m[1])
			high, _ := strconv.Atoi(m[3])
			if m[3] != "" && high < low {
				return nil, fmt.Errorf("does not compile: quantifier %s is out of order", m[0])
			}
			if low <= maxRE2Repeat && high <= maxRE2Repeat {
				out.WriteString(m[0])
			} else {
				// RE2 caps repeat counts at 1000; JavaScript does not.
				out.WriteString(fmt.Sprintf("{%d,}", min(low, maxRE2Repeat)))
				approximate = true
			}
			i += len(m[0]) - 1
		case syntax.unicode && (ch == '}' || ch == ']'):
			return nil, fmt.Errorf("does not compile: a lone %c must be escaped in Unicode mode", ch)
		default:
			out.WriteByte(ch)
		}
	}
	if inClass {
		return nil, errors.New("does not compile: unterminated character class")
	}
	prefix := ""
	for _, f := range "ims" {
		if strings.ContainsRune(flags, f) {
			prefix += string(f)
		}
	}
	if prefix != "" {
		prefix = "(?" + prefix + ")"
	}
	re, err := regexp.Compile(prefix + out.String())
	if err != nil {
		return nil, fmt.Errorf("does not compile: %w", err)
	}
	return &jsRegex{Regexp: re, approximate: approximate}, nil
}

// scanJSGroups returns the named groups and the number of capturing groups,
// skipping escaped characters and character classes.
func scanJSGroups(pattern string) (map[string]bool, int) {
	names := map[string]bool{}
	groups := 0
	inClass := false
	for i := 0; i < len(pattern); i++ {
		switch ch := pattern[i]; {
		case ch == '\\':
			i++
		case inClass:
			inClass = ch != ']'
		case ch == '[':
			inClass = true
			// A ] right after [ or [^ closes the class in JavaScript.
			if strings.HasPrefix(pattern[i+1:], "]") {
				inClass = false
				i++
			} else if strings.HasPrefix(pattern[i+1:], "^]") {
				inClass = false
				i += 2
			}
		case ch == '(':
			rest := pattern[i+1:]
			if !strings.HasPrefix(rest, "?") {
				groups++
				continue
			}
			if strings.HasPrefix(rest, "?<") && !strings.HasPrefix(rest, "?<=") && !strings.HasPrefix(rest, "?<!") {
				if end := strings.IndexByte(rest, '>'); end > 2 {
					names[rest[2:end]] = true
				}
				groups++
			}
		}
	}
	return names, groups
}

// escape reads the escape at the start of s (s[0] is the backslash) and returns
// its RE2 form, the number of bytes it spans, and whether the RE2 form only
// approximates it.
func (js jsSyntax) escape(s string, inClass bool) (string, int, bool, error) {
	if len(s) < 2 {
		return "", 0, false, errors.New("trailing backslash")
	}
	c := s[1]
	switch {
	case c >= '1' && c <= '9':
		end := 1
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		if n, _ := strconv.Atoi(s[1:end]); !inClass && n <= js.groups {
			return "(?:)", end, true, nil // a backreference, which RE2 lacks
		}
		if js.unicode {
			return "", 0, false, fmt.Errorf(`\%s refers to no group`, s[1:end])
		}
		return legacyOctal(s)
	case c == '0':
		if len(s) < 3 || s[2] < '0' || s[2] > '9' {
			return `\x{0}`, 2, false, nil
		}
		if js.unicode {
			return "", 0, false, errors.New(`\0 followed by a digit is not allowed in Unicode mode`)
		}
		return legacyOctal(s)
	case c == 'b' && inClass:
		return `\x{8}`, 2, false, nil // backspace inside a class
	case c == 'B' && inClass:
		if js.unicode {
			return "", 0, false, errors.New(`\B is not allowed in a character class`)
		}
		return "B", 2, false, nil
	case c == 'u':
		return js.unicodeEscape(s)
	case c == 'x':
		if len(s) >= 4 && isHex(s[2:4]) {
			return `\x{` + s[2:4] + `}`, 4, false, nil
		}
		if js.unicode {
			return "", 0, false, errors.New(`\x needs two hex digits in Unicode mode`)
		}
		return "x", 2, false, nil
	case c == 'c':
		if len(s) >= 3 && isASCIILetter(s[2]) {
			return fmt.Sprintf(`\x{%X}`, s[2]%32), 3, false, nil
		}
		if !js.unicode && inClass && len(s) >= 3 && (s[2] >= '0' && s[2] <= '9' || s[2] == '_') {
			return fmt.Sprintf(`\x{%X}`, s[2]%32), 3, false, nil // Annex B ClassControlLetter
		}
		if js.unicode {
			return "", 0, false, errors.New(`\c needs an ASCII letter`)
		}
		return `\\c`, 2, false, nil // Annex B: a literal backslash, then c
	case c == 'k':
		if len(js.names) == 0 && !js.unicode {
			return "k", 2, false, nil // Annex B: without named groups \k is k
		}
		end := strings.IndexByte(s, '>')
		if !strings.HasPrefix(s[2:], "<") || end < 0 {
			return "", 0, false, errors.New(`\k needs <name>`)
		}
		name := s[3:end]
		if !js.names[name] {
			return "", 0, false, fmt.Errorf(`\k<%s> refers to no named group`, name)
		}
		if inClass {
			return "", 0, false, errors.New(`\k is not allowed in a character class`)
		}
		return "(?:)", end + 1, true, nil
	case c == 'p' || c == 'P':
		if !js.unicode {
			return string(c), 2, false, nil
		}
		end := strings.IndexByte(s, '}')
		if !strings.HasPrefix(s[2:], "{") || end < 0 {
			return "", 0, false, fmt.Errorf(`\%c needs {property} in Unicode mode`, c)
		}
		goForm, err := propertyEscape(c, s[3:end])
		return goForm, end + 1, false, err
	case c == 's' || c == 'S':
		return whitespaceEscape(c == 'S', inClass), 2, false, nil
	case strings.IndexByte("dDwWbBfnrtv", c) >= 0:
		return s[:2], 2, false, nil
	case c < utf8.RuneSelf && (isASCIILetter(c) || c >= '0' && c <= '9'):
		if js.unicode {
			return "", 0, false, fmt.Errorf(`unknown escape \%c in Unicode mode`, c)
		}
		return string(c), 2, false, nil // Annex B identity escape
	case c < utf8.RuneSelf:
		if js.unicode && !js.identityEscape(c, inClass) {
			return "", 0, false, fmt.Errorf(`escape \%c is not allowed in Unicode mode`, c)
		}
		// A code-point escape stays literal everywhere, even \- inside a class,
		// which QuoteMeta would leave as a range operator.
		return fmt.Sprintf(`\x{%X}`, c), 2, false, nil
	}
	r, width := utf8.DecodeRuneInString(s[1:])
	if js.unicode {
		return "", 0, false, fmt.Errorf(`escape \%c is not allowed in Unicode mode`, r)
	}
	return regexp.QuoteMeta(string(r)), 1 + width, false, nil
}

// classEscape reports whether the escape at the start of s stands for a set of
// characters (\d, \w, \s, and \p in Unicode mode) rather than one character.
func (js jsSyntax) classEscape(s string) bool {
	return len(s) >= 2 && (strings.IndexByte("dDwWsS", s[1]) >= 0 || js.unicode && (s[1] == 'p' || s[1] == 'P'))
}

// classRanges follows the items of one character class to check its ranges.
// Unicode mode forbids a class escape such as \d as a range end; Annex B reads
// that - as a literal, which RE2 would reject. v mode also requires every
// unescaped - to form a range and reserves -- for set subtraction, which RE2
// lacks.
type classRanges struct {
	start      bool // the last item can start a range
	startClass bool // the last item is a class escape
	open       bool // a - waits for the range's end
}

// item records one class member; classEscape is true for \d, \w, \s or \p.
func (r *classRanges) item(js jsSyntax, classEscape bool) error {
	if r.open {
		r.open, r.start = false, false
		if js.unicode && (r.startClass || classEscape) {
			return errors.New("a class escape cannot bound a range in Unicode mode")
		}
		return nil
	}
	r.start, r.startClass = true, classEscape
	return nil
}

// dash records an unescaped -, whose pattern rest follows it, and returns its
// RE2 form.
func (r *classRanges) dash(js jsSyntax, rest string) (string, error) {
	if r.start && !r.open && rest != "" && rest[0] != ']' {
		if js.vMode && rest[0] == '-' {
			return "", errors.New("set subtraction (--) in a v-mode class is not supported")
		}
		r.open = true
		if !js.unicode && (r.startClass || js.classEscape(rest)) {
			// Annex B: a class escape at either end makes the - a literal,
			// and the item after it still ends the would-be range.
			return `\x{2D}`, nil
		}
		return "-", nil
	}
	if js.vMode && !r.open {
		return "", errors.New("an unescaped - in a v-mode class must form a range")
	}
	return `\x{2D}`, r.item(js, false) // a literal -
}

// jsSpace lists JavaScript's \s, which RE2's \s narrows to ASCII without \v:
// WhiteSpace and LineTerminator code points.
const jsSpace = `\t\n\v\f\r\x{20}\x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// jsNonSpace is the complement of jsSpace, for \S inside a class.
const jsNonSpace = `\x{0}-\x{8}\x{E}-\x{1F}\x{21}-\x{9F}\x{A1}-\x{167F}\x{1681}-\x{1FFF}\x{200B}-\x{2027}\x{202A}-\x{202E}\x{2030}-\x{205E}\x{2060}-\x{2FFF}\x{3001}-\x{FEFE}\x{FF00}-\x{10FFFF}`

// whitespaceEscape returns the RE2 form of \s, or of \S when negated.
func whitespaceEscape(negated, inClass bool) string {
	switch {
	case inClass && negated:
		return jsNonSpace
	case inClass:
		return jsSpace
	case negated:
		return "[^" + jsSpace + "]"
	default:
		return "[" + jsSpace + "]"
	}
}

// jsPropertyName matches a property name or value in \p{...}.
var jsPropertyName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// propertyEscape returns the RE2 form of \p{body} or \P{body} in Unicode mode.
// JavaScript names a script as Script=Name (or sc=, or Script_Extensions=)
// and a category bare or as General_Category=Value; RE2 spells both bare.
// Names RE2 does not know, such as binary properties, fail when RE2 compiles.
func propertyEscape(c byte, body string) (string, error) {
	name, value, keyed := strings.Cut(body, "=")
	if !jsPropertyName.MatchString(name) || keyed && !jsPropertyName.MatchString(value) {
		return "", fmt.Errorf(`\%c{%s} is not a property name`, c, body)
	}
	switch {
	case !keyed:
		if _, script := unicode.Scripts[name]; script && unicode.Categories[name] == nil {
			return "", fmt.Errorf(`\%c{%s} names a script, which JavaScript spells \%c{Script=%s}`, c, body, c, name)
		}
		value = name
	case name == "Script" || name == "sc":
		if unicode.Scripts[value] == nil {
			return "", fmt.Errorf(`\%c{%s}: RE2 knows no script %s`, c, body, value)
		}
	case name == "General_Category" || name == "gc":
		if unicode.Categories[value] == nil {
			return "", fmt.Errorf(`\%c{%s}: RE2 knows no general category %s`, c, body, value)
		}
	default:
		return "", fmt.Errorf(`\%c{%s}: RE2 does not support the %s property`, c, body, name)
	}
	return `\` + string(c) + "{" + value + "}", nil
}

// identityEscape reports whether Unicode mode allows c after a backslash.
func (js jsSyntax) identityEscape(c byte, inClass bool) bool {
	if strings.IndexByte(`^$\.*+?()[]{}|/`, c) >= 0 {
		return true
	}
	if inClass && c == '-' {
		return true
	}
	// v-mode classes also allow escaping their reserved punctuators.
	return js.vMode && inClass && strings.IndexByte("&-!#%,:;<=>@`~", c) >= 0
}

// unicodeEscape handles \uXXXX, \u{...} and, outside Unicode mode, a bare \u.
func (js jsSyntax) unicodeEscape(s string) (string, int, bool, error) {
	if len(s) >= 6 && isHex(s[2:6]) {
		code, _ := strconv.ParseUint(s[2:6], 16, 32)
		if code >= 0xD800 && code <= 0xDBFF && len(s) >= 12 && s[6] == '\\' && s[7] == 'u' && isHex(s[8:12]) {
			low, _ := strconv.ParseUint(s[8:12], 16, 32)
			if low >= 0xDC00 && low <= 0xDFFF {
				// A UTF-16 surrogate pair is one code point.
				combined := 0x10000 + (code-0xD800)<<10 + (low - 0xDC00)
				return fmt.Sprintf(`\x{%X}`, combined), 12, false, nil
			}
		}
		if code >= 0xD800 && code <= 0xDFFF {
			// A lone surrogate cannot occur in Go's UTF-8 strings.
			return `[^\x{0}-\x{10FFFF}]`, 6, true, nil
		}
		return `\x{` + s[2:6] + `}`, 6, false, nil
	}
	if !js.unicode {
		return "u", 2, false, nil // Annex B: \u{3} is u repeated three times
	}
	if !strings.HasPrefix(s[2:], "{") {
		return "", 0, false, errors.New(`\u needs four hex digits or {hex} in Unicode mode`)
	}
	end := strings.IndexByte(s, '}')
	if end < 0 {
		return "", 0, false, errors.New(`unterminated \u{`)
	}
	digits := s[3:end]
	if !isHex(digits) {
		return "", 0, false, fmt.Errorf(`invalid escape \u{%s}`, digits)
	}
	trimmed := strings.TrimLeft(digits, "0")
	if trimmed == "" {
		trimmed = "0"
	}
	if code, err := strconv.ParseUint(trimmed, 16, 32); err != nil || code > 0x10FFFF {
		return "", 0, false, fmt.Errorf(`escape \u{%s} is beyond U+10FFFF`, digits)
	}
	return `\x{` + trimmed + `}`, end + 1, false, nil
}

// legacyOctal reads an Annex B octal escape (up to three digits, at most 0377)
// at the start of s; \8 and \9 are the digits themselves.
func legacyOctal(s string) (string, int, bool, error) {
	if s[1] == '8' || s[1] == '9' {
		return string(s[1]), 2, false, nil
	}
	end, value := 1, 0
	for end < len(s) && end < 4 && s[end] >= '0' && s[end] <= '7' {
		next := value*8 + int(s[end]-'0')
		if next > 0377 {
			break
		}
		value = next
		end++
	}
	return fmt.Sprintf(`\x{%X}`, value), end, false, nil
}

func isASCIILetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune("0123456789abcdefABCDEF", rune(s[i])) {
			return false
		}
	}
	return s != ""
}
