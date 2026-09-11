package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	cursorproto "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/cursor/proto"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestCursorExecutorIdentifier(t *testing.T) {
	e := NewCursorExecutor(&config.Config{})
	if got := e.Identifier(); got != "cursor" {
		t.Fatalf("Identifier() = %q, want cursor", got)
	}
	var _ cliproxyauth.ProviderExecutor = e
	var _ cliproxyauth.ExecutionSessionCloser = e
}

func TestClassifyCursorError(t *testing.T) {
	if err := classifyCursorError(nil); err != nil {
		t.Fatalf("nil error classified as %v", err)
	}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"resource_exhausted", &cursorproto.ConnectError{Code: "resource_exhausted", Message: "quota"}, 429},
		{"unauthenticated", &cursorproto.ConnectError{Code: "unauthenticated", Message: "bad token"}, 401},
		{"permission_denied", &cursorproto.ConnectError{Code: "permission_denied", Message: "nope"}, 403},
		{"unavailable", &cursorproto.ConnectError{Code: "unavailable", Message: "down"}, 503},
		{"unknown connect code", &cursorproto.ConnectError{Code: "weird", Message: "?"}, 502},
		{"fuzzy rate limit", errors.New("cursor: stream error: rate limit exceeded"), 429},
		{"fuzzy rst_stream", errors.New("h2: RST_STREAM received"), 502},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyCursorError(tc.err)
			se, ok := got.(cursorStatusErr)
			if !ok {
				t.Fatalf("classifyCursorError(%v) = %T, want cursorStatusErr", tc.err, got)
			}
			if se.StatusCode() != tc.want {
				t.Fatalf("status = %d, want %d", se.StatusCode(), tc.want)
			}
			if se.RetryAfter() != nil {
				t.Fatal("RetryAfter should be nil for cursor errors")
			}
		})
	}
	plain := errors.New("something else")
	if got := classifyCursorError(plain); got != plain {
		t.Fatalf("unclassified error was rewritten to %v", got)
	}
}

func TestParseOpenAIRequest(t *testing.T) {
	payload := []byte(`{"model":"composer-2","stream":true,"messages":[
		{"role":"system","content":"sys A"},
		{"role":"system","content":[{"type":"text","text":"sys B"}]},
		{"role":"user","content":"first question"},
		{"role":"assistant","content":"first answer"},
		{"role":"user","content":[{"type":"text","text":"second"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}
	],"tools":[{"type":"function","function":{"name":"read_file","description":"reads","parameters":{"type":"object"}}}]}`)
	p := parseOpenAIRequest(payload)
	if p.Model != "composer-2" || !p.Stream {
		t.Fatalf("model/stream = %q/%v", p.Model, p.Stream)
	}
	if p.SystemPrompt != "sys A\nsys B" {
		t.Fatalf("SystemPrompt = %q", p.SystemPrompt)
	}
	if len(p.Turns) != 1 || p.Turns[0].UserText != "first question" || p.Turns[0].AssistantText != "first answer" {
		t.Fatalf("Turns = %+v", p.Turns)
	}
	if p.UserText != "second" {
		t.Fatalf("UserText = %q", p.UserText)
	}
	if len(p.Images) != 1 || p.Images[0].MimeType != "image/png" || string(p.Images[0].Data) != "hello" {
		t.Fatalf("Images = %+v", p.Images)
	}
	if len(p.Tools) != 1 {
		t.Fatalf("Tools = %d, want 1", len(p.Tools))
	}

	params := buildRunRequestParams(p, "conv-1")
	if params.ConversationId != "conv-1" || len(params.McpTools) != 1 || params.McpTools[0].Name != "read_file" {
		t.Fatalf("RunRequestParams = %+v", params)
	}

	toolPayload := []byte(`{"model":"composer-2","messages":[
		{"role":"user","content":"run it"},
		{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"file body"}
	]}`)
	tp := parseOpenAIRequest(toolPayload)
	if len(tp.ToolResults) != 1 || tp.ToolResults[0].ToolCallId != "call_1" || tp.ToolResults[0].Content != "file body" {
		t.Fatalf("ToolResults = %+v", tp.ToolResults)
	}
	if tp.SystemPrompt != "You are a helpful assistant." {
		t.Fatalf("default SystemPrompt = %q", tp.SystemPrompt)
	}
	flattenConversationIntoUserText(tp)
	if tp.Turns != nil || tp.ToolResults != nil || !strings.Contains(tp.UserText, "TOOL_RESULT (call_id: call_1): file body") {
		t.Fatalf("flattened request = %+v", tp)
	}
}

func TestDeriveConversationId(t *testing.T) {
	a := deriveConversationId("key", "sess-1", "prompt")
	b := deriveConversationId("key", "sess-1", "different prompt")
	if a != b {
		t.Fatal("session id should dominate the conversation id")
	}
	if deriveConversationId("other", "sess-1", "prompt") == a {
		t.Fatal("api key must be part of the conversation id")
	}
	c := deriveConversationId("key", "", "system cch=abc123; rest")
	d := deriveConversationId("key", "", "system cch=zzz999; rest")
	if c != d {
		t.Fatal("volatile cch value should be stripped from the fallback hash")
	}
	if len(strings.Split(a, "-")) != 5 {
		t.Fatalf("conversation id %q is not uuid-shaped", a)
	}
	sid := extractClaudeCodeSessionId([]byte(`{"metadata":{"user_id":"{\"session_id\":\"abc\",\"device_id\":\"d\"}"}}`))
	if sid != "abc" {
		t.Fatalf("extractClaudeCodeSessionId = %q", sid)
	}
}

func TestParseModelsResponse(t *testing.T) {
	var entry []byte
	entry = protowire.AppendTag(entry, 1, protowire.BytesType)
	entry = protowire.AppendString(entry, "composer-2")
	entry = protowire.AppendTag(entry, 2, protowire.BytesType) // thinkingDetails
	entry = protowire.AppendBytes(entry, []byte{})
	entry = protowire.AppendTag(entry, 4, protowire.BytesType)
	entry = protowire.AppendString(entry, "Composer 2")

	var plain []byte
	plain = protowire.AppendTag(plain, 1, protowire.BytesType)
	plain = protowire.AppendString(plain, "gpt-4o")

	var resp []byte
	for _, m := range [][]byte{entry, plain} {
		resp = protowire.AppendTag(resp, 1, protowire.BytesType)
		resp = protowire.AppendBytes(resp, m)
	}

	models := parseModelsResponse(cursorproto.FrameConnectMessage(resp, 0))
	if len(models) != 2 {
		t.Fatalf("parsed %d models, want 2", len(models))
	}
	if models[0].ID != "composer-2" || models[0].DisplayName != "Composer 2" || models[0].Thinking == nil {
		t.Fatalf("models[0] = %+v", models[0])
	}
	if models[1].ID != "gpt-4o" || models[1].DisplayName != "gpt-4o" || models[1].Thinking != nil {
		t.Fatalf("models[1] = %+v", models[1])
	}
	for _, m := range models {
		if m.OwnedBy != "cursor" || m.Type != "cursor" {
			t.Fatalf("model %s provider = %q/%q", m.ID, m.OwnedBy, m.Type)
		}
	}
}

func TestFetchCursorModelsFallsBackWithoutToken(t *testing.T) {
	got := FetchCursorModels(context.Background(), &cliproxyauth.Auth{ID: "cursor.json", Provider: "cursor"}, &config.Config{})
	want := registry.GetCursorModels()
	if len(got) != len(want) {
		t.Fatalf("fallback returned %d models, want %d", len(got), len(want))
	}
}

func TestCursorExecutorRefreshRequiresRefreshToken(t *testing.T) {
	e := NewCursorExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{ID: "cursor.json", Provider: "cursor", Metadata: map[string]any{"access_token": "x"}}
	if _, err := e.Refresh(context.Background(), auth); err == nil {
		t.Fatal("Refresh without refresh_token should fail")
	}
	if cursorAccessToken(auth) != "x" || cursorRefreshToken(auth) != "" {
		t.Fatal("token accessors returned unexpected values")
	}
}

func TestCursorExecutorCountTokens(t *testing.T) {
	e := NewCursorExecutor(&config.Config{})
	payload := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello there"}]}`)
	resp, err := e.CountTokens(context.Background(), nil, cliproxyexecutor.Request{Model: "gpt-4o", Payload: payload}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil {
		t.Fatal(err)
	}
	if n := gjson.GetBytes(resp.Payload, "usage.prompt_tokens").Int(); n <= 0 {
		t.Fatalf("prompt_tokens = %d in %s", n, resp.Payload)
	}
}
