package skillcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// thirdPartyRow maps each module go.mod requires to the link text of its row
// in the "cw dependencies" table of THIRD_PARTY.md. A new dependency fails the
// test until it has a row and an entry here.
var thirdPartyRow = map[string]string{
	"github.com/pelletier/go-toml/v2":      "pelletier/go-toml",
	"github.com/spf13/cobra":               "spf13/cobra",
	"github.com/spf13/pflag":               "spf13/pflag",
	"github.com/inconshreveable/mousetrap": "inconshreveable/mousetrap",
	"golang.org/x/sys":                     "golang.org/x/sys",
	"gopkg.in/yaml.v3":                     "go-yaml/yaml",
	"toolchain":                            "Go runtime and standard library",
}

// tableRow matches "| [text](url) | version | ..." in a markdown table.
var tableRow = regexp.MustCompile(`^\| \[([^\]]+)\]\([^)]*\) \| ([^ |]+) \|`)

// goModVersions returns each required module's version from go.mod, plus the
// toolchain directive under the key "toolchain".
func goModVersions(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "//")
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case fields[0] == "toolchain" && len(fields) == 2:
			versions["toolchain"] = fields[1]
		case fields[0] == "require" && len(fields) == 2 && fields[1] == "(":
			inBlock = true
		case fields[0] == ")":
			inBlock = false
		case fields[0] == "require" && len(fields) == 3:
			versions[fields[1]] = fields[2]
		case inBlock && len(fields) == 2:
			versions[fields[0]] = fields[1]
		}
	}
	return versions
}

// oracle: every module go.mod requires, and the toolchain, appears in the
// "cw dependencies" table of THIRD_PARTY.md at the version go.mod names.
func TestThirdPartyVersionsMatchGoMod(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "THIRD_PARTY.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(data), "\n## cw dependencies\n")
	if !found {
		t.Fatal(`THIRD_PARTY.md has no "## cw dependencies" section`)
	}
	section, _, _ = strings.Cut(section, "\n## ")
	listed := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if m := tableRow.FindStringSubmatch(line); m != nil {
			listed[m[1]] = m[2]
		}
	}
	versions := goModVersions(t)
	if len(versions) < 2 {
		t.Fatalf("parsed only %v from go.mod", versions)
	}
	for module, version := range versions {
		row, ok := thirdPartyRow[module]
		if !ok {
			t.Errorf("go.mod requires %s %s: add a THIRD_PARTY.md row and a thirdPartyRow entry", module, version)
			continue
		}
		if got, ok := listed[row]; !ok {
			t.Errorf("THIRD_PARTY.md has no row %q for %s", row, module)
		} else if got != version {
			t.Errorf("THIRD_PARTY.md lists %s at %s, go.mod has %s", row, got, version)
		}
	}
}
