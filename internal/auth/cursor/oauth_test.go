package cursor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestGenerateAuthParams(t *testing.T) {
	params, err := GenerateAuthParams()
	if err != nil {
		t.Fatal(err)
	}
	if params.Verifier == "" || params.Challenge == "" || params.UUID == "" {
		t.Fatalf("empty PKCE params: %+v", params)
	}
	if !strings.HasPrefix(params.LoginURL, CursorLoginURL+"?challenge="+params.Challenge) {
		t.Fatalf("unexpected login url %q", params.LoginURL)
	}
	if !strings.Contains(params.LoginURL, "uuid="+params.UUID) || !strings.Contains(params.LoginURL, "redirectTarget=cli") {
		t.Fatalf("login url missing uuid/redirect target: %q", params.LoginURL)
	}
	if len(strings.Split(params.UUID, "-")) != 5 {
		t.Fatalf("uuid %q is not dash-separated", params.UUID)
	}
}

func TestParseJWTSubAndExpiry(t *testing.T) {
	exp := time.Now().Add(2 * time.Hour).Unix()
	token := makeJWT(t, map[string]any{"sub": "auth0|user_123", "exp": exp})

	if got := ParseJWTSub(token); got != "auth0|user_123" {
		t.Fatalf("ParseJWTSub = %q", got)
	}
	want := time.Unix(exp, 0).Add(-5 * time.Minute)
	if got := GetTokenExpiry(token); !got.Equal(want) {
		t.Fatalf("GetTokenExpiry = %v, want %v", got, want)
	}

	if got := ParseJWTSub("not-a-jwt"); got != "" {
		t.Fatalf("ParseJWTSub(garbage) = %q, want empty", got)
	}
	fallback := GetTokenExpiry("not-a-jwt")
	if until := time.Until(fallback); until < 55*time.Minute || until > 65*time.Minute {
		t.Fatalf("fallback expiry %v not ~1h away", until)
	}
}

func TestSubToShortHash(t *testing.T) {
	if got := SubToShortHash(""); got != "" {
		t.Fatalf("SubToShortHash(\"\") = %q", got)
	}
	got := SubToShortHash("auth0|user_123")
	if len(got) != 8 {
		t.Fatalf("SubToShortHash length = %d, want 8 (%q)", len(got), got)
	}
	if got != SubToShortHash("auth0|user_123") {
		t.Fatal("SubToShortHash is not deterministic")
	}
}

func TestCredentialFileNameAndLabel(t *testing.T) {
	cases := []struct {
		label, hash, wantFile, wantLabel string
	}{
		{"", "", "cursor.json", "Cursor User"},
		{"", "a3f8b2c1", "cursor.a3f8b2c1.json", "Cursor a3f8b2c1"},
		{"work", "a3f8b2c1", "cursor.work.json", "Cursor work"},
		{" work ", "", "cursor.work.json", "Cursor work"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q/%q", tc.label, tc.hash), func(t *testing.T) {
			if got := CredentialFileName(tc.label, tc.hash); got != tc.wantFile {
				t.Fatalf("CredentialFileName = %q, want %q", got, tc.wantFile)
			}
			if got := DisplayLabel(tc.label, tc.hash); got != tc.wantLabel {
				t.Fatalf("DisplayLabel = %q, want %q", got, tc.wantLabel)
			}
		})
	}
}
