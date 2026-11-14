package taskflow

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// helperFastExit is the race runtime option that stops a successful
// race-built helper from sleeping about a second before it exits.
const helperFastExit = "atexit_sleep_ms=0"

// helperGORACE appends helperFastExit to the caller's race options, keeping
// every option the caller already set. GORACE is read at process start and
// later options win, so the appended option applies to helper children and to
// the descendants they start with an inherited environment.
func helperGORACE(current string) string {
	options := strings.Fields(current)
	if len(options) > 0 && options[len(options)-1] == helperFastExit {
		return current
	}
	return strings.Join(append(options, helperFastExit), " ")
}

// TestMain sets the check-helper environment once, before any test runs, so
// helperPlan needs no t.Setenv and works in parallel tests. Check children
// inherit the test process environment, so every helper child and each
// descendant it starts with an inherited environment gets both variables.
// TestMain also runs in each re-executed helper child, where the variables are
// already set and helperGORACE leaves GORACE unchanged.
func TestMain(m *testing.M) {
	for name, value := range map[string]string{
		"CW_TASKFLOW_HELPER": "1",
		"GORACE":             helperGORACE(os.Getenv("GORACE")),
	} {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintf(os.Stderr, "set %s: %v\n", name, err)
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}
