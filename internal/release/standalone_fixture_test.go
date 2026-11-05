package release

import (
	"os"
	"path/filepath"
	"testing"
)

// R-R2, R-R3: these are test-owned synthetic resources and the approved ABI.
// The disposable release export initializes embeddedRaw and sets embeddedMarker;
// its generator declares neither variable. FixtureResources keeps both reachable.
const standaloneFixtureShim = `package bundled
var embeddedRaw []byte
var embeddedMarker string
func FixtureResources() ([]byte, string) { return embeddedRaw, embeddedMarker }
`

const standaloneFixtureManifest = `{
  "version": 3,
  "registrations": {"demo": "skills/demo"},
  "global_instructions": "global/AGENTS.md",
  "model_policy": "skills/demo/policy.yaml",
  "claude_model_policy": "skills/demo/claude.yaml",
  "adoption_catalog": "global/adoption.json",
  "required_licenses": ["licenses/demo.txt"]
}
`

// Resource names deliberately do not overlap the compiler/embed/source-mutation
// inputs or notice markers in TestBuildPinsBinariesAndNoticesWhenWorktreeChanges.
func writeStandaloneFixtureSource(t *testing.T, source string) {
	t.Helper()
	inputs := map[string]string{
		"skills-manifest.json":        standaloneFixtureManifest,
		"internal/bundled/fixture.go": standaloneFixtureShim,
		"skills/demo/SKILL.md":        "Synthetic standalone fixture skill\n",
		"skills/demo/policy.yaml":     "Synthetic model policy\n",
		"skills/demo/claude.yaml":     "Synthetic Claude policy\n",
		"global/AGENTS.md":            "Synthetic standalone instructions\n",
		"global/adoption.json":        "Synthetic adoption catalog\n",
		"licenses/demo.txt":           "Synthetic standalone fixture notice\n",
	}
	for name, data := range inputs {
		path := filepath.Join(source, filepath.FromSlash(name))
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("synthetic fixture resource would overwrite existing input %s: %v", name, err)
		}
		writeTest(t, path, []byte(data))
	}
}

// Positive bundle fixtures use physical roots on platforms where TempDir can
// contain symlink aliases. No personal directory or configuration is touched.
func standaloneFixtureDirectory(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// R-R1, R-R3: builds may use only a disposable export. Tracked mutation markers
// can remain dirty, but HEAD and the original checkout's file inventory stay put.
func assertStandaloneFixtureCheckout(t *testing.T, source, revision string) {
	t.Helper()
	if current := gitTest(t, source, "rev-parse", "HEAD"); current != revision {
		t.Fatalf("build changed source HEAD from %s to %s", revision, current)
	}
	if extra := gitTest(t, source, "ls-files", "--others", "--exclude-standard"); extra != "" {
		t.Fatalf("build left generated embedding inputs in the original checkout: %s", extra)
	}
}
