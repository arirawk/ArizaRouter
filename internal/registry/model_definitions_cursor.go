package registry

// GetCursorModels returns the fallback Cursor model definitions.
// The executor prefers the live GetUsableModels listing and falls back to this list.
func GetCursorModels() []*ModelInfo {
	return []*ModelInfo{
		{ID: "composer-2", Object: "model", OwnedBy: "cursor", Type: "cursor", DisplayName: "Composer 2", ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: &ThinkingSupport{Max: 50000, DynamicAllowed: true}},
		{ID: "claude-4-sonnet", Object: "model", OwnedBy: "cursor", Type: "cursor", DisplayName: "Claude 4 Sonnet", ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: &ThinkingSupport{Max: 50000, DynamicAllowed: true}},
		{ID: "claude-3.5-sonnet", Object: "model", OwnedBy: "cursor", Type: "cursor", DisplayName: "Claude 3.5 Sonnet", ContextLength: 200000, MaxCompletionTokens: 8192},
		{ID: "gpt-4o", Object: "model", OwnedBy: "cursor", Type: "cursor", DisplayName: "GPT-4o", ContextLength: 128000, MaxCompletionTokens: 16384},
		{ID: "cursor-small", Object: "model", OwnedBy: "cursor", Type: "cursor", DisplayName: "Cursor Small", ContextLength: 200000, MaxCompletionTokens: 64000},
		{ID: "gemini-2.5-pro", Object: "model", OwnedBy: "cursor", Type: "cursor", DisplayName: "Gemini 2.5 Pro", ContextLength: 1000000, MaxCompletionTokens: 65536, Thinking: &ThinkingSupport{Max: 50000, DynamicAllowed: true}},
	}
}
