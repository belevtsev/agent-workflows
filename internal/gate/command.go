package gate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/shellwords"
)

// shellPrefixWords are words that may precede a simple command without being
// its program and take no argument of their own: shell keywords that open a
// compound command and the common command prefixes.
var shellPrefixWords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "while": true, "until": true, "do": true, "{": true, "!": true, "exec": true, "builtin": true, "nohup": true, "noglob": true}

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

// shells run a command string given with -c, which the hook reads as a
// command line of its own.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// publication is one git commit or git push in a command line.
type publication struct {
	dir  string
	push bool
	// all is a commit with -a or --all, which also commits modified tracked
	// files that are not staged.
	all bool
}

// gitPublications returns every git commit or push in a shell command line,
// with the directory it runs in resolved from cwd through any preceding
// `cd DIR` or `pushd DIR`, a wrapper's `env -C DIR` or `sudo -D DIR` and
// git's own -C DIR options, without duplicates and in command order. The
// command is split into simple commands and words by shellwords.Split, so
// quoted text, heredoc bodies and comments are never read as commands. The
// command inside a `$(...)` or backtick substitution is read on its own, from
// the directory in effect where it appears, and so is the command string of
// `sh -c '...'` and the words of `eval "..."`. A directory change inside
// parentheses, a substitution or `sh -c` ends with them, as in a subshell.
// After a directory change the hook cannot follow (`cd` with options,
// `cd -`, `~`, `$VAR`, `popd`), the directory is unknown and later git
// commands are not checked until an absolute `cd` or -C, since the hook
// fails open. Redirections are not words, so `cd DIR >/dev/null 2>&1` moves
// to DIR, and `"$(pwd)"` or `"$(git rev-parse --show-toplevel)"` stays in the
// same repository.
func gitPublications(command, cwd string) []publication {
	// A shell would end the command at the first NUL; dropping every NUL
	// instead reads at least as much of it.
	command = strings.ReplaceAll(command, "\x00", "")
	command = toplevelSubstitution.ReplaceAllString(command, ".")
	command = backtickSubstitution.ReplaceAllStringFunc(command, func(match string) string {
		if toplevelCommand.MatchString(match[1 : len(match)-1]) {
			return "."
		}
		return match
	})
	var scanner publicationScanner
	scanner.scan(shellwords.Split(command), cwd, 0)
	return scanner.found
}

// toplevelSubstitution matches a `$(...)` that names the current directory or
// the repository's top level, and toplevelCommand the command inside such a
// backtick substitution. A cd or -C into one stays in the same repository, so
// it is rewritten to `.` before the command is split.
var (
	toplevelSubstitution = regexp.MustCompile(`\$\(\s*(git\s+rev-parse\s+--show-toplevel|pwd)\s*\)`)
	toplevelCommand      = regexp.MustCompile(`^\s*(git\s+rev-parse\s+--show-toplevel|pwd)\s*$`)
)

// backtickSubstitution matches a backtick substitution without nested
// backticks.
var backtickSubstitution = regexp.MustCompile("`[^`]*`")

// maxScriptDepth bounds how deeply `sh -c` and `eval` strings nest before the
// scanner stops reading them, so each level's new split keeps the work
// linear in the command's length.
const maxScriptDepth = 8

// publicationScanner collects the publications of a command line.
type publicationScanner struct {
	found []publication
}

// scan reads the simple commands of one command line from cwd and returns
// the directory in effect at its top level afterwards. A subshell or
// substitution starts from the directory in effect where it opens and
// restores it when it ends.
func (s *publicationScanner) scan(commands []shellwords.SimpleCommand, cwd string, depth int) string {
	dir := cwd
	var outer []string // the directory each open subshell or substitution started from
	for _, command := range commands {
		for range command.Leave {
			if n := len(outer); n > 0 {
				dir, outer = outer[n-1], outer[:n-1]
			}
		}
		for range command.Enter {
			outer = append(outer, dir)
		}
		dir = s.visit(command.Words, dir, depth)
	}
	if len(outer) > 0 {
		return outer[0]
	}
	return dir
}

// visit reads one simple command run from dir and returns the directory in
// effect after it.
func (s *publicationScanner) visit(all []shellwords.Word, dir string, depth int) string {
	words, chdirs, script := commandWords(all)
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
	if script != nil {
		if depth < maxScriptDepth {
			after := s.scan(shellwords.Split(script.text), runs, depth+1)
			if script.sameShell && len(chdirs) == 0 {
				return after // eval runs in the current shell
			}
		}
		return dir
	}
	target, subcommand, arguments := gitInvocation(words, runs)
	if target == "" || (subcommand != "commit" && subcommand != "push") {
		return dir
	}
	next := publication{dir: target, push: subcommand == "push", all: subcommand == "commit" && commitsAll(arguments)}
	if !slices.Contains(s.found, next) {
		s.found = append(s.found, next)
	}
	return dir
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

// followablePath reports whether a cd argument is a literal path the hook
// can resolve without a shell: not an option, not `-`, and free of `~`, `$`
// and glob characters.
func followablePath(path string) bool {
	return path != "" && !strings.HasPrefix(path, "-") && !strings.ContainsAny(path, "~$*?[{")
}

// script is a command string that a simple command runs: the string of
// `sh -c`, which runs in a shell of its own, or the words of `eval`, which
// run in the current one.
type script struct {
	text      string
	sameShell bool
}

// commandWords returns the words of a simple command with their quoting
// resolved (an empty word, such as `""`, is dropped), without what precedes
// its program: NAME=value assignments, shell prefix words, wrappers with their
// options (`env -i`, `nice -n 5`, `timeout 30`, `sudo -u x`) and a backslash
// that bypasses an alias. It also returns, in order, the directories
// `env -C DIR` and `sudo -D DIR` move the command to, one per wrapper
// (`env -C a env -C b` runs in a/b). For `sh -c STRING`, and for `eval` with
// a quoted or escaped word, it returns the script to read instead of words;
// `eval` with plain words only is a prefix word.
func commandWords(all []shellwords.Word) ([]string, []string, *script) {
	var words, chdirs []string
	quoted := -1 // the index in words of the last word whose quoting was resolved
	for _, word := range all {
		if word.Text != "" {
			if word.Text != word.Raw {
				quoted = len(words)
			}
			words = append(words, word.Text)
		}
	}
	total := len(words)
	for len(words) > 0 {
		word := strings.TrimPrefix(words[0], `\`)
		_, wrapper := wrapperArgumentOptions[word]
		switch {
		case word == "eval":
			if quoted > total-len(words) {
				return nil, chdirs, &script{text: strings.Join(words[1:], " "), sameShell: true}
			}
			words = words[1:]
		case shellPrefixWords[word] || (strings.Contains(word, "=") && !strings.HasPrefix(word, "-")):
			words = words[1:]
		case wrapper:
			var moves []string
			words, moves = wrapperOptions(word, words[1:])
			chdirs = append(chdirs, moves...)
			if word == "timeout" && len(words) > 0 {
				words = words[1:] // the duration
			}
		case shells[word] && len(words) > 1 && commandStringOption(words[1]):
			if len(words) < 3 {
				return nil, chdirs, nil
			}
			return nil, chdirs, &script{text: words[2]}
		default:
			words[0] = word
			return words, chdirs, nil
		}
	}
	return words, chdirs, nil
}

// wrapperOptions drops a wrapper's leading options, with the argument of
// each one listed in wrapperArgumentOptions, up to and including a `--`. A
// short option's argument may be attached (`-oL`, `-C/dir`) or follow a
// cluster (`-iC /dir`). It returns the remaining words and the directory
// `env -C`/`--chdir` or `sudo -D`/`--chdir` moves the command to, if any;
// as in env and sudo, the last one wins and a relative one is resolved from
// the directory the wrapper runs in.
func wrapperOptions(wrapper string, words []string) ([]string, []string) {
	takes := wrapperArgumentOptions[wrapper]
	chdir := map[string]string{"env": "-C", "sudo": "-D"}[wrapper]
	var chdirs []string
	for len(words) > 0 && strings.HasPrefix(words[0], "-") && words[0] != "-" {
		option := words[0]
		words = words[1:]
		if option == "--" {
			break
		}
		if strings.HasPrefix(option, "--") {
			name, value, attached := strings.Cut(option, "=")
			if !attached && slices.Contains(takes, name) && len(words) > 0 {
				value, attached = words[0], true
				words = words[1:]
			}
			if name == "--chdir" && attached && chdir != "" {
				chdirs = []string{value}
			}
			continue
		}
		for i := 1; i < len(option); i++ {
			letter := "-" + option[i:i+1]
			if !slices.Contains(takes, letter) {
				continue
			}
			value := option[i+1:]
			if value == "" && len(words) > 0 {
				value = words[0]
				words = words[1:]
			}
			if letter == chdir {
				chdirs = []string{value}
			}
			break // the rest of the cluster is this option's argument
		}
	}
	return words, chdirs
}

// commandStringOption reports whether a shell option cluster such as -c,
// -lc or -ec makes the shell read its command from the next argument.
func commandStringOption(word string) bool {
	return len(word) > 1 && word[0] == '-' && word[1] != '-' && strings.ContainsRune(word[1:], 'c')
}

// gitInvocation returns the directory git runs in, its subcommand and the
// subcommand's arguments when words invoke git, or "" when they do not.
// git's global options are skipped; the ones that take a separate argument
// consume it, and each -C DIR moves the directory like git does.
func gitInvocation(words []string, dir string) (string, string, []string) {
	if len(words) == 0 || filepath.Base(words[0]) != "git" {
		return "", "", nil
	}
	for i := 1; i < len(words); i++ {
		word := words[i]
		if !strings.HasPrefix(word, "-") {
			return dir, word, words[i+1:]
		}
		switch word {
		case "-C":
			if i+1 < len(words) {
				dir = resolveDir(dir, words[i+1])
			}
			i++
		case "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--super-prefix", "--config-env":
			i++
		}
	}
	return "", "", nil
}

// resolveDir follows a cd-like path change from dir. A relative path from
// an unknown directory ("") stays unknown. Paths the hook cannot resolve
// lexically, such as $VAR after git -C, are left to git, which then fails
// and lets the command through.
func resolveDir(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, path)
}
