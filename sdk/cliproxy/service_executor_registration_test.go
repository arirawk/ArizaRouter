package cliproxy

import (
	"context"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type serviceTestPluginExecutor struct{}
type serviceTestSDKExecutor struct{ serviceTestPluginExecutor }

func (serviceTestSDKExecutor) Identifier() string { return "sdk-provider" }

func (serviceTestPluginExecutor) Identifier() string {
	return "plugin-provider"
}

func (serviceTestPluginExecutor) Execute(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (serviceTestPluginExecutor) ExecuteStream(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (serviceTestPluginExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (serviceTestPluginExecutor) CountTokens(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (serviceTestPluginExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestRegisterAvailableExecutors(t *testing.T) {
	oldRegisterPluginExecutors := registerPluginExecutors
	pluginRegisterCalls := 0
	var expectedPluginHost *pluginhost.Host
	var expectedManager *coreauth.Manager
	registerPluginExecutors = func(host *pluginhost.Host, manager *coreauth.Manager) {
		pluginRegisterCalls++
		if host != expectedPluginHost {
			t.Fatalf("plugin executor registration host = %p, want %p", host, expectedPluginHost)
		}
		if manager != expectedManager {
			t.Fatalf("plugin executor registration manager = %p, want %p", manager, expectedManager)
		}
		manager.RegisterExecutor(serviceTestPluginExecutor{})
	}
	t.Cleanup(func() {
		registerPluginExecutors = oldRegisterPluginExecutors
	})

	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
		pluginHost:  pluginhost.New(),
	}
	expectedPluginHost = service.pluginHost
	expectedManager = service.coreManager
	service.ensureWebsocketGateway()

	service.registerAvailableExecutors(nil, executorRegistrationOptions{
		includeBaseline: true,
		includePlugins:  true,
	})

	if pluginRegisterCalls != 1 {
		t.Fatalf("plugin executor registration calls = %d, want 1", pluginRegisterCalls)
	}

	providers := []string{
		"codex",
		"claude",
		"gemini",
		"gemini-interactions",
		"vertex",
		"aistudio",
		"antigravity",
		"kimi",
		"xai",
		"github-copilot",
		"cursor",
		"kiro",
		"qwen",
		"iflow",
		"codebuddy",
		"codebuddy-intl",
		"openai-compatibility",
		"plugin-provider",
	}
	for _, provider := range providers {
		resolved, ok := service.coreManager.Executor(provider)
		if !ok || resolved == nil {
			t.Fatalf("expected executor for provider %s after registration", provider)
		}
	}

	resolved, _ := service.coreManager.Executor("plugin-provider")
	if _, isPlugin := resolved.(serviceTestPluginExecutor); !isPlugin {
		t.Fatalf("executor type = %T, want serviceTestPluginExecutor", resolved)
	}
}

func TestSyncPluginModelRuntimePreservesSDKExecutorUnlessForced(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	custom := serviceTestSDKExecutor{}
	manager.RegisterExecutor(custom)
	auth := &coreauth.Auth{ID: "private-auth", Provider: custom.Identifier()}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatal(err)
	}
	service := &Service{cfg: &config.Config{}, coreManager: manager, pluginHost: pluginhost.New()}

	service.syncPluginModelRuntime(context.Background())
	got, ok := manager.Executor(custom.Identifier())
	if !ok || got != custom {
		t.Fatalf("plugin model sync replaced SDK executor with %T", got)
	}

	service.registerExecutorForAuth(auth, true)
	got, ok = manager.Executor(custom.Identifier())
	if !ok {
		t.Fatal("forced registration removed executor")
	}
	if _, replaced := got.(*runtimeexecutor.OpenAICompatExecutor); !replaced {
		t.Fatalf("forced registration kept %T, want *executor.OpenAICompatExecutor", got)
	}
}

func TestRegisterExecutorForAuth_OpenAICompatUsesNamespacedProviderKey(t *testing.T) {
	testCases := []struct {
		name  string
		auths []*coreauth.Auth
	}{
		{
			name: "native first",
			auths: []*coreauth.Auth{
				{ID: "native-kimi", Provider: "kimi"},
				openAICompatKimiAuth(),
			},
		},
		{
			name: "compat first",
			auths: []*coreauth.Auth{
				openAICompatKimiAuth(),
				{ID: "native-kimi", Provider: "kimi"},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				cfg:         &config.Config{},
				coreManager: coreauth.NewManager(nil, nil, nil),
			}

			service.registerExecutorsForAuths(tt.auths, true)

			nativeExecutor, okNative := service.coreManager.Executor("kimi")
			if !okNative {
				t.Fatal("expected native kimi executor")
			}
			if _, okKimi := nativeExecutor.(*runtimeexecutor.KimiExecutor); !okKimi {
				t.Fatalf("native executor type = %T, want *executor.KimiExecutor", nativeExecutor)
			}

			compatExecutor, okCompat := service.coreManager.Executor("openai-compatible-kimi")
			if !okCompat {
				t.Fatal("expected namespaced OpenAI-compatible executor")
			}
			if _, okOpenAICompat := compatExecutor.(*runtimeexecutor.OpenAICompatExecutor); !okOpenAICompat {
				t.Fatalf("compat executor type = %T, want *executor.OpenAICompatExecutor", compatExecutor)
			}
		})
	}
}

func openAICompatKimiAuth() *coreauth.Auth {
	return &coreauth.Auth{
		ID:       "compat-kimi",
		Provider: "openai-compatibility",
		Label:    "kimi",
		Attributes: map[string]string{
			"compat_name":  "kimi",
			"provider_key": "kimi",
		},
	}
}

func TestRegisterExecutorForAuth_GitHubCopilot(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	service.registerExecutorForAuth(&coreauth.Auth{ID: "github-copilot-octocat.json", Provider: "github-copilot"}, false)

	got, ok := manager.Executor("github-copilot")
	if !ok {
		t.Fatal("github-copilot executor was not registered")
	}
	if _, isCopilot := got.(*runtimeexecutor.GitHubCopilotExecutor); !isCopilot {
		t.Fatalf("registered executor is %T, want *executor.GitHubCopilotExecutor", got)
	}

	found := false
	for _, auth := range baselineExecutorAuths() {
		if auth != nil && auth.Provider == "github-copilot" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("github-copilot missing from baselineExecutorAuths()")
	}
}

func TestRegisterExecutorForAuth_Cursor(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	service.registerExecutorForAuth(&coreauth.Auth{ID: "cursor.a3f8b2c1.json", Provider: "cursor"}, false)

	got, ok := manager.Executor("cursor")
	if !ok {
		t.Fatal("cursor executor was not registered")
	}
	cursorExec, isCursor := got.(*runtimeexecutor.CursorExecutor)
	if !isCursor {
		t.Fatalf("registered executor is %T, want *executor.CursorExecutor", got)
	}
	if cursorExec.Identifier() != "cursor" {
		t.Fatalf("Identifier() = %q, want cursor", cursorExec.Identifier())
	}

	found := false
	for _, auth := range baselineExecutorAuths() {
		if auth != nil && auth.Provider == "cursor" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cursor missing from baselineExecutorAuths()")
	}
}

func TestRegisterExecutorForAuth_Kiro(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	service.registerExecutorForAuth(&coreauth.Auth{ID: "kiro-aws-user.json", Provider: "kiro"}, false)

	got, ok := manager.Executor("kiro")
	if !ok {
		t.Fatal("kiro executor was not registered")
	}
	kiroExec, isKiro := got.(*runtimeexecutor.KiroExecutor)
	if !isKiro {
		t.Fatalf("registered executor is %T, want *executor.KiroExecutor", got)
	}
	if kiroExec.Identifier() != "kiro" {
		t.Fatalf("kiro executor Identifier() = %q, want kiro", kiroExec.Identifier())
	}

	found := false
	for _, auth := range baselineExecutorAuths() {
		if auth != nil && auth.Provider == "kiro" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("kiro missing from baselineExecutorAuths()")
	}
}

func TestRegisterExecutorForAuth_Qwen(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	service.registerExecutorForAuth(&coreauth.Auth{ID: "qwen-user.json", Provider: "qwen"}, false)

	got, ok := manager.Executor("qwen")
	if !ok {
		t.Fatal("qwen executor was not registered")
	}
	qwenExec, isQwen := got.(*runtimeexecutor.QwenExecutor)
	if !isQwen {
		t.Fatalf("registered executor is %T, want *executor.QwenExecutor", got)
	}
	if qwenExec.Identifier() != "qwen" {
		t.Fatalf("Identifier() = %q, want qwen", qwenExec.Identifier())
	}

	found := false
	for _, auth := range baselineExecutorAuths() {
		if auth != nil && auth.Provider == "qwen" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("qwen missing from baselineExecutorAuths()")
	}
}

func TestRegisterExecutorForAuth_IFlow(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	service.registerExecutorForAuth(&coreauth.Auth{ID: "iflow-user.json", Provider: "iflow"}, false)

	got, ok := manager.Executor("iflow")
	if !ok {
		t.Fatal("iflow executor was not registered")
	}
	iflowExec, isIFlow := got.(*runtimeexecutor.IFlowExecutor)
	if !isIFlow {
		t.Fatalf("registered executor is %T, want *executor.IFlowExecutor", got)
	}
	if iflowExec.Identifier() != "iflow" {
		t.Fatalf("Identifier() = %q, want iflow", iflowExec.Identifier())
	}

	found := false
	for _, auth := range baselineExecutorAuths() {
		if auth != nil && auth.Provider == "iflow" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("iflow missing from baselineExecutorAuths()")
	}
}

func TestRegisterExecutorForAuth_CodeBuddy(t *testing.T) {
	for _, provider := range []string{"codebuddy", "codebuddy-intl"} {
		manager := coreauth.NewManager(nil, nil, nil)
		service := &Service{cfg: &config.Config{}, coreManager: manager}

		service.registerExecutorForAuth(&coreauth.Auth{ID: provider + "-user.json", Provider: provider}, false)

		got, ok := manager.Executor(provider)
		if !ok {
			t.Fatalf("%s executor was not registered", provider)
		}
		cbExec, isCodeBuddy := got.(*runtimeexecutor.CodeBuddyExecutor)
		if !isCodeBuddy {
			t.Fatalf("registered executor is %T, want *executor.CodeBuddyExecutor", got)
		}
		if cbExec.Identifier() != provider {
			t.Fatalf("Identifier() = %q, want %s", cbExec.Identifier(), provider)
		}

		found := false
		for _, auth := range baselineExecutorAuths() {
			if auth != nil && auth.Provider == provider {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s missing from baselineExecutorAuths()", provider)
		}
	}
}
