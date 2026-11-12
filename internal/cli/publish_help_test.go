package cli

import (
	"strings"
	"testing"
)

// cw publish --help says when a run appends its publication entries.
// Oracle: internal/publish/run.go runs the gate, the open-record check, the
// authorization and preconditions before start writes the started entry, and
// docs/operations.md (#134) says so: once the checks pass, a started entry
// and a finished entry when the run ends; a run its checks stop appends none;
// a refusal at a later step keeps the steps done, and the finished entry
// records it. The help no longer says that every run appends both.
func TestPublishHelpSaysARunStoppedAtItsChecksAppendsNoEntry(t *testing.T) {
	config, out := testConfig(t)
	if err := Execute(t.Context(), []string{"publish", "--help"}, config); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, want := range []string{
		"Once its checks pass, a run appends a started entry, publication-NNNNNN.json, to the task directory, and a finished entry when it ends.",
		"A run that stops at its checks, such as one they deny (exit 3) or refuse (exit 4), commits nothing and appends no entry;",
		"a refusal at a later step keeps the commit or push already done, and the finished entry records it.",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("cw publish --help does not say %q:\n%s", want, help)
		}
	}
	if old := "Each run appends a started and a finished entry"; strings.Contains(help, old) {
		t.Errorf("cw publish --help still says %q", old)
	}
}
