package userconfig_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/userconfig"
)

const upDirMode = "private configuration directory must be owned by the current user with mode 0700"

// upDir makes a resolved <tmp>/cfg with mode, holding packs.json (0600) when body is non-empty.
func upDir(t *testing.T, mode os.FileMode, body string) string {
	base, err := filepath.EvalSymlinks(t.TempDir())
	dir := filepath.Join(base, "cfg")
	if err != nil || os.Mkdir(dir, 0o700) != nil {
		t.Fatal("cannot make the configuration directory")
	}
	if body != "" {
		upWrite(t, dir, body, 0o600)
	}
	if os.Chmod(dir, mode) != nil {
		t.Fatal("chmod failed")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	return dir
}

func upWrite(t *testing.T, dir, body string, mode os.FileMode) {
	p := filepath.Join(dir, "packs.json")
	if os.WriteFile(p, []byte(body), mode) != nil || os.Chmod(p, mode) != nil {
		t.Fatal("cannot write packs.json")
	}
}

func upLink(t *testing.T, target string) string {
	link := filepath.Join(filepath.Dir(target), "link")
	if os.Symlink(target, link) != nil {
		t.Fatal("symlink failed")
	}
	return link
}

// upMatch: exact, or prefix when want ends with ':', or contained when it starts with '~'.
func upMatch(err error, want string) bool {
	switch {
	case err == nil:
		return false
	case strings.HasPrefix(want, "~"):
		return strings.Contains(err.Error(), want[1:])
	case strings.HasSuffix(want, ":"):
		return strings.HasPrefix(err.Error(), want)
	}
	return err.Error() == want
}

// A12: SelectedPacks errors, in check order, always with (nil, false).
func TestSelectedPacksErrors(t *testing.T) {
	js := func(s string) func(*testing.T) string { return func(t *testing.T) string { return upDir(t, 0o700, s) } }
	cases := []struct {
		name string
		dir  func(*testing.T) string
		want string
	}{
		{"relative even when absent", func(*testing.T) string { return "cfg" }, "private configuration requires an absolute clean path"},
		{"absolute but unclean, even when absent", func(t *testing.T) string { return upDir(t, 0o700, "") + "/../cfg" }, "private configuration requires an absolute clean path"},
		{"non-directory component", func(t *testing.T) string {
			d := upDir(t, 0o700, `{"version":1}`)
			return filepath.Join(d, "packs.json", "cfg")
		}, "~unsafe directory"},
		{"symlink component", func(t *testing.T) string { return upLink(t, upDir(t, 0o700, `{"version":1}`)) }, "~unsafe directory"},
		{"directory 0755, before decoding", func(t *testing.T) string { return upDir(t, 0o755, "{") }, upDirMode},
		{"file 0644, before decoding", func(t *testing.T) string {
			d := upDir(t, 0o700, "")
			upWrite(t, d, "{", 0o644)
			return d
		}, "pack selection must be a current-user regular file with mode 0600"},
		{"file is a symlink", func(t *testing.T) string {
			d := upDir(t, 0o700, "")
			target := filepath.Join(filepath.Dir(d), "target.json")
			if os.WriteFile(target, []byte(`{"version":1}`), 0o600) != nil || os.Symlink(target, filepath.Join(d, "packs.json")) != nil {
				t.Fatal("setup failed")
			}
			return d
		}, "pack selection must be a current-user regular file with mode 0600"},
		{"file is a directory", func(t *testing.T) string {
			d := upDir(t, 0o700, "")
			if os.Mkdir(filepath.Join(d, "packs.json"), 0o700) != nil {
				t.Fatal("setup failed")
			}
			return d
		}, "pack selection must be a current-user regular file with mode 0600"},
		{"file over 64KiB", js(`{"version":1,"packs":[]}` + strings.Repeat(" ", 64<<10)), "pack selection must be a current-user regular file with mode 0600"},
		{"invalid JSON", js("{"), "invalid pack selection:"},
		{"unknown member", js(`{"version":1,"packs":[],"extra":1}`), "invalid pack selection:"},
		{"duplicate member", js(`{"version":1,"version":1,"packs":[]}`), "invalid pack selection:"},
		{"wrong packs type", js(`{"version":1,"packs":"go"}`), "invalid pack selection:"},
		{"wrong id type", js(`{"version":1,"packs":[1]}`), "invalid pack selection:"},
		{"version absent", js(`{"packs":["go"]}`), "pack selection requires version 1"},
		{"version null", js(`{"version":null,"packs":[]}`), "pack selection requires version 1"},
		{"version 2", js(`{"version":2,"packs":[]}`), "pack selection version 2 is not supported; this cw reads version 1"},
		{"invalid id", js(`{"version":1,"packs":["go","Bad_Id"]}`), `invalid pack id "Bad_Id" in pack selection`},
		{"repeated id", js(`{"version":1,"packs":["go","zz","go"]}`), `pack "go" is listed twice in pack selection`},
	}
	for _, c := range cases {
		ids, found, err := userconfig.SelectedPacks(c.dir(t))
		if !upMatch(err, c.want) || found || ids != nil {
			t.Errorf("A12 violated (%s): got (%q, %v, %v), want (nil, false, %q)", c.name, ids, found, err, c.want)
		}
	}
}

// A12: an absent file reads as (nil, false, nil) without creating or checking
// anything; valid documents give sorted ids or nil.
func TestSelectedPacksValues(t *testing.T) {
	missing := filepath.Join(upDir(t, 0o755, ""), "nope", "cfg")
	for _, dir := range []string{missing, filepath.Dir(filepath.Dir(missing))} {
		if ids, found, err := userconfig.SelectedPacks(dir); ids != nil || found || err != nil {
			t.Fatalf("A12 violated: absent file in %s gives (%q, %v, %v)", dir, ids, found, err)
		}
	}
	if _, err := os.Lstat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatalf("A12 violated: reading an absent selection created a directory")
	}
	for body, want := range map[string][]string{
		`{"version":1,"packs":["zz","go","a-1"]}`: {"a-1", "go", "zz"}, `{"version":1,"packs":[]}`: nil,
		`{"version":1,"packs":null}`: nil, `{"version":1}`: nil,
	} {
		ids, found, err := userconfig.SelectedPacks(upDir(t, 0o700, body))
		if err != nil || !found || !reflect.DeepEqual(ids, want) {
			t.Errorf("A12 violated: %s gives (%#v, %v, %v), want (%#v, true, nil)", body, ids, found, err, want)
		}
	}
}

// A13: SavePacks writes {version:1, packs:[sorted distinct]} plus a newline at
// 0600, creates missing directories at 0700, leaves no other file, round-trips
// and is idempotent.
func TestSavePacks(t *testing.T) {
	for i, c := range []struct{ ids, doc, back []string }{
		{[]string{"zz", "go", "zz", "unknown-pack"}, []string{"go", "unknown-pack", "zz"}, []string{"go", "unknown-pack", "zz"}},
		{nil, []string{}, nil},
		{[]string{}, []string{}, nil},
	} {
		dir := filepath.Join(upDir(t, 0o700, ""), "new", "cfg")
		for n := 0; n < 2; n++ {
			if err := userconfig.SavePacks(dir, c.ids); err != nil {
				t.Fatalf("A13 violated (%d): %v", i, err)
			}
			data, _ := os.ReadFile(filepath.Join(dir, "packs.json"))
			var doc map[string]json.RawMessage
			var packs []string
			if json.Unmarshal(data, &doc) != nil || len(doc) != 2 || string(doc["version"]) != "1" ||
				json.Unmarshal(doc["packs"], &packs) != nil || packs == nil || !reflect.DeepEqual(packs, c.doc) || !bytes.HasSuffix(data, []byte("\n")) {
				t.Fatalf("A13 violated (%d): document %q", i, data)
			}
			if ids, found, err := userconfig.SelectedPacks(dir); err != nil || !found || !reflect.DeepEqual(ids, c.back) {
				t.Fatalf("A13 violated (%d): read back (%#v, %v, %v)", i, ids, found, err)
			}
		}
		for d, want := range map[string]os.FileMode{filepath.Dir(dir): 0o700, dir: 0o700, filepath.Join(dir, "packs.json"): 0o600} {
			if info, err := os.Lstat(d); err != nil || info.Mode().Perm() != want || (want == 0o600) != info.Mode().IsRegular() {
				t.Fatalf("A13 violated: %s is %v, %v; want mode %v", d, info, err, want)
			}
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Fatalf("A13 violated: directory holds %d entries, want only packs.json", len(entries))
		}
	}
}

// A13: each failure leaves the previous file and the directory listing unchanged,
// so nothing is created below a symlink or a non-directory component.
func TestSavePacksErrors(t *testing.T) {
	prev := `{"version":1,"packs":["old"]}`
	cases := []struct {
		name  string
		setup func(t *testing.T) (dir, watch string)
		ids   []string
		want  string
	}{
		{"invalid id", func(t *testing.T) (string, string) { d := upDir(t, 0o700, prev); return d, d }, []string{"go", "Bad_Id"}, `invalid pack id "Bad_Id"`},
		{"directory 0755", func(t *testing.T) (string, string) { d := upDir(t, 0o755, prev); return d, d }, []string{"go"}, upDirMode},
		{"symlink component", func(t *testing.T) (string, string) {
			d := upDir(t, 0o700, prev)
			return filepath.Join(upLink(t, d), "sub"), d
		}, []string{"go"}, "~unsafe directory"},
		{"symlink as the directory", func(t *testing.T) (string, string) { d := upDir(t, 0o700, prev); return upLink(t, d), d }, []string{"go"}, "~unsafe directory"},
		{"non-directory component", func(t *testing.T) (string, string) {
			d := upDir(t, 0o700, prev)
			return filepath.Join(d, "packs.json", "sub"), d
		}, []string{"go"}, "~unsafe directory"},
	}
	for _, c := range cases {
		dir, watch := c.setup(t)
		before, _ := os.ReadDir(watch)
		if err := userconfig.SavePacks(dir, c.ids); !upMatch(err, c.want) {
			t.Errorf("A13 violated (%s): error %v, want %q", c.name, err, c.want)
		}
		after, _ := os.ReadDir(watch)
		got, _ := os.ReadFile(filepath.Join(watch, "packs.json"))
		if info, err := os.Lstat(filepath.Join(watch, "packs.json")); err != nil || !info.Mode().IsRegular() || string(got) != prev || len(after) != len(before) {
			t.Errorf("A13 violated (%s): previous file now %q, %d entries (was %d)", c.name, got, len(after), len(before))
		}
	}
}
