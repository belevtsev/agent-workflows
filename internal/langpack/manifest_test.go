package langpack_test

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// mfGoText is the shipped Go pack.yaml: only the fields cw reads.
const mfGoText = `# Go language pack. cw embeds this file (docs/packs.md).
schema: 1
id: go
kind: language
detect: [go.mod, go.work]
commands:
  race: [go, test, -race, -shuffle=on, ./...]
evidence_rule: go.package-test/v2
`

// mfGoWant is mfGoText decoded by hand.
func mfGoWant() langpack.Manifest {
	return langpack.Manifest{
		Schema: 1, ID: "go", Kind: "language", Detect: []string{"go.mod", "go.work"},
		Commands:     langpack.Commands{Race: []string{"go", "test", "-race", "-shuffle=on", "./..."}},
		EvidenceRule: "go.package-test/v2",
	}
}

const mfBase = "schema: 1\nid: x\nkind: language\ndetect: [x.mod]\n"

func mfHead(id, kind, detect string) string {
	return "schema: 1\nid: " + id + "\nkind: " + kind + "\n" + detect
}

// mfPadded returns a valid manifest of exactly n bytes.
func mfPadded(n int) string { return mfBase + "#" + strings.Repeat("x", n-len(mfBase)-2) + "\n" }

func mfList(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf("f%d", i)
	}
	return "[" + strings.Join(items, ", ") + "]"
}

// A1, A4: the Go pack decodes field for field; documents at the rule edges are accepted.
func TestParseManifestAccepts(t *testing.T) {
	got, err := langpack.ParseManifest([]byte(mfGoText))
	if err != nil || !reflect.DeepEqual(got, mfGoWant()) {
		t.Fatalf("A1 violated: got %#v, %v", got, err)
	}
	for name, doc := range map[string]string{
		"domain, unknown well-formed rule": mfHead("x", "domain", "detect: [x.mod]\n") + "evidence_rule: example.rule/v1\n",
		"16 detect entries":                mfHead("x", "language", "detect: "+mfList(16)+"\n"),
		"race command, no rule":            mfBase + "commands: {race: [go, test, -race]}\n",
		"document of exactly 64KiB":        mfPadded(64 << 10),
		"32-byte id":                       mfHead("a"+strings.Repeat("b", 31), "language", "detect: [x.mod]\n"),
	} {
		if _, err := langpack.ParseManifest([]byte(doc)); err != nil {
			t.Errorf("A4 violated: %s refused: %v", name, err)
		}
	}
}

// A2, A3, A4: each invalid document is refused with an error naming the field,
// key or value (any listed substring; none for document-level cases).
func TestParseManifestRefuses(t *testing.T) {
	cases := []struct {
		name, doc string
		any       []string
	}{
		{"A2.a missing schema", "id: x\nkind: language\ndetect: [x.mod]\n", []string{"schema"}},
		{"A2.b schema 2", "schema: 2\nid: x\nkind: language\ndetect: [x.mod]\n", []string{"newer"}},
		{"A2.b schema 2 after unknown key and bad values", "id: BAD_ID\nzzz: 1\nkind: nope\nschema: 2\n", []string{"newer"}},
		{"A2.b schema 3", "schema: 3\nid: x\nkind: language\ndetect: [x.mod]\n", []string{"newer"}},
		{"A2.b schema 3 with unknown key and bad values", "schema: 3\nzzz: 1\nid: BAD_ID\ndetect: 3\n", []string{"newer"}},
		{"A2.b schema 1000", "schema: 1000\nid: x\nkind: language\ndetect: [x.mod]\n", []string{"newer"}},
		{"A2.b schema 1000 with unknown key and bad values", "kind: nope\nzzz: [1\n  , 2]\nschema: 1000\n", []string{"newer"}},
		{"A2.b SchemaVersion+1", fmt.Sprintf("schema: %d\nzzz: 1\ndetect: 3\n", langpack.SchemaVersion+1), []string{"newer"}},
		{"A2.c zero", "schema: 0\nid: x\nkind: language\ndetect: [x.mod]\n", []string{"schema"}},
		{"A2.c string", "schema: \"1\"\nid: x\nkind: language\ndetect: [x.mod]\n", []string{"schema"}},
		{"A2.c float", "schema: 1.5\nid: x\nkind: language\ndetect: [x.mod]\n", []string{"schema"}},
		{"A3 top", mfBase + "zzunknowna: 1\n", []string{"zzunknowna"}},
		{"A3 commands", mfBase + "commands: {race: [go], zzunknownb: [x]}\n", []string{"zzunknownb"}},
		{"commands race empty list", mfBase + "commands: {race: []}\n", []string{"commands.race"}},
		{"commands race empty element", mfBase + "commands: {race: [go, \"\"]}\n", []string{"commands.race"}},
		{"id fails ValidID", mfHead("Badid", "language", "detect: [x.mod]\n"), []string{"id", "Badid"}},
		{"kind", mfHead("x", "plugin", "detect: [x.mod]\n"), []string{"kind", "plugin"}},
		{"detect absent", mfHead("x", "language", ""), []string{"detect"}},
		{"detect empty", mfHead("x", "language", "detect: []\n"), []string{"detect"}},
		{"detect 17 entries", mfHead("x", "language", "detect: "+mfList(17)+"\n"), []string{"detect"}},
		{"detect repeated", mfHead("x", "language", "detect: [go.mod, go.mod]\n"), []string{"detect", "go.mod"}},
		{"detect empty entry", mfHead("x", "language", "detect: [\"\"]\n"), []string{"detect"}},
		{"detect dot", mfHead("x", "language", "detect: [\".\"]\n"), []string{"detect"}},
		{"detect dotdot", mfHead("x", "language", "detect: [\"..\"]\n"), []string{"detect", ".."}},
		{"detect slash", mfHead("x", "language", "detect: [a/b]\n"), []string{"detect", "a/b"}},
		{"detect backslash", mfHead("x", "language", "detect: ['a\\b']\n"), []string{"detect", `a\b`}},
		{"detect NUL", mfHead("x", "language", "detect: [\"a\\0b\"]\n"), []string{"detect"}},
		{"rule without a dot", mfBase + "evidence_rule: gorule/v1\n", []string{"evidence_rule", "gorule/v1"}},
		{"rule v0", mfBase + "evidence_rule: go.rule/v0\n", []string{"evidence_rule", "go.rule/v0"}},
		{"rule without version", mfBase + "evidence_rule: go.rule\n", []string{"evidence_rule", "go.rule"}},
		{"rule uppercase", mfBase + "evidence_rule: Go.rule/v1\n", []string{"evidence_rule", "Go.rule/v1"}},
		{"document over 64KiB", mfPadded(64<<10 + 1), nil},
		{"document not UTF-8", mfBase + "# \xff\xfe\n", nil},
		{"document a sequence", "- schema\n- 1\n", nil},
		{"document a scalar", "schema\n", nil},
		{"two documents", mfBase + "---\n" + mfBase, nil},
	}
	for _, c := range cases {
		_, err := langpack.ParseManifest([]byte(c.doc))
		ok := err != nil && len(c.any) == 0
		for _, s := range c.any {
			ok = ok || (err != nil && strings.Contains(err.Error(), s))
		}
		if !ok {
			t.Errorf("%s: invalid manifest gave %v, want an error containing one of %q", c.name, err, c.any)
		}
	}
}

// A manifest declares only what cw reads. Oracle: each field that no cw code
// reads (the schema-1 slots dropped by QR-31) is refused as an unknown key,
// with an error naming it.
func TestParseManifestRefusesFieldsNothingReads(t *testing.T) {
	for key, doc := range map[string]string{
		"build":             "commands: {build: [go, build, ./...]}\n",
		"test":              "commands: {test: [go, test, ./...]}\n",
		"lint":              "commands: {lint: [go, vet, ./...]}\n",
		"format":            "commands: {format: [gofmt, -l, .]}\n",
		"tests":             "tests: {unit: {files: [\"*_test.go\"], argv: [go, test, ./...]}}\n",
		"interface_extract": "interface_extract: [cw, task, contract-extract]\n",
		"skills":            "skills: {implement: [go-principal-engineer]}\n",
		"review":            "review: [Changed concurrent code ran under the race command.]\n",
	} {
		if _, err := langpack.ParseManifest([]byte(mfBase + doc)); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("manifest with unread field %q gave %v, want an error naming it", key, err)
		}
	}
}

func mfPack(id, extra string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(mfHead(id, "language", "detect: [x.mod]\n") + extra)}
}

// A5: LoadManifests sorts, ignores other paths, and fails whole with the path.
func TestLoadManifests(t *testing.T) {
	got, err := langpack.LoadManifests(fstest.MapFS{
		"packs/b/pack.yaml": mfPack("b", ""), "packs/a/pack.yaml": mfPack("a", ""),
		"packs/a/other.yaml": {Data: []byte("bad: [")}, "packs/c/d/pack.yaml": {Data: []byte("bad: [")},
	})
	if err != nil || len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("A5 violated: want [a b], got %v, %v", got, err)
	}
	if got, err := langpack.LoadManifests(fstest.MapFS{}); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("A5 violated: no match gives %#v, %v; want an empty slice and nil", got, err)
	}
	got, err = langpack.LoadManifests(fstest.MapFS{"packs/a/pack.yaml": mfPack("a", ""), "packs/bad/pack.yaml": mfPack("bad", "zzz: 1\n")})
	if got != nil || err == nil || !strings.HasPrefix(err.Error(), "packs/bad/pack.yaml: ") {
		t.Fatalf("A5 violated: invalid file gives %v, %v", got, err)
	}
	got, err = langpack.LoadManifests(fstest.MapFS{"packs/foo/pack.yaml": mfPack("bar", "")})
	if want := `packs/foo/pack.yaml: pack id "bar" does not match its directory "foo"`; got != nil || err == nil || err.Error() != want {
		t.Fatalf("A5 violated: id mismatch gives %v, %v; want %q", got, err, want)
	}
}

// A6: ValidID is exactly the id pattern (examples and seeded random strings);
// KnownRule knows v1 only among the listed ids.
func TestValidIDAndKnownRule(t *testing.T) {
	re := regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	for id, want := range map[string]bool{"go": true, "a-1": true, "a" + strings.Repeat("9", 31): true,
		"a" + strings.Repeat("9", 32): false, "": false, "1a": false, "A": false, "a_b": false, "a\n": false} {
		if langpack.ValidID(id) != want {
			t.Errorf("A6 violated: ValidID(%q) != %v", id, want)
		}
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 5000; i++ {
		b := make([]byte, rng.Intn(36))
		for j := range b {
			b[j] = "az09-_A./\n"[rng.Intn(10)]
		}
		if s := string(b); langpack.ValidID(s) != re.MatchString(s) {
			t.Fatalf("A6 violated: ValidID(%q) disagrees with the pattern", s)
		}
	}
	for id, want := range map[string]bool{"go.package-test/v2": true, "go.package-test/v1": false, "": false, "go.package-test/v9": false, "example.rule/v1": false} {
		if langpack.KnownRule(id) != want {
			t.Errorf("A6 violated: KnownRule(%q) != %v", id, want)
		}
	}
}

// A6: ValidID agrees with the pattern on every fuzzed input.
func FuzzValidID(f *testing.F) {
	re := regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	for _, s := range []string{"go", "", "a-", "Z", "a\x00", strings.Repeat("a", 33)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if langpack.ValidID(s) != re.MatchString(s) {
			t.Fatalf("A6 violated: ValidID(%q) disagrees with the pattern", s)
		}
	})
}

func mfManifest(id, kind, rule string, detect ...string) langpack.Manifest {
	return langpack.Manifest{Schema: 1, ID: id, Kind: kind, Detect: detect, EvidenceRule: rule}
}

// A7: Applicable keeps selected order, picks the first detect entry, omits misses,
// and never matches a current entry with a slash.
func TestApplicable(t *testing.T) {
	sel := []langpack.Manifest{mfManifest("a", "language", "r.a/v1", "x", "y"), mfManifest("b", "language", "", "z"),
		mfManifest("c", "domain", "", "y", "x"), mfManifest("d", "language", "", "sub/go.mod")}
	got := langpack.Applicable(sel, []string{"y", "x", "q"})
	want := []langpack.Applied{{ID: "a", Detected: "x", EvidenceRule: "r.a/v1"}, {ID: "c", Detected: "y"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("A7 violated: got %#v, want %#v", got, want)
	}
	if got := langpack.Applicable(sel, []string{"q", "sub/go.mod"}); len(got) != 0 {
		t.Fatalf("A7 violated: want no match, got %#v", got)
	}
}

// A8, A9: R-DEFAULT-SELECTION picks language packs in order; the embedded set is
// exactly the Go pack of mfGoText, embedded verbatim.
func TestDefaultSelectionAndEmbeddedPacks(t *testing.T) {
	in := []langpack.Manifest{mfManifest("z", "language", "", "a"), mfManifest("d", "domain", "", "a"), mfManifest("b", "language", "", "a")}
	if got := langpack.DefaultSelection(in); len(got) != 2 || got[0].ID != "z" || got[1].ID != "b" {
		t.Fatalf("A8 violated: got %v, want [z b]", got)
	}
	builtIn, err := langpack.LoadManifests(agentworkflows.Packs)
	if err != nil || len(builtIn) != 1 || builtIn[0].ID != "go" {
		t.Fatalf("A9 violated: embedded set %v, %v; want exactly go", builtIn, err)
	}
	if got := langpack.DefaultSelection(builtIn); len(got) != 1 || got[0].ID != "go" {
		t.Fatalf("A8 violated: shipped default selection %v", got)
	}
	if m := builtIn[0]; !reflect.DeepEqual(m, mfGoWant()) || !langpack.KnownRule(m.EvidenceRule) {
		t.Fatalf("A9 violated: embedded go pack differs from mfGoText or names an unknown rule: %#v", m)
	}
	embedded, err1 := fs.ReadFile(agentworkflows.Packs, "packs/go/pack.yaml")
	disk, err2 := os.ReadFile("../../packs/go/pack.yaml")
	if err1 != nil || err2 != nil || string(embedded) != string(disk) {
		t.Fatalf("A9 violated: embedded pack.yaml differs from the checked-in file (%v, %v)", err1, err2)
	}
}
