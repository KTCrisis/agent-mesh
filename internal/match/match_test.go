package match

import "testing"

func TestGlob(t *testing.T) {
	tests := []struct {
		pattern, value string
		want           bool
	}{
		{"*", "anything", true},
		{"foo", "foo", true},
		{"foo", "bar", false},
		{"foo.*", "foo.bar", true},
		{"foo.*", "baz.bar", false},
		{"test-*", "test-agent", true},
	}
	for _, tt := range tests {
		if got := Glob(tt.pattern, tt.value); got != tt.want {
			t.Errorf("Glob(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
		}
	}
}

func TestGlobAny(t *testing.T) {
	tests := []struct {
		patterns []string
		value    string
		want     bool
	}{
		{[]string{"foo", "bar"}, "foo", true},
		{[]string{"foo", "bar"}, "baz", false},
		{[]string{"foo.*", "bar.*"}, "foo.get", true},
		{[]string{"*"}, "anything", true},
	}
	for _, tt := range tests {
		if got := GlobAny(tt.patterns, tt.value); got != tt.want {
			t.Errorf("GlobAny(%v, %q) = %v, want %v", tt.patterns, tt.value, got, tt.want)
		}
	}
}
