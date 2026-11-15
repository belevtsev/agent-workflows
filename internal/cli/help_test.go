package cli

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
)

// A word of a usage line that the help of the command has to explain: an
// option, or an argument in angle brackets.
var usageWord = regexp.MustCompile(`--[a-z-]+|<[a-z]+>`)

// `gatecub <command> --help` explains every option and every argument that the
// usage line of the command names (U10): the skill `gatecub` sends an agent there
// for the options.
func TestHelpOfACommandExplainsEveryWordOfItsUsageLine(t *testing.T) {
	t.Parallel()
	for _, c := range commands() {
		for _, way := range [][]string{{c.name, "--help"}, {c.name, "-h"}, {"help", c.name}} {
			var out, errOut bytes.Buffer
			if err := Execute(context.Background(), way, Config{Out: &out, Err: &errOut}); err != nil {
				t.Fatalf("gatecub %s: %v", strings.Join(way, " "), err)
			}
			head, explained, _ := strings.Cut(out.String(), "\n\n"+c.summary+".\n")
			if head != "usage: "+usageLine(c) {
				t.Errorf("gatecub %s starts with\n%s\nwant the usage line and the summary of %s", strings.Join(way, " "), out.String(), c.name)
			}
			if (c.args == "") != (explained == "") {
				t.Errorf("gatecub %s: a command explains arguments exactly when it takes some; got %q", strings.Join(way, " "), explained)
			}
			for _, word := range usageWord.FindAllString(c.args, -1) {
				if !strings.Contains(explained, word) {
					t.Errorf("gatecub %s does not explain %s:\n%s", strings.Join(way, " "), word, explained)
				}
			}
			for line := range strings.SplitSeq(strings.Trim(explained, "\n"), "\n") {
				if explained != "" && (!strings.HasPrefix(line, "  ") || len(strings.Fields(line)) < 3) {
					t.Errorf("gatecub %s: %q is not a line of the form `  <word>  <what it does>`", strings.Join(way, " "), line)
				}
			}
			if errOut.Len() > 0 {
				t.Errorf("gatecub %s wrote to standard error: %s", strings.Join(way, " "), errOut.String())
			}
		}
	}
}

// The option lines of a command are aligned: every explanation starts in
// the same column.
func TestHelpLinesOfACommandAreAligned(t *testing.T) {
	t.Parallel()
	gap := regexp.MustCompile(`\S {2,}\S`)
	for _, c := range commands() {
		column := -1
		for line := range strings.SplitSeq(strings.Trim(c.options, "\n"), "\n") {
			if line == "" {
				continue
			}
			at := gap.FindStringIndex(line[2:])
			if at == nil {
				t.Errorf("%s: %q has no explanation after two spaces", c.name, line)
				continue
			}
			if column >= 0 && at[1] != column {
				t.Errorf("%s: %q starts its explanation in another column than the line above", c.name, line)
			}
			column = at[1]
		}
	}
}
