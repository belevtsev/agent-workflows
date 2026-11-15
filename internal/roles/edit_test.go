package roles

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestEditPreservesUnrelatedSettings(t *testing.T) {
	t.Parallel()
	const other = "\n# Test scope\n[paths]\ntests = [\n  'tests/**', # keep this\n]\n\n[claude.developer]\nmodel = 'opus'\n"
	for _, section := range []string{
		"[codex.developer]\nmodel = 'old' # selected model\neffort = 'low'\n",
		"['codex'.'developer']\nmodel = '''old'''\neffort = 'low'\n",
		"[codex]\ndeveloper.model = 'old'\ndeveloper.effort = 'low'\n",
		"codex.developer.model = 'old'\ncodex.developer.effort = 'low'\n",
		"codex = {developer = {model = 'old', effort = 'low'}, reviewer = {model = 'keep'}}\n",
		"[codex]\ndeveloper = {model = 'old', effort = 'low'}\n",
	} {
		t.Run(section, func(t *testing.T) {
			data := []byte("# My roles\nversion = 1\n" + section + other)
			before, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			wantModel := "custom\"model\\id\nnext"
			updated, err := Edit(data, "codex", "developer", Model{Model: wantModel}, false)
			if err != nil {
				t.Fatal(err)
			}
			after, err := Parse(updated)
			if err != nil {
				t.Fatal(err)
			}
			selection := before.Models["codex"]["developer"]
			selection.Model = wantModel
			before.Models["codex"]["developer"] = selection
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("unrelated settings changed:\n%#v\n%#v", before, after)
			}
			if !bytes.HasPrefix(updated, []byte("# My roles\n")) || !bytes.Contains(updated, []byte(other)) {
				t.Fatalf("unrelated formatting/comments changed:\n%s", updated)
			}
			reset, err := Edit(updated, "codex", "developer", Model{}, true)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := Parse(reset)
			if err != nil || cfg.Models["codex"]["developer"] != (Model{}) {
				t.Fatalf("reset kept an override: %s; %v", reset, err)
			}
		})
	}
}

func TestEditAddsMissingFields(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"version = 1", "version = 1\r\n\r\n[paths]\r\ntests = ['tests/**']\r\n",
		"version = 1\n[codex.developer]\neffort = 'low' # keep\n[paths]\ntests = []\n",
		"version = 1\ncodex.developer.effort = 'low'\n[paths]\ntests = []\n",
		"version = 1\ncodex = {reviewer = {effort = 'max'}}\n",
		"version = 1\n[codex]\ndeveloper.effort = 'low'", "version = 1\n[codex.developer]\n",
	} {
		t.Run(source, func(t *testing.T) {
			updated, err := Edit([]byte(source), "codex", "developer", Model{Model: "new", Effort: "high"}, false)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := Parse(updated)
			if err != nil || cfg.Models["codex"]["developer"] != (Model{Model: "new", Effort: "high"}) {
				t.Fatalf("missing selection: %s; %v", updated, err)
			}
			if strings.Contains(source, "\r\n") && strings.Contains(strings.ReplaceAll(string(updated), "\r\n", ""), "\n") {
				t.Fatalf("introduced mixed line endings: %q", updated)
			}
		})
	}
}

func TestEditRejectsInvalidConfigAndResetIsIdempotent(t *testing.T) {
	t.Parallel()
	if _, err := Edit([]byte("version = 1\n[invalid]\n"), "codex", "developer", Model{Model: "new"}, false); err == nil {
		t.Fatal("edited invalid configuration")
	}
	data := []byte("version = 1\n# no overrides\n")
	got, err := Edit(data, "codex", "developer", Model{}, true)
	if err != nil || !bytes.Equal(data, got) {
		t.Fatalf("reset without overrides changed the file: %q; %v", got, err)
	}
}
