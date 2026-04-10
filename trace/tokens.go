package trace

import (
	"encoding/json"
	"fmt"
	"strings"
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

// ExtractLLMTokens pulls real input/output token counts from a provider response
// when the tool is a known LLM endpoint. Returns ok=false otherwise, so callers
// can fall back to EstimateTokens.
//
// Supported shapes:
//   - ollama.*          → prompt_eval_count / eval_count
//   - anthropic.*       → usage.input_tokens / usage.output_tokens
//   - openai.*          → usage.prompt_tokens / usage.completion_tokens
func ExtractLLMTokens(toolName string, result any) (in int, out int, ok bool) {
	m := asMap(result)
	if m == nil {
		return 0, 0, false
	}

	switch {
	case strings.HasPrefix(toolName, "ollama."):
		in = toInt(m["prompt_eval_count"])
		out = toInt(m["eval_count"])
	case strings.HasPrefix(toolName, "anthropic."):
		usage := asMap(m["usage"])
		if usage == nil {
			return 0, 0, false
		}
		in = toInt(usage["input_tokens"])
		out = toInt(usage["output_tokens"])
	case strings.HasPrefix(toolName, "openai."):
		usage := asMap(m["usage"])
		if usage == nil {
			return 0, 0, false
		}
		in = toInt(usage["prompt_tokens"])
		out = toInt(usage["completion_tokens"])
	default:
		return 0, 0, false
	}

	if in == 0 && out == 0 {
		return 0, 0, false
	}
	return in, out, true
}

// asMap coerces an any into a map[string]any if possible. MCP forwarders
// sometimes wrap responses, so we also unwrap a single "content" text payload
// holding a JSON string.
func asMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case *map[string]any:
		if m == nil {
			return nil
		}
		return *m
	}
	return nil
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
