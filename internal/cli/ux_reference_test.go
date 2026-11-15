package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var updateUXReference = flag.Bool("update-cli-reference", false, "regenerate the CLI reference from command metadata")

// The checked-in reference is the actual offline help output. A new flag or
// command must update both its terminal help and its documentation together.
func TestUXCommandReferenceIsGenerated(t *testing.T) {
	var document bytes.Buffer
	fmt.Fprintln(&document, "# CLI command reference\n\nGenerated from command metadata. For setup and worked examples, see the [CLI guide](cli.md).\nRegenerate with `GOWORK=off go test ./internal/cli -run TestUXCommandReferenceIsGenerated -update-cli-reference`.")
	for _, group := range uxGroups {
		fmt.Fprintf(&document, "\n## %s\n", group[0])
		for _, command := range uxCommands() {
			if command.hidden || !strings.HasPrefix(command.path, group[0]+" ") {
				continue
			}
			fmt.Fprintf(&document, "\n### kitsu %s\n\n```text\n", command.path)
			if err := uxPrintLeaf(Config{Out: &document}, command); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(&document, "```")
		}
	}
	fmt.Fprintln(&document, "\n## Compatibility aliases and integrations\n\nExisting spellings retain their original argument rules. Nested help names their preferred replacement.")
	for _, command := range commands() {
		fmt.Fprintf(&document, "\n### kitsu %s\n\n```text\n", command.name)
		if err := helpFor(Config{Out: &document}, command.name); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&document, "```")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository command reference")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "docs", "cli-reference.md")
	if *updateUXReference {
		if err := os.WriteFile(path, document.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, document.Bytes()) {
		t.Fatalf("CLI reference differs from command help; regenerate with -update-cli-reference (%v)", err)
	}
}
