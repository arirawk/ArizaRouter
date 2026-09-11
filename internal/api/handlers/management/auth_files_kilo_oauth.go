package management

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	kiloauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// kiloDeviceFlowFallbackTimeout bounds the device-code poll when Kilo does not
// report an expiry for the code.
const kiloDeviceFlowFallbackTimeout = 15 * time.Minute

// RequestKiloToken starts the Kilo AI device flow and completes it in the
// background: the user opens the returned URL and enters user_code, and the
// poller saves the auth file once Kilo approves the code. When the account
// belongs to several organizations the first one is used; pass
// organization_id to pick a specific one.
func (h *Handler) RequestKiloToken(c *gin.Context) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	fmt.Println("Initializing Kilo authentication...")

	kiloAuth := kiloauth.NewKiloAuth()
	deviceFlow, errStart := kiloAuth.InitiateDeviceFlow(ctx)
	if errStart != nil {
		log.Errorf("Failed to start Kilo device flow: %v", errStart)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start device authorization flow"})
		return
	}
	if strings.TrimSpace(deviceFlow.Code) == "" || strings.TrimSpace(deviceFlow.VerificationURL) == "" {
		log.Error("Kilo device flow returned an empty code or verification url")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start device authorization flow"})
		return
	}

	requestedOrgID := strings.TrimSpace(c.Query("organization_id"))
	state := fmt.Sprintf("kilo-%d", time.Now().UnixNano())
	RegisterOAuthSession(state, "kilo")

	pollTimeout := kiloDeviceFlowFallbackTimeout
	if deviceFlow.ExpiresIn > 0 {
		pollTimeout = time.Duration(deviceFlow.ExpiresIn) * time.Second
	}

	go func() {
		pollCtx, cancelPoll := context.WithTimeout(ctx, pollTimeout)
		defer cancelPoll()
		go watchOAuthSessionCancel(pollCtx, cancelPoll, state, "kilo")

		fmt.Println("Waiting for Kilo authorization...")
		status, errPoll := kiloAuth.PollForToken(pollCtx, deviceFlow.Code)
		if errPoll != nil {
			if !IsOAuthSessionPending(state, "kilo") {
				return
			}
			SetOAuthSessionError(state, oauthSessionErrorWithCause("Authentication failed", errPoll))
			fmt.Printf("Authentication failed: %v\n", errPoll)
			return
		}
		if !IsOAuthSessionPending(state, "kilo") {
			return
		}
		if status == nil || strings.TrimSpace(status.Token) == "" {
			SetOAuthSessionError(state, "Authentication failed: empty token")
			return
		}

		email := strings.TrimSpace(status.UserEmail)
		orgID := requestedOrgID
		profile, errProfile := kiloAuth.GetProfile(ctx, status.Token)
		if errProfile != nil {
			log.Warnf("kilo: failed to fetch profile: %v", errProfile)
		} else {
			if email == "" {
				email = strings.TrimSpace(profile.Email)
			}
			if orgID == "" && len(profile.Orgs) > 0 {
				orgID = profile.Orgs[0].ID
				if len(profile.Orgs) > 1 {
					log.Infof("kilo: multiple organizations found, defaulting to %s (%s)", profile.Orgs[0].Name, profile.Orgs[0].ID)
				}
			}
		}
		if email == "" {
			SetOAuthSessionError(state, "Authentication failed: missing account email")
			return
		}

		defaults, errDefaults := kiloAuth.GetDefaults(ctx, status.Token, orgID)
		if errDefaults != nil {
			log.Warnf("kilo: failed to fetch defaults: %v", errDefaults)
			defaults = &kiloauth.Defaults{}
		}

		metadata := map[string]any{
			"type":                   "kilo",
			"kilocodeToken":          strings.TrimSpace(status.Token),
			"kilocodeOrganizationId": orgID,
			"kilocodeModel":          defaults.Model,
			"email":                  email,
			"timestamp":              time.Now().UnixMilli(),
		}

		fileName := kiloauth.CredentialFileName(email)
		record := &coreauth.Auth{
			ID:       fileName,
			Provider: "kilo",
			FileName: fileName,
			Label:    email,
			Metadata: metadata,
		}
		if errGuard := guardOAuthSessionPendingForSave(state, "kilo"); errGuard != nil {
			return
		}
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		fmt.Println("You can now use Kilo services through this CLI")
		CompleteOAuthSession(state)
	}()

	response := gin.H{
		"status":    "ok",
		"url":       deviceFlow.VerificationURL,
		"state":     state,
		"flow":      "device",
		"user_code": deviceFlow.Code,
	}
	if deviceFlow.ExpiresIn > 0 {
		response["expires_in"] = deviceFlow.ExpiresIn
	} else {
		response["expires_in"] = int(kiloDeviceFlowFallbackTimeout / time.Second)
	}
	c.JSON(http.StatusOK, response)
}
