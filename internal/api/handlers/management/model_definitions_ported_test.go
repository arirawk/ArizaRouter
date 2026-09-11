package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// TestGetStaticModelDefinitions_PortedProviders covers
// GET /v0/management/model-definitions/<channel> for the cline, kilo and
// gitlab channels added by the provider ports.
func TestGetStaticModelDefinitions_PortedProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, coreauth.NewManager(nil, nil, nil))

	cases := map[string]string{
		"cline":  "cline/auto",
		"kilo":   "kilo/auto",
		"gitlab": "gitlab-duo",
	}
	for channel, wantModel := range cases {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/model-definitions/"+channel, nil)
		ctx.Params = gin.Params{{Key: "channel", Value: channel}}

		h.GetStaticModelDefinitions(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", channel, rec.Code, rec.Body.String())
		}
		var resp struct {
			Channel string `json:"channel"`
			Models  []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"models"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode response: %v", channel, err)
		}
		if resp.Channel != channel {
			t.Fatalf("%s: channel = %q", channel, resp.Channel)
		}
		found := false
		for _, m := range resp.Models {
			if m.Type != channel {
				t.Fatalf("%s: model %s has type %q", channel, m.ID, m.Type)
			}
			if m.ID == wantModel {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: model %q missing from %s", channel, wantModel, rec.Body.String())
		}
	}
}
