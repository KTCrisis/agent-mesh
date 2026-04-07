package match

import "path/filepath"

// Glob returns true if value matches the glob pattern.
// Supports * and ? wildcards via filepath.Match.
func Glob(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	matched, _ := filepath.Match(pattern, value)
	return matched
}

// GlobAny returns true if value matches any of the given patterns.
func GlobAny(patterns []string, value string) bool {
	for _, p := range patterns {
		if p == value || Glob(p, value) {
			return true
		}
	}
	return false
}
