package cli

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden rewrites the golden files from the current output:
// go test ./internal/cli -run Help -update-golden.
var updateGolden = flag.Bool("update-golden", false, "rewrite the golden files in testdata")

// assertGolden compares got with testdata/name, or rewrites the file under
// -update-golden.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("output differs from %s (rerun with -update-golden after checking the change):\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

// cw --help and cw help list every top-level command under "Core commands"
// or "Experimental commands" (docs/operations.md, Stability), and none under
// cobra's "Additional Commands".
// oracle: the text equals testdata/cw-help.golden, which lists consult,
// graphify, keys and metrics as experimental and every other command as core.
func TestRootHelpGroupsCommandsAsCoreAndExperimental(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}} {
		t.Run(args[0], func(t *testing.T) {
			config, out := testConfig(t)
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatal(err)
			}
			assertGolden(t, "cw-help.golden", out.Bytes())
		})
	}
}
