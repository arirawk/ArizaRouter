package util

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestGetProviderNameFallsBackToCursor(t *testing.T) {
	reg := registry.GetGlobalRegistry()
	const model = "cursor-fallback-test-model-unknown"

	if got := GetProviderName(model); len(got) != 0 {
		t.Fatalf("GetProviderName(%q) = %v before cursor registration, want none", model, got)
	}

	reg.RegisterClient("test-cursor-fallback", "cursor", []*registry.ModelInfo{
		{ID: "composer-2", Type: "cursor"},
	})
	t.Cleanup(func() { reg.UnregisterClient("test-cursor-fallback") })

	if got := GetProviderName(model); len(got) != 1 || got[0] != "cursor" {
		t.Fatalf("GetProviderName(%q) = %v, want [cursor]", model, got)
	}
	if got := GetProviderName("composer-2"); len(got) == 0 || got[0] != "cursor" {
		t.Fatalf("GetProviderName(composer-2) = %v, want cursor first", got)
	}
}
