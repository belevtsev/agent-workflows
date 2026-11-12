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
func gitPublications(command, cwd string) []publication {
	found, certain := splitPublications(command, cwd)
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
// options, without duplicates and in command order. Quoted text, heredoc
// bodies and comments are never read as commands. The command inside a
// `$(...)` or backtick substitution is read on its own, from the directory
// in effect where it appears, and so is the command string of `sh -c '...'`
// and the words of `eval "..."`. A directory change inside parentheses, a
// substitution or `sh -c` ends with them, as in a subshell. After a directory
// change the hook cannot follow (`cd` with options, `cd -`, `~`, `$VAR`,
// `popd`), the directory is unknown and later git commands are not checked
// until an absolute `cd` or -C. Redirections are not words, so
// `cd DIR >/dev/null 2>&1` moves to DIR, and `"$(pwd)"` or
// `"$(git rev-parse --show-toplevel)"` stays in the same repository.
//
// The reading is certain when shellwords.Split is certain at every level and
// every program in the command line is one that never runs its arguments or
// its input as shell commands (inertProgram), so text the reading took for
// data stays data.
func splitPublications(command, cwd string) ([]publication, bool) {
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
	scanner := wordScanner{certain: true}
	scanner.scan(command, cwd, 0)
	return scanner.found, scanner.certain
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

// wordScanner collects the publications of a command line read with
// shellwords.Split, and whether that reading is certain.
type wordScanner struct {
	found   []publication
	certain bool
}

// scan reads one command line from cwd and returns the directory in effect
// at its top level afterwards. A subshell or substitution starts from the
// directory in effect where it opens and restores it when it ends.
func (s *wordScanner) scan(command, cwd string, depth int) string {
	result := shellwords.Split(command)
	if result.Uncertain != "" {
		s.certain = false
	}
	dir := cwd
	var outer []string // the directory each open subshell or substitution started from
	for _, simple := range result.Commands {
		for range simple.Leave {
			if n := len(outer); n > 0 {
				dir, outer = outer[n-1], outer[:n-1]
			}
		}
		for range simple.Enter {
			outer = append(outer, dir)
		}
		dir = s.visit(simple.Words, dir, depth)
	}
	if len(outer) > 0 {
		return outer[0]
	}
	return dir
}

// visit reads one simple command run from dir and returns the directory in
// effect after it.
func (s *wordScanner) visit(all []shellwords.Word, dir string, depth int) string {
	words, chdirs, run, known := programWords(all)
	if !known || run == nil && !inertProgram(words) {
		s.certain = false
	}
	if len(words) > 0 && (words[0] == "cd" || words[0] == "pushd" || words[0] == "popd") {
		if len(words) == 2 && words[0] != "popd" && followablePath(words[1]) {
			return resolveDir(dir, words[1])
		}
		return ""
	}
	runs := dir
	for _, chdir := range chdirs {
		if !followablePath(chdir) {
			return dir
		}
		runs = resolveDir(runs, chdir)
	}
	if run != nil {
		if strings.ContainsAny(run.text, "$`") {
			s.certain = false // text the shell expands before it runs it
		}
		if depth >= maxScriptDepth {
			// Too deep to read: count it as a commit and a push from here.
			s.certain = false
			s.add(publication{dir: runs})
			s.add(publication{dir: runs, push: true})
			return dir
		}
		after := s.scan(run.text, runs, depth+1)
		if run.sameShell && len(chdirs) == 0 {
			return after // eval runs in the current shell
		}
		return dir
	}
	target, subcommand, arguments := gitInvocation(words, runs)
	if target == "" || (subcommand != "commit" && subcommand != "push") {
		return dir
	}
	s.add(publication{dir: target, push: subcommand == "push", all: subcommand == "commit" && commitsAll(arguments)})
	return dir
}

func (s *wordScanner) add(next publication) {
	if next.dir != "" && !slices.Contains(s.found, next) {
		s.found = append(s.found, next)
	}
}

// script is a command string that a simple command runs: the string of
// `sh -c`, which runs in a shell of its own, or the words of `eval`, which
// run in the current one.
type script struct {
	text      string
	sameShell bool
}

// programWords returns the words of a simple command with their quoting
// resolved (an empty word, such as `""`, is dropped), without what precedes
// its program: NAME=value assignments, shell prefix words, wrappers with
// their options (`env -i`, `nice -n 5`, `timeout 30`, `sudo -u x`) and a
// backslash that bypasses an alias. It also returns, in order, the
// directories `env -C DIR` and `sudo -D DIR` move the command to, one per
// wrapper (`env -C a env -C b` runs in a/b). For a shell's -c string, and for
// `eval` with a quoted or escaped word, it returns the script to read
// instead of words; `eval` with plain words only is a prefix word. For
// `env -S STRING` it returns STRING as the script with known false: env
// splits it without a shell, so the reading is not certain.
func programWords(all []shellwords.Word) (words, chdirs []string, run *script, known bool) {
	quoted := -1 // the index in words of the last word whose quoting was resolved
	for _, word := range all {
		if word.Text != "" {
			if word.Text != word.Raw {
				quoted = len(words)
			}
			words = append(words, word.Text)
		}
	}
	total, known := len(words), true
	for len(words) > 0 {
		word := strings.TrimPrefix(words[0], `\`)
		_, wrapper := wrapperArgumentOptions[word]
		switch {
		case word == "eval":
			if quoted > total-len(words) {
				return nil, chdirs, &script{text: strings.Join(words[1:], " "), sameShell: true}, known
			}
			words = words[1:]
		case shellPrefixWords[word] || (strings.Contains(word, "=") && !strings.HasPrefix(word, "-")):
			words = words[1:]
		case wrapper:
			if text, ok := envString(words[1:]); word == "env" && ok {
				// env splits the string itself, without a shell: reading it
				// as a command line finds at least what env runs.
				return nil, chdirs, &script{text: text}, false
			}
			var moves []string
			words, moves = wrapperOptions(word, words[1:])
			chdirs = append(chdirs, moves...)
			if word == "timeout" && len(words) > 0 {
				words = words[1:] // the duration
			}
		case shells[word]:
			if text, ok := shellString(words[1:]); ok {
				return nil, chdirs, &script{text: text}, known
			}
			words[0] = word
			return words, chdirs, nil, known
		default:
			words[0] = word
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

// inertPrograms never run their arguments or their input as shell commands.
// Shell keywords that end a compound command (`fi`, `done`, `esac`, `}`) and
// `for`, whose words are a variable and a list, are here too.
var inertPrograms = map[string]bool{
	"echo": true, "printf": true, "cat": true, "tee": true, "rg": true, "grep": true, "egrep": true, "fgrep": true,
	"head": true, "tail": true, "wc": true, "sort": true, "uniq": true, "cut": true, "tr": true, "jq": true,
	"ls": true, "mkdir": true, "touch": true, "cp": true, "mv": true, "rm": true, "ln": true, "chmod": true,
	"diff": true, "cmp": true, "basename": true, "dirname": true, "realpath": true, "readlink": true, "pwd": true,
	"date": true, "sleep": true, "test": true, "[": true, "[[": true, "true": true, "false": true, ":": true,
	"cd": true, "pushd": true, "popd": true, "export": true, "unset": true, "local": true, "declare": true,
	"readonly": true, "set": true, "read": true, "wait": true, "exit": true, "return": true,
	"for": true, "fi": true, "done": true, "esac": true, "}": true, "cw": true,
}

// inertGit are the git subcommands that never run their arguments as
// commands; the gate checks commit and push itself. inertGH are the gh
// command groups that never do.
var (
	inertGit = map[string]bool{
		"status": true, "log": true, "diff": true, "show": true, "add": true, "rm": true, "mv": true,
		"restore": true, "switch": true, "checkout": true, "branch": true, "fetch": true, "pull": true,
		"stash": true, "tag": true, "rev-parse": true, "ls-files": true, "blame": true, "reset": true,
		"init": true, "worktree": true, "remote": true, "grep": true, "shortlog": true, "describe": true,
		"rev-list": true, "merge-base": true, "cat-file": true, "ls-tree": true, "show-ref": true,
		"config": true, "commit": true, "push": true,
	}
	inertGH = map[string]bool{
		"pr": true, "issue": true, "api": true, "run": true, "repo": true, "release": true, "search": true,
		"label": true, "browse": true, "auth": true, "status": true,
	}
)

// inertProgram reports whether a command, its words without what precedes
// its program, never runs its arguments or input as shell commands. git
// qualifies only for a subcommand in inertGit and without `-c` or
// `--config-env`, which can set a command git runs.
func inertProgram(words []string) bool {
	if len(words) == 0 {
		return true // assignments or redirections only
	}
	switch filepath.Base(words[0]) {
	case "git":
		for i := 1; i < len(words); i++ {
			switch word := words[i]; {
			case word == "-c" || word == "--config-env" || strings.HasPrefix(word, "--config-env="):
				return false
			case word == "-C" || word == "--git-dir" || word == "--work-tree" || word == "--namespace" ||
				word == "--exec-path" || word == "--super-prefix":
				i++ // the option's argument
			case !strings.HasPrefix(word, "-"):
				return inertGit[word]
			}
		}
		return true
	case "gh":
		return len(words) > 1 && inertGH[words[1]]
	default:
		return inertPrograms[filepath.Base(words[0])]
	}
}
