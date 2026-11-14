// Package shellwords reads a shell command line as simple commands of words,
// and says whether a call in it skips git's hooks or turns the gate of cw
// off. It runs nothing.
package shellwords

import (
	"slices"
	"strings"
)

// Split reads line as simple commands of words: quotes and backslashes are
// undone; ; & | ( ) ` and newline end a command. A double-quoted text that
// holds $ or ` is also read as commands. It returns false at an open quote.
func Split(line string) (cmds [][]string, ok bool) {
	words, word, started := []string(nil), []byte(nil), false // started: perhaps an empty quoted word
	flush := func(cut bool) {
		if started {
			words, word, started = append(words, string(word)), nil, false
		}
		if cut && len(words) > 0 {
			cmds, words = append(cmds, words), nil
		}
	}
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line):
			if i++; line[i] != '\n' {
				word, started = append(word, line[i]), true
			}
		case c == '\'' || c == '"':
			j := i + 1
			for ; j < len(line) && line[j] != c; j++ {
				if c == '"' && line[j] == '\\' && j+1 < len(line) && strings.IndexByte("\"\\$`", line[j+1]) >= 0 {
					j++
				}
				word = append(word, line[j])
			}
			if j == len(line) {
				return nil, false
			}
			if inner := line[i+1 : j]; c == '"' && strings.ContainsAny(inner, "$`") {
				more, _ := Split(inner)
				cmds = append(cmds, more...)
			}
			i, started = j, true
		case strings.IndexByte(" \t\r;&|()`\n", c) >= 0: // $( leaves a word $
			flush(strings.IndexByte(" \t\r", c) < 0)
		default:
			word, started = append(word, c), true
		}
	}
	flush(true)
	return cmds, true
}

// SkipsHooks says how a command line makes git skip its hooks, or "". Text
// it cannot split skips when it holds --no-verify.
func SkipsHooks(line string) string { return judge(line, 3) }

func judge(line string, depth int) string {
	cmds, ok := Split(line)
	if !ok && strings.Contains(line, "--no-verify") {
		return "holds --no-verify"
	}
	for _, words := range cmds {
		if why := simple(words, depth); why != "" {
			return why
		}
	}
	return ""
}

var (
	wrappers = []string{"env", "command", "sudo", "time", "nice", "xargs", "timeout", "exec", "nohup"}
	// Options of git before the subcommand, and of commit, that take the next word.
	globalValued = []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env", "--super-prefix"}
	longValued   = []string{"--message", "--file", "--author", "--date", "--template", "--reuse-message",
		"--reedit-message", "--fixup", "--trailer", "--cleanup"}
)

// simple judges one simple command. NAME=value words and wrappers before the
// program are stepped over; after a wrapper, so is every word up to it.
func simple(words []string, depth int) string {
	wrapped := false
	for i, w := range words {
		name := strings.TrimSuffix(strings.ToLower(w[strings.LastIndexAny(w, `/\`)+1:]), ".exe")
		switch {
		case name == "git":
			return gitCall(words[i+1:])
		case slices.Contains([]string{"sh", "bash", "zsh", "dash"}, name) && depth > 0:
			return judge(shellCommand(words[i+1:]), depth-1)
		case name == "eval" && depth > 0:
			return judge(strings.Join(words[i+1:], " "), depth-1)
		case name == "cw" && len(words) > i+1 && (words[i+1] == "uninstall" || words[i+1] == "init" && slices.ContainsFunc(words[i+2:], observes)):
			return "runs `" + strings.Join(words[i:], " ") + "`"
		case strings.Contains(w, "=") && strings.Contains(strings.ToLower(w), "core.hookspath"):
			return "sets core.hooksPath in the environment of git"
		case !wrapped && !strings.Contains(w, "=") && !slices.Contains(wrappers, name):
			return ""
		}
		wrapped = wrapped || slices.Contains(wrappers, name)
	}
	return ""
}

// observes says whether a word of `cw init` is the option that makes the
// hooks allow everything, in any of the spellings the program reads.
func observes(word string) bool { return strings.HasPrefix(strings.TrimLeft(word, "-"), "observe") }

// shellCommand is TEXT in `sh -c TEXT` or `sh -lc TEXT`, or "".
func shellCommand(args []string) string {
	for i := 0; i+1 < len(args) && strings.HasPrefix(args[i], "-"); i++ {
		if !strings.HasPrefix(args[i], "--") && strings.Contains(args[i], "c") {
			return args[i+1]
		}
	}
	return ""
}

// gitCall judges the arguments of one git call.
func gitCall(args []string) string {
	for i := 0; i < len(args); i++ {
		name, value, eq := strings.Cut(args[i], "=")
		if !strings.HasPrefix(name, "-") {
			return subcommand(args[i], args[i+1:])
		} else if !eq && slices.Contains(globalValued, name) && i+1 < len(args) {
			i++
			value = args[i]
		}
		if key, _, _ := strings.Cut(value, "="); (name == "-c" || name == "--config-env") && strings.EqualFold(key, "core.hooksPath") {
			return "sets core.hooksPath for one git call"
		}
	}
	return ""
}

// subcommand judges the words after a subcommand of git, up to a "--".
func subcommand(sub string, rest []string) string {
	for j := 0; j < len(rest) && rest[j] != "--"; j++ {
		a := rest[j]
		switch {
		case len(a) >= len("--no-veri") && strings.HasPrefix("--no-verify", a): // git takes a prefix
			return "gives git " + sub + " the option " + a
		case sub == "config" && strings.EqualFold(a, "core.hooksPath") && (j < len(rest)-1 || slices.ContainsFunc(rest, func(w string) bool { return w == "unset" || strings.HasPrefix(w, "--unset") })):
			return "changes core.hooksPath with git config"
		case sub != "commit" || len(a) < 2 || a[0] != '-':
		case slices.Contains(longValued, a):
			j++
		case a[1] != '-':
			// A cluster of short options ends at the first that takes a value.
			end := strings.IndexAny(a+"m", "mFCctSu") // len(a) when none does
			if strings.Contains(a[:end], "n") {
				return "gives git commit the option -n, the short form of --no-verify"
			} else if end == len(a)-1 && !strings.ContainsAny(a[end:], "Su") {
				j++ // the value is the next word
			}
		}
	}
	return ""
}
