package registry

import (
	"strings"
	"testing"
)

func TestGetStaticModelDefinitionsByChannelSupportsGeminiInteractions(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("gemini-interactions")
	if len(models) == 0 {
		t.Fatal("GetStaticModelDefinitionsByChannel(gemini-interactions) returned no models")
	}
}

func TestModelOverrideHeadersFromEmbeddedModels(t *testing.T) {
	const wantUA = "codex-tui/0.153.3 (Mac OS 26.5.1; arm64) iTerm.app/3.6.11 (codex-tui; 0.153.3)"
	got := ModelOverrideHeaders("gpt-5.6-luna")
	if got == nil {
		t.Fatal("ModelOverrideHeaders(gpt-5.6-luna) = nil, want headers")
	}
	if got["user-agent"] != wantUA {
		t.Fatalf("user-agent = %q, want %q", got["user-agent"], wantUA)
	}
	if got := ModelOverrideHeaders("gpt-5.4"); got != nil {
		t.Fatalf("ModelOverrideHeaders(gpt-5.4) = %#v, want nil", got)
	}
}

func TestGeminiVertexModelsUseFlashLiteReleaseID(t *testing.T) {
	const releaseID = "gemini-3.1-flash-lite"
	const previewID = releaseID + "-preview"

	for _, model := range GetGeminiVertexModels() {
		if model == nil {
			continue
		}
		if model.ID == previewID {
			t.Fatalf("Vertex model ID = %q, want release ID %q", model.ID, releaseID)
		}
		if model.ID == releaseID {
			return
		}
	}

	t.Fatalf("Vertex models do not contain %q", releaseID)
}

func TestWithXAIBuiltinsIncludesImage20(t *testing.T) {
	models := WithXAIBuiltins(nil)
	for _, model := range models {
		if model != nil && model.ID == xaiBuiltinImage20ModelID {
			if model.Created != 1786060800 {
				t.Fatalf("created = %d, want 1786060800 (2026-08-07)", model.Created)
			}
			return
		}
	}
	t.Fatalf("expected xAI builtin model %s", xaiBuiltinImage20ModelID)
}

func TestWithXAIBuiltinsIncludesVideo15GAAndPreviewAlias(t *testing.T) {
	models := WithXAIBuiltins(nil)
	foundGA := false
	foundPreviewAlias := false

	for _, model := range models {
		if model == nil {
			continue
		}
		if model.ID == xaiBuiltinVideo15ModelID {
			foundGA = true
		}
		if model.ID == xaiBuiltinVideo15PreviewID {
			foundPreviewAlias = true
		}
	}

	if !foundGA {
		t.Fatalf("expected xAI builtin model %s", xaiBuiltinVideo15ModelID)
	}
	if !foundPreviewAlias {
		t.Fatalf("expected xAI builtin compatibility alias %s", xaiBuiltinVideo15PreviewID)
	}
}

func TestAntigravityWebSearchModelForRequiresRequestedModelCapability(t *testing.T) {
	registryRef := GetGlobalRegistry()
	registryRef.RegisterClient("test-antigravity-websearch-route", "antigravity", []*ModelInfo{
		{ID: "gemini-route-test"},
		{ID: "gemini-web-search-test", SupportsWebSearch: true},
	})
	registryRef.RegisterClient("test-gemini-websearch-route", "gemini", []*ModelInfo{
		{ID: "gemini-cross-provider-route"},
		{ID: "gemini-cross-provider-search", SupportsWebSearch: true},
	})
	t.Cleanup(func() {
		registryRef.UnregisterClient("test-antigravity-websearch-route")
		registryRef.UnregisterClient("test-gemini-websearch-route")
	})

	if got := AntigravityWebSearchModelFor("gemini-route-test"); got != "" {
		t.Fatalf("route model without web search support should not get fallback model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-route-test(high)"); got != "" {
		t.Fatalf("suffix route model without web search support should not get fallback model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-web-search-test"); got != "gemini-web-search-test" {
		t.Fatalf("AntigravityWebSearchModelFor capable model = %q, want itself", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-cross-provider-route"); got != "" {
		t.Fatalf("cross-provider model should not get Antigravity web search model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("unknown-model"); got != "" {
		t.Fatalf("unknown model should not get Antigravity web search model, got %q", got)
	}
}

func TestWithCodexBuiltinsIncludesImage25Models(t *testing.T) {
	models := WithCodexBuiltins(nil)
	expectedModels := map[string]string{
		"gpt-image-2.5-flare":    "GPT Image 2.5 Flare",
		"gpt-image-2.5-sunburst": "GPT Image 2.5 Sunburst",
		"gpt-image-2.5":          "GPT Image 2.5",
	}

	found := make(map[string]*ModelInfo)
	for _, model := range models {
		if model != nil {
			if _, ok := expectedModels[model.ID]; ok {
				found[model.ID] = model
			}
		}
	}

	for id, wantDisplayName := range expectedModels {
		model, ok := found[id]
		if !ok {
			t.Fatalf("expected builtin model %s in WithCodexBuiltins", id)
		}
		if model.DisplayName != wantDisplayName {
			t.Errorf("model %s DisplayName = %q, want %q", id, model.DisplayName, wantDisplayName)
		}
		if model.Object != "model" {
			t.Errorf("model %s Object = %q, want model", id, model.Object)
		}
		if model.OwnedBy != "openai" {
			t.Errorf("model %s OwnedBy = %q, want openai", id, model.OwnedBy)
		}
		if model.Type != "openai" {
			t.Errorf("model %s Type = %q, want openai", id, model.Type)
		}
		if model.Version != id {
			t.Errorf("model %s Version = %q, want %q", id, model.Version, id)
		}
		if model.Created != 1704067200 {
			t.Errorf("model %s Created = %d, want 1704067200", id, model.Created)
		}
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsGitHubCopilot(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("github-copilot")
	if len(models) != 5 {
		t.Fatalf("GetStaticModelDefinitionsByChannel(github-copilot) returned %d models, want 5", len(models))
	}
	for _, m := range models {
		if m.OwnedBy != "github-copilot" || m.Type != "github-copilot" {
			t.Fatalf("model %s owned_by=%q type=%q, want github-copilot", m.ID, m.OwnedBy, m.Type)
		}
		if !IsAllowedGitHubCopilotModel(m.ID) {
			t.Fatalf("static model %s is not in the allow list", m.ID)
		}
	}
	if info := LookupStaticModelInfo("gemini-3-flash-preview"); info == nil {
		t.Fatal("LookupStaticModelInfo(gemini-3-flash-preview) = nil")
	}
	if IsAllowedGitHubCopilotModel("gpt-5.5") {
		t.Fatal("gpt-5.5 should not be an allowed GitHub Copilot model")
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsCursor(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("cursor")
	if len(models) != 6 {
		t.Fatalf("GetStaticModelDefinitionsByChannel(cursor) returned %d models, want 6", len(models))
	}
	for _, m := range models {
		if m.OwnedBy != "cursor" || m.Type != "cursor" {
			t.Fatalf("model %s owned_by=%q type=%q, want cursor", m.ID, m.OwnedBy, m.Type)
		}
	}
	if info := LookupStaticModelInfo("composer-2"); info == nil {
		t.Fatal("LookupStaticModelInfo(composer-2) = nil")
	} else if info.Thinking == nil || !info.Thinking.DynamicAllowed {
		t.Fatalf("composer-2 thinking = %+v, want dynamic thinking support", info.Thinking)
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsKiro(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("kiro")
	if len(models) == 0 {
		t.Fatal("GetStaticModelDefinitionsByChannel(kiro) returned no models")
	}
	seen := make(map[string]bool, len(models))
	for _, m := range models {
		if m.OwnedBy != "aws" || m.Type != "kiro" {
			t.Fatalf("model %s owned_by=%q type=%q, want aws/kiro", m.ID, m.OwnedBy, m.Type)
		}
		if !strings.HasPrefix(m.ID, "kiro-") {
			t.Fatalf("model %s does not carry the kiro- prefix", m.ID)
		}
		if seen[m.ID] {
			t.Fatalf("duplicate kiro model id %s", m.ID)
		}
		seen[m.ID] = true
	}
	for _, want := range []string{"kiro-auto", "kiro-claude-sonnet-4-5", "kiro-claude-opus-4-5-agentic"} {
		if !seen[want] {
			t.Fatalf("expected kiro model %s in static definitions", want)
		}
	}
	if info := LookupStaticModelInfo("kiro-claude-sonnet-4-5"); info == nil || info.Type != "kiro" {
		t.Fatalf("LookupStaticModelInfo(kiro-claude-sonnet-4-5) = %+v", info)
	}
	amazonq := GetStaticModelDefinitionsByChannel("amazonq")
	if len(amazonq) == 0 {
		t.Fatal("GetStaticModelDefinitionsByChannel(amazonq) returned no models")
	}
	for _, m := range amazonq {
		if m.Type != "kiro" || !strings.HasPrefix(m.ID, "amazonq-") {
			t.Fatalf("amazonq model %s type=%q, want kiro executor type with amazonq- prefix", m.ID, m.Type)
		}
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsQwen(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("qwen")
	if len(models) != 1 {
		t.Fatalf("GetStaticModelDefinitionsByChannel(qwen) returned %d models, want 1", len(models))
	}
	for _, m := range models {
		if m.OwnedBy != "qwen" || m.Type != "qwen" {
			t.Fatalf("model %s owned_by=%q type=%q, want qwen", m.ID, m.OwnedBy, m.Type)
		}
	}
	if info := LookupStaticModelInfo("coder-model"); info == nil {
		t.Fatal("LookupStaticModelInfo(coder-model) = nil")
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsIFlow(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("iflow")
	if len(models) != 15 {
		t.Fatalf("GetStaticModelDefinitionsByChannel(iflow) returned %d models, want 15", len(models))
	}
	for _, m := range models {
		if m.OwnedBy != "iflow" || m.Type != "iflow" {
			t.Fatalf("model %s owned_by=%q type=%q, want iflow", m.ID, m.OwnedBy, m.Type)
		}
	}
	info := LookupStaticModelInfo("glm-4.6")
	if info == nil {
		t.Fatal("LookupStaticModelInfo(glm-4.6) = nil")
	}
	if info.Thinking == nil || len(info.Thinking.Levels) == 0 {
		t.Fatalf("glm-4.6 thinking = %+v, want level-based thinking support", info.Thinking)
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsCodeBuddy(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("codebuddy")
	if len(models) != 10 {
		t.Fatalf("GetStaticModelDefinitionsByChannel(codebuddy) returned %d models, want 10", len(models))
	}
	for _, m := range models {
		if m.Type != "codebuddy" {
			t.Fatalf("model %s type=%q, want codebuddy", m.ID, m.Type)
		}
	}
	intl := GetStaticModelDefinitionsByChannel("codebuddy-intl")
	if len(intl) == 0 {
		t.Fatal("GetStaticModelDefinitionsByChannel(codebuddy-intl) returned no models")
	}
	for _, m := range intl {
		if m.Type != "codebuddy-intl" {
			t.Fatalf("model %s type=%q, want codebuddy-intl", m.ID, m.Type)
		}
	}
	if info := LookupStaticModelInfo("glm-5v-turbo"); info == nil {
		t.Fatal("LookupStaticModelInfo(glm-5v-turbo) = nil")
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsCline(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("cline")
	if len(models) == 0 {
		t.Fatal("GetStaticModelDefinitionsByChannel(cline) returned no models")
	}
	for _, m := range models {
		if m.OwnedBy != "cline" || m.Type != "cline" {
			t.Fatalf("model %s owned_by=%q type=%q, want cline", m.ID, m.OwnedBy, m.Type)
		}
	}
	if info := LookupStaticModelInfo("cline/auto"); info == nil || info.Type != "cline" {
		t.Fatalf("LookupStaticModelInfo(cline/auto) = %+v", info)
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsKilo(t *testing.T) {
	for _, channel := range []string{"kilo", "kilocode"} {
		models := GetStaticModelDefinitionsByChannel(channel)
		if len(models) == 0 {
			t.Fatalf("GetStaticModelDefinitionsByChannel(%s) returned no models", channel)
		}
		for _, m := range models {
			if m.OwnedBy != "kilo" || m.Type != "kilo" {
				t.Fatalf("model %s owned_by=%q type=%q, want kilo", m.ID, m.OwnedBy, m.Type)
			}
		}
	}
	if info := LookupStaticModelInfo("kilo/auto"); info == nil || info.Type != "kilo" {
		t.Fatalf("LookupStaticModelInfo(kilo/auto) = %+v", info)
	}
}

func TestGetStaticModelDefinitionsByChannelSupportsGitLab(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("gitlab")
	if len(models) < 5 {
		t.Fatalf("GetStaticModelDefinitionsByChannel(gitlab) returned %d models, want the agentic catalog", len(models))
	}
	seen := map[string]bool{}
	for _, m := range models {
		if m.OwnedBy != "gitlab" || m.Type != "gitlab" {
			t.Fatalf("model %s owned_by=%q type=%q, want gitlab", m.ID, m.OwnedBy, m.Type)
		}
		seen[m.ID] = true
	}
	for _, want := range []string{"gitlab-duo", "duo-chat-opus-4-6", "duo-chat-gpt-5-codex", "duo-chat-haiku-4-6"} {
		if !seen[want] {
			t.Fatalf("expected gitlab model %s in static definitions", want)
		}
	}
	if info := LookupStaticModelInfo("duo-chat-sonnet-4-5"); info == nil || info.Type != "gitlab" {
		t.Fatalf("LookupStaticModelInfo(duo-chat-sonnet-4-5) = %+v", info)
	}
}
