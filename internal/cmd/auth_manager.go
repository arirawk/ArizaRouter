package cmd

import (
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
)

// newAuthManager creates a new authentication manager instance with all supported
// authenticators and a file-based token store. It initializes authenticators for
// Codex, Claude, Antigravity, Kimi, xAI, GitHub Copilot, Cursor, Kiro, Cline, Kilo, and GitLab Duo providers.
//
// Returns:
//   - *sdkAuth.Manager: A configured authentication manager instance
func newAuthManager() *sdkAuth.Manager {
	store := sdkAuth.GetTokenStore()
	manager := sdkAuth.NewManager(store,
		sdkAuth.NewCodexAuthenticator(),
		sdkAuth.NewClaudeAuthenticator(),
		sdkAuth.NewAntigravityAuthenticator(),
		sdkAuth.NewKimiAuthenticator(),
		sdkAuth.NewXAIAuthenticator(),
		sdkAuth.NewGitHubCopilotAuthenticator(),
		sdkAuth.NewCursorAuthenticator(),
		sdkAuth.NewKiroAuthenticator(),
		sdkAuth.NewClineAuthenticator(),
		sdkAuth.NewKiloAuthenticator(),
		sdkAuth.NewGitLabAuthenticator(),
	)
	return manager
}
