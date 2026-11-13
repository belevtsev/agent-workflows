package gate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/shellwords"
)

// shellPrefixWords are words that may precede a simple command without being
// its program and take no argument of their own: shell keywords that open a
// compound command and the common command prefixes.
var shellPrefixWords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "while": true, "until": true, "do": true, "{": true, "!": true, "exec": true, "builtin": true, "nohup": true, "noglob": true, "eval": true}

// wrapperArgumentOptions lists the wrappers that run their remaining words
// as a command after options of their own, with the options that take a
// separate argument. `timeout` also takes a duration before the command.
// `command -v git` stays a lookup: git without a subcommand publishes
// nothing.
var wrapperArgumentOptions = map[string][]string{
	"command": {},
	"env":     {"-u", "--unset", "-C", "--chdir", "-P"},
	"time":    {"-f", "--format", "-o", "--output"},
	"nice":    {"-n", "--adjustment"},
	"timeout": {"-s", "--signal", "-k", "--kill-after"},
	"stdbuf":  {"-i", "-o", "-e", "--input", "--output", "--error"},
	"sudo":    {"-u", "--user", "-g", "--group", "-h", "--host", "-p", "--prompt", "-U", "--other-user", "-r", "--role", "-t", "--type", "-C", "--close-from", "-D", "--chdir", "-R", "--chroot", "-T", "--command-timeout"},
}

// shells run a command string given with -c; its words follow on the same
// line, so the hook reads them as the command.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// publication is one git commit or git push in a command line.
type publication struct {
	dir  string
	push bool
	// all is a commit with -a or --all, which also commits modified tracked
	// files that are not staged.
	all bool
	// staged is what the gate can tell of the content a commit records
	// (stagedDifferences).
	staged stagedRule
	// unresolved marks a publication that runs after a directory change the
	// gate could not follow, such as `cd $OTHER` or `cd -`, and names it as
	// written; dir is then the session's directory, checked in its place.
	unresolved string
}

// flatPublications is the flat split the gate used before shellwords, kept
// as its fallback: gitPublications adds what it finds whenever the shellwords
// reading is not certain, so a command the flat split denies is never
// allowed on an uncertain reading. It returns every git commit or push in a
// shell command line,
// with the directory it runs in resolved from cwd through any preceding
// `cd DIR` or `pushd DIR`, a wrapper's `env -C DIR` or `sudo -D DIR` and
// git's own -C DIR options, without duplicates and in command order. The
// split is deliberately simple: simple commands are separated by `;`, `|`,
// `&`, newlines and parentheses, and words by whitespace with surrounding
// quotes removed, so the words of a quoted `sh -c '...'` or `eval "..."`
// string are read as the command they run. A `$(...)` or backtick
// substitution without nested parentheses or backticks stands for one word
// of the command around it, and the command inside it is read on its own
// from the directory in effect there. A directory change inside parentheses
// or a substitution ends with them, as in a subshell. After a directory
// change the hook cannot follow (`cd` with options, `cd -`, `~`, `$VAR`,
// quoted paths with spaces, `popd`), the directory is unknown and later git
// commands are not checked until an absolute `cd` or -C, since the hook
// fails open. Redirections on a cd (`cd DIR >/dev/null 2>&1`) are ignored,
// and `"$(pwd)"` or `"$(git rev-parse --show-toplevel)"` stays in the same
// repository.
func flatPublications(command, cwd string) []publication {
	// NUL is reserved for substitution markers; a command that carries one
	// could otherwise make a substitution reference itself.
	command = strings.ReplaceAll(command, "\x00", "")
	command = backtickSubstitution.ReplaceAllString(command, "$$($1)")
	command = toplevelSubstitution.ReplaceAllString(command, ".")
	var substitutions []string
	for {
		next := commandSubstitution.ReplaceAllStringFunc(command, func(match string) string {
			substitutions = append(substitutions, match[2:len(match)-1])
			return substitutionMarker(len(substitutions) - 1)
		})
		if next == command {
			break
		}
		command = next
	}
	scanner := publicationScanner{substitutions: substitutions}
	scanner.scan(command, cwd)
	return scanner.found
}

// backtickSubstitution matches a backtick substitution without nested
// backticks; it is rewritten to the equivalent `$(...)`.
var backtickSubstitution = regexp.MustCompile("`([^`]*)`")

// commandSubstitution matches a `$(...)` without nested parentheses.
var commandSubstitution = regexp.MustCompile(`\$\([^()]*\)`)

// substitutionReference matches the marker that stands for a substitution's
// word; gitPublications removes any NUL from the command first.
var substitutionReference = regexp.MustCompile("\x00([0-9]+)\x00")

func substitutionMarker(index int) string { return "\x00" + strconv.Itoa(index) + "\x00" }

// publicationScanner collects the publications of a command line whose
// substitutions are replaced by markers indexing substitutions.
type publicationScanner struct {
	substitutions []string
	found         []publication
	// touched reports that a command read so far may change git's index
	// (keepsIndex), so a later commit is not judged by the index as it
	// stands.
	touched bool
}

// scan reads command from cwd. Each substitution referenced by a simple
// command is read, in its own scope, from the directory in effect for that
// simple command before the command is.
func (s *publicationScanner) scan(command, cwd string) {
	dir := cwd
	var scopes []string
	visit := func(segment string) {
		for _, match := range substitutionReference.FindAllStringSubmatch(segment, -1) {
			if index, err := strconv.Atoi(match[1]); err == nil && index < len(s.substitutions) {
				s.scan(s.substitutions[index], dir)
			}
		}
		words, chdirs := commandWords(substitutionReference.ReplaceAllString(segment, "_"))
		defer func(keeps bool) { s.touched = s.touched || !keeps }(keepsIndex(words))
		if len(words) > 0 && (words[0] == "cd" || words[0] == "pushd" || words[0] == "popd") {
			words = withoutRedirections(words)
			if len(words) == 2 && words[0] != "popd" && followablePath(words[1]) {
				dir = resolveDir(dir, words[1])
			} else {
				dir = ""
			}
			return
		}
		target := dir
		for _, chdir := range chdirs {
			if !followablePath(chdir) {
				return
			}
			target = resolveDir(target, chdir)
		}
		subcommand, moves := gitInvocation(words)
		if subcommand == 0 || (words[subcommand] != "commit" && words[subcommand] != "push") {
			return
		}
		for _, move := range moves {
			target = resolveDir(target, words[move])
		}
		if target == "" {
			return
		}
		next := publication{dir: target, push: words[subcommand] == "push"}
		if !next.push {
			next.all, next.staged = commitsAll(words[subcommand+1:]), commitStaged(words[subcommand+1:])
			if s.touched {
				next.staged = stagedUnknown
			}
		}
		if !slices.Contains(s.found, next) {
			s.found = append(s.found, next)
		}
	}
	start := 0
	for i, r := range command {
		switch r {
		case ';', '|', '&', '\n', '(', ')', '`':
		default:
			continue
		}
		visit(command[start:i])
		start = i + 1
		switch r {
		case '(':
			scopes = append(scopes, dir)
		case ')':
			if len(scopes) > 0 {
				dir = scopes[len(scopes)-1]
				scopes = scopes[:len(scopes)-1]
			}
		}
	}
	visit(command[start:])
}

// commitsAll reports whether git commit arguments include -a or --all,
// alone or in a cluster of short options such as -am, before an option
// that takes a value.
func commitsAll(arguments []string) bool {
	for _, argument := range arguments {
		switch {
		case argument == "--":
			return false
		case argument == "--all":
			return true
		case strings.HasPrefix(argument, "--") || !strings.HasPrefix(argument, "-"):
			continue
		}
		for _, option := range argument[1:] {
			if option == 'a' {
				return true
			}
			if strings.ContainsRune("mFcCt", option) {
				break // the rest of the cluster is this option's value
			}
		}
	}
	return false
}

// toplevelSubstitution matches a command substitution that names the
// current directory or repository's top level. A cd or -C into it stays in
// the same repository, so it is rewritten to `.` before the command is
// split.
var toplevelSubstitution = regexp.MustCompile(`"?\$\(\s*(git\s+rev-parse\s+--show-toplevel|pwd)\s*\)"?`)

// withoutRedirections drops redirection words (`>FILE`, `2>FILE`, `>>FILE`,
// `<FILE`, or the operator alone followed by its target word) from a simple
// command. `&` is already a command separator, so `2>&1` and `&>FILE` arrive
// here split.
func withoutRedirections(words []string) []string {
	kept := words[:1:1]
	for i := 1; i < len(words); i++ {
		operator := strings.TrimLeft(words[i], "0123456789")
		if !strings.HasPrefix(operator, ">") && !strings.HasPrefix(operator, "<") {
			kept = append(kept, words[i])
			continue
		}
		if strings.Trim(operator, "<>") == "" {
			i++ // the operator stands alone; its target is the next word
		}
	}
	return kept
}

// followablePath reports whether a cd argument is a literal path the hook
// can resolve without a shell: not an option, not `-`, and free of `~`, `$`,
// backquotes and glob characters.
func followablePath(path string) bool {
	return path != "" && !strings.HasPrefix(path, "-") && !strings.ContainsAny(path, "~$`*?[{")
}

// expansion is what the gate knows of the variables a cd or -C argument may
// start with: HOME, which `~` also names, and PWD, the directory in effect.
type expansion struct {
	// home is HOME's value, or "" when unknown.
	home string
	// pwd reports whether $PWD is the directory in effect.
	pwd bool
	// ifs reports whether the command may change IFS, which changes how the
	// shell splits an unquoted expansion.
	ifs bool
}

// newExpansion returns what the gate knows of HOME and PWD before a command
// line runs, given HOME's value in the environment.
func newExpansion(home string) expansion {
	if !filepath.IsAbs(home) {
		home = ""
	}
	return expansion{home: home, pwd: true}
}

// after returns what the gate knows of HOME, PWD and IFS where commands run:
// a variable that any of them assigns, exports, unsets or reads (assigns) may
// have changed, so its value is unknown throughout them.
func (v expansion) after(commands []shellwords.SimpleCommand) expansion {
	for _, simple := range commands {
		if assigns(simple.Words, "HOME") {
			v.home = ""
		}
		if assigns(simple.Words, "PWD") {
			v.pwd = false
		}
		if assigns(simple.Words, "IFS") {
			v.ifs = true
		}
	}
	return v
}

// variableSetters are the builtins that set, export, unset or read a
// variable that one of their words names.
var variableSetters = map[string]bool{
	"export": true, "unset": true, "readonly": true, "declare": true, "typeset": true, "local": true,
	"read": true, "getopts": true, "printf": true, "mapfile": true, "readarray": true, "vared": true,
	"for": true, "select": true, "let": true,
}

// assigns reports whether a simple command's words assign, export, unset or
// read the variable name: a word that starts `NAME=`, `NAME+=` or `NAME[`,
// the word NAME after a builtin in variableSetters (`unset HOME`,
// `read HOME`, `printf -v HOME`, `for HOME in`), or NAME anywhere in the
// script of an eval, which runs in the same shell.
func assigns(words []shellwords.Word, name string) bool {
	if _, _, run, _ := programWords(words); run != nil && run.sameShell && strings.Contains(run.text, name) {
		return true
	}
	setter := false
	for _, word := range words {
		if rest, ok := strings.CutPrefix(word.Text, name); ok {
			if rest == "" && setter || strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, "+=") || strings.HasPrefix(rest, "[") {
				return true
			}
		}
		setter = setter || variableSetters[strings.TrimPrefix(word.Text, `\`)]
	}
	return false
}

// expandDir returns the directory a cd, pushd or -C argument names, resolved
// from dir as bash, zsh and dash expand it, and false when the gate cannot
// tell which directory that is. A leading unquoted `~`, alone or before a
// slash, names HOME, and a leading `$HOME`, `${HOME}`, `$PWD` or `${PWD}`,
// unquoted or in double quotes, names HOME or dir; the rest of the argument,
// or an argument without them, must be literal as written (literalRaw), as a
// quoted `~` or glob character is. Any other variable (`$OTHER`), another
// user's home (`~user`), a substitution, an unquoted glob, and an unquoted
// `$HOME` or `$PWD` the shell may split or glob are not resolved. A relative
// path from an unknown dir ("") stays unknown: the result is then "".
func expandDir(word shellwords.Word, dir string, vars expansion) (string, bool) {
	if word.Text == "" {
		return "", false
	}
	if word.Raw == "~" || strings.HasPrefix(word.Raw, "~/") {
		// The shell neither splits nor globs what a tilde expands to.
		return expandedPath(vars.home, word.Text[1:], literalRaw(word.Raw[1:], false, false))
	}
	quoted := strings.HasPrefix(word.Raw, `"`)
	unquoted := strings.TrimPrefix(word.Raw, `"`)
	for _, variable := range []string{"$HOME", "${HOME}", "$PWD", "${PWD}"} {
		if !strings.HasPrefix(unquoted, variable) || !strings.HasPrefix(word.Text, variable) {
			continue
		}
		if next := unquoted[len(variable):]; variable[1] != '{' && next != "" && isNameByte(next[0]) {
			return "", false // a longer name, such as $HOMEDIR
		}
		value, rest, literal := vars.home, word.Text[len(variable):], literalRaw(unquoted[len(variable):], quoted, false)
		if strings.Contains(variable, "PWD") {
			if !vars.pwd {
				return "", false
			}
			if dir == "" {
				return "", literal
			}
			value = dir
		}
		if !quoted && (vars.ifs || strings.ContainsAny(value, " \t\n*?[")) {
			return "", false
		}
		return expandedPath(value, rest, literal)
	}
	if !literalRaw(word.Raw, false, true) {
		return "", false
	}
	return resolveDir(dir, word.Text), true
}

// expandedPath returns an expanded variable's absolute value followed by the
// rest of the word, when that rest is literal.
func expandedPath(value, rest string, literal bool) (string, bool) {
	if !literal || !filepath.IsAbs(value) {
		return "", false
	}
	return filepath.Clean(value + rest), true
}

// literalRaw reports whether raw, a word or the rest of one as written,
// stands for its Word.Text and nothing else: no `$` or backquote outside
// single quotes, no glob or brace character outside quotes, no unquoted `~`
// at the start of the word or after `=` or `:`, no zsh `=` expansion at its
// start, and no backslash escape inside $'...', which Word.Text keeps as
// written. Quoted and escaped characters are literal. inDouble starts the
// scan inside double quotes; start reports whether raw begins the word.
func literalRaw(raw string, inDouble, start bool) bool {
	return scanRaw(raw, inDouble, func(i int, c, quote byte) bool {
		switch {
		case quote == 'a':
			return c != '\\'
		case c == '$' || c == '`':
			return false
		case quote == '"':
			return true
		case strings.IndexByte("*?[{", c) >= 0:
			return false
		case c == '~':
			return !(start && i == 0) && !(i > 0 && (raw[i-1] == '=' || raw[i-1] == ':'))
		case c == '=':
			return !(start && i == 0)
		}
		return true
	})
}

// expands reports whether the shell expands a parameter, or substitutes a
// command, in any of words as written: a `$` or backquote outside single
// quotes and $'...' that no backslash escapes.
func expands(words []shellwords.Word) bool {
	return slices.ContainsFunc(words, func(word shellwords.Word) bool {
		return !scanRaw(word.Raw, false, func(_ int, c, quote byte) bool { return quote == 'a' || c != '$' && c != '`' })
	})
}

// scanRaw calls active, in order, for each character of raw, a word as
// written, that is neither inside single quotes nor escaped by a backslash,
// with the quoting it is in: 0 unquoted, '"' in double quotes or 'a' in
// $'...'. Quote characters are not passed, nor the `$` that opens $'...'.
// It returns false as soon as active does.
func scanRaw(raw string, inDouble bool, active func(i int, c, quote byte) bool) bool {
	quote := byte(0)
	if inDouble {
		quote = '"'
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
			continue
		case quote == 'a' && c == '\'':
			quote = 0
			continue
		case quote == '"' && c == '"':
			quote = 0
			continue
		case quote != 'a' && c == '\\':
			i++ // escaped
			continue
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
			continue
		case quote == 0 && c == '$' && i+1 < len(raw) && raw[i+1] == '\'':
			quote = 'a'
			i++
			continue
		}
		if !active(i, c, quote) {
			return false
		}
		if quote == 'a' && c == '\\' {
			i++ // the escaped character, a quote among them, stays in $'...'
		}
	}
	return true
}

// isNameByte reports whether b may continue a shell variable name.
func isNameByte(b byte) bool {
	return b == '_' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// commandWords splits a simple command into words with surrounding quotes
// removed (a word that was only quotes is dropped), then drops what precedes
// its program: NAME=value assignments, shell prefix words, wrappers with
// their options (`env -i`, `nice -n 5`, `timeout 30`, `sudo -u x`), `sh -c`
// and a backslash that bypasses an alias. It also returns, in order, the
// directories `env -C DIR` and `sudo -D DIR` move the command to, one per
// wrapper (`env -C a env -C b` runs in a/b).
func commandWords(segment string) ([]string, []string) {
	var words, chdirs []string
	for _, word := range strings.Fields(segment) {
		if word = strings.Trim(word, `'"`); word != "" {
			words = append(words, word)
		}
	}
	for len(words) > 0 {
		word := strings.TrimPrefix(words[0], `\`)
		_, wrapper := wrapperArgumentOptions[word]
		switch {
		case shellPrefixWords[word] || (strings.Contains(word, "=") && !strings.HasPrefix(word, "-")):
			words = words[1:]
		case wrapper:
			rest, moves := wrapperOptions(word, literalWords(words[1:]))
			words = wordTexts(rest)
			chdirs = append(chdirs, wordTexts(moves)...)
			if word == "timeout" && len(words) > 0 {
				words = words[1:] // the duration
			}
		case shells[word] && len(words) > 1 && commandStringOption(words[1]):
			words = words[2:]
		default:
			words[0] = word
			return words, chdirs
		}
	}
	return words, chdirs
}

// wrapperOptions drops a wrapper's leading options, with the argument of
// each one listed in wrapperArgumentOptions, up to and including a `--`. A
// short option's argument may be attached (`-oL`, `-C/dir`) or follow a
// cluster (`-iC /dir`). It returns the remaining words and the directory
// `env -C`/`--chdir` or `sudo -D`/`--chdir` moves the command to, if any;
// as in env and sudo, the last one wins and a relative one is resolved from
// the directory the wrapper runs in. A directory attached to its option
// keeps the option word as its Raw, so it never reads as a leading `~` or
// variable.
func wrapperOptions(wrapper string, words []shellwords.Word) ([]shellwords.Word, []shellwords.Word) {
	takes := wrapperArgumentOptions[wrapper]
	chdir := map[string]string{"env": "-C", "sudo": "-D"}[wrapper]
	var chdirs []shellwords.Word
	for len(words) > 0 && strings.HasPrefix(words[0].Text, "-") && words[0].Text != "-" {
		option := words[0]
		words = words[1:]
		if option.Text == "--" {
			break
		}
		if strings.HasPrefix(option.Text, "--") {
			name, text, attached := strings.Cut(option.Text, "=")
			value := shellwords.Word{Text: text, Raw: option.Raw}
			if !attached && slices.Contains(takes, name) && len(words) > 0 {
				value, attached = words[0], true
				words = words[1:]
			}
			if name == "--chdir" && attached && chdir != "" {
				chdirs = []shellwords.Word{value}
			}
			continue
		}
		for i := 1; i < len(option.Text); i++ {
			letter := "-" + option.Text[i:i+1]
			if !slices.Contains(takes, letter) {
				continue
			}
			value := shellwords.Word{Text: option.Text[i+1:], Raw: option.Raw}
			if value.Text == "" && len(words) > 0 {
				value = words[0]
				words = words[1:]
			}
			if letter == chdir {
				chdirs = []shellwords.Word{value}
			}
			break // the rest of the cluster is this option's argument
		}
	}
	return words, chdirs
}

// literalWords returns words as shellwords.Words written without quotes.
func literalWords(words []string) []shellwords.Word {
	literal := make([]shellwords.Word, len(words))
	for i, word := range words {
		literal[i] = shellwords.Word{Text: word, Raw: word}
	}
	return literal
}

// wordRaws returns the Raw of each word.
func wordRaws(words []shellwords.Word) []string {
	raws := make([]string, len(words))
	for i, word := range words {
		raws[i] = word.Raw
	}
	return raws
}

// wordTexts returns the Text of each word.
func wordTexts(words []shellwords.Word) []string {
	texts := make([]string, len(words))
	for i, word := range words {
		texts[i] = word.Text
	}
	return texts
}

// commandStringOption reports whether a shell option cluster such as -c,
// -lc or -ec makes the shell read its command from the next argument.
func commandStringOption(word string) bool {
	return len(word) > 1 && word[0] == '-' && word[1] != '-' && strings.ContainsRune(word[1:], 'c')
}

// gitInvocation returns, when words invoke git, the index of its subcommand
// and the index of the directory each -C DIR before it names, in order; each
// moves the directory git runs in like a cd. The subcommand's index is 0
// when words do not invoke git with one. git's global options are skipped;
// the ones that take a separate argument consume it.
func gitInvocation(words []string) (int, []int) {
	if len(words) == 0 || filepath.Base(words[0]) != "git" {
		return 0, nil
	}
	var chdirs []int
	for i := 1; i < len(words); i++ {
		word := words[i]
		if !strings.HasPrefix(word, "-") {
			return i, chdirs
		}
		switch word {
		case "-C":
			if i+1 < len(words) {
				chdirs = append(chdirs, i+1)
			}
			i++
		case "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--super-prefix", "--config-env":
			i++
		}
	}
	return 0, nil
}

// resolveDir follows a cd-like path change from dir. A relative path from
// an unknown directory ("") stays unknown. In the flat split, paths the hook
// cannot resolve lexically, such as $VAR after git -C, are left to git,
// which then fails and lets the command through.
func resolveDir(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, path)
}
