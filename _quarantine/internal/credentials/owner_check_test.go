package credentials

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// Oracle: safeDirectoryStat accepts the configuration directory (mode 0700)
// and safeFileStat a credential or lock file (mode 0600, one link) only when
// the caller owns them. The same stat with another uid, such as root's or
// nobody's, is refused, so a directory or file another user owns is never
// read, written or locked. The stats come from a real directory and files the
// test creates; only the uid is made up, so the refusal runs without root.
func TestOwnerChecksRefuseAnotherUsersUID(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(directory, documentName)
	lock := filepath.Join(directory, lockName)
	for _, file := range []string{document, lock} {
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(file, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		path string
		safe func(*unix.Stat_t) bool
	}{
		{"directory", directory, safeDirectoryStat},
		{"credential file", document, safeFileStat},
		{"lock file", lock, safeFileStat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var own unix.Stat_t
			if err := unix.Lstat(c.path, &own); err != nil {
				t.Fatal(err)
			}
			if !c.safe(&own) {
				t.Fatalf("own %s (uid %d, mode %o): refused, want accepted", c.name, own.Uid, own.Mode)
			}
			for _, uid := range []uint32{own.Uid + 1, 0, 65534} {
				if uid == own.Uid {
					continue
				}
				foreign := own
				foreign.Uid = uid
				if c.safe(&foreign) {
					t.Errorf("%s owned by uid %d (you are %d): accepted, want refused", c.name, uid, own.Uid)
				}
			}
		})
	}
}
