package auth

import "strings"

// canonicalizeAuthProvider maps legacy auth-file type aliases onto the
// provider key the executors are registered under.
func canonicalizeAuthProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "kilocode" {
		return "kilo"
	}
	return provider
}
