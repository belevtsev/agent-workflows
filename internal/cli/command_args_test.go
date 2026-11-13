package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// takesArguments are the commands whose usage names positional arguments;
// every other command, parents included, takes none.
var takesArguments = map[string]bool{
	"cw pack add":                  true,
	"cw pack remove":               true,
	"cw keys set":                  true,
	"cw keys unset":                true,
	"cw metrics summarize":         true,
	"cw graphify query":            true,
	"cw task schema":               true,
	"cw hook git pre-commit":       true,
	"cw hook git pre-push":         true,
	"cw hook git pre-merge-commit": true,
}

// honoursDryRun are the commands with a --dry-run flag: each one writes a
// preview, or reports the change, without writing.
var honoursDryRun = map[string]bool{
	"cw consult":     true,
	"cw pack add":    true,
	"cw pack remove": true,
}

// commandPaths lists every command in the cw tree as its argv after "cw".
func commandPaths(t *testing.T) map[string][]string {
	t.Helper()
	config, _ := testConfig(t)
	paths := map[string][]string{}
	var walk func(command *cobra.Command, argv []string)
	walk = func(command *cobra.Command, argv []string) {
		paths[command.CommandPath()] = argv
		for _, child := range command.Commands() {
			walk(child, append(append([]string{}, argv...), child.Name()))
		}
	}
	walk(NewCommand(config), nil)
	if len(paths) < 40 {
		t.Fatalf("walked %d commands; the tree is missing commands", len(paths))
	}
	return paths
}

// Oracle: docs/operations.md, Exit statuses: an argument a command does not
// take is a usage error, exit 2, for every command, and the message names
// the argument and the command. The arguments are rejected before the
// command runs, so nothing is read or written.
func TestEveryCommandWithoutArgumentsRejectsOneWithUsage(t *testing.T) {
	paths := commandPaths(t)
	for path, argv := range paths {
		if takesArguments[path] {
			continue
		}
		t.Run(path, func(t *testing.T) {
			config, _ := testConfig(t)
			config.Getenv = func(string) string { t.Fatal("the command ran"); return "" }
			err := Execute(t.Context(), append(argv, "extra-argument"), config)
			if got := ExitCode(err); got != ExitUsage {
				t.Fatalf("exit %d (%v), want %d", got, err, ExitUsage)
			}
			if want := `unexpected argument "extra-argument" for "` + path + `"`; err.Error() != want {
				t.Fatalf("error %q, want %q", err, want)
			}
		})
	}
	for path := range takesArguments {
		if _, ok := paths[path]; !ok {
			t.Errorf("%s is listed as taking arguments but is not a command", path)
		}
	}
}

// Oracle: docs/operations.md, Paths: --dry-run belongs to cw consult,
// cw pack add and cw pack remove only; on any other command it is an
// unknown flag, exit 2. The second unknown flag shows which one the parser
// stopped at, so neither command runs. The git hook commands are left out:
// they ignore unknown flags, because git may pass a remote URL that starts
// with "-".
func TestOnlyTheCommandsThatHonourDryRunAcceptIt(t *testing.T) {
	for path, argv := range commandPaths(t) {
		if strings.HasPrefix(path, "cw hook git ") {
			continue
		}
		t.Run(path, func(t *testing.T) {
			config, _ := testConfig(t)
			err := Execute(t.Context(), append(argv, "--dry-run", "--not-a-flag"), config)
			if got := ExitCode(err); got != ExitUsage {
				t.Fatalf("exit %d (%v), want %d", got, err, ExitUsage)
			}
			rejected := "--dry-run"
			if honoursDryRun[path] {
				rejected = "--not-a-flag"
			}
			if want := "unknown flag: " + rejected; err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error %v, want it to contain %q", err, want)
			}
		})
	}
}

// Oracle: cw task show only reads, and used to ignore --dry-run silently;
// now it rejects the flag as unknown, exit 2.
func TestTaskShowRejectsDryRun(t *testing.T) {
	config, _ := testConfig(t)
	err := Execute(t.Context(), []string{"task", "show", "--task", t.TempDir(), "--dry-run"}, config)
	var usageErr usageError
	if !errors.As(err, &usageErr) || !strings.Contains(err.Error(), "unknown flag: --dry-run") {
		t.Fatalf("task show --dry-run returned %v, want the usage error for an unknown flag", err)
	}
}
