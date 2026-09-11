package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	kiroauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/kiro"
)

type fakeKiroDeviceTokenClient struct {
	responses []func() (*kiroauth.CreateTokenResponse, error)
	calls     int
	regions   []string
}

func (f *fakeKiroDeviceTokenClient) CreateTokenWithRegion(_ context.Context, _, _, _ string, region string) (*kiroauth.CreateTokenResponse, error) {
	f.regions = append(f.regions, region)
	if f.calls >= len(f.responses) {
		return nil, errors.New("unexpected extra call")
	}
	resp := f.responses[f.calls]
	f.calls++
	return resp()
}

func TestPollKiroDeviceTokenRetriesPendingAndSlowDown(t *testing.T) {
	client := &fakeKiroDeviceTokenClient{
		responses: []func() (*kiroauth.CreateTokenResponse, error){
			func() (*kiroauth.CreateTokenResponse, error) { return nil, kiroauth.ErrAuthorizationPending },
			func() (*kiroauth.CreateTokenResponse, error) { return nil, kiroauth.ErrSlowDown },
			func() (*kiroauth.CreateTokenResponse, error) {
				return &kiroauth.CreateTokenResponse{AccessToken: "at", RefreshToken: "rt", ExpiresIn: 3600}, nil
			},
		},
	}
	flow := &kiroauth.StartDeviceAuthResponse{DeviceCode: "dev", Interval: 0, ExpiresIn: 60}

	// Interval 0 falls back to DefaultPollInterval (5s), which is too slow for a
	// unit test; use a cancellable context with a generous timeout and a tiny
	// interval via the flow's Interval field instead.
	flow.Interval = 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := pollKiroDeviceToken(ctx, client, "cid", "secret", flow, "eu-west-1")
	if err != nil {
		t.Fatalf("pollKiroDeviceToken returned error: %v", err)
	}
	if resp == nil || resp.AccessToken != "at" || resp.RefreshToken != "rt" {
		t.Fatalf("unexpected token response: %+v", resp)
	}
	if client.calls != 3 {
		t.Fatalf("expected 3 CreateToken calls, got %d", client.calls)
	}
	for _, region := range client.regions {
		if region != "eu-west-1" {
			t.Fatalf("expected region eu-west-1 on every poll, got %q", region)
		}
	}
}

func TestPollKiroDeviceTokenStopsOnCancel(t *testing.T) {
	client := &fakeKiroDeviceTokenClient{
		responses: []func() (*kiroauth.CreateTokenResponse, error){
			func() (*kiroauth.CreateTokenResponse, error) { return nil, kiroauth.ErrAuthorizationPending },
		},
	}
	flow := &kiroauth.StartDeviceAuthResponse{DeviceCode: "dev", Interval: 1, ExpiresIn: 600}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := pollKiroDeviceToken(ctx, client, "cid", "secret", flow, "us-east-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("expected no CreateToken calls after cancellation, got %d", client.calls)
	}
}

func TestPollKiroDeviceTokenSurfacesFatalErrors(t *testing.T) {
	fatal := errors.New("access_denied")
	client := &fakeKiroDeviceTokenClient{
		responses: []func() (*kiroauth.CreateTokenResponse, error){
			func() (*kiroauth.CreateTokenResponse, error) { return nil, fatal },
		},
	}
	flow := &kiroauth.StartDeviceAuthResponse{DeviceCode: "dev", Interval: 1, ExpiresIn: 600}

	_, err := pollKiroDeviceToken(context.Background(), client, "cid", "secret", flow, "us-east-1")
	if !errors.Is(err, fatal) {
		t.Fatalf("expected fatal error to be returned, got %v", err)
	}
}

func TestNormalizeOAuthProviderSupportsKiro(t *testing.T) {
	provider, err := NormalizeOAuthProvider(" Kiro ")
	if err != nil || provider != "kiro" {
		t.Fatalf("NormalizeOAuthProvider(kiro) = %q, %v", provider, err)
	}
}

func TestGetStaticModelDefinitionsServesKiroChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/model-definitions/kiro", nil)
	c.Params = gin.Params{{Key: "channel", Value: "kiro"}}

	h.GetStaticModelDefinitions(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Channel string `json:"channel"`
		Models  []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Channel != "kiro" || len(payload.Models) == 0 {
		t.Fatalf("unexpected payload: %s", recorder.Body.String())
	}
	for _, m := range payload.Models {
		if m.Type != "kiro" {
			t.Fatalf("model %s type = %q, want kiro", m.ID, m.Type)
		}
	}
}
