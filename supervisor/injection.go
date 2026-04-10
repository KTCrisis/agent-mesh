package supervisor

import (
	"fmt"
	"regexp"
	"strings"
)

// injectionPatterns are compiled at init time for performance.
var injectionPatterns []*regexp.Regexp

func init() {
	raw := []string{
		`(?i)ignore\s+(all\s+)?previous\s+instructions`,
		`(?i)you\s+are\s+now`,
		`(?i)system\s*prompt\s*:`,
		`(?i)override\s+(the\s+)?policy`,
		`(?i)pre-?approved`,
		`(?i)do\s+not\s+deny`,
		`(?i)do\s+not\s+escalate`,
		`(?i)must\s+(be\s+)?approv`,
		`(?i)pretend\s+(you\s+are|to\s+be)`,
		`(?i)act\s+as\s+(a\s+|an\s+)?`,
		`(?i)IMPORTANT\s*:\s*(this|override|approve|ignore)`,
		`(?i)confidence\s+should\s+be`,
	}
	injectionPatterns = make([]*regexp.Regexp, 0, len(raw))
	for _, r := range raw {
		injectionPatterns = append(injectionPatterns, regexp.MustCompile(r))
	}
}

// DetectInjection scans all string values in params for known prompt
// injection patterns. Returns true if any pattern matches.
func DetectInjection(params map[string]any) bool {
	if params == nil {
		return false
	}
	text := flattenToString(params)
	for _, pat := range injectionPatterns {
		if pat.MatchString(text) {
			return true
		}
	}
	return false
}

// flattenToString concatenates all string values from a map (recursively).
func flattenToString(m map[string]any) string {
	var b strings.Builder
	flattenInto(&b, m)
	return b.String()
}

func flattenInto(b *strings.Builder, v any) {
	switch val := v.(type) {
	case string:
		b.WriteString(val)
		b.WriteByte(' ')
	case map[string]any:
		for _, v2 := range val {
			flattenInto(b, v2)
		}
	case []any:
		for _, v2 := range val {
			flattenInto(b, v2)
		}
	default:
		fmt.Fprintf(b, "%v ", val)
	}
}
