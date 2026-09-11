package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/gitlab"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// gitlabPATRequest carries the GitLab personal access token login parameters.
// They can be supplied as query parameters or as a JSON body; "token" and
// "personal_access_token" are accepted interchangeably.
type gitlabPATRequest struct {
	BaseURL             string `json:"base_url"`
	Token               string `json:"token"`
	PersonalAccessToken string `json:"personal_access_token"`
}

func (r gitlabPATRequest) token() string {
	if token := strings.TrimSpace(r.Token); token != "" {
		return token
	}
	return strings.TrimSpace(r.PersonalAccessToken)
}

func parseGitLabPATRequest(c *gin.Context) (gitlabPATRequest, error) {
	req := gitlabPATRequest{
		BaseURL:             strings.TrimSpace(c.Query("base_url")),
		Token:               strings.TrimSpace(c.Query("token")),
		PersonalAccessToken: strings.TrimSpace(c.Query("personal_access_token")),
	}
	if c.Request == nil || c.Request.Body == nil || c.Request.ContentLength == 0 {
		return req, nil
	}
	if ct := strings.ToLower(strings.TrimSpace(c.ContentType())); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return req, nil
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<16))
	if err != nil {
		return req, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return req, nil
	}
	var body gitlabPATRequest
	if err := json.Unmarshal(data, &body); err != nil {
		return req, err
	}
	if v := strings.TrimSpace(body.BaseURL); v != "" {
		req.BaseURL = v
	}
	if v := strings.TrimSpace(body.Token); v != "" {
		req.Token = v
	}
	if v := strings.TrimSpace(body.PersonalAccessToken); v != "" {
		req.PersonalAccessToken = v
	}
	return req, nil
}

// RequestGitLabPATToken validates a GitLab personal access token, fetches the
// user and Duo model gateway details, persists the credential as an auth file
// and registers an already-completed OAuth session so the regular
// get-auth-status polling reports the login as finished.
//
// Serves GET/POST /v0/management/gitlab-auth-url and POST /v0/management/gitlab-pat.
func (h *Handler) RequestGitLabPATToken(c *gin.Context) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	req, err := parseGitLabPATRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid body"})
		return
	}

	pat := req.token()
	if pat == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "token (or personal_access_token) is required"})
		return
	}
	baseURL := gitlab.NormalizeBaseURL(req.BaseURL)

	client := gitlab.NewAuthClient(h.cfg)

	user, err := client.GetCurrentUser(ctx, baseURL, pat)
	if err != nil {
		log.WithError(err).Error("failed to validate GitLab personal access token")
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "error": "failed to validate GitLab token"})
		return
	}

	if _, err = client.GetPersonalAccessTokenSelf(ctx, baseURL, pat); err != nil {
		log.WithError(err).Warn("failed to fetch GitLab PAT self info; continuing with token validation")
	}

	direct, err := client.FetchDirectAccess(ctx, baseURL, pat)
	if err != nil {
		log.WithError(err).Warn("failed to fetch GitLab direct access info; continuing with basic token validation")
		direct = nil
	}

	modelProvider := ""
	modelName := ""
	if direct != nil && direct.ModelDetails != nil {
		modelProvider = strings.TrimSpace(direct.ModelDetails.ModelProvider)
		modelName = strings.TrimSpace(direct.ModelDetails.ModelName)
	}

	email := strings.TrimSpace(user.Email)
	if email == "" {
		email = strings.TrimSpace(user.PublicEmail)
	}
	identifier := strings.TrimSpace(user.Username)
	if identifier == "" {
		identifier = email
	}
	if identifier == "" {
		identifier = "user"
	}

	fileName := fmt.Sprintf("gitlab-%s-pat.json", gitLabSafeFileName(identifier))
	metadata := map[string]any{
		"type":                     "gitlab",
		"auth_method":              "pat",
		"auth_kind":                "personal_access_token",
		"base_url":                 baseURL,
		"personal_access_token":    pat,
		"token_preview":            gitLabMaskToken(pat),
		"user_id":                  user.ID,
		"username":                 strings.TrimSpace(user.Username),
		"name":                     strings.TrimSpace(user.Name),
		"last_refresh":             time.Now().UTC().Format(time.RFC3339),
		"refresh_interval_seconds": 240,
	}
	if email != "" {
		metadata["email"] = email
	}
	mergeGitLabDirectAccessMetadata(metadata, direct)

	label := identifier + " (PAT)"
	record := &coreauth.Auth{
		ID:       fileName,
		Provider: "gitlab",
		FileName: fileName,
		Label:    label,
		Metadata: metadata,
	}

	savedPath, err := h.saveTokenRecord(ctx, record)
	if err != nil {
		log.WithError(err).Error("failed to save GitLab auth record")
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to save auth record"})
		return
	}

	// The PAT flow completes synchronously; register a finished session so
	// clients that poll get-auth-status see the login as done.
	state := fmt.Sprintf("gitlab-%d", time.Now().UnixNano())
	RegisterOAuthSession(state, "gitlab")
	CompleteOAuthSession(state)

	fmt.Printf("GitLab Duo PAT authentication successful! Token saved to %s\n", savedPath)

	response := gin.H{
		"status": "ok",
		"state":  state,
		"label":  label,
	}
	if modelProvider != "" {
		response["model_provider"] = modelProvider
	}
	if modelName != "" {
		response["model_name"] = modelName
	}
	c.JSON(http.StatusOK, response)
}

// mergeGitLabDirectAccessMetadata copies the Duo gateway details returned by
// the direct_access endpoint into the auth metadata (same shape as sdk/auth).
func mergeGitLabDirectAccessMetadata(metadata map[string]any, direct *gitlab.DirectAccessResponse) {
	if metadata == nil || direct == nil {
		return
	}
	if base := strings.TrimSpace(direct.BaseURL); base != "" {
		metadata["duo_gateway_base_url"] = base
	}
	if token := strings.TrimSpace(direct.Token); token != "" {
		metadata["duo_gateway_token"] = token
	}
	if direct.ExpiresAt > 0 {
		expiry := time.Unix(direct.ExpiresAt, 0).UTC()
		metadata["duo_gateway_expires_at"] = expiry.Format(time.RFC3339)
		if ttl := expiry.Sub(time.Now().UTC()); ttl > 0 {
			interval := int(ttl.Seconds()) / 2
			switch {
			case interval < 60:
				interval = 60
			case interval > 240:
				interval = 240
			}
			metadata["refresh_interval_seconds"] = interval
		}
	}
	if len(direct.Headers) > 0 {
		headers := make(map[string]string, len(direct.Headers))
		for key, value := range direct.Headers {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				continue
			}
			headers[key] = value
		}
		if len(headers) > 0 {
			metadata["duo_gateway_headers"] = headers
		}
	}
	if direct.ModelDetails != nil {
		modelDetails := map[string]any{}
		if provider := strings.TrimSpace(direct.ModelDetails.ModelProvider); provider != "" {
			modelDetails["model_provider"] = provider
			metadata["model_provider"] = provider
		}
		if model := strings.TrimSpace(direct.ModelDetails.ModelName); model != "" {
			modelDetails["model_name"] = model
			metadata["model_name"] = model
		}
		if len(modelDetails) > 0 {
			metadata["model_details"] = modelDetails
		}
	}
}

func gitLabSafeFileName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "user"
	}
	return out
}

func gitLabMaskToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if len(trimmed) <= 8 {
		return trimmed
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}
