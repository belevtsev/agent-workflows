package cli

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
)

// Thresholds of the ask for a substantial-looking change without a task
// record: a publication meeting at least two of the three signs prompts.
const substantialChangeLines = 300

// codeExtensions are the source file extensions whose change, without any
// test file changing, counts as code changed without tests.
var codeExtensions = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
	".rs": true, ".java": true, ".kt": true, ".kts": true, ".scala": true, ".c": true, ".h": true, ".cc": true,
	".cpp": true, ".cxx": true, ".hpp": true, ".m": true, ".mm": true, ".swift": true, ".rb": true, ".php": true,
	".cs": true, ".fs": true, ".sh": true, ".bash": true, ".zsh": true, ".lua": true, ".ex": true, ".exs": true,
	".erl": true, ".hs": true, ".clj": true, ".dart": true, ".vue": true, ".svelte": true,
}

// testDirectories are path components that hold tests.
var testDirectories = map[string]bool{"test": true, "tests": true, "testdata": true, "__tests__": true, "spec": true}

// substantialChangeSigns returns the signs that the change a publication in
// worktree would publish is substantial: more than 300 changed lines, files
// under more than one top-level directory, and code changed with no test
// file changed. A commit publishes the staged changes (with -a, every
// change to a tracked file); a push publishes the commits between the
// merge base with the upstream branch, or else with origin's default
// branch, and HEAD. Any git failure yields no signs.
func substantialChangeSigns(ctx context.Context, worktree string, publication publication) []string {
	args := []string{"diff", "--numstat", "-z", "--no-renames", "--no-ext-diff", "--no-textconv"}
	switch {
	case publication.push:
		base, err := gitOutput(ctx, worktree, "rev-parse", "--verify", "--quiet", "@{upstream}")
		if err != nil {
			base, err = gitOutput(ctx, worktree, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/HEAD")
		}
		if err != nil {
			return nil
		}
		args = append(args, base+"...HEAD")
	case publication.all:
		args = append(args, "HEAD")
	default:
		args = append(args, "--cached")
	}
	output, err := gitRun(ctx, worktree, args...)
	if err != nil {
		return nil
	}
	lines, paths, ok := parseNumstat(output)
	if !ok {
		return nil
	}
	var signs []string
	if lines > substantialChangeLines {
		signs = append(signs, fmt.Sprintf("%d changed lines (more than %d)", lines, substantialChangeLines))
	}
	if directories := topLevelDirectories(paths); len(directories) > 1 {
		signs = append(signs, fmt.Sprintf("changes in %d top-level directories (%s)", len(directories), strings.Join(directories, ", ")))
	}
	if slices.ContainsFunc(paths, isCodeFile) && !slices.ContainsFunc(paths, isTestFile) {
		signs = append(signs, "code changed with no test file changed")
	}
	return signs
}

// parseNumstat sums the added and deleted lines of `git diff --numstat -z
// --no-renames` output and lists its paths. A binary file counts no lines.
func parseNumstat(output string) (int, []string, bool) {
	lines := 0
	var paths []string
	for _, entry := range strings.Split(output, "\x00") {
		if entry == "" {
			continue
		}
		fields := strings.SplitN(entry, "\t", 3)
		if len(fields) != 3 {
			return 0, nil, false
		}
		for _, count := range fields[:2] {
			if count == "-" {
				continue
			}
			value, err := strconv.Atoi(count)
			if err != nil {
				return 0, nil, false
			}
			lines += value
		}
		paths = append(paths, fields[2])
	}
	return lines, paths, true
}

// topLevelDirectories returns the sorted first path components of the paths
// inside a directory; a file at the repository root belongs to none.
func topLevelDirectories(paths []string) []string {
	var directories []string
	for _, file := range paths {
		if top, _, nested := strings.Cut(file, "/"); nested && !slices.Contains(directories, top) {
			directories = append(directories, top)
		}
	}
	slices.Sort(directories)
	return directories
}

// isTestFile reports whether a repository path names a test: a Go, Python,
// Ruby, JavaScript, Swift, JVM or .NET-style test file name, or a file under
// a test directory (in any letter case, such as Tests/).
func isTestFile(file string) bool {
	components := strings.Split(file, "/")
	for _, directory := range components[:len(components)-1] {
		if testDirectories[strings.ToLower(directory)] {
			return true
		}
	}
	name := components[len(components)-1]
	stem := strings.TrimSuffix(name, path.Ext(name))
	return strings.HasSuffix(stem, "_test") || strings.HasSuffix(stem, "_spec") || strings.HasPrefix(name, "test_") ||
		strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") ||
		strings.HasSuffix(stem, "Test") || strings.HasSuffix(stem, "Tests")
}

// isCodeFile reports whether a repository path names non-test source code.
func isCodeFile(file string) bool {
	return codeExtensions[path.Ext(file)] && !isTestFile(file)
}

// unrecordedChangeReason explains the ask for a substantial-looking change
// that no task record covers.
func unrecordedChangeReason(worktree string, publication publication, signs []string) string {
	subject := "this commit looks"
	if publication.push {
		subject = "the commits being pushed look"
	}
	return fmt.Sprintf("agent-workflows: %s like a substantial change with no cw task record for %s: %s. "+
		"Consider recording it with `cw task start --repo %s --plan PLAN.json --summary` and a passing cw task verify before publishing, or confirm to continue. "+
		"Set small_task_prompt=false in the plugin config to stop this prompt.", subject, worktree, strings.Join(signs, "; "), worktree)
}

// uncommittedTrackedChanges reports whether worktree has staged or unstaged
// changes to tracked files. A git failure reports none.
func uncommittedTrackedChanges(ctx context.Context, worktree string) bool {
	output, err := gitRun(ctx, worktree, "status", "--porcelain", "--untracked-files=no", "--ignore-submodules=none")
	return err == nil && strings.TrimSpace(output) != ""
}
