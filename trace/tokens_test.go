package trace

import "testing"

func TestExtractLLMTokens(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		result any
		wantIn int
		wantOu int
		wantOk bool
	}{
		{
			name: "ollama chat",
			tool: "ollama.chat",
			result: map[string]any{
				"prompt_eval_count": float64(42),
				"eval_count":        float64(137),
			},
			wantIn: 42, wantOu: 137, wantOk: true,
		},
		{
			name: "ollama generate int",
			tool: "ollama.generate",
			result: map[string]any{
				"prompt_eval_count": 10,
				"eval_count":        20,
			},
			wantIn: 10, wantOu: 20, wantOk: true,
		},
		{
			name: "anthropic messages",
			tool: "anthropic.messages",
			result: map[string]any{
				"usage": map[string]any{
					"input_tokens":  float64(100),
					"output_tokens": float64(250),
				},
			},
			wantIn: 100, wantOu: 250, wantOk: true,
		},
		{
			name: "openai chat completion",
			tool: "openai.chat_completions",
			result: map[string]any{
				"usage": map[string]any{
					"prompt_tokens":     float64(15),
					"completion_tokens": float64(35),
				},
			},
			wantIn: 15, wantOu: 35, wantOk: true,
		},
		{
			name:   "unknown tool returns not ok",
			tool:   "weather.forecast",
			result: map[string]any{"temp": 20},
			wantOk: false,
		},
		{
			name:   "ollama with zero counts returns not ok",
			tool:   "ollama.chat",
			result: map[string]any{"prompt_eval_count": 0, "eval_count": 0},
			wantOk: false,
		},
		{
			name:   "nil result",
			tool:   "ollama.chat",
			result: nil,
			wantOk: false,
		},
		{
			name:   "anthropic missing usage",
			tool:   "anthropic.messages",
			result: map[string]any{"content": "hello"},
			wantOk: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, out, ok := ExtractLLMTokens(c.tool, c.result)
			if ok != c.wantOk {
				t.Fatalf("ok: got %v, want %v", ok, c.wantOk)
			}
			if ok && (in != c.wantIn || out != c.wantOu) {
				t.Fatalf("tokens: got (%d,%d), want (%d,%d)", in, out, c.wantIn, c.wantOu)
			}
		})
	}
}
