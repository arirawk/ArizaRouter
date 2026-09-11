package registry

import "strings"

// gitLabCatalogEntry describes a built-in GitLab Duo agentic chat model.
type gitLabCatalogEntry struct {
	ID          string
	DisplayName string
	Provider    string
}

// gitLabAgenticCatalog lists the GitLab Duo agentic chat models that are
// available regardless of what the account's direct-access metadata reports.
var gitLabAgenticCatalog = []gitLabCatalogEntry{
	{ID: "duo-chat-gpt-5-1", DisplayName: "GitLab Duo (GPT-5.1)", Provider: "openai"},
	{ID: "duo-chat-opus-4-6", DisplayName: "GitLab Duo (Claude Opus 4.6)", Provider: "anthropic"},
	{ID: "duo-chat-opus-4-5", DisplayName: "GitLab Duo (Claude Opus 4.5)", Provider: "anthropic"},
	{ID: "duo-chat-sonnet-4-6", DisplayName: "GitLab Duo (Claude Sonnet 4.6)", Provider: "anthropic"},
	{ID: "duo-chat-sonnet-4-5", DisplayName: "GitLab Duo (Claude Sonnet 4.5)", Provider: "anthropic"},
	{ID: "duo-chat-gpt-5-mini", DisplayName: "GitLab Duo (GPT-5 Mini)", Provider: "openai"},
	{ID: "duo-chat-gpt-5-2", DisplayName: "GitLab Duo (GPT-5.2)", Provider: "openai"},
	{ID: "duo-chat-gpt-5-2-codex", DisplayName: "GitLab Duo (GPT-5.2 Codex)", Provider: "openai"},
	{ID: "duo-chat-gpt-5-codex", DisplayName: "GitLab Duo (GPT-5 Codex)", Provider: "openai"},
	{ID: "duo-chat-haiku-4-5", DisplayName: "GitLab Duo (Claude Haiku 4.5)", Provider: "anthropic"},
}

// GitLabModelAliases maps alias model IDs onto the upstream GitLab Duo model.
var GitLabModelAliases = map[string]string{
	"duo-chat-haiku-4-6": "duo-chat-haiku-4-5",
}

// GetGitLabModels returns the built-in GitLab Duo model definitions: the
// stable "gitlab-duo" alias, the agentic chat catalog and the alias entries.
// Models discovered from an account's direct-access metadata are appended by
// the executor at registration time.
func GetGitLabModels() []*ModelInfo {
	models := make([]*ModelInfo, 0, len(gitLabAgenticCatalog)+len(GitLabModelAliases)+1)
	models = append(models, newGitLabModel("gitlab-duo", "GitLab Duo", "gitlab"))
	for _, entry := range gitLabAgenticCatalog {
		models = append(models, newGitLabModel(entry.ID, entry.DisplayName, entry.Provider))
	}
	for alias, upstream := range GitLabModelAliases {
		provider := InferGitLabProviderFromModel(upstream)
		displayName := "GitLab Duo Alias"
		if provider != "" {
			displayName = "GitLab Duo Alias (" + provider + ")"
		}
		models = append(models, newGitLabModel(alias, displayName, provider))
	}
	return models
}

// InferGitLabProviderFromModel guesses the upstream vendor ("anthropic" or
// "openai") from a GitLab Duo model name; it returns "" when unknown.
func InferGitLabProviderFromModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(model, "claude"), strings.Contains(model, "opus"), strings.Contains(model, "sonnet"), strings.Contains(model, "haiku"):
		return "anthropic"
	case strings.Contains(model, "gpt"), strings.Contains(model, "o1"), strings.Contains(model, "o3"), strings.Contains(model, "o4"):
		return "openai"
	default:
		return ""
	}
}

func newGitLabModel(id, displayName, provider string) *ModelInfo {
	return &ModelInfo{
		ID:          id,
		Object:      "model",
		Created:     1732752000,
		OwnedBy:     "gitlab",
		Type:        "gitlab",
		DisplayName: displayName,
		Description: provider,
		UserDefined: true,
	}
}
