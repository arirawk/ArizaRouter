package auth

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

// firstAuthSelector always returns the first candidate, which makes an over-broad
// candidate list observable: without model eligibility filtering it would hand a
// request for another provider's model id to whichever credential sorts first.
type firstAuthSelector struct{}

func (firstAuthSelector) Pick(_ context.Context, _, _ string, _ cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	if len(auths) == 0 {
		return nil, &Error{Code: "auth_not_found", Message: "no auth candidates"}
	}
	return auths[0], nil
}

func eligibilityCompatAuth(name string) *Auth {
	return &Auth{
		ID:         name + "-auth",
		Provider:   "openai-compatibility",
		Prefix:     name,
		Status:     StatusActive,
		Attributes: map[string]string{"compat_name": name},
	}
}

func eligibilityLCPRequest(content string) cliproxyexecutor.Options {
	return cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatOpenAI,
		OriginalRequest: []byte(`{"messages":[{"role":"system","content":"stable"},{"role":"user","content":"` + content + `"}]}`),
		Metadata: map[string]any{
			cliproxyexecutor.CallerScopeMetadataKey: "caller-eligibility",
		},
	}
}

func registerEligibilityClient(t *testing.T, auth *Auth, models ...string) {
	t.Helper()
	infos := make([]*registry.ModelInfo, 0, len(models))
	for _, id := range models {
		infos = append(infos, &registry.ModelInfo{ID: id})
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, infos)
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
}

// Two openai-compatibility providers with force-model-prefix advertise different prefixed
// model ids. A conversation bound to bb through bb/x must not be served by bb when the same
// conversation asks for ocbackup/x, even if bb is handed to the selector as a candidate.
func TestSessionAffinitySelectorLCPBindingDoesNotHijackOtherProviderModel(t *testing.T) {
	bb := eligibilityCompatAuth("bb")
	backup := eligibilityCompatAuth("ocbackup")
	registerEligibilityClient(t, bb, "bb/x")
	registerEligibilityClient(t, backup, "ocbackup/x")

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: firstAuthSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()
	auths := []*Auth{bb, backup}
	ctx := context.Background()

	first, errFirst := selector.Pick(ctx, "mixed", "bb/x", eligibilityLCPRequest("same conversation"), auths)
	if errFirst != nil {
		t.Fatalf("bb/x Pick() error = %v", errFirst)
	}
	if first == nil || first.ID != bb.ID {
		t.Fatalf("bb/x Pick() = %v, want %s", first, bb.ID)
	}

	second, errSecond := selector.Pick(ctx, "mixed", "ocbackup/x", eligibilityLCPRequest("same conversation"), auths)
	if errSecond != nil {
		t.Fatalf("ocbackup/x Pick() error = %v", errSecond)
	}
	if second == nil || second.ID != backup.ID {
		t.Fatalf("ocbackup/x Pick() = %v, want %s (bb binding must not be honoured for another provider's model)", second, backup.ID)
	}

	// The original binding is still honoured for the model it was created with.
	third, errThird := selector.Pick(ctx, "mixed", "bb/x", eligibilityLCPRequest("same conversation"), auths)
	if errThird != nil {
		t.Fatalf("repeat bb/x Pick() error = %v", errThird)
	}
	if third == nil || third.ID != bb.ID {
		t.Fatalf("repeat bb/x Pick() = %v, want %s", third, bb.ID)
	}
}

// An existing LCP hit is treated as a miss once the bound credential no longer advertises
// the requested model, and the conversation is rebound to an eligible credential.
func TestSessionAffinitySelectorLCPHitIgnoredWhenBoundAuthStopsServingModel(t *testing.T) {
	bb := eligibilityCompatAuth("bb")
	backup := eligibilityCompatAuth("ocbackup")
	registerEligibilityClient(t, bb, "shared-model")
	registerEligibilityClient(t, backup, "shared-model")

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: firstAuthSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()
	auths := []*Auth{bb, backup}
	ctx := context.Background()

	first, errFirst := selector.Pick(ctx, "mixed", "shared-model", eligibilityLCPRequest("sticky"), auths)
	if errFirst != nil {
		t.Fatalf("first Pick() error = %v", errFirst)
	}
	if first == nil || first.ID != bb.ID {
		t.Fatalf("first Pick() = %v, want %s", first, bb.ID)
	}

	// bb's upstream catalog changes and shared-model disappears from it.
	registry.GetGlobalRegistry().RegisterClient(bb.ID, bb.Provider, []*registry.ModelInfo{{ID: "other-model"}})

	second, errSecond := selector.Pick(ctx, "mixed", "shared-model", eligibilityLCPRequest("sticky"), auths)
	if errSecond != nil {
		t.Fatalf("second Pick() error = %v", errSecond)
	}
	if second == nil || second.ID != backup.ID {
		t.Fatalf("second Pick() = %v, want %s after bb stopped serving shared-model", second, backup.ID)
	}

	// The rebinding sticks on subsequent requests.
	third, errThird := selector.Pick(ctx, "mixed", "shared-model", eligibilityLCPRequest("sticky"), auths)
	if errThird != nil {
		t.Fatalf("third Pick() error = %v", errThird)
	}
	if third == nil || third.ID != backup.ID {
		t.Fatalf("third Pick() = %v, want %s", third, backup.ID)
	}
}
