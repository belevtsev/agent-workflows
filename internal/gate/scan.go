package gate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/shellwords"
)

// gitPublications returns every git commit or push in a shell command line,
// with the directory it runs in, as splitPublications reads it. That reading
// may drop a publication the flat split of flatPublications finds only when
// it is certain; otherwise the flat split's publications are added. So the
// gate fails closed: it denies at least every command the flat split denies,
// and allows one of them only when the reading shows that no git commit or
// push in it runs.
func gitPublications(command, cwd, home string) []publication {
	found, certain := splitPublications(command, cwd, home)
	if certain {
		return found
	}
	for _, flat := range flatPublications(command, cwd) {
		if !slices.Contains(found, flat) {
			found = append(found, flat)
		}
	}
	return found
}

// splitPublications returns every git commit or push in a shell command
// line, read with shellwords.Split, with the directory it runs in resolved
// from cwd through any preceding `cd DIR` or `pushd DIR`, quoted paths
// included, a wrapper's `env -C DIR` or `sudo -D DIR` and git's own -C DIR
// options, without duplicates and in command order. Each DIR may start with
// `~` or `$HOME`, which name home, or `$PWD` (expandDir). Quoted text,
// heredoc bodies and comments are never read as commands. The command inside
// a `$(...)` or backtick substitution is read on its own, from the directory
// in effect where it appears, and so is the command string of `sh -c '...'`
// and the words of `eval "..."`. A directory change inside parentheses, a
// substitution or `sh -c` ends with them, as in a subshell. A bare `cd` moves
// to home. After a directory change the hook cannot follow, a DIR it cannot
// resolve, such as `$OTHER`, or `cd` with options, `cd -`, pushd without a
// DIR or `popd`, later git commands run in the session's directory, cwd,
// marked unresolved, until an absolute `cd` or -C. Redirections are not
// words, so `cd DIR >/dev/null 2>&1` moves to DIR, and `"$(pwd)"` or
// `"$(git rev-parse --show-toplevel)"` stays in the same repository.
//
// The reading is certain when shellwords.Split is certain at every level and
// every simple command in the line, inside substitutions too, is one the gate
// trusts (trusted): a deliberately small set that cannot run text the
// reading took for data. Any other command keeps the flat split's decision.
func splitPublications(command, cwd, home string) ([]publication, bool) {
	scanner := splitCommand(command, cwd, home)
	return scanner.found, scanner.certain
}

// abandonment is one `cw task abandon` in a command line: task is the
// directory its last --task option names, as written, and dir that
// directory resolved as expandDir resolves a cd argument, from the directory
// in effect for the command; either is "" when the gate does not know it.
type abandonment struct {
	task string
	dir  string
}

// abandonments returns every `cw task abandon` in a shell command line, read
// as splitPublications reads it (cwAbandon), without duplicates and in
// command order, inside substitutions, `sh -c` strings and eval too. It
// fails closed like gitPublications: when that reading is not certain, and
// flatAbandons shows cw, task and abandon while the reading finds none, or
// fewer than flatAbandons counts, it adds one abandonment the gate cannot
// identify, such as cw run by xargs or named through a variable. A reading
// of a line that runs cw is never certain: the gate does not trust cw, whose
// `cw task verify` runs plan commands.
func abandonments(command, cwd, home string) []abandonment {
	scanner := splitCommand(command, cwd, home)
	found := scanner.abandoned
	if !scanner.certain {
		if shows, count := flatAbandons(command); shows && (len(found) == 0 || count > scanner.abandonRuns) {
			found = append(found, abandonment{})
		}
	}
	return found
}

// flatSeparators are the characters at which flatAbandons splits a command
// line: blanks, quotes, backslash, and the characters that separate commands,
// redirect, or open an expansion or assignment.
const flatSeparators = " \t\r\n'\"\\;|&()<>`$={}:"

// flatAbandons reports whether a command line, split into tokens at
// flatSeparators with leading hyphens removed, holds a token whose program
// name (programName) is cw, the token task and the token abandon, anywhere
// and in any order. So it finds cw written as a path (/usr/local/bin/cw),
// quoted, as another program's argument (xargs cw, find -exec cw,
// go run …/cmd/cw@v3.0.0), as a variable's value (C=cw, ${C:-cw}) or in a
// substitution ($(command -v cw)). It also counts the tokens abandon that
// directly follow a token task, each a `task abandon` the line may run.
func flatAbandons(command string) (bool, int) {
	cw, task, abandon, count := false, false, false, 0
	previous := ""
	for _, token := range strings.FieldsFunc(command, func(r rune) bool { return strings.ContainsRune(flatSeparators, r) }) {
		token = strings.TrimLeft(token, "-")
		cw = cw || programName(token) == "cw"
		task = task || token == "task"
		if token == "abandon" {
			abandon = true
			if previous == "task" {
				count++
			}
		}
		previous = token
	}
	return cw && task && abandon, count
}

// programName returns the base name of a program word, without the
// @VERSION that a go run package argument ends with (…/cmd/cw@v3.0.0).
func programName(word string) string {
	name, _, _ := strings.Cut(filepath.Base(word), "@")
	return name
}

// cwValueOptions are the options that take a separate value on the way to
// `cw task abandon` and in it: cw's global ones and abandon's own.
var cwValueOptions = map[string]bool{"--config-dir": true, "--home": true, "--codex-home": true, "--claude-home": true, "--state-dir": true, "--task": true, "--reason": true}

// cwAbandon reports whether words, a simple command from its program on, run
// `cw task abandon` from at: a program named cw (programName), by any path,
// whose first two operands are task and abandon, as cobra finds the command
// among options and the values of those in cwValueOptions. `--help` counts
// too: the reading of a line that runs cw is never certain, so the flat split
// would ask about it anyway. It returns the directory the last `--task DIR`
// or `--task=DIR` names, resolved from at.
func cwAbandon(words []shellwords.Word, at place, vars expansion) (abandonment, bool) {
	if len(words) == 0 || programName(words[0].Text) != "cw" {
		return abandonment{}, false
	}
	var operands []string
	var task *shellwords.Word
	written := ""
	for i := 1; i < len(words); i++ {
		text := words[i].Text
		switch name, value, attached := strings.Cut(text, "="); {
		case !strings.HasPrefix(text, "-") || text == "-":
			operands = append(operands, text)
		case name == "--task" && attached:
			// The value keeps the option word as its Raw, so it never reads
			// as a leading ~ or variable (wrapperOptions).
			task = &shellwords.Word{Text: value, Raw: words[i].Raw}
			_, written, _ = strings.Cut(words[i].Raw, "=")
		case cwValueOptions[text] && i+1 < len(words):
			i++
			if text == "--task" {
				task, written = &words[i], words[i].Raw
			}
		}
	}
	if len(operands) < 2 || operands[0] != "task" || operands[1] != "abandon" {
		return abandonment{}, false
	}
	if task == nil {
		return abandonment{}, true
	}
	next := abandonment{task: written}
	if dir, ok := expandDir(*task, at.dir, vars); ok {
		next.dir = dir // "" for a relative directory from an unknown one
	}
	return next, true
}

// approvalOption is the cw option that carries the owner's words.
const approvalOption = "--owner-approval"

// ownerApprovals returns the text of every --owner-approval a cw command in
// a shell command line passes, read as abandonments reads `cw task abandon`
// (cwApproval), without duplicates and in command order. It fails closed the
// same way: when the reading is not certain, and the line shows cw and
// owner-approval while the reading finds fewer, it adds "", an approval
// whose words the gate cannot read.
func ownerApprovals(command, cwd, home string) []string {
	scanner := splitCommand(command, cwd, home)
	found := scanner.approvals
	if !scanner.certain {
		if shows, count := flatApprovals(command); shows && (len(found) == 0 || count > scanner.approvalRuns) {
			found = append(found, "")
		}
	}
	return found
}

// flatApprovals reports whether a command line, split as flatAbandons splits
// it, holds a token whose program name is cw and the token owner-approval,
// and counts the latter.
func flatApprovals(command string) (bool, int) {
	cw, count := false, 0
	for _, token := range strings.FieldsFunc(command, func(r rune) bool { return strings.ContainsRune(flatSeparators, r) }) {
		token = strings.TrimLeft(token, "-")
		cw = cw || programName(token) == "cw"
		if token == strings.TrimLeft(approvalOption, "-") {
			count++
		}
	}
	return cw && count > 0, count
}

// cwApproval reports whether words, a simple command from its program on,
// run cw, by any path, with --owner-approval TEXT or --owner-approval=TEXT,
// and returns the last TEXT; "" when the option has no value.
func cwApproval(words []shellwords.Word) (string, bool) {
	if len(words) == 0 || programName(words[0].Text) != "cw" {
		return "", false
	}
	text, found := "", false
	for i := 1; i < len(words); i++ {
		name, value, attached := strings.Cut(words[i].Text, "=")
		switch {
		case name == approvalOption && attached:
			text, found = value, true
		case words[i].Text == approvalOption:
			text, found = "", true
			if i+1 < len(words) {
				i++
				text = words[i].Text
			}
		}
	}
	return text, found
}

// splitCommand reads a shell command line as splitPublications describes and
// returns the scanner that holds its publications, its abandonments and
// whether the reading is certain.
func splitCommand(command, cwd, home string) wordScanner {
	// A shell would end the command at the first NUL; dropping every NUL
	// instead reads at least as much of it.
	command = strings.ReplaceAll(command, "\x00", "")
	command = splitToplevel.ReplaceAllString(command, ".")
	command = splitBackquotes.ReplaceAllStringFunc(command, func(match string) string {
		if toplevelCommand.MatchString(match[1 : len(match)-1]) {
			return "."
		}
		return match
	})
	scanner := wordScanner{certain: true, session: cwd}
	scanner.scan(command, place{dir: cwd}, 0, newExpansion(home))
	return scanner
}

// splitToplevel matches a `$(...)` that names the current directory or the
// repository's top level, and toplevelCommand the command inside such a
// backtick substitution. A cd or -C into one stays in the same repository,
// so it is rewritten to `.` before the command is split.
var (
	splitToplevel   = regexp.MustCompile(`\$\(\s*(git\s+rev-parse\s+--show-toplevel|pwd)\s*\)`)
	toplevelCommand = regexp.MustCompile(`^\s*(git\s+rev-parse\s+--show-toplevel|pwd)\s*$`)
)

// splitBackquotes matches a backtick substitution without nested backticks.
var splitBackquotes = regexp.MustCompile("`[^`]*`")

// maxScriptDepth bounds how deeply `sh -c` and `eval` strings nest before the
// scanner stops reading them, so the work of splitting each level again stays
// linear in the command's length. A script nested deeper counts as a commit
// and a push from the directory in effect there.
const maxScriptDepth = 8

// wordScanner collects the publications and abandonments of a command line
// read with shellwords.Split, and whether that reading is certain.
type wordScanner struct {
	found     []publication
	abandoned []abandonment
	// abandonRuns counts the abandonments found, duplicates included.
	abandonRuns int
	// approvals holds the owner's words each cw --owner-approval passes,
	// and approvalRuns counts those options, duplicates included.
	approvals    []string
	approvalRuns int
	certain      bool
	// session is the directory the command line starts in; a publication
	// after a directory change the scanner cannot follow is checked there.
	session string
}

// place is the directory a command runs in as the scanner follows it. When
// dir is "" the scanner cannot tell: unresolved then names the directory
// change it could not follow, as written, such as `cd $OTHER`, `cd -` or the
// `$OTHER` of `git -C $OTHER`; it is "" only when the session's directory is
// unknown too.
type place struct {
	dir        string
	unresolved string
}

// scan reads one command line from at, where vars holds what the scanner
// knows of HOME and PWD, and returns the directory in effect at its top level
// afterwards. A subshell or substitution starts from the directory in effect
// where it opens and restores it when it ends.
func (s *wordScanner) scan(command string, at place, depth int, vars expansion) place {
	result := shellwords.Split(command)
	if result.Uncertain != "" {
		s.certain = false
	}
	vars = vars.after(result.Commands)
	var outer []place // the directory each open subshell or substitution started from
	for _, simple := range result.Commands {
		for range simple.Leave {
			if n := len(outer); n > 0 {
				at, outer = outer[n-1], outer[:n-1]
			}
		}
		for range simple.Enter {
			outer = append(outer, at)
		}
		if !trusted(simple) {
			s.certain = false
		}
		at = s.visit(simple.Words, at, depth, vars)
	}
	if len(outer) > 0 {
		return outer[0]
	}
	return at
}

// visit reads one simple command run from at and returns the directory in
// effect after it.
func (s *wordScanner) visit(all []shellwords.Word, at place, depth int, vars expansion) place {
	words, chdirs, run, known := programWords(all)
	if !known {
		s.certain = false
	}
	if len(words) > 0 && (words[0].Text == "cd" || words[0].Text == "pushd" || words[0].Text == "popd") {
		change := strings.Join(wordRaws(words), " ")
		if slices.ContainsFunc(all, func(word shellwords.Word) bool { return word.Text == "" }) {
			return place{unresolved: change} // `cd ""`: bash refuses it, zsh and dash stay
		}
		return chdir(words[0].Text, words[1:], at, change, vars)
	}
	runs := at
	for _, chdir := range chdirs {
		runs = move(runs, chdir, chdir.Raw, vars)
	}
	if run != nil {
		if strings.ContainsAny(run.text, "$`") {
			s.certain = false // text the shell expands before it runs it
		}
		if depth >= maxScriptDepth {
			// Too deep to read: count it as a commit and a push from here.
			s.certain = false
			s.add(runs, publication{})
			s.add(runs, publication{push: true})
			return at
		}
		inner := vars
		if run.expanded {
			// The shell here expanded $PWD in the text: to this directory,
			// not to the one the text has moved to where $PWD appears.
			inner.pwd = false
		}
		after := s.scan(run.text, runs, depth+1, inner)
		if run.sameShell && len(chdirs) == 0 {
			return after // eval runs in the current shell
		}
		return at
	}
	if text, ok := cwApproval(words); ok {
		s.approvalRuns++
		if !slices.Contains(s.approvals, text) {
			s.approvals = append(s.approvals, text)
		}
	}
	if next, ok := cwAbandon(words, runs, vars); ok {
		s.abandonRuns++
		if !slices.Contains(s.abandoned, next) {
			s.abandoned = append(s.abandoned, next)
		}
		return at
	}
	texts := wordTexts(words)
	subcommand, moves := gitInvocation(texts)
	if subcommand == 0 || (texts[subcommand] != "commit" && texts[subcommand] != "push") {
		return at
	}
	plain := runs.dir // each -C directory read as plain text, as the gate read it before expandDir
	for _, index := range moves {
		runs = move(runs, words[index], words[index].Raw, vars)
		plain = resolveDir(plain, texts[index])
	}
	next := publication{push: texts[subcommand] == "push", all: texts[subcommand] == "commit" && commitsAll(texts[subcommand+1:])}
	s.add(runs, next)
	if plain != "" && plain != runs.dir {
		// Where the two readings differ, the gate also checks the plain one,
		// so it denies at least every -C directory it denied before.
		s.add(place{dir: plain}, next)
	}
	return at
}

// stackEntry matches a cd or pushd argument that names an entry of the
// directory stack, as `+1` does in bash's pushd and in zsh's cd and pushd.
var stackEntry = regexp.MustCompile(`^[+-][0-9]+$`)

// chdir returns the directory a cd, pushd or popd command, with args after
// the program, moves to from at. It follows a directory after the options
// -L, -P (unless the directory has a `..` component, which -P resolves after
// symbolic links) and --, `pushd -n`, which stays, and a bare cd, which
// moves to HOME. OLDPWD (`cd -`), the directory stack (popd, a bare pushd,
// `+1`), any other option and more than one directory leave the directory
// unknown, named by change, the command as written.
func chdir(program string, args []shellwords.Word, at place, change string, vars expansion) place {
	unknown := place{unresolved: change}
	if program == "popd" {
		return unknown
	}
	physical, ended := false, false
	for !ended && len(args) > 0 && strings.HasPrefix(args[0].Text, "-") && args[0].Text != "-" {
		option := args[0].Text
		args = args[1:]
		switch {
		case option == "--":
			ended = true
		case program == "pushd" && option == "-n":
			return at // adds to the stack without changing directory
		case program == "cd" && strings.Trim(option[1:], "LP") == "":
			physical = option[len(option)-1] == 'P' // the last one wins
		default:
			return unknown
		}
	}
	switch {
	case len(args) == 0 && program == "cd":
		return move(at, shellwords.Word{Text: "~", Raw: "~"}, change, vars)
	case len(args) != 1:
		return unknown
	case !ended && (args[0].Text == "-" || stackEntry.MatchString(args[0].Text)):
		return unknown
	case physical && slices.Contains(strings.Split(args[0].Text, "/"), ".."):
		return unknown
	}
	return move(at, args[0], change, vars)
}

// move returns the directory a cd, pushd or -C argument moves to from at. An
// argument expandDir cannot resolve leaves the directory unknown, named by
// change: the directory change as written.
func move(at place, word shellwords.Word, change string, vars expansion) place {
	dir, ok := expandDir(word, at.dir, vars)
	switch {
	case !ok:
		return place{unresolved: change}
	case dir == "":
		return at // relative to an unknown directory
	}
	return place{dir: dir}
}

// add records next as run at: in its directory, or, after a directory change
// the scanner could not follow, in the session's directory, marked unresolved.
func (s *wordScanner) add(at place, next publication) {
	next.dir = at.dir
	if next.dir == "" && at.unresolved != "" {
		next.dir, next.unresolved = s.session, at.unresolved
	}
	if next.dir != "" && !slices.Contains(s.found, next) {
		s.found = append(s.found, next)
	}
}

// script is a command string that a simple command runs: the string of
// `sh -c`, which runs in a shell of its own, or the words of `eval`, which
// run in the current one. expanded reports that the simple command's words
// expand a parameter (expands), so the text may hold values the
// shell running the simple command expanded, such as its own $PWD.
type script struct {
	text      string
	sameShell bool
	expanded  bool
}

// programWords returns the words of a simple command, as written and with
// their quoting resolved (an empty word, such as `""`, is dropped), without
// what precedes its program: NAME=value assignments, shell prefix words,
// wrappers with their options (`env -i`, `nice -n 5`, `timeout 30`,
// `sudo -u x`) and a backslash that bypasses an alias, which it removes from
// the program's Text. It also returns, in order, the directories
// `env -C DIR` and `sudo -D DIR` move the command to, one per wrapper
// (`env -C a env -C b` runs in a/b). For a shell's -c string, and for `eval`
// with a quoted or escaped word, it returns the script to read instead of
// words; `eval` with plain words only is a prefix word. For `env -S STRING`
// it returns STRING as the script with known false: env splits it without a
// shell, so the reading is not certain.
func programWords(all []shellwords.Word) (words, chdirs []shellwords.Word, run *script, known bool) {
	quoted := -1 // the index in words of the last word whose quoting was resolved
	for _, word := range all {
		if word.Text != "" {
			if word.Text != word.Raw {
				quoted = len(words)
			}
			words = append(words, word)
		}
	}
	total, known := len(words), true
	for len(words) > 0 {
		word := strings.TrimPrefix(words[0].Text, `\`)
		_, wrapper := wrapperArgumentOptions[word]
		switch {
		case word == "eval":
			if quoted > total-len(words) {
				return nil, chdirs, &script{text: strings.Join(wordTexts(words[1:]), " "), sameShell: true, expanded: expands(words[1:])}, known
			}
			words = words[1:]
		case shellPrefixWords[word] || (strings.Contains(word, "=") && !strings.HasPrefix(word, "-")):
			words = words[1:]
		case wrapper:
			if text, ok := envString(wordTexts(words[1:])); word == "env" && ok {
				// env splits the string itself, without a shell: reading it
				// as a command line finds at least what env runs.
				return nil, chdirs, &script{text: text, expanded: expands(words[1:])}, false
			}
			var moves []shellwords.Word
			words, moves = wrapperOptions(word, words[1:])
			chdirs = append(chdirs, moves...)
			if word == "timeout" && len(words) > 0 {
				words = words[1:] // the duration
			}
		case shells[word]:
			if text, ok := shellString(wordTexts(words[1:])); ok {
				return nil, chdirs, &script{text: text, expanded: expands(words[1:])}, known
			}
			words[0].Text = word
			return words, chdirs, nil, known
		default:
			words[0].Text = word
			return words, chdirs, nil, known
		}
	}
	return words, chdirs, nil, known
}

// shellString returns the command string a shell runs with -c: its first
// operand after options of which one holds c (`-c`, `-lc`, `-ec`), up to an
// optional `--`. `-o NAME` and `-O NAME` take an argument.
func shellString(args []string) (string, bool) {
	withC := false
	for len(args) > 0 {
		option := args[0]
		if option == "--" {
			args = args[1:]
			break
		}
		if len(option) < 2 || (option[0] != '-' && option[0] != '+') {
			break
		}
		args = args[1:]
		if option[0] == '-' && option[1] != '-' && strings.ContainsRune(option[1:], 'c') {
			withC = true
		}
		if (option == "-o" || option == "+o" || option == "-O" || option == "+O") && len(args) > 0 {
			args = args[1:]
		}
	}
	if !withC || len(args) == 0 {
		return "", false
	}
	return args[0], true
}

// envString returns the command `env -S STRING [ARG...]` runs, when env's
// options, the words after it, include -S or --split-string: STRING and the
// arguments after it. A short option cluster that holds an S counts, with
// the rest of the cluster or the next word as STRING.
func envString(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		option := args[i]
		if option == "--" || option == "-" || !strings.HasPrefix(option, "-") {
			return "", false
		}
		value, rest := "", args[i+1:]
		switch at := strings.IndexByte(option, 'S'); {
		case option == "--split-string" || !strings.HasPrefix(option, "--") && at == len(option)-1:
			if len(rest) == 0 {
				return "", false
			}
			value, rest = rest[0], rest[1:]
		case strings.HasPrefix(option, "--split-string="):
			value = strings.TrimPrefix(option, "--split-string=")
		case !strings.HasPrefix(option, "--") && at > 0:
			value = option[at+1:]
		default:
			if slices.Contains(wrapperArgumentOptions["env"], option) {
				i++ // the option's argument
			}
			continue
		}
		return strings.Join(append([]string{value}, rest...), " "), true
	}
	return "", false
}

// trustedPrograms are the programs whose simple commands the gate trusts,
// with trustedArguments, and the reserved words that make up control
// structure. The set is deliberately the minimum the false positives it
// fixes need: `echo`, `printf`, `cat`, `rg`, `grep` and `jq` write or search
// text and run nothing, `cd` moves the directory the scanner follows, and
// git commit and git push are the gate's own targets. Every other command,
// however harmless, keeps the flat split's decision.
var trustedPrograms = map[string]bool{
	"echo": true, "printf": true, "cat": true, "rg": true, "grep": true, "jq": true, "cd": true,
	"git": true, "gh": true,
	"if": true, "then": true, "else": true, "fi": true, "for": true, "do": true, "done": true,
	"while": true, "case": true, "esac": true, "{": true, "}": true, "!": true,
}

// leadingKeywords are the reserved words that may come before a command's
// program.
var leadingKeywords = map[string]bool{"if": true, "then": true, "else": true, "while": true, "do": true, "{": true, "!": true}

// trusted reports whether the gate trusts a simple command: its words, as
// written, are reserved words from trustedPrograms followed by a trusted
// program with trustedArguments, and each of its redirections is trusted. A
// `NAME=value` assignment, a wrapper such as env or sudo, or a quoted or
// escaped program name is not trusted.
func trusted(simple shellwords.SimpleCommand) bool {
	for _, redirect := range simple.Redirects {
		if !trustedRedirect(redirect) {
			return false
		}
	}
	words := simple.Words
	for len(words) > 0 && leadingKeywords[words[0].Raw] {
		words = words[1:]
	}
	return len(words) == 0 || trustedPrograms[words[0].Raw] && trustedArguments(words[0].Raw, words[1:])
}

// trustedArguments reports whether a trusted program's arguments keep it
// from running anything: printf without -v, which assigns a variable; rg
// without --pre or --pre-glob, which run a program on each file; git only
// as `git commit` or `git push`; gh only as `gh pr`.
func trustedArguments(program string, args []shellwords.Word) bool {
	switch program {
	case "printf":
		return !slices.ContainsFunc(args, func(arg shellwords.Word) bool { return strings.HasPrefix(arg.Text, "-v") })
	case "rg":
		return !slices.ContainsFunc(args, func(arg shellwords.Word) bool { return strings.HasPrefix(arg.Text, "--pre") })
	case "git":
		return len(args) > 0 && (args[0].Raw == "commit" || args[0].Raw == "push")
	case "gh":
		return len(args) > 0 && args[0].Raw == "pr"
	}
	return true
}

// trustedRedirect reports whether a redirection leaves the trust in its
// command: a heredoc or here-string, which is input, or a redirection to
// /dev/null or a duplicated file descriptor (2>&1). Output to a file, and
// input from one, are not trusted.
func trustedRedirect(redirect shellwords.Redirect) bool {
	op, target := strings.TrimLeft(redirect.Op, "0123456789"), redirect.Target.Raw
	switch {
	case op == "<<" || op == "<<-" || op == "<<<":
		return true
	case target == "/dev/null":
		return true
	case op == ">&" || op == "<&":
		return target == "-" || target != "" && strings.Trim(target, "0123456789") == ""
	}
	return false
}
