package management

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/qwen"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// RequestQwenToken starts the Qwen OAuth device-code flow and completes it in
// the background, saving the auth file once the user authorizes the device.
func (h *Handler) RequestQwenToken(c *gin.Context) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	fmt.Println("Initializing Qwen authentication...")

	state := fmt.Sprintf("qwn-%d", time.Now().UnixNano())
	qwenAuth := qwen.NewQwenAuth(h.cfg)

	deviceFlow, errInitiate := qwenAuth.InitiateDeviceFlow(ctx)
	if errInitiate != nil {
		log.Errorf("Failed to start Qwen device flow: %v", errInitiate)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}
	authURL := strings.TrimSpace(deviceFlow.VerificationURIComplete)
	if authURL == "" {
		authURL = strings.TrimSpace(deviceFlow.VerificationURI)
	}

	RegisterOAuthSession(state, "qwen")

	go func() {
		pollCtx, cancelPoll := context.WithCancel(ctx)
		defer cancelPoll()
		go watchOAuthSessionCancel(pollCtx, cancelPoll, state, "qwen")

		fmt.Println("Waiting for Qwen authorization...")
		tokenData, errPoll := qwenAuth.PollForTokenWithContext(pollCtx, deviceFlow.DeviceCode, deviceFlow.CodeVerifier)
		if errPoll != nil {
			if !IsOAuthSessionPending(state, "qwen") {
				return
			}
			SetOAuthSessionError(state, oauthSessionErrorWithCause("Authentication failed", errPoll))
			fmt.Printf("Authentication failed: %v\n", errPoll)
			return
		}
		if !IsOAuthSessionPending(state, "qwen") {
			return
		}

		tokenStorage := qwenAuth.CreateTokenStorage(tokenData)
		// Qwen's device flow does not expose an account identifier, so the
		// credential is keyed by the login timestamp (matches the CLI flow).
		tokenStorage.Email = fmt.Sprintf("%d", time.Now().UnixMilli())

		fileName := fmt.Sprintf("qwen-%s.json", tokenStorage.Email)
		record := &coreauth.Auth{
			ID:       fileName,
			Provider: "qwen",
			FileName: fileName,
			Label:    "Qwen User",
			Storage:  tokenStorage,
			Metadata: map[string]any{"email": tokenStorage.Email},
		}
		if errGuard := guardOAuthSessionPendingForSave(state, "qwen"); errGuard != nil {
			return
		}
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		fmt.Println("You can now use Qwen services through this CLI")
		CompleteOAuthSession(state)
	}()

	response := gin.H{"status": "ok", "url": authURL, "state": state, "flow": "device"}
	if userCode := strings.TrimSpace(deviceFlow.UserCode); userCode != "" {
		response["user_code"] = userCode
	}
	if deviceFlow.ExpiresIn > 0 {
		response["expires_in"] = deviceFlow.ExpiresIn
	}
	c.JSON(http.StatusOK, response)
}
