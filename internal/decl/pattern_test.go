package decl

import "testing"

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
