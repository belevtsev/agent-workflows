package gate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
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
		if len(words) > 0 && (words[0] == "cd" || words[0] == "pushd" || words[0] == "popd") {
			words = withoutRedirections(words)
			if len(words) == 2 && words[0] != "popd" && followablePath(words[1]) {
				dir = resolveDir(dir, words[1])
			} else {
				dir = ""
			}
			return
		}
		runs := dir
		for _, chdir := range chdirs {
			if !followablePath(chdir) {
				return
			}
			runs = resolveDir(runs, chdir)
		}
		target, subcommand, arguments := gitInvocation(words, runs)
		if target == "" || (subcommand != "commit" && subcommand != "push") {
			return
		}
		next := publication{dir: target, push: subcommand == "push", all: subcommand == "commit" && commitsAll(arguments)}
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
// can resolve without a shell: not an option, not `-`, and free of `~`, `$`
// and glob characters.
func followablePath(path string) bool {
	return path != "" && !strings.HasPrefix(path, "-") && !strings.ContainsAny(path, "~$*?[{")
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
			var moves []string
			words, moves = wrapperOptions(word, words[1:])
			chdirs = append(chdirs, moves...)
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
