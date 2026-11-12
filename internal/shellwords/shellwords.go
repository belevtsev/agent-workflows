// Package shellwords splits a POSIX shell command line into its simple
// commands and their words the way a shell reads them before it expands
// anything: quotes, backslash escapes, comments, control operators,
// redirections, heredocs, subshells and command substitutions.
//
// Nothing is expanded or run, and it is not a shell parser: reserved words
// such as `if`, `do` and `{` are ordinary words, and aliases, functions and
// other programs' arguments are not followed. Malformed input never fails:
// an unclosed quote, substitution or heredoc runs to the end of the command.
// The work is linear in the length of the command, however deeply it nests.
package shellwords

import "strings"

// Word is one word of a simple command.
type Word struct {
	// Text is the word with its quoting resolved: quotes removed and
	// backslash escapes applied. Parameter and arithmetic expansions ($VAR,
	// ${...}, $((...))), a leading ~ and globs are kept as written; Raw tells
	// whether they were quoted. A command or process substitution keeps only
	// its delimiters (`$()`, "``", `<()`, `>()`), because the commands inside
	// it are simple commands of their own.
	Text string
	// Raw is the word as written in the command.
	Raw string
}

// Redirect is one redirection of a simple command.
type Redirect struct {
	// Op is the operator with any file descriptor number written before it:
	// ">", "2>", ">>", "&>", ">&", "<", "<>", "<<<", or "<<" and "<<-" for
	// a heredoc.
	Op string
	// Target is the word after the operator, empty when none follows. For a
	// heredoc it is the delimiter, with Text free of quotes.
	Target Word
	// Line is the line the operator is on, counted from 0.
	Line int
	// Body is a heredoc's body: the lines after the end of the operator's
	// line up to the delimiter line, or to the end of the command when no
	// delimiter line follows. It is empty for other redirections.
	Body string
}

// SimpleCommand is one simple command: a program and its arguments as the
// shell would run them, with the redirections that apply to it.
type SimpleCommand struct {
	// Words are the command's words in order, without its redirections. An
	// assignment such as `NAME=value` before the program is a word.
	Words []Word
	// Redirects are the command's redirections in order.
	Redirects []Redirect
	// Line and EndLine are the first and last lines of the command's text,
	// counted from 0 over the whole command line, heredoc bodies included.
	Line, EndLine int
	// Leave and Enter place the command among subshells, `( ... )`, and
	// command or process substitutions, which run in a shell of their own.
	// From the command before it in the list, the shell leaves Leave of the
	// subshells or substitutions that held that command, innermost first,
	// and then enters Enter new ones that hold this one. A reader that pops
	// Leave entries off a stack and then pushes Enter has the innermost one
	// holding the command on top.
	Leave, Enter int
}

// Split returns the simple commands of a shell command line in the order
// the shell runs them: the commands inside a substitution come before the
// command whose word holds it. Commands are separated by `;`, `&`, `&&`,
// `|`, `||`, `|&`, newlines and parentheses. Quoted text, heredoc bodies and
// comments are never commands; the command substitutions inside double quotes
// and inside an unquoted heredoc body are, because the shell runs them.
func Split(command string) []SimpleCommand {
	commands, _ := split(command)
	return commands
}

// split is Split that also returns its work: the bytes each step moved over,
// in either direction, plus the bytes of each heredoc body line it compared
// with the delimiter. The work stays within 2*len(command).
func split(command string) ([]SimpleCommand, int) {
	l := &lexer{src: command, cmd: newBuilder()}
	l.sink.discard = true
	for l.i < len(l.src) {
		before := l.i
		l.step()
		l.work += max(l.i-before, before-l.i) // a step back would be read twice
	}
	l.finish()
	return l.out, l.work
}

// kind is the kind of an open frame: a quote, an expansion, a substitution,
// a subshell or a heredoc body the lexer is inside.
type kind byte

const (
	subshell     kind = iota + 1 // ( ... )
	substitution                 // $( ... ), <( ... ), >( ... )
	backquote                    // ` ... `
	single                       // ' ... '
	double                       // " ... "
	ansi                         // $' ... '
	arithmetic                   // $(( ... )) and (( ... ))
	parameter                    // ${ ... }
	body                         // a heredoc body
)

// frame is one open quote, expansion, subshell, substitution or heredoc
// body.
type frame struct {
	kind kind
	// raw: the frame's bytes go into the word as written, because it is
	// inside a parameter or arithmetic expansion.
	raw bool
	// depth counts the parentheses (arithmetic) or braces (parameter) open
	// inside the frame, or the subshells it stands for: directly nested
	// subshells share one frame.
	depth int
	// start is where a substitution began.
	start int
	// outer is the builder a substitution or a started heredoc body
	// suspended.
	outer *builder
	// doc indexes lexer.docs for a heredoc body.
	doc int
	// begun is where a heredoc body begins, -1 until it does; lineStart
	// marks the start of a body line, where the delimiter may stand.
	begun     int
	lineStart bool
}

// builder collects one context's simple command: the top level's, or a
// substitution's.
type builder struct {
	words     []Word
	redirects []Redirect
	docs      []int // the heredocs of the command, as indexes into lexer.docs
	text      []byte
	inWord    bool
	start     int // where the current word began
	target    int // the redirection waiting for its target word, or -1
	line, end int // the command's first and last line, line -1 before any
	discard   bool
}

func newBuilder() *builder { return &builder{target: -1, line: -1} }

// heredoc is one heredoc operator whose body is due or read.
type heredoc struct {
	owner     *builder
	index     int // in the owner's redirects
	command   int // the owner's index in lexer.out once emitted, else -1
	delimiter string
	literal   bool // the delimiter was quoted: no substitution in the body
}

type lexer struct {
	src  string
	i    int
	line int
	work int
	out  []SimpleCommand
	// frames are the open frames, innermost last.
	frames []frame
	// cmd builds the command in progress; sink takes what an unquoted
	// heredoc body would add to a word, and is never emitted.
	cmd  *builder
	sink builder
	docs []heredoc
	// unread are the heredocs whose body starts after the next newline.
	unread []int
	// scopes counts the open subshells and substitutions; active, those of
	// them the last emitted command was in; leave, those left since.
	scopes, active, leave int
}

func (l *lexer) top() *frame {
	if len(l.frames) == 0 {
		return nil
	}
	return &l.frames[len(l.frames)-1]
}

func (l *lexer) push(f frame) { l.frames = append(l.frames, f) }

func (l *lexer) pop() frame {
	f := l.frames[len(l.frames)-1]
	l.frames = l.frames[:len(l.frames)-1]
	return f
}

// raw reports whether bytes go into the word as written.
func (l *lexer) raw() bool {
	top := l.top()
	return top != nil && top.raw
}

func (l *lexer) peek(offset int) byte {
	if l.i+offset < len(l.src) {
		return l.src[l.i+offset]
	}
	return 0
}

func (l *lexer) step() {
	top := l.top()
	if top == nil {
		l.command()
		return
	}
	switch top.kind {
	case subshell, substitution, backquote:
		l.command()
	case single:
		l.single()
	case double:
		l.double()
	case ansi:
		l.ansi()
	case arithmetic, parameter:
		l.expansion(top)
	case body:
		l.body(top)
	}
}

// command reads one byte or operator where a command, a word or an
// operator may start.
func (l *lexer) command() {
	c := l.src[l.i]
	switch c {
	case ' ', '\t', '\r', '\f', '\v':
		l.endWord()
		l.i++
	case '\n':
		l.endCommand()
		l.i++
		l.line++
		l.readHeredocs()
	case ';':
		l.endCommand()
		l.i++
	case '|':
		l.endCommand()
		l.i++
		if next := l.peek(0); next == '|' || next == '&' {
			l.i++
		}
	case '&':
		if l.peek(1) == '>' {
			l.redirect()
			return
		}
		l.endCommand()
		l.i++
		if l.peek(0) == '&' {
			l.i++
		}
	case '(':
		if l.peek(1) == '(' { // an arithmetic command, (( ... ))
			l.beginWord()
			l.appendText("((")
			l.i += 2
			l.push(frame{kind: arithmetic, raw: true})
			return
		}
		l.endCommand()
		l.i++
		if top := l.top(); top != nil && top.kind == subshell {
			top.depth++
		} else {
			l.push(frame{kind: subshell, depth: 1})
		}
		l.scopes++
	case ')':
		l.endCommand()
		l.i++
		if top := l.top(); top != nil && top.kind == subshell {
			l.closeSubshell()
		} else if top != nil && top.kind == substitution {
			l.closeSubstitution()
		}
	case '`':
		if top := l.top(); top != nil && top.kind == backquote {
			l.endCommand()
			l.i++
			l.closeSubstitution()
			return
		}
		l.beginWord()
		l.openSubstitution(backquote, 1)
	case '#':
		if l.cmd.inWord {
			l.appendByte(c)
			l.i++
			return
		}
		if end := strings.IndexByte(l.src[l.i:], '\n'); end >= 0 {
			l.i += end
		} else {
			l.i = len(l.src)
		}
	case '<', '>':
		if l.peek(1) == '(' { // a process substitution
			l.beginWord()
			l.openSubstitution(substitution, 2)
			return
		}
		l.redirect()
	default:
		l.wordByte(c)
	}
}

// wordByte reads a byte of a word in a command: a backslash escape, an
// opening quote, a $ or a plain byte.
func (l *lexer) wordByte(c byte) {
	switch c {
	case '\\':
		if l.peek(1) == '\n' { // a line continuation joins the lines
			l.i += 2
			l.line++
			return
		}
		l.beginWord()
		if l.i+1 < len(l.src) {
			l.appendByte(l.src[l.i+1])
			l.i += 2
			return
		}
		l.appendByte(c)
		l.i++
	case '\'', '"':
		l.beginWord()
		l.i++
		l.push(frame{kind: quoteKind(c)})
	case '$':
		l.dollar(true)
	default:
		l.beginWord()
		l.appendByte(c)
		l.i++
	}
}

// quoteKind is the frame an opening ' or " starts.
func quoteKind(c byte) kind {
	if c == '"' {
		return double
	}
	return single
}

// dollar reads a $: a substitution, an expansion, a $'...' string where
// ansiQuotes allows one, or a plain $.
func (l *lexer) dollar(ansiQuotes bool) {
	l.beginWord()
	switch next := l.peek(1); {
	case next == '(' && l.peek(2) == '(':
		l.appendText("$((")
		l.i += 3
		l.push(frame{kind: arithmetic, raw: true})
	case next == '(':
		l.openSubstitution(substitution, 2)
	case next == '{':
		l.appendText("${")
		l.i += 2
		l.push(frame{kind: parameter, raw: true})
	case next == '\'' && ansiQuotes:
		raw := l.raw()
		if raw {
			l.appendText("$'")
		}
		l.i += 2
		l.push(frame{kind: ansi, raw: raw})
	default:
		l.appendByte('$')
		l.i++
	}
}

func (l *lexer) single() {
	c := l.src[l.i]
	l.i++
	if c == '\'' {
		if l.raw() {
			l.appendByte(c)
		}
		l.pop()
		return
	}
	l.countLine(c)
	l.appendByte(c)
}

func (l *lexer) double() {
	c := l.src[l.i]
	switch c {
	case '"':
		if l.raw() {
			l.appendByte(c)
		}
		l.i++
		l.pop()
	case '\\':
		next := l.peek(1)
		switch {
		case l.i+1 >= len(l.src):
			l.appendByte(c)
			l.i++
			return
		case next == '\n' && !l.raw():
			l.line++ // a line continuation
		case strings.IndexByte("$`\"\\", next) >= 0 && !l.raw():
			l.appendByte(next)
		default:
			l.countLine(next)
			l.appendByte(c)
			l.appendByte(next)
		}
		l.i += 2
	case '$':
		l.dollar(false)
	case '`':
		l.openSubstitution(backquote, 1)
	default:
		l.countLine(c)
		l.appendByte(c)
		l.i++
	}
}

// ansiEscapes resolves the common escapes of a $'...' string; any other
// escape is kept as written.
var ansiEscapes = map[byte]byte{'n': '\n', 't': '\t', 'r': '\r', '\\': '\\', '\'': '\'', '"': '"'}

func (l *lexer) ansi() {
	c := l.src[l.i]
	switch {
	case c == '\'':
		if l.raw() {
			l.appendByte(c)
		}
		l.i++
		l.pop()
	case c == '\\' && l.i+1 < len(l.src):
		next := l.src[l.i+1]
		l.countLine(next)
		if resolved, ok := ansiEscapes[next]; ok && !l.raw() {
			l.appendByte(resolved)
		} else {
			l.appendByte(c)
			l.appendByte(next)
		}
		l.i += 2
	default:
		l.countLine(c)
		l.appendByte(c)
		l.i++
	}
}

// expansion reads a byte of a parameter or arithmetic expansion, which goes
// into the word as written. Quotes and substitutions nest inside it.
func (l *lexer) expansion(f *frame) {
	c := l.src[l.i]
	open, close := byte('{'), byte('}')
	if f.kind == arithmetic {
		open, close = '(', ')'
	}
	switch c {
	case open:
		f.depth++
	case close:
		l.appendByte(c)
		l.i++
		if f.depth > 0 {
			f.depth--
			return
		}
		if f.kind == arithmetic && l.peek(0) == ')' { // the second ) of ))
			l.appendByte(')')
			l.i++
		}
		l.pop()
		return
	case '\'', '"':
		l.appendByte(c)
		l.i++
		l.push(frame{kind: quoteKind(c), raw: true})
		return
	case '$':
		l.dollar(true)
		return
	case '`':
		l.openSubstitution(backquote, 1)
		return
	case '\\':
		if l.i+1 < len(l.src) {
			l.countLine(l.src[l.i+1])
			l.appendText(l.src[l.i : l.i+2])
			l.i += 2
			return
		}
	}
	l.countLine(c)
	l.appendByte(c)
	l.i++
}

// body reads a heredoc body: at the start of each line it looks for the
// delimiter line, and in an unquoted body it finds the substitutions.
func (l *lexer) body(f *frame) {
	doc := l.docs[f.doc]
	if f.lineStart {
		f.lineStart = false
		end := strings.IndexByte(l.src[l.i:], '\n')
		if end < 0 {
			end = len(l.src)
		} else {
			end += l.i
		}
		l.work += end - l.i // the comparison reads the line
		if strings.TrimSpace(l.src[l.i:end]) == doc.delimiter {
			text := l.src[f.begun:max(f.begun, l.i-1)] // without the newline before the delimiter line
			if end < len(l.src) {
				end++
				l.line++
			}
			l.i = end
			l.endBody(text)
			return
		}
		if doc.literal {
			l.i = end
			if end < len(l.src) {
				l.i++
				l.line++
				f.lineStart = true
			}
			return
		}
	}
	c := l.src[l.i]
	switch c {
	case '\n':
		l.i++
		l.line++
		f.lineStart = true
	case '\\':
		l.countLine(l.peek(1))
		l.i = min(l.i+2, len(l.src))
	case '$':
		l.dollar(false)
	case '`':
		l.openSubstitution(backquote, 1)
	default:
		l.i++
	}
}

// readHeredocs starts the bodies of the heredocs whose operator was on the
// line that just ended, first heredoc first.
func (l *lexer) readHeredocs() {
	if len(l.unread) == 0 {
		return
	}
	for k := len(l.unread) - 1; k >= 0; k-- {
		l.push(frame{kind: body, doc: l.unread[k], begun: -1})
	}
	l.unread = nil
	l.beginBody()
}

// beginBody starts the heredoc body on top of the frames at the current
// line.
func (l *lexer) beginBody() {
	f := l.top()
	f.begun, f.lineStart, f.outer = l.i, true, l.cmd
	l.sink = builder{text: l.sink.text[:0], target: -1, line: -1, discard: true}
	l.cmd = &l.sink
}

// endBody gives the heredoc on top of the frames its body and starts the
// next heredoc body of the same line, if any.
func (l *lexer) endBody(text string) {
	f := l.pop()
	if doc := l.docs[f.doc]; doc.command >= 0 {
		l.out[doc.command].Redirects[doc.index].Body = text
	} else {
		doc.owner.redirects[doc.index].Body = text
	}
	l.cmd = f.outer
	if top := l.top(); top != nil && top.kind == body && top.begun < 0 {
		l.beginBody()
	}
}

// redirect reads a redirection operator at l.i, with the file descriptor
// number the word before it may be. A << that no delimiter word follows is
// text.
func (l *lexer) redirect() {
	isHeredoc := strings.HasPrefix(l.src[l.i:], "<<") && !strings.HasPrefix(l.src[l.i:], "<<<")
	var end int
	var delimiter string
	var literal bool
	if isHeredoc {
		var ok bool
		if end, delimiter, literal, ok = heredocMarker(l.src, l.i); !ok {
			l.beginWord()
			l.appendText("<<")
			l.i += 2
			return
		}
	}
	b := l.cmd
	fd := ""
	if b.inWord && l.src[l.i] != '&' && isDigits(b.text) && l.src[b.start:l.i] == string(b.text) {
		fd = string(b.text)
		b.text, b.inWord = b.text[:0], false
	} else {
		l.endWord()
	}
	if b.line < 0 {
		b.line = l.line
	}
	b.end = l.line
	if isHeredoc {
		op := "<<"
		if l.peek(2) == '-' {
			op = "<<-"
		}
		raw := strings.TrimLeft(l.src[l.i+len(op):end], " \t\f\r")
		b.redirects = append(b.redirects, Redirect{Op: fd + op, Target: Word{Text: delimiter, Raw: raw}, Line: l.line})
		l.docs = append(l.docs, heredoc{owner: b, index: len(b.redirects) - 1, command: -1, delimiter: delimiter, literal: literal})
		b.docs = append(b.docs, len(l.docs)-1)
		l.unread = append(l.unread, len(l.docs)-1)
		b.target = -1
		l.i = end
		return
	}
	op := operator(l.src[l.i:])
	b.redirects = append(b.redirects, Redirect{Op: fd + op, Line: l.line})
	b.target = len(b.redirects) - 1
	l.i += len(op)
}

// operators are the redirection operators, longest first.
var operators = []string{"&>>", "&>", "<<<", "<<", "<>", "<&", "<", ">>", ">|", ">&", ">"}

// operator returns the redirection operator text starts with.
func operator(text string) string {
	for _, op := range operators {
		if strings.HasPrefix(text, op) {
			return op
		}
	}
	return text[:1]
}

func isDigits(text []byte) bool {
	for _, c := range text {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(text) > 0
}

// heredocMarker matches a heredoc marker at text[at:], which starts with
// "<<": an optional "-", blanks, an optional quote or backslash, the
// delimiter word and an optional quote, the shape of
// <<-?[ \t]*(['"\\]?)([A-Za-z_][A-Za-z0-9_]*)['"]?. It reads only the
// marker's own bytes, so finding every marker of a line stays linear, and
// reports whether the delimiter was quoted.
func heredocMarker(text string, at int) (end int, delimiter string, literal, ok bool) {
	i := at + 2
	if i < len(text) && text[i] == '-' {
		i++
	}
	for i < len(text) && strings.IndexByte(" \t\f\r", text[i]) >= 0 {
		i++
	}
	if i < len(text) && (text[i] == '\'' || text[i] == '"' || text[i] == '\\') {
		literal = true
		i++
	}
	start := i
	for i < len(text) && isDelimiterByte(text[i], i == start) {
		i++
	}
	if i == start {
		return 0, "", false, false
	}
	delimiter = text[start:i]
	if i < len(text) && (text[i] == '\'' || text[i] == '"') {
		i++
	}
	return i, delimiter, literal, true
}

// isDelimiterByte reports whether ch can be part of a heredoc delimiter word
// ([A-Za-z_] first, then [A-Za-z0-9_]).
func isDelimiterByte(ch byte, first bool) bool {
	return ch == '_' || 'A' <= ch && ch <= 'Z' || 'a' <= ch && ch <= 'z' || !first && '0' <= ch && ch <= '9'
}

// openSubstitution starts a substitution whose opening delimiter is n bytes
// at l.i. The commands inside it go to a builder of their own.
func (l *lexer) openSubstitution(k kind, n int) {
	l.beginWord()
	l.push(frame{kind: k, start: l.i, outer: l.cmd})
	l.cmd = newBuilder()
	l.scopes++
	l.i += n
}

// closeSubstitution ends the substitution on top of the frames, after its
// closing delimiter, and resumes the word around it.
func (l *lexer) closeSubstitution() {
	f := l.pop()
	l.leaveScope()
	l.cmd = f.outer
	l.appendText(substitutionText(l.src[f.start:min(f.start+2, len(l.src))], f.kind))
}

// substitutionText is what a substitution that opens with opening leaves in
// its word's Text: its delimiters.
func substitutionText(opening string, k kind) string {
	if k == backquote {
		return "``"
	}
	return opening[:1] + "()"
}

// closeSubshell ends the innermost subshell, on top of the frames.
func (l *lexer) closeSubshell() {
	top := l.top()
	if top.depth--; top.depth == 0 {
		l.pop()
	}
	l.leaveScope()
}

// leaveScope closes the innermost subshell or substitution.
func (l *lexer) leaveScope() {
	if l.active == l.scopes {
		l.leave++
		l.active--
	}
	l.scopes--
}

func (l *lexer) countLine(c byte) {
	if c == '\n' {
		l.line++
	}
}

func (l *lexer) beginWord() {
	b := l.cmd
	if b.inWord {
		return
	}
	b.inWord, b.start = true, l.i
	if b.line < 0 {
		b.line = l.line
	}
}

func (l *lexer) appendByte(c byte) {
	if !l.cmd.discard {
		l.cmd.text = append(l.cmd.text, c)
	}
}

func (l *lexer) appendText(text string) {
	if !l.cmd.discard {
		l.cmd.text = append(l.cmd.text, text...)
	}
}

// endWord ends the word in progress, as a word or as the target of the
// redirection waiting for one.
func (l *lexer) endWord() {
	b := l.cmd
	if !b.inWord {
		return
	}
	b.inWord, b.end = false, l.line
	if b.discard {
		b.text = b.text[:0]
		return
	}
	word := Word{Text: string(b.text), Raw: l.src[b.start:l.i]}
	b.text = b.text[:0]
	if b.target >= 0 {
		b.redirects[b.target].Target = word
		b.target = -1
		return
	}
	b.words = append(b.words, word)
}

// endCommand ends the simple command in progress and adds it to the result
// unless it is empty.
func (l *lexer) endCommand() {
	l.endWord()
	b := l.cmd
	b.target = -1
	if b.discard || len(b.words) == 0 && len(b.redirects) == 0 {
		b.line = -1
		return
	}
	for _, doc := range b.docs {
		l.docs[doc].command = len(l.out)
	}
	l.out = append(l.out, SimpleCommand{Words: b.words, Redirects: b.redirects, Line: b.line, EndLine: b.end, Leave: l.leave, Enter: l.scopes - l.active})
	l.active, l.leave = l.scopes, 0
	// The emitted command keeps these arrays; the next one gets new ones.
	b.words, b.redirects, b.docs, b.line = nil, nil, nil, -1
}

// finish ends whatever the command line left open, innermost first: an
// unclosed heredoc body runs to the end, and an unclosed substitution or
// subshell ends with its commands.
func (l *lexer) finish() {
	for len(l.frames) > 0 {
		switch top := l.top(); top.kind {
		case body:
			if top.begun < 0 {
				l.pop()
				continue
			}
			l.endBody(l.src[top.begun:])
		case substitution, backquote:
			l.endCommand()
			l.closeSubstitution()
		case subshell:
			l.endCommand()
			l.closeSubshell()
		default:
			l.pop()
		}
	}
	l.endCommand()
}
