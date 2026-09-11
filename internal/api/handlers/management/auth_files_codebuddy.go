package management

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/codebuddy"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// codeBuddyPollTimeoutSeconds mirrors codebuddy.maxPollDuration for the
// expires_in hint returned to the Web UI.
const codeBuddyPollTimeoutSeconds = 300

// RequestCodeBuddyToken starts the CodeBuddy (copilot.tencent.com) browser
// login and completes it in the background.
func (h *Handler) RequestCodeBuddyToken(c *gin.Context) {
	h.requestCodeBuddyToken(c, "codebuddy", "CodeBuddy", codebuddy.NewCodeBuddyAuth(h.cfg), codebuddy.BaseURL)
}

// RequestCodeBuddyIntlToken starts the CodeBuddy International (codebuddy.ai)
// browser login and completes it in the background.
func (h *Handler) RequestCodeBuddyIntlToken(c *gin.Context) {
	h.requestCodeBuddyToken(c, "codebuddy-intl", "CodeBuddy International", codebuddy.NewCodeBuddyIntlAuth(h.cfg), codebuddy.IntlBaseURL)
}

// requestCodeBuddyToken fetches a login state + URL from the CodeBuddy auth
// API, returns the URL to the caller, and polls the token endpoint until the
// user finishes the browser login (device-style flow without a user code).
func (h *Handler) requestCodeBuddyToken(c *gin.Context, provider, displayName string, authSvc *codebuddy.CodeBuddyAuth, baseURL string) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	fmt.Printf("Initializing %s authentication...\n", displayName)

	state := fmt.Sprintf("cbd-%d", time.Now().UnixNano())

	authState, errState := authSvc.FetchAuthState(ctx)
	if errState != nil {
		log.Errorf("Failed to fetch %s auth state: %v", displayName, errState)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}

	RegisterOAuthSession(state, provider)

	go func() {
		pollCtx, cancelPoll := context.WithCancel(ctx)
		defer cancelPoll()
		go watchOAuthSessionCancel(pollCtx, cancelPoll, state, provider)

		fmt.Printf("Waiting for %s authorization...\n", displayName)
		storage, errPoll := authSvc.PollForToken(pollCtx, authState.State)
		if errPoll != nil {
			if !IsOAuthSessionPending(state, provider) {
				return
			}
			SetOAuthSessionError(state, oauthSessionErrorWithCause(codebuddy.GetUserFriendlyMessage(errPoll), errPoll))
			fmt.Printf("Authentication failed: %v\n", errPoll)
			return
		}
		if !IsOAuthSessionPending(state, provider) {
			return
		}

		label := strings.TrimSpace(storage.Email)
		if label == "" {
			label = strings.TrimSpace(storage.UserID)
		}
		if label == "" {
			label = provider + "-user"
		}
		userID := strings.TrimSpace(storage.UserID)
		if userID == "" {
			userID = fmt.Sprintf("%d", time.Now().UnixMilli())
		}
		fileName := fmt.Sprintf("%s-%s.json", provider, userID)
		metadata := map[string]any{
			"type":          provider,
			"access_token":  storage.AccessToken,
			"refresh_token": storage.RefreshToken,
			"user_id":       storage.UserID,
			"domain":        storage.Domain,
			"expires_in":    storage.ExpiresIn,
			"base_url":      baseURL,
			"timestamp":     time.Now().UnixMilli(),
		}
		if storage.Email != "" {
			metadata["email"] = storage.Email
		}
		if storage.ExpiresIn > 0 {
			metadata["expired"] = time.Now().Add(time.Duration(storage.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
		}

		record := &coreauth.Auth{
			ID:       fileName,
			Provider: provider,
			FileName: fileName,
			Label:    label,
			Storage:  storage,
			Metadata: metadata,
		}
		if errGuard := guardOAuthSessionPendingForSave(state, provider); errGuard != nil {
			return
		}
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		fmt.Printf("You can now use %s services through this CLI\n", displayName)
		CompleteOAuthSession(state)
	}()

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"url":        authState.AuthURL,
		"state":      state,
		"flow":       "device",
		"expires_in": codeBuddyPollTimeoutSeconds,
	})
}
