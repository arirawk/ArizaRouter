package registry

import "strings"

// defaultCopilotClaudeContextLength is the conservative prompt token limit for
// Claude models served through GitHub Copilot. Individual plans enforce ~128K
// prompt tokens; the live /models endpoint overrides this per account.
const defaultCopilotClaudeContextLength = 128000

// GetGitHubCopilotModels returns conservative fallback models for GitHub Copilot.
// The executor prefers the live /models listing and falls back to this list.
func GetGitHubCopilotModels() []*ModelInfo {
	now := int64(1732752000) // 2024-11-27

	return []*ModelInfo{
		{
			ID:                  "claude-haiku-4.5",
			Object:              "model",
			Created:             now,
			OwnedBy:             "github-copilot",
			Type:                "github-copilot",
			DisplayName:         "Claude Haiku 4.5",
			Description:         "Anthropic Claude Haiku 4.5 via GitHub Copilot",
			ContextLength:       defaultCopilotClaudeContextLength,
			MaxCompletionTokens: 64000,
		},
		{
			ID:                  "gemini-2.5-pro",
			Object:              "model",
			Created:             now,
			OwnedBy:             "github-copilot",
			Type:                "github-copilot",
			DisplayName:         "Gemini 2.5 Pro",
			Description:         "Google Gemini 2.5 Pro via GitHub Copilot",
			ContextLength:       defaultCopilotClaudeContextLength,
			MaxCompletionTokens: 65536,
		},
		{
			ID:                  "gemini-3-pro-preview",
			Object:              "model",
			Created:             now,
			OwnedBy:             "github-copilot",
			Type:                "github-copilot",
			DisplayName:         "Gemini 3 Pro (Preview)",
			Description:         "Google Gemini 3 Pro Preview via GitHub Copilot",
			ContextLength:       defaultCopilotClaudeContextLength,
			MaxCompletionTokens: 65536,
		},
		{
			ID:                  "gemini-3.1-pro-preview",
			Object:              "model",
			Created:             now,
			OwnedBy:             "github-copilot",
			Type:                "github-copilot",
			DisplayName:         "Gemini 3.1 Pro (Preview)",
			Description:         "Google Gemini 3.1 Pro Preview via GitHub Copilot",
			ContextLength:       defaultCopilotClaudeContextLength,
			MaxCompletionTokens: 65536,
		},
		{
			ID:                  "gemini-3-flash-preview",
			Object:              "model",
			Created:             now,
			OwnedBy:             "github-copilot",
			Type:                "github-copilot",
			DisplayName:         "Gemini 3 Flash (Preview)",
			Description:         "Google Gemini 3 Flash Preview via GitHub Copilot",
			ContextLength:       defaultCopilotClaudeContextLength,
			MaxCompletionTokens: 65536,
		},
	}
}

// IsAllowedGitHubCopilotModel reports Copilot models that may be advertised even
// when GitHub's /models endpoint returns additional unsupported models.
func IsAllowedGitHubCopilotModel(modelID string) bool {
	switch strings.ToLower(strings.TrimSpace(modelID)) {
	case "claude-haiku-4.5",
		"gemini-2.5-pro",
		"gemini-3-pro-preview",
		"gemini-3.1-pro-preview",
		"gemini-3-flash-preview":
		return true
	default:
		return false
	}
}
