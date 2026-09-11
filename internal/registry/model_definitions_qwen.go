package registry

// GetQwenModels returns the static Qwen Code (portal.qwen.ai) model definitions.
// Qwen exposes a single "coder-model" alias that resolves to the current
// Qwen coding model on the upstream side.
func GetQwenModels() []*ModelInfo {
	return []*ModelInfo{
		{
			ID:                  "coder-model",
			Object:              "model",
			Created:             1771171200,
			OwnedBy:             "qwen",
			Type:                "qwen",
			DisplayName:         "Qwen 3.6 Plus",
			Version:             "3.6",
			Description:         "efficient hybrid model with leading coding performance",
			ContextLength:       1048576,
			MaxCompletionTokens: 65536,
			SupportedParameters: []string{"temperature", "top_p", "max_tokens", "stream", "stop"},
		},
	}
}
