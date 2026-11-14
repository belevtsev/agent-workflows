package decl

import (
	"strings"
	"testing"
	"time"
)

func TestValidPattern(t *testing.T) {
	cases := []struct {
		pattern string
		ok      bool
	}{
		{"src/**", true},
		{"**/*.go", true},
		{"go.mod", true},
		{"a/b/c?.txt", true},
		{"**", true},
		{"", false},
		{"/src", false},
		{"src/", false},
		{"src//a", false},
		{"/", false},
	}
	for _, tc := range cases {
		err := ValidPattern(tc.pattern)
		if (err == nil) != tc.ok {
			t.Errorf("ValidPattern(%q) = %v, want ok=%v", tc.pattern, err, tc.ok)
		}
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// ** as a whole segment crosses directories, also none.
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"**/*.go", "main.go.bak", false},
		{"**", "a", true},
		{"**", "a/b/c", true},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "a/x/y/c", false},
		// dir/** covers everything below, and dir itself.
		{"docs/**", "docs/a.md", true},
		{"docs/**", "docs/x/y.md", true},
		{"docs/**", "docs", true},
		{"docs/**", "docs.md", false},
		{"docs/**", "other/docs/a.md", false},
		{"vendor/lib/**", "vendor/lib", true},
		// A plain name matches at the root only.
		{"go.mod", "go.mod", true},
		{"go.mod", "tools/go.mod", false},
		{"src", "src/a.txt", false},
		// * and ? stay inside one segment.
		{"*.go", "main.go", true},
		{"*.go", "a/main.go", false},
		{"src/*", "src/a", true},
		{"src/*", "src/a/b", false},
		{"src/*.txt", "src/.txt", true},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
		{"*", "", true},
		{"file?.txt", "file1.txt", true},
		{"file?.txt", "file.txt", false},
		{"file?.txt", "file12.txt", false},
		{"a?b", "a/b", false},
		{"ü?.txt", "üx.txt", true},
		{"?", "ü", true},
		// ** inside a segment is not special beyond two stars.
		{"a**", "abc", true},
		{"a**", "a/b", false},
		// Case is respected.
		{"README.md", "readme.md", false},
		{"Src/**", "src/a", false},
		// Nothing else is special.
		{"[ab].txt", "a.txt", false},
		{"[ab].txt", "[ab].txt", true},
		{"a\\*", "a\\x", true},
		{"{a,b}", "a", false},
	}
	for _, tc := range cases {
		if got := Match(tc.pattern, tc.path); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestMatchAny(t *testing.T) {
	patterns := []string{"src/**", "unit.sh"}
	cases := []struct {
		path string
		want bool
	}{
		{"src/app.txt", true},
		{"unit.sh", true},
		{"lint.sh", false},
		{"tools/unit.sh", false},
	}
	for _, tc := range cases {
		if got := MatchAny(patterns, tc.path); got != tc.want {
			t.Errorf("MatchAny(%q, %q) = %v, want %v", patterns, tc.path, got, tc.want)
		}
	}
	if MatchAny(nil, "a") {
		t.Error("MatchAny(nil) matched")
	}
}

// naive is the plain recursive meaning of a pattern: "**" matches any number
// of segments. Its work grows without bound with the number of "**", which
// is why Match does not use it; here it is the oracle.
func naive(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(path); skip++ {
			if naive(pattern[1:], path[skip:]) {
				return true
			}
		}
		return false
	}
	return len(path) > 0 && matchSegment([]rune(pattern[0]), []rune(path[0])) && naive(pattern[1:], path[1:])
}

// Match gives the same answer as the recursive meaning for every pattern and
// path of up to five segments over a small alphabet.
func TestMatchAgreesWithTheRecursiveMeaning(t *testing.T) {
	words := []string{"a", "b", "**", "*", "a*"}
	var all func(n int) [][]string
	all = func(n int) [][]string {
		if n == 0 {
			return [][]string{nil}
		}
		var out [][]string
		for _, rest := range all(n - 1) {
			for _, w := range words {
				out = append(out, append([]string{w}, rest...))
			}
		}
		return out
	}
	for size := 1; size <= 5; size++ {
		for _, pattern := range all(size) {
			for pathSize := 1; pathSize <= 4; pathSize++ {
				for _, path := range all(pathSize) {
					if strings.Contains(strings.Join(path, "/"), "*") {
						continue
					}
					if got, want := matchSegments(pattern, path), naive(pattern, path); got != want {
						t.Fatalf("Match(%q, %q) = %v, the recursive meaning gives %v", strings.Join(pattern, "/"), strings.Join(path, "/"), got, want)
					}
				}
			}
		}
	}
}

// A pattern with many "**" is matched in bounded work. With the recursive
// meaning this call does not end within hours.
func TestMatchIsBoundedForManyDoubleStars(t *testing.T) {
	pattern := strings.Repeat("**/", 40) + "zz"
	path := strings.Repeat("d/", 200) + "file"
	done := make(chan bool, 1)
	go func() { done <- Match(pattern, path) }()
	select {
	case got := <-done:
		if got {
			t.Error("the pattern matched a path that does not end in zz")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Match had not answered after 30 s")
	}
}
