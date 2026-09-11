// Package cline provides authentication and token management functionality
// for Cline AI services using WorkOS OAuth.
package cline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	// BaseURL is the base URL for the Cline API.
	BaseURL = "https://api.cline.bot"

	// AuthTimeout is the timeout for OAuth authentication flow.
	AuthTimeout = 10 * time.Minute

	// CallbackPort is the localhost port Cline redirects to after login.
	CallbackPort = 1455
)

// TokenResponse represents the response from Cline token endpoints.
type TokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    string `json:"expiresAt"` // Cline returns ISO 8601 timestamp string
	Email        string `json:"email"`
}

// ClineAuth provides methods for handling the Cline WorkOS authentication flow.
type ClineAuth struct {
	client *http.Client
	cfg    *config.Config
}

// NewClineAuth creates a new instance of ClineAuth.
func NewClineAuth(cfg *config.Config) *ClineAuth {
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		client = util.SetProxy(&cfg.SDKConfig, client)
	}
	client.Timeout = 30 * time.Second
	return &ClineAuth{
		client: client,
		cfg:    cfg,
	}
}

// GenerateAuthURL generates the Cline OAuth authorization URL.
// The state parameter is used for CSRF protection.
func (c *ClineAuth) GenerateAuthURL(state, callbackURL string) string {
	// Cline uses WorkOS OAuth with the following parameters:
	// client_type=extension&callback_url={cb}&redirect_uri={cb}
	authURL := fmt.Sprintf("%s/api/v1/auth/authorize?client_type=extension&callback_url=%s&redirect_uri=%s&state=%s",
		BaseURL,
		callbackURL,
		callbackURL,
		state)
	return authURL
}

// ExchangeCode exchanges the authorization code for access and refresh tokens.
func (c *ClineAuth) ExchangeCode(ctx context.Context, code, redirectURI string) (*TokenResponse, error) {
	payload := map[string]string{
		"grant_type":   "authorization_code",
		"code":         code,
		"redirect_uri": redirectURI,
		"client_type":  "extension",
		"provider":     "workos",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cline: failed to marshal token request: %w", err)
	}

	tokenURL := BaseURL + "/api/v1/auth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("cline: failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Cline/3.0.0")
	req.Header.Set("HTTP-Referer", "https://cline.bot")
	req.Header.Set("X-Title", "Cline")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cline: token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cline: failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Debugf("cline: token exchange failed (status %d): %s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("cline: token exchange failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("cline: failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshToken refreshes an expired access token using the refresh token.
func (c *ClineAuth) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	payload := map[string]string{
		"grantType":    "refresh_token",
		"refreshToken": refreshToken,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cline: failed to marshal refresh request: %w", err)
	}

	refreshURL := BaseURL + "/api/v1/auth/refresh"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("cline: failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Cline/3.0.0")
	req.Header.Set("HTTP-Referer", "https://cline.bot")
	req.Header.Set("X-Title", "Cline")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cline: refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cline: failed to read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Debugf("cline: token refresh failed (status %d): %s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("cline: token refresh failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("cline: failed to parse refresh response: %w", err)
	}

	return &tokenResp, nil
}

// ShouldRefresh checks if the token should be refreshed (expires in less than 5 minutes).
func ShouldRefresh(expiresAt int64) bool {
	return time.Until(time.Unix(expiresAt, 0)) < 5*time.Minute
}

// DecodeCallbackToken tries to interpret the callback "code" parameter as a
// base64-encoded token payload. Cline usually returns the tokens directly this
// way; when decoding fails the caller should fall back to ExchangeCode.
func DecodeCallbackToken(code string) (*TokenResponse, bool) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, false
	}
	decodeStrategies := []func(string) ([]byte, error){
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
	}
	for _, decode := range decodeStrategies {
		decoded, errDecode := decode(code)
		if errDecode != nil {
			continue
		}
		var token TokenResponse
		parseErr := json.Unmarshal(decoded, &token)
		if parseErr != nil {
			if jsonOnly := extractFirstJSONObject(decoded); len(jsonOnly) > 0 {
				parseErr = json.Unmarshal(jsonOnly, &token)
			}
		}
		if parseErr == nil && token.AccessToken != "" {
			return &token, true
		}
		log.Debugf("cline: base64 decode succeeded but JSON parse failed: %v", parseErr)
	}
	return nil, false
}

// ParseExpiresAt converts the ISO 8601 expiresAt string Cline returns into a
// Unix timestamp; it returns 0 when the value is empty or unparsable.
func ParseExpiresAt(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.Unix()
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.Unix()
	}
	log.Debugf("cline: failed to parse expiresAt %q", raw)
	return 0
}

func extractFirstJSONObject(input []byte) []byte {
	start := -1
	depth := 0
	inString := false
	escapeNext := false

	for i, b := range input {
		if start == -1 {
			if b == '{' {
				start = i
				depth = 1
			}
			continue
		}

		if inString {
			if escapeNext {
				escapeNext = false
				continue
			}
			if b == '\\' {
				escapeNext = true
				continue
			}
			if b == '"' {
				inString = false
			}
			continue
		}

		if b == '"' {
			inString = true
			continue
		}

		if b == '{' {
			depth++
			continue
		}

		if b == '}' {
			depth--
			if depth == 0 {
				return input[start : i+1]
			}
		}
	}

	if start != -1 {
		return input[start:]
	}

	return nil
}
