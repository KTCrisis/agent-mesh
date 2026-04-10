package trace

import (
	"encoding/json"
	"fmt"
)

// EstimateTokens returns an approximate token count for a value.
// Uses the chars/4 heuristic (1 token ≈ 4 characters).
func EstimateTokens(v any) int {
	if v == nil {
		return 0
	}
	var chars int
	switch val := v.(type) {
	case string:
		chars = len(val)
	case map[string]any:
		data, _ := json.Marshal(val)
		chars = len(data)
	default:
		chars = len(fmt.Sprintf("%v", val))
	}
	tokens := chars / 4
	if tokens == 0 && chars > 0 {
		tokens = 1
	}
	return tokens
}
