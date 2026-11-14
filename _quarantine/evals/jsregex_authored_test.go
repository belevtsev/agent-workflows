package evals

import (
	"strings"
	"testing"
)

// authored2RegexCase is one row of the compileJS escape table. A row that must
// not compile names a substring the error must contain. A compiling row states
// whether the compiled form is approximate; strings in match and noMatch are
// checked with MatchString. An approximate row lists strings only when the
// contract states their result for that pattern.
type authored2RegexCase struct {
	req     string
	pattern string
	flags   string
	ok      bool
	errSub  string
	approx  bool
	match   []string
	noMatch []string
}

func authored2RegexCases() []authored2RegexCase {
	const bad = "does not compile"
	// Four-hex escapes are assembled at run time so the source never holds a
	// backslash directly followed by u and four hex digits (see contract v2).
	bs := "\\"
	uA := bs + "u0041"                  // A
	uC := bs + "u0043"                  // C
	pair := bs + "uD83D" + bs + "uDE00" // U+1F600 as a surrogate pair
	loneHi := bs + "uD800"              // a high surrogate with no partner
	loneLo := bs + "uDE00"              // a low surrogate with no partner
	grin := string(rune(0x1F600))
	alpha := string(rune(0x03B1)) // Greek small alpha
	grin2 := string(rune(0x1F601))
	lone := bs + "uD83D" // a high surrogate not followed by a low one
	eAcute := string(rune(0x00E9))
	eCirc := string(rune(0x00EA))
	yUml := string(rune(0x00FF))
	cyrA := string(rune(0x0430))
	cyrYa := string(rune(0x044F))
	cyrIe := string(rune(0x0435))
	ctl := func(r rune) string { return string(r) }
	rows := []authored2RegexCase{
		// E1: \uXXXX is the code point; outside Unicode mode a short \u is the letter u.
		{req: "E1", pattern: uA, ok: true, match: []string{"A", "xAx"}, noMatch: []string{"B", "u0041", ""}},
		{req: "E1", pattern: uA, flags: "u", ok: true, match: []string{"A"}, noMatch: []string{"B", "u0041"}},
		{req: "E1", pattern: "^" + uA + "$", flags: "v", ok: true, match: []string{"A"}, noMatch: []string{"a"}},
		{req: "E1", pattern: "[" + uA + "-" + uC + "]", ok: true, match: []string{"B", "A", "C"}, noMatch: []string{"D", "u", "0", "-"}},
		{req: "E1", pattern: "[" + uA + "-" + uC + "]", flags: "u", ok: true, match: []string{"B"}, noMatch: []string{"D", "u"}},
		{req: "E1", pattern: `\u12`, flags: "u", errSub: bad},
		{req: "E1", pattern: `\uZZZZ`, flags: "u", errSub: bad},
		{req: "E1", pattern: `\u12`, flags: "v", errSub: bad},
		{req: "E1", pattern: `\u12`, ok: true, match: []string{"u12", "xu12x"}, noMatch: []string{"12", "\x12", "u1"}},

		// E2: \u{...} in Unicode mode; otherwise a quantified letter u.
		{req: "E2", pattern: `\u{1F600}`, flags: "u", ok: true, match: []string{"😀", "a😀b"}, noMatch: []string{"u", "A", ""}},
		{req: "E2", pattern: `\u{41}`, flags: "u", ok: true, match: []string{"A"}, noMatch: []string{"B", "u{41}"}},
		{req: "E2", pattern: `\u{41}`, flags: "v", ok: true, match: []string{"A"}, noMatch: []string{"B"}},
		{req: "E2", pattern: `^\u{10FFFF}$`, flags: "u", ok: true, match: []string{"\U0010FFFF"}, noMatch: []string{"A"}},
		{req: "E2", pattern: `\u{110000}`, flags: "u", errSub: bad},
		{req: "E2", pattern: `\u{zz}`, flags: "u", errSub: bad},
		{req: "E2", pattern: `\u{}`, flags: "u", errSub: bad},
		{req: "E2", pattern: `\u{110000}`, flags: "v", errSub: bad},
		{req: "E2", pattern: `^\u{3}$`, ok: true, match: []string{"uuu"}, noMatch: []string{"uu", "uuuu", "\x03", "u{3}"}},

		// E3: \cX control escapes.
		{req: "E3", pattern: `\cJ`, ok: true, match: []string{"\n"}, noMatch: []string{"J", "cJ", "\t"}},
		{req: "E3", pattern: `\cj`, ok: true, match: []string{"\n"}, noMatch: []string{"j", "\t"}},
		{req: "E3", pattern: `\cI`, ok: true, match: []string{"\t"}, noMatch: []string{"I", "\n"}},
		{req: "E3", pattern: `\cJ`, flags: "u", ok: true, match: []string{"\n"}, noMatch: []string{"J"}},
		{req: "E3", pattern: `\c1`, flags: "u", errSub: bad},
		{req: "E3", pattern: `a\c`, flags: "u", errSub: bad},
		{req: "E3", pattern: `\c1`, ok: true, match: []string{`\c1`, `x\c1y`}, noMatch: []string{"c1", "\x11", "1"}},

		// E4: named backreferences and the Annex B literal \k.
		{req: "E4", pattern: `(?<name>a)\k<name>`, ok: true, approx: true},
		{req: "E4", pattern: `(?<name>a)\k<name>`, flags: "u", ok: true, approx: true},
		{req: "E4", pattern: `(?<other>a)\k<name>`, errSub: bad},
		{req: "E4", pattern: `(?<other>a)\k<name>`, flags: "u", errSub: bad},
		{req: "E4", pattern: `\k<a>`, flags: "u", errSub: bad},
		{req: "E4", pattern: `\k<a>`, ok: true, match: []string{"k<a>", "xk<a>y"}, noMatch: []string{"a", "k<a", "<a>", ""}},

		// E5: empty classes.
		{req: "E5", pattern: `[^]`, ok: true, match: []string{"\n", "a", " "}, noMatch: []string{""}},
		{req: "E5", pattern: `^[^]$`, ok: true, match: []string{"\n", "a"}, noMatch: []string{"", "ab", "\n\n"}},
		{req: "E5", pattern: `a[^]b`, ok: true, match: []string{"a\nb", "axb"}, noMatch: []string{"ab"}},
		{req: "E5", pattern: `[^]`, flags: "u", ok: true, match: []string{"\n", "a"}, noMatch: []string{""}},
		{req: "E5", pattern: `[]`, ok: true, noMatch: []string{"a", "", "\n", "[]"}},
		{req: "E5", pattern: `a[]`, ok: true, noMatch: []string{"a", "ab", "a[]"}},
		{req: "E5", pattern: `[^`, errSub: bad},
		{req: "E5", pattern: `[`, errSub: bad},

		// E6: behaviour that must survive the change.
		{req: "E6", pattern: `(?=a)b`, ok: true, approx: true},
		{req: "E6", pattern: `(?i)abc`, errSub: "inline flag"},
		{req: "E6", pattern: `(a`, errSub: bad},
		{req: "E6", pattern: `abc`, flags: "i", ok: true, match: []string{"ABC", "abc", "aBc"}, noMatch: []string{"ABD", "ab"}},
		{req: "E6", pattern: `abc`, ok: true, match: []string{"abc"}, noMatch: []string{"ABC"}},

		// E7: Unicode mode is strict about identity escapes; Annex B is not.
		{req: "E7", pattern: `a\-b`, flags: "u", errSub: bad},
		{req: "E7", pattern: `a\-b`, flags: "v", errSub: bad},
		{req: "E7", pattern: `\"`, flags: "u", errSub: bad},
		{req: "E7", pattern: `\_`, flags: "u", errSub: bad},
		{req: "E7", pattern: `\:`, flags: "u", errSub: bad},
		{req: "E7", pattern: `a\ b`, flags: "u", errSub: bad},
		{req: "E7", pattern: `\é`, flags: "u", errSub: bad},
		{req: "E7", pattern: `\é`, flags: "v", errSub: bad},
		{req: "E7", pattern: `\pL`, flags: "u", errSub: bad},
		{req: "E7", pattern: `\1`, flags: "u", errSub: bad},
		{req: "E7", pattern: `(a)\2`, flags: "u", errSub: bad},
		{req: "E7", pattern: `[\B]`, flags: "u", errSub: bad},
		{req: "E7", pattern: `[\-]`, flags: "u", ok: true, match: []string{"-"}, noMatch: []string{"a", `\`}},
		{req: "E7", pattern: `^\p{L}$`, flags: "u", ok: true, match: []string{"a", "é"}, noMatch: []string{"1", "pL", " "}},
		{req: "E7", pattern: `\/\.\*\+\?\(\)\[\]\{\}\|\^\$\\`, flags: "u", ok: true, match: []string{`/.*+?()[]{}|^$\`}, noMatch: []string{"/a"}},
		{req: "E7", pattern: `(a)\1`, flags: "u", ok: true, approx: true},
		{req: "E7", pattern: `a\-b`, ok: true, match: []string{"a-b"}, noMatch: []string{"ab", `a\-b`}},
		{req: "E7", pattern: `\_\:\"`, ok: true, match: []string{`_:"`}, noMatch: []string{"_:"}},
		{req: "E7", pattern: `a\ b`, ok: true, match: []string{"a b"}, noMatch: []string{"ab"}},
		{req: "E7", pattern: `\é`, ok: true, match: []string{"é"}, noMatch: []string{"e", `\`}},
		{req: "E7", pattern: `\pL`, ok: true, match: []string{"pL"}, noMatch: []string{"a", "L", "p"}},
		{req: "E7", pattern: `^[\B]$`, ok: true, match: []string{"B"}, noMatch: []string{"b", `\`, ""}},

		// E8: flag v reserves class syntax characters.
		{req: "E8", pattern: `[(]`, flags: "v", errSub: bad},
		{req: "E8", pattern: `[\(]`, flags: "v", ok: true, match: []string{"("}, noMatch: []string{"a", `\`}},
		{req: "E8", pattern: `[(]`, flags: "u", ok: true, match: []string{"("}, noMatch: []string{"a"}},

		// E9: a surrogate pair written as two four-hex escapes is one code point.
		{req: "E9", pattern: "^" + pair + "$", ok: true, match: []string{grin}, noMatch: []string{"a", grin + grin, ""}},
		{req: "E9", pattern: "^" + pair + "$", flags: "u", ok: true, match: []string{grin}, noMatch: []string{"a", grin + grin}},
		{req: "E9", pattern: "^[" + pair + "]$", flags: "u", ok: true, match: []string{grin}, noMatch: []string{"a", ""}},
		{req: "E9", pattern: "x" + pair + "y", ok: true, match: []string{"x" + grin + "y"}, noMatch: []string{"xy"}},

		// E10: \u{...} bounds the value, not the digit count.
		{req: "E10", pattern: bs + "u{00000041}", flags: "u", ok: true, match: []string{"A"}, noMatch: []string{"B", "u"}},
		{req: "E10", pattern: "^" + bs + "u{0000000041}$", flags: "u", ok: true, match: []string{"A"}, noMatch: []string{"AA"}},

		// E11: Annex B legacy forms without u or v.
		{req: "E11", pattern: `^[\c1]$`, ok: true, match: []string{ctl(0x11)}, noMatch: []string{"1", "c", `\`, ctl(0x01)}},
		{req: "E11", pattern: `^[\c_]$`, ok: true, match: []string{ctl(0x1F)}, noMatch: []string{"_", "c", `\`}},
		{req: "E11", pattern: `^\00$`, ok: true, match: []string{ctl(0)}, noMatch: []string{ctl(0) + "0", "0", "00"}},
		{req: "E11", pattern: `^[\1]$`, ok: true, match: []string{ctl(0x01)}, noMatch: []string{"1", `\`}},
		{req: "E11", pattern: `^\101$`, ok: true, match: []string{"A"}, noMatch: []string{"101", ctl(0x41) + "1"}},
		{req: "E11", pattern: `^\8$`, ok: true, match: []string{"8"}, noMatch: []string{`\8`, ctl(0x08)}},
		{req: "E11", pattern: `^[\8]$`, ok: true, match: []string{"8"}, noMatch: []string{`\`, ctl(0x08)}},

		// E12: only real group syntax defines a named group.
		{req: "E12", pattern: `\(?<b>\k`, ok: true, match: []string{"(?<b>k", "(<b>k", "<b>k"}, noMatch: []string{"(?<b>", "<b>"}},
		{req: "E12", pattern: `(?<a>x)\k<a>`, ok: true, approx: true},

		// E13: quantifier braces and lone brackets.
		{req: "E13", pattern: `^a{2}$`, ok: true, match: []string{"aa"}, noMatch: []string{"a", "aaa", "a{2}"}},
		{req: "E13", pattern: `^a{2}$`, flags: "u", ok: true, match: []string{"aa"}, noMatch: []string{"a", "aaa"}},
		{req: "E13", pattern: `^a{1,3}$`, ok: true, match: []string{"a", "aaa"}, noMatch: []string{"aaaa", ""}},
		{req: "E13", pattern: `a{3,2}`, errSub: bad},
		{req: "E13", pattern: `a{3,2}`, flags: "u", errSub: bad},
		{req: "E13", pattern: `a{1001}`, ok: true, approx: true},
		{req: "E13", pattern: `x{2000,3000}`, flags: "u", ok: true, approx: true},
		{req: "E13", pattern: `a{,5}`, ok: true, match: []string{"a{,5}", "xa{,5}y"}, noMatch: []string{"a", "aaaaa", "a{5}"}},
		{req: "E13", pattern: `a{`, ok: true, match: []string{"a{"}, noMatch: []string{"a", "{"}},
		{req: "E13", pattern: `a}`, ok: true, match: []string{"a}"}, noMatch: []string{"a"}},
		{req: "E13", pattern: `a]`, ok: true, match: []string{"a]"}, noMatch: []string{"a"}},
		{req: "E13", pattern: `a{`, flags: "u", errSub: bad},
		{req: "E13", pattern: `a}`, flags: "u", errSub: bad},
		{req: "E13", pattern: `a]`, flags: "u", errSub: bad},

		// E14: an escaped hyphen in a class is a member, never a range operator.
		{req: "E14", pattern: `[a\-z]`, ok: true, match: []string{"-", "a", "z"}, noMatch: []string{"m", "b"}},
		{req: "E14", pattern: `[a\-z]`, flags: "u", ok: true, match: []string{"-", "a", "z"}, noMatch: []string{"m"}},
		{req: "E14", pattern: `[+\-*]`, ok: true, match: []string{"-", "+", "*"}, noMatch: []string{",", "a"}},
		{req: "E14", pattern: `[+\-*]`, flags: "u", ok: true, match: []string{"-", "+", "*"}, noMatch: []string{","}},
		{req: "E14", pattern: `[\w\-.]`, ok: true, match: []string{"-", ".", "a"}, noMatch: []string{"/", " "}},
		{req: "E14", pattern: `[\w\-.]`, flags: "u", ok: true, match: []string{"-", ".", "a"}, noMatch: []string{"/"}},
		{req: "E14", pattern: `a\-b`, ok: true, match: []string{"a-b"}, noMatch: []string{"ab"}},

		// E15: a literal [ inside a class is a member; there are no POSIX classes.
		{req: "E15", pattern: `^[[:alpha:]]$`, ok: true, match: []string{"a]", ":]", "[]"}, noMatch: []string{"a", "b]"}},
		{req: "E15", pattern: `[[:alpha:]]`, ok: true, match: []string{"a]", ":]"}, noMatch: []string{"a", "b]"}},
		{req: "E15", pattern: `[[]`, ok: true, match: []string{"["}, noMatch: []string{"a", "]"}},

		// E16: lone surrogates in Unicode mode.
		{req: "E16", pattern: loneHi, flags: "u", ok: true, approx: true, noMatch: []string{"abc"}},
		{req: "E16", pattern: loneLo + loneLo, flags: "u", ok: true, approx: true},

		// E17: [\b] is backspace.
		{req: "E17", pattern: `[\b]`, ok: true, match: []string{ctl(0x08)}, noMatch: []string{"b", `\`}},
		{req: "E17", pattern: `[\b]`, flags: "u", ok: true, match: []string{ctl(0x08)}, noMatch: []string{"b"}},

		// E18: group counting.
		{req: "E18", pattern: `[(?<a>]\k<b>`, ok: true, match: []string{"ak<b>", "(k<b>"}, noMatch: []string{"k<b>", "xk<b>", "aa"}},
		{req: "E18", pattern: `(?<n>a)\1`, flags: "u", ok: true, approx: true, match: []string{"aa"}},
		{req: "E18", pattern: `(a)[x\1]`, ok: true, match: []string{"a" + ctl(0x01), "ax"}, noMatch: []string{"a1", "aa"}},

		// E19: hex escapes.
		{req: "E19", pattern: `\x41`, ok: true, match: []string{"A"}, noMatch: []string{"x41", "B"}},
		{req: "E19", pattern: `\x41`, flags: "u", ok: true, match: []string{"A"}, noMatch: []string{"x41"}},
		{req: "E19", pattern: `\x4`, ok: true, match: []string{"x4"}, noMatch: []string{ctl(0x04), "4"}},
		{req: "E19", pattern: `\x4`, flags: "u", errSub: bad},

		// E20: control escapes outside a class.
		{req: "E20", pattern: `\cA`, ok: true, match: []string{ctl(0x01)}, noMatch: []string{"A", "cA"}},
		{req: "E20", pattern: `\cA`, flags: "u", ok: true, match: []string{ctl(0x01)}, noMatch: []string{"A"}},
		{req: "E20", pattern: `\cZ`, ok: true, match: []string{ctl(0x1A)}, noMatch: []string{"Z"}},
		{req: "E20", pattern: `\cz`, ok: true, match: []string{ctl(0x1A)}, noMatch: []string{"z"}},

		// E21: \a and \e are identity escapes only outside Unicode mode.
		{req: "E21", pattern: `\a`, flags: "u", errSub: bad},
		{req: "E21", pattern: `\e`, flags: "u", errSub: bad},
		{req: "E21", pattern: `\a`, ok: true, match: []string{"a"}, noMatch: []string{ctl(0x07), "b"}},
		{req: "E21", pattern: `\e`, ok: true, match: []string{"e"}, noMatch: []string{ctl(0x1B), "f"}},

		// E22: \0 and the 377 limit of legacy octal.
		{req: "E22", pattern: `^\0$`, ok: true, match: []string{ctl(0)}, noMatch: []string{"0", ""}},
		{req: "E22", pattern: `^\0$`, flags: "u", ok: true, match: []string{ctl(0)}, noMatch: []string{"0", ""}},
		{req: "E22", pattern: `^\400$`, ok: true, match: []string{" 0"}, noMatch: []string{" ", ctl(0x100), "400"}},

		// E23: quantifier edges.
		{req: "E23", pattern: `^a{2,2}$`, ok: true, match: []string{"aa"}, noMatch: []string{"aaa", "a"}},
		{req: "E23", pattern: `a{5,2000}`, ok: true, approx: true},
		{req: "E23", pattern: `^a{5,2000}$`, ok: true, approx: true, match: []string{"aaaaa"}},

		// E24: \k inside a class is an error once the pattern has named groups.
		{req: "E24", pattern: `(?<a>x)[\k<a>]`, errSub: bad},
		{req: "E24", pattern: `(?<a>x)[\k<a>]`, flags: "u", errSub: bad},

		// E25: [\&] is valid only with flag v.
		{req: "E25", pattern: `[\&]`, flags: "v", ok: true, match: []string{"&"}, noMatch: []string{"a", `\`}},
		{req: "E25", pattern: `[\&]`, flags: "u", errSub: bad},

		// E26: a class escape as a range end.
		{req: "E26", pattern: `[\w-a]`, flags: "u", errSub: bad},
		{req: "E26", pattern: `[a-\d]`, flags: "u", errSub: bad},
		{req: "E26", pattern: `[\s-\w]`, flags: "u", errSub: bad},
		{req: "E26", pattern: `[\p{L}-z]`, flags: "u", errSub: bad},
		{req: "E26", pattern: `[\w-a]`, ok: true, match: []string{"-", "b", "a"}, noMatch: []string{"!"}},
		{req: "E26", pattern: `[a-\d]`, ok: true, match: []string{"-", "a", "5"}, noMatch: []string{"b"}},
		{req: "E26", pattern: `[a-z-\w]`, flags: "u", ok: true, match: []string{"-", "_", "m"}, noMatch: []string{"!"}},
		{req: "E26", pattern: `[\w-]`, flags: "u", ok: true, match: []string{"-", "a"}, noMatch: []string{"!"}},
		{req: "E26", pattern: `[-\w]`, flags: "u", ok: true, match: []string{"-", "a"}, noMatch: []string{"!"}},

		// E27: with flag v an unescaped - must form a range.
		{req: "E27", pattern: `[-]`, flags: "v", errSub: bad},
		{req: "E27", pattern: `[a-]`, flags: "v", errSub: bad},
		{req: "E27", pattern: `[-a]`, flags: "v", errSub: bad},
		{req: "E27", pattern: `[+--]`, flags: "v", errSub: bad},
		{req: "E27", pattern: `[a--b]`, flags: "v", errSub: bad}, // unsupported: set subtraction
		{req: "E27", pattern: `[a-c]`, flags: "v", ok: true, match: []string{"b"}, noMatch: []string{"d"}},
		{req: "E27", pattern: `[\-]`, flags: "v", ok: true, match: []string{"-"}, noMatch: []string{"a"}},
		{req: "E27", pattern: `[-]`, flags: "u", ok: true, match: []string{"-"}, noMatch: []string{"a"}},

		// E28: doubled punctuators in a v-mode class.
		{req: "E28", pattern: `[a!!b]`, flags: "v", errSub: bad},
		{req: "E28", pattern: `[..]`, flags: "v", errSub: bad},
		{req: "E28", pattern: `[a&&b]`, flags: "v", errSub: bad}, // unsupported: set intersection
		{req: "E28", pattern: `[a&b]`, flags: "v", ok: true, match: []string{"&", "a"}, noMatch: []string{"c"}},
		{req: "E28", pattern: `[a&&b]`, flags: "u", ok: true, match: []string{"&", "a"}, noMatch: []string{"c"}},

		// E30: property escapes with flag u.
		{req: "E30", pattern: `\p{Script=Greek}`, flags: "u", ok: true, match: []string{alpha}, noMatch: []string{"a"}},
		{req: "E30", pattern: `\p{sc=Greek}`, flags: "u", ok: true, match: []string{alpha}, noMatch: []string{"a"}},
		{req: "E30", pattern: `\P{Script=Greek}`, flags: "u", ok: true, match: []string{"a"}, noMatch: []string{alpha}},
		{req: "E30", pattern: `\p{gc=Lu}`, flags: "u", ok: true, match: []string{"A"}, noMatch: []string{"a"}},
		{req: "E30", pattern: `\p{General_Category=Lu}`, flags: "u", ok: true, match: []string{"A"}, noMatch: []string{"a"}},
		{req: "E30", pattern: `^[\p{Script=Greek}a]$`, flags: "u", ok: true, match: []string{alpha, "a"}, noMatch: []string{"b"}},
		{req: "E30", pattern: `\p{L}`, flags: "u", ok: true, match: []string{"a"}, noMatch: []string{"1"}},
		{req: "E30", pattern: `\p{Greek}`, flags: "u", errSub: bad},
		{req: "E30", pattern: `\p{^Greek}`, flags: "u", errSub: bad},
		{req: "E30", pattern: `\p{Script=}`, flags: "u", errSub: bad},
		{req: "E30", pattern: `\p{=Greek}`, flags: "u", errSub: bad},
		{req: "E30", pattern: `\p{Foo=Bar}`, flags: "u", errSub: bad},

		// E32: an open repeat beyond RE2's limit.
		{req: "E32", pattern: `^a{5,2000}$`, ok: true, approx: true, match: []string{"aaaaa", strings.Repeat("a", 6)}, noMatch: []string{"aaaa"}},
	}

	// E29: \s and \S use JavaScript's whitespace set, with and without u.
	jsSpace := []string{ctl(0x0B), ctl(0xA0), ctl(0xFEFF), ctl(0x3000), ctl(0x2028), ctl(0x1680), " "}
	zwsp := ctl(0x200B)
	for _, f := range []string{"", "u"} {
		rows = append(rows,
			authored2RegexCase{req: "E29", pattern: `^\s$`, flags: f, ok: true, match: jsSpace, noMatch: []string{"a", zwsp}},
			authored2RegexCase{req: "E29", pattern: `^\S$`, flags: f, ok: true, match: []string{zwsp, "a"}, noMatch: []string{ctl(0xA0), ctl(0x0B)}},
			authored2RegexCase{req: "E29", pattern: `^[\S]$`, flags: f, ok: true, match: []string{zwsp, "a"}, noMatch: []string{ctl(0xA0), ctl(0x0B)}},
			authored2RegexCase{req: "E29", pattern: `^[^\s]$`, flags: f, ok: true, match: []string{zwsp, "a"}, noMatch: []string{ctl(0xA0), ctl(0x0B)}},
			authored2RegexCase{req: "E29", pattern: `^[^\S]$`, flags: f, ok: true, match: []string{ctl(0xA0), ctl(0x0B)}, noMatch: []string{zwsp, "a"}},
		)
	}
	rows = append(rows, authored2RegexCase{req: "E29", pattern: `^[\sa]$`, flags: "u", ok: true, match: []string{ctl(0x1680), "a"}, noMatch: []string{"b"}})

	rows = append(rows,
		// E33: a non-ASCII class member is one character, whatever its UTF-8 length.
		authored2RegexCase{req: "E33", pattern: "[a-" + eAcute + "-z]", flags: "v", errSub: bad},
		authored2RegexCase{req: "E33", pattern: "[a-" + eAcute + `-\d]`, flags: "u", ok: true, match: []string{"-", "5", "c", eAcute}, noMatch: []string{eCirc}},
		authored2RegexCase{req: "E33", pattern: "[" + cyrA + "-" + cyrYa + `-\d]`, flags: "u", ok: true, match: []string{"-", "5", cyrIe}, noMatch: []string{"a"}},
		authored2RegexCase{req: "E33", pattern: `^[\d-` + eAcute + "-" + yUml + "]$", ok: true, match: []string{"-", "5", eAcute, yUml}, noMatch: []string{eCirc, "a"}},

		// E34: a class range bounded by a character beyond U+FFFF.
		authored2RegexCase{req: "E34", pattern: "[" + grin + "-" + grin + "]", errSub: bad},
		authored2RegexCase{req: "E34", pattern: "[" + pair + "-" + pair + "]", errSub: bad},
		authored2RegexCase{req: "E34", pattern: "[a-" + grin + "]", errSub: bad}, // unsupported
		authored2RegexCase{req: "E34", pattern: "[" + grin + "-" + grin + "]", flags: "u", ok: true, match: []string{grin}, noMatch: []string{"a"}},
		authored2RegexCase{req: "E34", pattern: "[" + pair + "-" + pair + "]", flags: "u", ok: true, match: []string{grin}, noMatch: []string{"a"}},
		authored2RegexCase{req: "E34", pattern: "[" + grin + `-\d]`, ok: true, match: []string{"-", "5"}, noMatch: []string{"a"}},

		// E35: each class starts fresh.
		authored2RegexCase{req: "E35", pattern: `^[\w][-a]$`, flags: "u", ok: true, match: []string{"a-", "aa"}, noMatch: []string{"ab", "-a"}},
		authored2RegexCase{req: "E35", pattern: `[a][-b]`, flags: "v", errSub: bad},

		// E37: a letter after a range is an ordinary member, not a class escape.
		authored2RegexCase{req: "E37", pattern: `[a-xd]`, ok: true, match: []string{"b", "d", "x"}, noMatch: []string{"-", "y"}},
		authored2RegexCase{req: "E37", pattern: `[a-xd]`, flags: "i", ok: true, match: []string{"B", "D"}, noMatch: []string{"-", "Y"}},
		authored2RegexCase{req: "E37", pattern: `^[a-zs]+$`, ok: true, match: []string{"abc", "s"}, noMatch: []string{"ab1", "-"}},
		authored2RegexCase{req: "E37", pattern: `[A-Zd]`, ok: true, match: []string{"Q", "d"}, noMatch: []string{"-", "e"}},
		authored2RegexCase{req: "E37", pattern: `[0-9S]`, ok: true, match: []string{"5", "S"}, noMatch: []string{"-", "s"}},
		authored2RegexCase{req: "E37", pattern: `[a-zw]`, ok: true, match: []string{"m", "w"}, noMatch: []string{"-"}},
		authored2RegexCase{req: "E37", pattern: `[z-ad]`, errSub: bad},

		// E38: an identity-escaped character beyond U+FFFF as a range end, no flags.
		authored2RegexCase{req: "E38", pattern: "[" + bs + grin + "-" + bs + grin2 + "]", errSub: bad},
		authored2RegexCase{req: "E38", pattern: "[" + bs + grin + "-" + bs + grin + "]", errSub: bad},

		// E39: lone surrogate escapes; inside a class they are unsupported.
		authored2RegexCase{req: "E39", pattern: "[" + lone + "x-z]", errSub: bad},
		authored2RegexCase{req: "E39", pattern: "[" + lone + "]", flags: "u", errSub: bad},
		authored2RegexCase{req: "E39", pattern: "[^" + lone + "]", errSub: bad},
		authored2RegexCase{req: "E39", pattern: "^" + lone + "$", flags: "u", ok: true, approx: true, noMatch: []string{"a"}},
	)
	return rows
}

// authored2JSSpace reports whether r is in JavaScript's \s set.
func authored2JSSpace(r rune) bool {
	switch {
	case r >= 0x09 && r <= 0x0D, r >= 0x2000 && r <= 0x200A:
		return true
	}
	switch r {
	case 0x20, 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return false
}

// TestAuthored2JSWhitespaceExhaustive covers E36.
func TestAuthored2JSWhitespaceExhaustive(t *testing.T) {
	count := 0
	for r := rune(0); r <= 0x10FFFF; r++ {
		if authored2JSSpace(r) {
			count++
		}
	}
	if count != 25 {
		t.Fatalf("authored2JSSpace holds %d code points, want 25", count)
	}
	type pat struct {
		src   string
		space bool // true: matches exactly the set; false: exactly its complement
		re    *jsRegex
	}
	pats := []*pat{{src: `^\s$`, space: true}, {src: `^\S$`}, {src: `^[\S]$`}, {src: `^[^\s]$`}}
	for _, p := range pats {
		re, err := compileJS(p.src, "u")
		if err != nil {
			t.Fatalf("compileJS(%q, \"u\"): %v", p.src, err)
		}
		p.re = re
	}
	failures := 0
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		s := string(r)
		in := authored2JSSpace(r)
		for _, p := range pats {
			want := in == p.space
			if got := p.re.MatchString(s); got != want {
				t.Errorf("%s with flag u on U+%04X: match = %v, want %v", p.src, r, got, want)
				failures++
				if failures >= 5 {
					t.Fatalf("stopping after %d mismatches", failures)
				}
			}
		}
	}
}

// TestAuthored2ValidFlags covers E31.
func TestAuthored2ValidFlags(t *testing.T) {
	for _, f := range []string{"", "gi", "u", "v", "dgimsuy", "dgimsvy"} {
		if !validFlags(f) {
			t.Errorf("validFlags(%q) = false, want true", f)
		}
	}
	for _, f := range []string{"uv", "vu", "gg", "x"} {
		if validFlags(f) {
			t.Errorf("validFlags(%q) = true, want false", f)
		}
	}
}

// TestAuthored2CompileJSEscapes covers E1-E30, E32-E35 and E37-E39.
func TestAuthored2CompileJSEscapes(t *testing.T) {
	for _, tc := range authored2RegexCases() {
		name := tc.req + "/" + tc.pattern + "/" + tc.flags
		t.Run(name, func(t *testing.T) {
			re, err := compileJS(tc.pattern, tc.flags)
			if !tc.ok {
				if err == nil {
					t.Fatalf("compileJS(%q, %q) = nil error, want an error containing %q", tc.pattern, tc.flags, tc.errSub)
				}
				if !strings.Contains(err.Error(), tc.errSub) {
					t.Fatalf("compileJS(%q, %q) error = %q, want it to contain %q", tc.pattern, tc.flags, err, tc.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("compileJS(%q, %q) error = %v, want it to compile", tc.pattern, tc.flags, err)
			}
			if re == nil || re.Regexp == nil {
				t.Fatalf("compileJS(%q, %q) returned no compiled regexp", tc.pattern, tc.flags)
			}
			if re.approximate != tc.approx {
				t.Fatalf("compileJS(%q, %q).approximate = %v, want %v", tc.pattern, tc.flags, re.approximate, tc.approx)
			}
			// Approximate rows list strings only where the contract states the
			// result (E16, E18, E23); the other approximate rows list none.
			for _, s := range tc.match {
				if !re.MatchString(s) {
					t.Errorf("compileJS(%q, %q) does not match %q, want a match", tc.pattern, tc.flags, s)
				}
			}
			for _, s := range tc.noMatch {
				if re.MatchString(s) {
					t.Errorf("compileJS(%q, %q) matches %q, want no match", tc.pattern, tc.flags, s)
				}
			}
		})
	}
}
