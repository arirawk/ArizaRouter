package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	clineauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/cline"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// RequestClineToken starts the Cline WorkOS OAuth flow. Cline redirects the
// browser to http://localhost:1455/callback; in Web UI mode a forwarder on that
// port bounces the redirect to /cline/callback on the management server, which
// persists the code for the waiting goroutine below.
func (h *Handler) RequestClineToken(c *gin.Context) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	fmt.Println("Initializing Cline authentication...")

	authSvc := clineauth.NewClineAuth(h.cfg)

	state, errState := misc.GenerateRandomState()
	if errState != nil {
		log.Errorf("Failed to generate state parameter: %v", errState)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}

	callbackURL := fmt.Sprintf("http://localhost:%d/callback", clineauth.CallbackPort)
	authURL := authSvc.GenerateAuthURL(state, callbackURL)

	RegisterOAuthSession(state, "cline")

	isWebUI := isWebUIRequest(c)
	var forwarder *callbackForwarder
	if isWebUI {
		targetURL, errTarget := h.managementCallbackURL("/cline/callback")
		if errTarget != nil {
			log.WithError(errTarget).Error("failed to compute cline callback target")
			CancelOAuthSession(state)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "callback server unavailable"})
			return
		}
		var errStart error
		if forwarder, errStart = startCallbackForwarder(clineauth.CallbackPort, "cline", targetURL); errStart != nil {
			log.WithError(errStart).Error("failed to start cline callback forwarder")
			CancelOAuthSession(state)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start callback server"})
			return
		}
	}

	go func() {
		if isWebUI {
			defer stopCallbackForwarderInstance(clineauth.CallbackPort, forwarder)
		}

		waitFile := filepath.Join(h.cfg.AuthDir, fmt.Sprintf(".oauth-cline-%s.oauth", state))
		deadline := time.Now().Add(5 * time.Minute)
		var authCode string
		for {
			if !IsOAuthSessionPending(state, "cline") {
				return
			}
			if time.Now().After(deadline) {
				log.Error("cline oauth flow timed out")
				SetOAuthSessionError(state, "OAuth flow timed out")
				return
			}
			if data, errReadFile := os.ReadFile(waitFile); errReadFile == nil {
				var payload map[string]string
				_ = json.Unmarshal(data, &payload)
				_ = os.Remove(waitFile)
				if errStr := strings.TrimSpace(payload["error"]); errStr != "" {
					log.Errorf("Authentication failed: %s", errStr)
					SetOAuthSessionError(state, "Authentication failed")
					return
				}
				// Cline does not always echo the state back; only compare when present.
				if payloadState := strings.TrimSpace(payload["state"]); payloadState != "" && payloadState != state {
					log.Errorf("Authentication failed: state mismatch")
					SetOAuthSessionError(state, "Authentication failed: state mismatch")
					return
				}
				authCode = strings.TrimSpace(payload["code"])
				if authCode == "" {
					log.Error("Authentication failed: code not found")
					SetOAuthSessionError(state, "Authentication failed: code not found")
					return
				}
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		tokenResp, decoded := clineauth.DecodeCallbackToken(authCode)
		if !decoded {
			var errToken error
			tokenResp, errToken = authSvc.ExchangeCode(ctx, authCode, callbackURL)
			if errToken != nil {
				log.Errorf("Failed to exchange token: %v", errToken)
				SetOAuthSessionError(state, oauthSessionErrorWithCause("Failed to exchange token", errToken))
				return
			}
		}
		if tokenResp == nil || strings.TrimSpace(tokenResp.AccessToken) == "" {
			log.Error("cline: token exchange returned empty access token")
			SetOAuthSessionError(state, "Failed to exchange token")
			return
		}

		email := strings.TrimSpace(tokenResp.Email)
		if email == "" {
			log.Error("cline: token response is missing the account email")
			SetOAuthSessionError(state, "Authentication failed: missing account email")
			return
		}

		expiresAt := clineauth.ParseExpiresAt(tokenResp.ExpiresAt)
		metadata := map[string]any{
			"type":         "cline",
			"accessToken":  strings.TrimSpace(tokenResp.AccessToken),
			"refreshToken": strings.TrimSpace(tokenResp.RefreshToken),
			"expiresAt":    expiresAt,
			"email":        email,
			"timestamp":    time.Now().UnixMilli(),
		}

		fileName := clineauth.CredentialFileName(email)
		record := &coreauth.Auth{
			ID:       fileName,
			Provider: "cline",
			FileName: fileName,
			Label:    email,
			Metadata: metadata,
		}
		if errGuard := guardOAuthSessionPendingForSave(state, "cline"); errGuard != nil {
			return
		}
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save token to file: %v", errSave)
			SetOAuthSessionError(state, "Failed to save token to file")
			return
		}

		CompleteOAuthSession(state)
		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		fmt.Println("You can now use Cline services through this CLI")
	}()

	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": authURL, "state": state})
}
