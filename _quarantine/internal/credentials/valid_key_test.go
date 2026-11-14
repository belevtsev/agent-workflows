package credentials_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
)

// Oracle: `cw keys set --help` (keys.go): a key is one line of
// text without spaces or control characters, at most 64 KiB. The empty key,
// on which the store's rule and cw keys set's former rule disagreed, is
// refused; 64 KiB is the last length accepted.
func TestValidKeyAppliesOneLengthAndCharacterRule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{"empty", "", false},
		{"one byte", "k", true},
		{"64 KiB", strings.Repeat("k", 64<<10), true},
		{"64 KiB plus one byte", strings.Repeat("k", 64<<10+1), false},
		{"64 KiB of two-byte runes", strings.Repeat("é", 32<<10), true},
		{"over 64 KiB of two-byte runes", strings.Repeat("é", 32<<10+1), false},
		{"inner space", "sk-a b", false},
		{"trailing line break", "sk-ab\n", false},
		{"control character", "sk-a\x00b", false},
		{"invalid UTF-8", "sk-\xff", false},
	} {
		if got := credentials.ValidKey(tc.value); got != tc.want {
			t.Errorf("%s: ValidKey = %v, want %v", tc.name, got, tc.want)
		}
	}
}
