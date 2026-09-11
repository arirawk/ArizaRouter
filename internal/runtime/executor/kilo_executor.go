package executor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const (
	kiloVersion      = "3.26.0"
	kiloTesterHeader = "X-Kilocode-Tester"
)

// KiloExecutor handles requests to Kilo API.
type KiloExecutor struct {
	cfg *config.Config
}

// NewKiloExecutor creates a new Kilo executor instance.
func NewKiloExecutor(cfg *config.Config) *KiloExecutor {
	return &KiloExecutor{cfg: cfg}
}

// Identifier returns the unique identifier for this executor.
func (e *KiloExecutor) Identifier() string { return "kilo" }

// PrepareRequest prepares the HTTP request before execution.
func (e *KiloExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	accessToken, _ := kiloCredentials(auth)
	if strings.TrimSpace(accessToken) == "" {
		return fmt.Errorf("kilo: missing access token")
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	return nil
}

// HttpRequest executes a raw HTTP request.
func (e *KiloExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("kilo executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute performs a non-streaming request.
func (e *KiloExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	ctx = helps.EnsureSessionContext(ctx, opts, req.Payload)
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	accessToken, orgID := kiloCredentials(auth)
	if accessToken == "" {
		return resp, fmt.Errorf("kilo: missing access token")
	}

	from := opts.SourceFormat
	to := sdktranslator.FromString("openai")
	endpoint := "/api/openrouter/chat/completions"

	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalPayload := originalPayloadSource
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayload, opts.Stream)
	translated := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, opts.Stream)
	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	translated = helps.ApplyPayloadConfigWithRoot(e.cfg, baseModel, to.String(), "", translated, originalTranslated, requestedModel, helps.PayloadRequestPath(opts))

	translated, err = thinking.ApplyThinking(translated, req.Model, from.String(), to.String(), e.Identifier())
	if err != nil {
		return resp, err
	}

	url := "https://api.kilo.ai" + endpoint
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(translated))
	if err != nil {
		return resp, err
	}
	applyKiloHeaders(httpReq, accessToken, orgID, false)
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(httpReq, attrs)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      translated,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer httpResp.Body.Close()

	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		helps.AppendAPIResponseChunk(ctx, e.cfg, b)
		logKiloUpstreamError(ctx, baseModel, url, httpResp.StatusCode, httpResp.Header.Get("Content-Type"), b)
		err = statusErr{code: httpResp.StatusCode, msg: string(b)}
		return resp, err
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, body)
	reporter.Publish(ctx, helps.ParseOpenAIUsage(body))
	reporter.EnsurePublished(ctx)

	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, cliproxyexecutor.ResponseFormatOrSource(opts), req.Model, opts.OriginalRequest, translated, body, &param)
	resp = cliproxyexecutor.Response{Payload: []byte(out)}
	return resp, nil
}

// ExecuteStream performs a streaming request.
func (e *KiloExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	ctx = helps.EnsureSessionContext(ctx, opts, req.Payload)
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	accessToken, orgID := kiloCredentials(auth)
	if accessToken == "" {
		return nil, fmt.Errorf("kilo: missing access token")
	}

	from := opts.SourceFormat
	to := sdktranslator.FromString("openai")
	endpoint := "/api/openrouter/chat/completions"

	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalPayload := originalPayloadSource
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayload, true)
	translated := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, true)
	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	translated = helps.ApplyPayloadConfigWithRoot(e.cfg, baseModel, to.String(), "", translated, originalTranslated, requestedModel, helps.PayloadRequestPath(opts))

	translated, err = thinking.ApplyThinking(translated, req.Model, from.String(), to.String(), e.Identifier())
	if err != nil {
		return nil, err
	}

	url := "https://api.kilo.ai" + endpoint
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(translated))
	if err != nil {
		return nil, err
	}
	applyKiloHeaders(httpReq, accessToken, orgID, true)

	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(httpReq, attrs)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      translated,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}

	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		helps.AppendAPIResponseChunk(ctx, e.cfg, b)
		httpResp.Body.Close()
		logKiloUpstreamError(ctx, baseModel, url, httpResp.StatusCode, httpResp.Header.Get("Content-Type"), b)
		err = statusErr{code: httpResp.StatusCode, msg: string(b)}
		return nil, err
	}

	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer httpResp.Body.Close()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800)
		var param any
		for scanner.Scan() {
			line := scanner.Bytes()
			helps.AppendAPIResponseChunk(ctx, e.cfg, line)
			if detail, ok := helps.ParseOpenAIStreamUsage(line); ok {
				reporter.Publish(ctx, detail)
			}
			if len(line) == 0 {
				continue
			}
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			chunks := sdktranslator.TranslateStream(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, bytes.Clone(line), &param)
			for i := range chunks {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(chunks[i])}
			}
		}
		if errScan := scanner.Err(); errScan != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, errScan)
			reporter.PublishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		}
		reporter.EnsurePublished(ctx)
	}()

	return &cliproxyexecutor.StreamResult{
		Headers: httpResp.Header.Clone(),
		Chunks:  out,
	}, nil
}

// Refresh validates the Kilo token.
func (e *KiloExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, fmt.Errorf("missing auth")
	}
	return auth, nil
}

// CountTokens returns the token count for the given request.
func (e *KiloExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, fmt.Errorf("kilo: count tokens not supported")
}

// kiloCredentials extracts access token and other info from auth.
func kiloCredentials(auth *cliproxyauth.Auth) (accessToken, orgID string) {
	if auth == nil {
		return "", ""
	}

	// Prefer kilocode-specific keys first, then fall back to generic keys.
	// Check metadata first, then attributes.
	if auth.Metadata != nil {
		if token, ok := auth.Metadata["kilocodeToken"].(string); ok && token != "" {
			accessToken = token
		} else if token, ok := auth.Metadata["token"].(string); ok && token != "" {
			accessToken = token
		} else if token, ok := auth.Metadata["access_token"].(string); ok && token != "" {
			accessToken = token
		}

		if org, ok := auth.Metadata["kilocodeOrganizationId"].(string); ok && org != "" {
			orgID = org
		} else if org, ok := auth.Metadata["organization_id"].(string); ok && org != "" {
			orgID = org
		}
	}

	if accessToken == "" && auth.Attributes != nil {
		if token := auth.Attributes["kilocodeToken"]; token != "" {
			accessToken = token
		} else if token := auth.Attributes["token"]; token != "" {
			accessToken = token
		} else if token := auth.Attributes["access_token"]; token != "" {
			accessToken = token
		}
	}

	if orgID == "" && auth.Attributes != nil {
		if org := auth.Attributes["kilocodeOrganizationId"]; org != "" {
			orgID = org
		} else if org := auth.Attributes["organization_id"]; org != "" {
			orgID = org
		}
	}

	return accessToken, orgID
}

// FetchKiloModels fetches models from Kilo API.
func FetchKiloModels(ctx context.Context, auth *cliproxyauth.Auth, cfg *config.Config) []*registry.ModelInfo {
	accessToken, orgID := kiloCredentials(auth)
	if accessToken == "" {
		log.Infof("kilo: no access token found, skipping dynamic model fetch (using static kilo/auto)")
		return registry.GetKiloModels()
	}

	log.Debugf("kilo: fetching dynamic models (orgID: %s)", orgID)

	httpClient := helps.NewProxyAwareHTTPClient(ctx, cfg, auth, 0)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.kilo.ai/api/openrouter/models", nil)
	if err != nil {
		log.Warnf("kilo: failed to create model fetch request: %v", err)
		return registry.GetKiloModels()
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	if orgID != "" {
		req.Header.Set("X-Kilocode-OrganizationID", orgID)
	}
	req.Header.Set("User-Agent", "cli-proxy-kilo")

	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.Warnf("kilo: fetch models canceled: %v", err)
		} else {
			log.Warnf("kilo: using static models (API fetch failed: %v)", err)
		}
		return registry.GetKiloModels()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warnf("kilo: failed to read models response: %v", err)
		return registry.GetKiloModels()
	}

	if resp.StatusCode != http.StatusOK {
		log.Warnf("kilo: fetch models failed: status %d, body: %s", resp.StatusCode, string(body))
		return registry.GetKiloModels()
	}

	result := gjson.GetBytes(body, "data")
	if !result.Exists() {
		// Try root if data field is missing
		result = gjson.ParseBytes(body)
		if !result.IsArray() {
			log.Debugf("kilo: response body: %s", string(body))
			log.Warn("kilo: invalid API response format (expected array or data field with array)")
			return registry.GetKiloModels()
		}
	}

	var dynamicModels []*registry.ModelInfo
	now := time.Now().Unix()
	count := 0
	totalCount := 0

	result.ForEach(func(key, value gjson.Result) bool {
		totalCount++
		id := value.Get("id").String()
		pIdxResult := value.Get("preferredIndex")
		preferredIndex := pIdxResult.Int()

		// Filter models where preferredIndex > 0 (Kilo-curated models)
		if preferredIndex <= 0 {
			return true
		}

		// Check if it's free. We look for :free suffix, is_free flag, or zero pricing.
		isFree := strings.HasSuffix(id, ":free") || id == "giga-potato" || value.Get("is_free").Bool()
		if !isFree {
			// Check pricing as fallback
			promptPricing := value.Get("pricing.prompt").String()
			if promptPricing == "0" || promptPricing == "0.0" {
				isFree = true
			}
		}

		if !isFree {
			log.Debugf("kilo: skipping curated paid model: %s", id)
			return true
		}

		log.Debugf("kilo: found curated model: %s (preferredIndex: %d)", id, preferredIndex)

		dynamicModels = append(dynamicModels, &registry.ModelInfo{
			ID:            id,
			DisplayName:   value.Get("name").String(),
			ContextLength: int(value.Get("context_length").Int()),
			OwnedBy:       "kilo",
			Type:          "kilo",
			Object:        "model",
			Created:       now,
		})
		count++
		return true
	})

	log.Debugf("kilo: fetched %d models from API, %d curated free (preferredIndex > 0)", totalCount, count)
	if count == 0 && totalCount > 0 {
		log.Warn("kilo: no curated free models found (check API response fields)")
	}

	staticModels := registry.GetKiloModels()
	// Always include kilo/auto (first static model)
	allModels := make([]*registry.ModelInfo, 0, 1+len(dynamicModels))
	allModels = append(allModels, staticModels[:1]...)
	allModels = append(allModels, dynamicModels...)

	return allModels
}

// logKiloUpstreamError records a non-2xx upstream response with a trimmed body summary.
func logKiloUpstreamError(ctx context.Context, model, url string, statusCode int, contentType string, body []byte) {
	helps.LogWithRequestID(ctx).Warnf("kilo model=%s upstream error: url=%s status=%d message=%s", model, url, statusCode, helps.SummarizeErrorBody(contentType, body))
}

func applyKiloHeaders(r *http.Request, token, orgID string, stream bool) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	if orgID != "" {
		r.Header.Set("X-Kilocode-OrganizationID", orgID)
	}
	r.Header.Set("HTTP-Referer", "https://kilocode.ai")
	r.Header.Set("X-Title", "Kilo Code")
	r.Header.Set("X-KiloCode-Version", kiloVersion)
	r.Header.Set("User-Agent", "Kilo-Code/"+kiloVersion)
	r.Header.Set(kiloTesterHeader, "SUPPRESS")
	r.Header.Set("X-KiloCode-EditorName", "Visual Studio Code 1.96.0")
	if stream {
		r.Header.Set("Accept", "text/event-stream")
		r.Header.Set("Cache-Control", "no-cache")
	} else {
		r.Header.Set("Accept", "application/json")
	}
}
