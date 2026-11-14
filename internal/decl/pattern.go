package decl

import (
	"fmt"
	"strings"
)

// ValidPattern says whether a path pattern can be used. A pattern is split
// at "/"; an empty segment, which a leading or a final "/" gives, is refused.
func ValidPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("a path pattern must not be empty")
	}
	for _, segment := range strings.Split(pattern, "/") {
		if segment == "" {
			return fmt.Errorf("the path pattern `%s` must be relative to the repository root, with no empty segment", pattern)
		}
	}
	return nil
}

// Match says whether a path, relative to the repository root with "/" as the
// separator, matches a pattern. "**" as a whole segment matches any number
// of segments, also none; "*" matches any run of characters inside one
// segment and "?" one character. Case is respected and nothing else is
// special.
func Match(pattern, path string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

// MatchAny says whether a path matches one of the patterns.
func MatchAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if Match(p, path) {
			return true
		}
	}
	return false
}

func matchSegments(pattern, path []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			rest := pattern[1:]
			for skip := 0; skip <= len(path); skip++ {
				if matchSegments(rest, path[skip:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 || !matchSegment([]rune(pattern[0]), []rune(path[0])) {
			return false
		}
		pattern, path = pattern[1:], path[1:]
	}
	return len(path) == 0
}

// matchSegment matches one segment, with the usual backtracking for "*".
func matchSegment(pattern, name []rune) bool {
	p, n := 0, 0
	star, mark := -1, 0
	for n < len(name) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, n
			p++
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == name[n]):
			p++
			n++
		case star >= 0:
			mark++
			p, n = star+1, mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
