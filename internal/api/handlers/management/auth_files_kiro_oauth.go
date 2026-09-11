package management

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	kiroauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/kiro"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// RequestKiroToken starts the AWS SSO OIDC device-code flow for Kiro and
// completes it in the background, saving the auth file once the user
// authorizes in the browser.
//
// By default the flow targets AWS Builder ID. Passing ?start_url=<IDC start
// URL> (optionally with ?region=) switches to an IAM Identity Center login.
// The response mirrors the other device-flow providers:
// {status, url, state, flow:"device", user_code, expires_in}.
func (h *Handler) RequestKiroToken(c *gin.Context) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	fmt.Println("Initializing Kiro authentication...")

	startURL := strings.TrimSpace(c.Query("start_url"))
	region := strings.TrimSpace(c.Query("region"))
	if region == "" {
		region = kiroauth.DefaultIDCRegion
	}
	authMethod := "builder-id"
	source := "aws"
	if startURL != "" {
		authMethod = "idc"
		source = "aws-idc"
	} else {
		startURL = kiroauth.BuilderIDStartURL
	}

	state := fmt.Sprintf("kiro-%d", time.Now().UnixNano())
	kiroauth.InitFingerprintConfig(h.cfg)
	ssoClient := kiroauth.NewSSOOIDCClient(h.cfg)

	regResp, errRegister := ssoClient.RegisterClientWithRegion(ctx, region)
	if errRegister != nil {
		log.Errorf("Failed to register Kiro OIDC client: %v", errRegister)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}

	deviceFlow, errStartDeviceFlow := ssoClient.StartDeviceAuthorizationWithIDC(ctx, regResp.ClientID, regResp.ClientSecret, startURL, region)
	if errStartDeviceFlow != nil {
		log.Errorf("Failed to start Kiro device authorization: %v", errStartDeviceFlow)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}
	authURL := strings.TrimSpace(deviceFlow.VerificationURIComplete)
	if authURL == "" {
		authURL = deviceFlow.VerificationURI
	}

	RegisterOAuthSession(state, "kiro")

	go func() {
		pollCtx, cancelPoll := context.WithCancel(ctx)
		defer cancelPoll()
		go watchOAuthSessionCancel(pollCtx, cancelPoll, state, "kiro")

		fmt.Println("Waiting for AWS authorization...")
		tokenResp, errWaitForAuthorization := pollKiroDeviceToken(pollCtx, ssoClient, regResp.ClientID, regResp.ClientSecret, deviceFlow, region)
		if errWaitForAuthorization != nil {
			if !IsOAuthSessionPending(state, "kiro") {
				return
			}
			SetOAuthSessionError(state, oauthSessionErrorWithCause("Authentication failed", errWaitForAuthorization))
			fmt.Printf("Authentication failed: %v\n", errWaitForAuthorization)
			return
		}
		if !IsOAuthSessionPending(state, "kiro") {
			return
		}

		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		var profileArn string
		if authMethod == "idc" {
			profileArn = ssoClient.FetchProfileArn(pollCtx, tokenResp.AccessToken, regResp.ClientID, tokenResp.RefreshToken)
		}
		email := kiroauth.FetchUserEmailWithFallback(pollCtx, h.cfg, tokenResp.AccessToken, regResp.ClientID, tokenResp.RefreshToken, authMethod)

		tokenData := &kiroauth.KiroTokenData{
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			ProfileArn:   profileArn,
			ExpiresAt:    expiresAt.Format(time.RFC3339),
			AuthMethod:   authMethod,
			Provider:     "AWS",
			ClientID:     regResp.ClientID,
			ClientSecret: regResp.ClientSecret,
			Email:        email,
			Region:       region,
		}
		if authMethod == "idc" {
			tokenData.StartURL = startURL
		}

		record := sdkauth.NewKiroAuthRecord(tokenData, source)
		if errGuard := guardOAuthSessionPendingForSave(state, "kiro"); errGuard != nil {
			return
		}
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		fmt.Println("You can now use Kiro services through this CLI")
		CompleteOAuthSession(state)
	}()

	response := gin.H{"status": "ok", "url": authURL, "state": state, "flow": "device"}
	if userCode := strings.TrimSpace(deviceFlow.UserCode); userCode != "" {
		response["user_code"] = userCode
	}
	if deviceFlow.ExpiresIn > 0 {
		response["expires_in"] = deviceFlow.ExpiresIn
	}
	c.JSON(200, response)
}

// kiroDeviceTokenClient is the subset of the SSO OIDC client used while polling.
type kiroDeviceTokenClient interface {
	CreateTokenWithRegion(ctx context.Context, clientID, clientSecret, deviceCode, region string) (*kiroauth.CreateTokenResponse, error)
}

// pollKiroDeviceToken polls CreateToken until the user authorizes the device
// code, the code expires, or ctx is cancelled. authorization_pending keeps the
// cadence, slow_down backs off by 5 seconds as the SSO OIDC spec requires.
func pollKiroDeviceToken(ctx context.Context, client kiroDeviceTokenClient, clientID, clientSecret string, deviceFlow *kiroauth.StartDeviceAuthResponse, region string) (*kiroauth.CreateTokenResponse, error) {
	if deviceFlow == nil {
		return nil, errors.New("device authorization missing")
	}
	interval := kiroauth.DefaultPollInterval
	if deviceFlow.Interval > 0 {
		interval = time.Duration(deviceFlow.Interval) * time.Second
	}
	deadline := time.Now().Add(time.Duration(deviceFlow.ExpiresIn) * time.Second)
	if deviceFlow.ExpiresIn <= 0 {
		deadline = time.Now().Add(10 * time.Minute)
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		tokenResp, err := client.CreateTokenWithRegion(ctx, clientID, clientSecret, deviceFlow.DeviceCode, region)
		if err == nil {
			return tokenResp, nil
		}
		if errors.Is(err, kiroauth.ErrAuthorizationPending) {
			continue
		}
		if errors.Is(err, kiroauth.ErrSlowDown) {
			interval += 5 * time.Second
			continue
		}
		return nil, err
	}
	return nil, errors.New("authorization timed out")
}
