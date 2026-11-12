// Package shellwords splits a POSIX shell command line into its simple
// commands and their words the way a shell reads them before it expands
// anything: quotes, backslash escapes, comments, control operators,
// redirections, heredocs, subshells and command substitutions.
//
// Nothing is expanded or run, and it is not a shell parser: reserved words
// such as `if`, `do` and `{` are ordinary words, and aliases, functions and
// other programs' arguments are not followed. Malformed input never fails.
// Split reads a closed set of forms exactly as a shell does; for anything
// else, such as an unclosed quote, a heredoc delimiter that is not a plain
// name or a `case` pattern, it still returns its best reading but says why it
// may be wrong in Result.Uncertain. The work is linear in the length of the
// command, however deeply it nests.
package shellwords

import "strings"

// Word is one word of a simple command.
type Word struct {
	// Text is the word with its quoting resolved: quotes removed and
	// backslash escapes applied. Parameter expansions ($VAR, ${...}),
	// arithmetic expansions ($((...))), the escapes of a $'...' string, a
	// leading ~ and globs are kept as written; Raw tells whether they were
	// quoted. A command or process substitution keeps only its delimiters
	// (`$()`, "``", `<()`, `>()`), because the commands inside it are simple
	// commands of their own.
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

// Result is a command line split into simple commands.
type Result struct {
	// Commands are the simple commands in the order the shell runs them:
	// the commands inside a substitution come before the command whose word
	// holds it.
	Commands []SimpleCommand
	// Uncertain is empty when Split read every form in the command line as
	// a shell does. Otherwise it names the first form Split may have read
	// differently, and Commands is a best effort that can miss commands a
	// shell would run.
	Uncertain string
}

// Split splits a shell command line into simple commands. Commands are
// separated by `;`, `&`, `&&`, `|`, `||`, `|&`, newlines and parentheses,
// and words by spaces and tabs. Quoted text, heredoc bodies and comments are
// never commands; the command substitutions inside double quotes and inside
// an unquoted heredoc body are, because the shell runs them.
func Split(command string) Result {
	result, _ := split(command)
	return result
}

// split is Split that also returns its work: the bytes each step moved over,
// the bytes of each heredoc body line it compared with the delimiter, and the
// bytes of each line it searched for the closing quote of a heredoc
// delimiter. The work stays within 3*len(command).
func split(command string) (Result, int) {
	l := &lexer{src: command, cmd: newBuilder()}
	l.sink.discard = true
	for {
		if l.i >= l.limit() {
			if len(l.stops) == 0 {
				break
			}
			if l.top().kind == body {
				l.endBody()
			} else {
				l.doubt("a quote or substitution in a heredoc body runs past its delimiter")
				l.unwind()
			}
			continue
		}
		before := l.i
		l.step()
		l.work += l.i - before
	}
	l.finish()
	return Result{Commands: l.out, Uncertain: l.uncertain}, l.work
}

// kind is the kind of an open frame: a quote, an arithmetic expansion, a
// substitution, a subshell or a heredoc body the lexer is inside.
type kind byte

const (
	subshell     kind = iota + 1 // ( ... )
	substitution                 // $( ... ), <( ... ), >( ... )
	backquote                    // ` ... `
	single                       // ' ... '
	double                       // " ... "
	ansi                         // $' ... '
	arithmetic                   // $(( ... )) and (( ... ))
	body                         // a heredoc body
)

// frame is one open quote, arithmetic expansion, subshell, substitution or
// heredoc body.
type frame struct {
	kind kind
	// raw: the frame's bytes go into the word as written, because it is
	// inside an arithmetic expansion.
	raw bool
	// depth counts the parentheses open inside an arithmetic expansion, or
	// the subshells a subshell frame stands for: directly nested subshells
	// share one frame.
	depth int
	// start is where a substitution began, or a heredoc body; a body's
	// start is -1 until it begins.
	start int
	// outer is the builder a substitution or a begun heredoc body suspended.
	outer *builder
	// doc indexes lexer.docs for a heredoc body; stop is where its delimiter
	// line starts and resume where the command line goes on after it.
	doc, stop, resume int
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
	tabs      bool // <<-: leading tabs are stripped from the delimiter line
}

type lexer struct {
	src       string
	i         int
	line      int
	work      int
	out       []SimpleCommand
	uncertain string
	// frames are the open frames, innermost last.
	frames []frame
	// cmd builds the command in progress; sink takes what an unquoted
	// heredoc body would add to a word, and is never emitted.
	cmd  *builder
	sink builder
	docs []heredoc
	// unread are the heredocs whose body starts after the next newline.
	unread []int
	// stops are where the begun heredoc bodies end, innermost last: nothing
	// inside a body reads past its delimiter line.
	stops []int
	// backquotes counts the open backquote substitutions.
	backquotes int
	// eol is where the line of the last closing-quote search in a heredoc
	// delimiter ends; quoteMiss holds, for ' and ", the end of the line of
	// the last such search that failed.
	eol       int
	quoteMiss [2]int
	// scopes counts the open subshells and substitutions; active, those of
	// them the last emitted command was in; leave, those left since.
	scopes, active, leave int
}

// doubt records why the result may differ from what a shell would run,
// keeping the first reason.
func (l *lexer) doubt(reason string) {
	if l.uncertain == "" {
		l.uncertain = reason
	}
}

// limit is where reading must stop: the delimiter line of the innermost
// begun heredoc body, or the end of the command line.
func (l *lexer) limit() int {
	if n := len(l.stops); n > 0 {
		return l.stops[n-1]
	}
	return len(l.src)
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
	case arithmetic:
		l.arithmetic(top)
	case body:
		l.body()
	}
}

// command reads one byte or operator where a command, a word or an
// operator may start.
func (l *lexer) command() {
	c := l.src[l.i]
	switch c {
	case ' ', '\t':
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
			l.doubt("a (( command, which dash reads as nested subshells")
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
		switch top := l.top(); {
		case top != nil && top.kind == subshell:
			l.closeSubshell()
		case top != nil && top.kind == substitution:
			l.closeSubstitution()
		default:
			l.doubt("an unmatched )")
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
		if end := strings.IndexByte(l.src[l.i:l.limit()], '\n'); end >= 0 {
			l.i += end
		} else {
			l.i = l.limit()
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
			l.escaped(l.src[l.i+1])
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

// escaped notes a backslash before c: inside a backquote substitution,
// \` starts a nested one, which Split does not follow.
func (l *lexer) escaped(c byte) {
	if c == '`' && l.backquotes > 0 {
		l.doubt("an escaped backquote inside a backquote substitution")
	}
}

// quoteKind is the frame an opening ' or " starts.
func quoteKind(c byte) kind {
	if c == '"' {
		return double
	}
	return single
}

// dollar reads a $: a substitution, an arithmetic expansion, a $'...' or
// $"..." string where quotes allows one, or a plain $. A parameter
// expansion is plain text: ${ and } are word bytes.
func (l *lexer) dollar(quotes bool) {
	l.beginWord()
	switch next := l.peek(1); {
	case next == '(' && l.peek(2) == '(':
		l.appendText("$((")
		l.i += 3
		l.push(frame{kind: arithmetic, raw: true})
	case next == '(':
		l.openSubstitution(substitution, 2)
	case next == '\'' && quotes:
		raw := l.raw()
		if raw {
			l.appendText("$'")
		}
		l.i += 2
		l.push(frame{kind: ansi, raw: raw})
	case next == '"' && quotes && !l.raw(): // a $"..." string reads like "..."
		l.i += 2
		l.push(frame{kind: double})
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
			l.escaped(next)
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

// ansi reads a byte of a $'...' string. Its escapes stay as written, so a
// word that holds one is not the text the program receives.
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
		l.doubt("a backslash escape in a $'...' string")
		l.countLine(l.src[l.i+1])
		l.appendText(l.src[l.i : l.i+2])
		l.i += 2
	default:
		l.countLine(c)
		l.appendByte(c)
		l.i++
	}
}

// arithmetic reads a byte of an arithmetic expansion or command, which goes
// into the word as written. Quotes and substitutions nest inside it. A ) at
// depth 0 must be the first of "))"; otherwise the "((" opened nested
// subshells or a substitution holding a subshell, which Split does not
// follow.
func (l *lexer) arithmetic(f *frame) {
	c := l.src[l.i]
	switch c {
	case '(':
		f.depth++
	case ')':
		l.appendByte(c)
		l.i++
		if f.depth > 0 {
			f.depth--
			return
		}
		if l.peek(0) == ')' {
			l.appendByte(')')
			l.i++
		} else {
			l.doubt("a (( or $(( that ) closes without ))")
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

// body reads a byte of an unquoted heredoc body, whose delimiter line was
// found first: it finds the substitutions the shell runs, and nothing it
// opens reads past the delimiter.
func (l *lexer) body() {
	switch c := l.src[l.i]; c {
	case '\n':
		l.i++
		l.line++
	case '\\':
		l.countLine(l.peek(1))
		l.i = min(l.i+2, l.limit())
	case '$':
		if l.peek(1) == '(' {
			l.dollar(false)
		} else {
			l.i++
		}
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
	for _, doc := range l.unread {
		if l.docs[doc].owner != l.cmd {
			l.doubt("a heredoc's body starts inside a substitution")
		}
	}
	for k := len(l.unread) - 1; k >= 0; k-- {
		l.push(frame{kind: body, doc: l.unread[k], start: -1})
	}
	l.unread = nil
	l.beginBody()
}

// beginBody starts the heredoc body on top of the frames at the current
// position. It finds the delimiter line first; a quoted body is then
// skipped whole. A heredoc inside another heredoc body runs to the end of
// that body unread, so no byte is searched more than twice.
func (l *lexer) beginBody() {
	f := l.top()
	doc := l.docs[f.doc]
	f.start, f.outer = l.i, l.cmd
	if len(l.stops) > 0 {
		l.doubt("a heredoc inside a heredoc body")
		f.stop, f.resume = l.limit(), l.limit()
	} else {
		f.stop, f.resume = l.delimiterLine(doc)
	}
	l.stops = append(l.stops, f.stop)
	l.sink = builder{text: l.sink.text[:0], target: -1, line: -1, discard: true}
	l.cmd = &l.sink
	if doc.literal || len(l.stops) > 1 {
		l.line += strings.Count(l.src[l.i:f.stop], "\n")
		l.i = f.stop
	}
}

// delimiterLine finds the delimiter line of a heredoc body that starts at
// l.i: where it starts, and where the command line goes on after it. The
// line must equal the delimiter, after its leading tabs for <<-. Without one
// before the limit, the body runs to the limit.
func (l *lexer) delimiterLine(doc heredoc) (stop, resume int) {
	limit := l.limit()
	for at := l.i; at < limit; {
		end := strings.IndexByte(l.src[at:limit], '\n')
		if end < 0 {
			end = limit
		} else {
			end += at
		}
		line := l.src[at:end]
		l.work += len(line)
		if doc.tabs {
			line = strings.TrimLeft(line, "\t")
		}
		if line == doc.delimiter {
			return at, min(end+1, limit)
		}
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), doc.delimiter) {
			l.doubt("a heredoc body line that nearly matches its delimiter")
		}
		at = end + 1
	}
	l.doubt("a heredoc without its delimiter line")
	return limit, limit
}

// endBody gives the heredoc on top of the frames its body, moves past its
// delimiter line and starts the next heredoc body of the same line, if any.
func (l *lexer) endBody() {
	f := l.pop()
	l.stops = l.stops[:len(l.stops)-1]
	text := l.src[f.start:f.stop]
	if f.stop < f.resume && f.stop > f.start {
		// The body ends before the newline that precedes its delimiter line.
		text = l.src[f.start : f.stop-1]
	}
	if doc := l.docs[f.doc]; doc.command >= 0 {
		l.out[doc.command].Redirects[doc.index].Body = text
	} else {
		doc.owner.redirects[doc.index].Body = text
	}
	l.line += strings.Count(l.src[min(l.i, f.resume):f.resume], "\n")
	l.i = f.resume
	l.cmd = f.outer
	if top := l.top(); top != nil && top.kind == body && top.start < 0 {
		l.beginBody()
	}
}

// redirect reads a redirection operator at l.i, with the file descriptor
// number the word before it may be.
func (l *lexer) redirect() {
	isHeredoc := strings.HasPrefix(l.src[l.i:], "<<") && !strings.HasPrefix(l.src[l.i:], "<<<")
	op := operator(l.src[l.i:])
	var doc heredoc
	var target Word
	end := l.i + len(op)
	if isHeredoc {
		if l.peek(2) == '-' {
			op = "<<-"
		}
		var ok bool
		if end, doc, target, ok = l.delimiterWord(l.i + len(op)); !ok {
			l.doubt("a << without a delimiter name")
			l.beginWord()
			l.appendText("<<")
			l.i += 2
			return
		}
		doc.tabs = op == "<<-"
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
	b.redirects = append(b.redirects, Redirect{Op: fd + op, Target: target, Line: l.line})
	if isHeredoc {
		doc.owner, doc.index, doc.command = b, len(b.redirects)-1, -1
		l.docs = append(l.docs, doc)
		b.docs = append(b.docs, len(l.docs)-1)
		l.unread = append(l.unread, len(l.docs)-1)
		b.target = -1
	} else {
		b.target = len(b.redirects) - 1
	}
	l.i = end
}

// delimiterWord reads the delimiter word of a heredoc operator that ends at
// from, after blanks, and unquotes it as a shell does. The plain forms NAME,
// 'NAME', "NAME" and \NAME, with NAME a letter or _ followed by letters,
// digits and _, are certain. A delimiter that does not start like a NAME
// is not a heredoc (ok false). A quote left open on the line falls back to
// the NAME after it, the reading this package has always made.
func (l *lexer) delimiterWord(from int) (end int, doc heredoc, target Word, ok bool) {
	i := from
	for i < len(l.src) && (l.src[i] == ' ' || l.src[i] == '\t') {
		i++
	}
	start := i
	var text []byte
	ends := " \t\n;&|()<>"
	if l.backquotes > 0 {
		ends += "`" // the shell cuts a backquote substitution's text first
	}
	for i < len(l.src) && strings.IndexByte(ends, l.src[i]) < 0 {
		switch c := l.src[i]; c {
		case '\'', '"':
			doc.literal = true
			closing := l.closingQuote(c, i+1)
			if closing < 0 {
				return l.openDelimiter(start)
			}
			text = append(text, l.src[i+1:i+1+closing]...)
			i += closing + 2
		case '\\':
			doc.literal = true
			if i+1 < len(l.src) && l.src[i+1] != '\n' {
				text = append(text, l.src[i+1])
			}
			i += 2
		default:
			text = append(text, c)
			i++
		}
	}
	i = min(i, len(l.src))
	doc.delimiter = string(text)
	raw := l.src[start:i]
	if doc.delimiter == "" || !isNameStart(doc.delimiter[0]) {
		return 0, heredoc{}, Word{}, false
	}
	plain := isName(doc.delimiter) && (raw == doc.delimiter || raw == "'"+doc.delimiter+"'" ||
		raw == `"`+doc.delimiter+`"` || raw == `\`+doc.delimiter)
	if !plain {
		l.doubt("a heredoc delimiter that is not a plain name")
	}
	return i, doc, Word{Text: doc.delimiter, Raw: raw}, true
}

// closingQuote returns the offset from `from` of the next c on the same
// line, or -1. The lexer only moves forward, so it finds each line's end
// once, and a failed search remembers that end: the delimiters of one line
// never search the same bytes twice.
func (l *lexer) closingQuote(c byte, from int) int {
	slot := 0
	if c == '"' {
		slot = 1
	}
	if from < l.quoteMiss[slot] {
		return -1
	}
	if from >= l.eol {
		l.eol = len(l.src)
		if end := strings.IndexByte(l.src[from:], '\n'); end >= 0 {
			l.eol = from + end
		}
		l.work += l.eol - from
	}
	closing := strings.IndexByte(l.src[from:l.eol], c)
	if closing < 0 {
		l.quoteMiss[slot] = l.eol
	}
	return closing
}

// openDelimiter reads a delimiter whose quote stays open on its line as the
// NAME after the quote, and records the doubt.
func (l *lexer) openDelimiter(start int) (end int, doc heredoc, target Word, ok bool) {
	l.doubt("a heredoc delimiter with an unclosed quote")
	i := start + 1
	for i < len(l.src) && isNameByte(l.src[i]) {
		i++
	}
	if i == start+1 || !isNameStart(l.src[start+1]) {
		return 0, heredoc{}, Word{}, false
	}
	doc = heredoc{delimiter: l.src[start+1 : i], literal: true}
	if i < len(l.src) && (l.src[i] == '\'' || l.src[i] == '"') {
		i++
	}
	return i, doc, Word{Text: doc.delimiter, Raw: l.src[start:i]}, true
}

func isNameStart(c byte) bool { return c == '_' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' }

func isNameByte(c byte) bool { return isNameStart(c) || '0' <= c && c <= '9' }

func isName(text string) bool {
	if text == "" || !isNameStart(text[0]) {
		return false
	}
	for i := 1; i < len(text); i++ {
		if !isNameByte(text[i]) {
			return false
		}
	}
	return true
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

// openSubstitution starts a substitution whose opening delimiter is n bytes
// at l.i. The commands inside it go to a builder of their own.
func (l *lexer) openSubstitution(k kind, n int) {
	l.beginWord()
	l.push(frame{kind: k, start: l.i, outer: l.cmd})
	l.cmd = newBuilder()
	l.scopes++
	if k == backquote {
		l.backquotes++
	}
	l.i += n
}

// closeSubstitution ends the substitution on top of the frames, after its
// closing delimiter, and resumes the word around it.
func (l *lexer) closeSubstitution() {
	f := l.pop()
	l.leaveScope()
	l.cmd = f.outer
	if f.kind == backquote {
		l.backquotes--
		l.appendText("``")
		return
	}
	l.appendText(l.src[f.start:f.start+1] + "()")
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
// redirection waiting for one. An unquoted `case` makes the result
// uncertain: the ) after each of its patterns is not a separator.
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
	if word.Raw == "case" {
		l.doubt("a case command")
	}
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

// unwind closes the frame on top of the frames as if its closing delimiter
// followed.
func (l *lexer) unwind() {
	switch top := l.top(); top.kind {
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

// finish ends whatever the command line left open, innermost first: an
// unclosed substitution or subshell ends with its commands.
func (l *lexer) finish() {
	if len(l.unread) > 0 {
		l.doubt("a heredoc without a body")
	}
	if len(l.frames) > 0 {
		l.doubt("an unclosed quote, substitution or subshell")
	}
	for len(l.frames) > 0 {
		l.unwind()
	}
	l.endCommand()
}
