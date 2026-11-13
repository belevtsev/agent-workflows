package gate

import (
	"slices"
	"strings"
)

// stagedRule is what the gate can tell, before a git commit runs, of the
// content it records for each path (stagedDifferences).
type stagedRule uint8

const (
	// stagedIndex is a commit that records the index as it stands: plain
	// git commit, and every commit git's own hooks judge, whose index git
	// names in GIT_INDEX_FILE.
	stagedIndex stagedRule = iota
	// stagedRemovals is git commit -a, which records the working tree's
	// bytes of every path in the index, so only a path the index no longer
	// holds can differ.
	stagedRemovals
	// stagedUnknown is a commit whose recorded content the plugin hook cannot
	// tell from the index as it stands: one with pathspecs, --include,
	// --only or --pathspec-from-file, which take the named paths from the
	// working tree, or one after another command in the same line that may
	// change the index first, such as git add. It is not judged.
	stagedUnknown
)

// commitValueOptions are the git commit options that take a separate value.
var commitValueOptions = map[string]bool{
	"-m": true, "-F": true, "-c": true, "-C": true, "-t": true,
	"--message": true, "--file": true, "--reedit-message": true, "--reuse-message": true, "--template": true,
	"--author": true, "--date": true, "--fixup": true, "--squash": true, "--cleanup": true, "--trailer": true,
}

// commitStaged returns the stagedRule of git commit arguments. A word it
// cannot place, such as the value of an abbreviated long option, reads as a
// pathspec, so it leaves the commit unjudged rather than judge content the
// commit may not record.
func commitStaged(arguments []string) stagedRule {
	rule := stagedIndex
	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		switch {
		case argument == "--":
			if i+1 < len(arguments) {
				return stagedUnknown
			}
			return rule
		case argument == "--all":
			rule = stagedRemovals
		case argument == "--only" || argument == "--include" || strings.HasPrefix(argument, "--pathspec-from-file"):
			return stagedUnknown
		case commitValueOptions[argument]:
			i++
		case strings.HasPrefix(argument, "--"):
		case !strings.HasPrefix(argument, "-") || argument == "-":
			return stagedUnknown // a pathspec
		default:
		cluster:
			for j, option := range argument[1:] {
				switch {
				case option == 'a':
					rule = stagedRemovals
				case option == 'i' || option == 'o':
					return stagedUnknown
				case strings.ContainsRune("mFcCt", option):
					if j == len(argument)-2 {
						i++ // the value is the next word
					}
					break cluster // the rest of the cluster is this option's value
				case option == 'u' || option == 'S':
					break cluster // an optional value, only ever attached
				}
			}
		}
	}
	return rule
}

// indexKeepers are the programs that leave git's index as it is, so a git
// commit after them in the same command line records the index the plugin
// hook reads: cd and the gate's trusted read-only commands, and the shell
// words that end a compound command.
var indexKeepers = map[string]bool{
	"cd": true, "pushd": true, "popd": true, "echo": true, "printf": true, "cat": true, "rg": true, "grep": true, "jq": true,
	"true": true, "false": true, ":": true, "pwd": true, "ls": true, "test": true, "[": true,
	"fi": true, "done": true, "esac": true, "}": true, "then": true, "else": true, "do": true,
}

// keepsIndex reports whether a simple command, from its program on, leaves
// git's index as it is: no command, git push, or one of indexKeepers that
// writes no file, so not rg with --pre or --pre-glob, which run a program,
// and no word that redirects output to a file (the flat split keeps
// redirections among the words; the shellwords reading checks its own with
// trustedRedirect).
func keepsIndex(words []string) bool {
	if len(words) == 0 {
		return true
	}
	if subcommand, _ := gitInvocation(words); subcommand > 0 && words[subcommand] == "push" {
		return true
	}
	if !indexKeepers[words[0]] || words[0] == "rg" && slices.ContainsFunc(words[1:], func(word string) bool { return strings.HasPrefix(word, "--pre") }) {
		return false
	}
	for i, word := range words {
		operator := strings.TrimLeft(word, "0123456789")
		if !strings.HasPrefix(operator, ">") {
			continue
		}
		target := strings.TrimLeft(operator, ">|&")
		if target == "" && i+1 < len(words) {
			target = words[i+1]
		}
		if target != "/dev/null" && strings.Trim(target, "0123456789-") != "" {
			return false
		}
	}
	return true
}

// selectsRepository reports whether a simple command's words, wrappers and
// assignments included, point git at another index, git directory or work
// tree: a GIT_INDEX_FILE, GIT_DIR or GIT_WORK_TREE assignment, or git's
// --git-dir or --work-tree before its subcommand. The plugin hook then
// cannot tell which index the commit records.
func selectsRepository(words []string) bool {
	return slices.ContainsFunc(words, func(word string) bool {
		word = strings.Trim(word, `'"`)
		for _, prefix := range []string{"GIT_INDEX_FILE=", "GIT_DIR=", "GIT_WORK_TREE=", "--git-dir", "--work-tree"} {
			if strings.HasPrefix(word, prefix) {
				return true
			}
		}
		return false
	})
}
