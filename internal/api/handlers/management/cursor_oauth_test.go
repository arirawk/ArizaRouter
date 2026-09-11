package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cursorauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/cursor"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestGetStaticModelDefinitionsCursor(t *testing.T) {
	h := &Handler{cfg: &config.Config{}}
	router := gin.New()
	router.GET("/model-definitions/:channel", h.GetStaticModelDefinitions)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/model-definitions/cursor", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Channel string `json:"channel"`
		Models  []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"models"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Channel != "cursor" || len(response.Models) != 6 {
		t.Fatalf("channel=%q models=%d, want cursor/6", response.Channel, len(response.Models))
	}
	for _, m := range response.Models {
		if m.Type != "cursor" {
			t.Fatalf("model %s type = %q, want cursor", m.ID, m.Type)
		}
	}
}

func TestRequestCursorTokenRegistersPendingSession(t *testing.T) {
	h := &Handler{cfg: &config.Config{}}
	router := gin.New()
	router.GET("/cursor-auth-url", h.RequestCursorToken)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/cursor-auth-url", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Status    string `json:"status"`
		URL       string `json:"url"`
		State     string `json:"state"`
		Flow      string `json:"flow"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	// Stop the background poller before it reaches the network.
	t.Cleanup(func() { CancelOAuthSession(response.State) })

	if response.Status != "ok" || response.Flow != "device" || response.ExpiresIn <= 0 {
		t.Fatalf("unexpected response %+v", response)
	}
	if !strings.HasPrefix(response.URL, cursorauth.CursorLoginURL+"?challenge=") || !strings.Contains(response.URL, "redirectTarget=cli") {
		t.Fatalf("url = %q", response.URL)
	}
	if !strings.HasPrefix(response.State, "cur-") {
		t.Fatalf("state = %q", response.State)
	}
	provider, _, ok := GetOAuthSession(response.State)
	if !ok || provider != "cursor" || !IsOAuthSessionPending(response.State, "cursor") {
		t.Fatalf("session = (%q, %v), want pending cursor session", provider, ok)
	}
	if !CancelOAuthSession(response.State) {
		t.Fatal("cancel of the pending cursor session failed")
	}
	if IsOAuthSessionPending(response.State, "cursor") {
		t.Fatal("session still pending after cancel")
	}
}
