package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	management "github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/controlplane"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestControlPlaneRemoteOAuthCallbackUsesExistingSessionAndSeparateAdmin(t *testing.T) {
	s, e := controlPlaneHTTPFixture(t)
	state := "control-plane-remote-callback-fixture"
	management.RegisterOAuthSession(state, "codex")
	t.Cleanup(func() { management.CancelOAuthSession(state) })
	body := `{"provider":"codex","state":"` + state + `","code":"fixture-callback-code"}`
	for _, token := range []string{"", "cp_inference-not-admin", os.Getenv("CP_TEST_ADMIN")} {
		req := httptest.NewRequest("POST", "/api/control-plane/oauth/callback", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		s.engine.ServeHTTP(response, req)
		want := 401
		if token == os.Getenv("CP_TEST_ADMIN") {
			want = 200
		}
		if response.Code != want {
			t.Fatalf("callback authorization: got %d want %d", response.Code, want)
		}
	}
	file := filepath.Join(s.cfg.AuthDir, ".oauth-codex-"+state+".oauth")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("existing OAuth callback store not used: %v", err)
	}
	management.CompleteOAuthSession(state)
	req := httptest.NewRequest("POST", "/api/control-plane/oauth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("CP_TEST_ADMIN"))
	response := httptest.NewRecorder()
	s.engine.ServeHTTP(response, req)
	if response.Code != 409 {
		t.Fatalf("completed callback replay accepted: %d", response.Code)
	}
	req = httptest.NewRequest("GET", "/api/control-plane/history/audit/page", nil)
	req.Header.Set("Authorization", "Bearer "+os.Getenv("CP_TEST_ADMIN"))
	response = httptest.NewRecorder()
	s.engine.ServeHTTP(response, req)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "/api/control-plane/oauth/callback") || strings.Contains(response.Body.String(), state) || strings.Contains(response.Body.String(), "fixture-callback-code") {
		t.Fatal("callback audit lost route or exposed OAuth evidence")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.calls) != 0 {
		t.Fatal("callback route unexpectedly dispatched inference")
	}
}

type controlPlaneHTTPExecutor struct {
	mu    sync.Mutex
	calls []string
}

func (e *controlPlaneHTTPExecutor) Identifier() string                             { return "codex" }
func (e *controlPlaneHTTPExecutor) PrepareRequest(*http.Request, *auth.Auth) error { return nil }
func (e *controlPlaneHTTPExecutor) Execute(ctx context.Context, a *auth.Auth, r executor.Request, _ executor.Options) (executor.Response, error) {
	if controlplane.RequestFromContext(ctx).KeyID == "" && os.Getenv("CP_BROWSER_SMOKE") == "" {
		return executor.Response{}, &auth.Error{Code: "context_missing", Message: "key scope lost", HTTPStatus: 500}
	}
	e.mu.Lock()
	e.calls = append(e.calls, a.ID)
	e.mu.Unlock()
	if os.Getenv("CP_BROWSER_SMOKE") != "" && os.Getenv("CP_BROWSER_SETTLED") == "1" {
		reporter := helps.NewUsageReporter(ctx, "codex", r.Model, a)
		reporter.Publish(ctx, usage.Detail{InputTokens: 1, OutputTokens: 1, TotalTokens: 2})
	}
	return executor.Response{Payload: []byte(`{"id":"resp_http_fixture","object":"response","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)}, nil
}
func (e *controlPlaneHTTPExecutor) ExecuteStream(context.Context, *auth.Auth, executor.Request, executor.Options) (*executor.StreamResult, error) {
	return nil, nil
}
func (e *controlPlaneHTTPExecutor) Refresh(_ context.Context, a *auth.Auth) (*auth.Auth, error) {
	return a, nil
}
func (e *controlPlaneHTTPExecutor) CountTokens(ctx context.Context, a *auth.Auth, r executor.Request, o executor.Options) (executor.Response, error) {
	return e.Execute(ctx, a, r, o)
}
func (e *controlPlaneHTTPExecutor) HttpRequest(ctx context.Context, a *auth.Auth, req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/alpha/search") {
		if controlplane.RequestFromContext(ctx).KeyID == "" {
			return nil, &auth.Error{Code: "context_missing", Message: "raw search key scope lost", HTTPStatus: 500}
		}
		e.mu.Lock()
		e.calls = append(e.calls, a.ID)
		e.mu.Unlock()
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000}}}`))}, nil
}

func TestControlPlaneRawSearchScopeAdmissionAndTelemetry(t *testing.T) {
	s, e := controlPlaneHTTPFixture(t)
	var account string
	stateRequest := httptest.NewRequest("GET", "/api/control-plane/state", nil)
	stateRequest.Header.Set("Authorization", "Bearer "+os.Getenv("CP_TEST_ADMIN"))
	stateResponse := httptest.NewRecorder()
	s.engine.ServeHTTP(stateResponse, stateRequest)
	var state struct{ Accounts []controlplane.Account }
	if err := json.Unmarshal(stateResponse.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	for _, a := range state.Accounts {
		if a.Label == "http-a" {
			account = a.ID
		}
	}
	key, secret, err := s.controlPlane.CreateKey(controlplane.APIKey{Name: "search-key", RequestLimit: 1, Bindings: map[string]controlplane.Binding{"codex": {Accounts: []string{account}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{200, 429} {
		req := httptest.NewRequest("POST", "/backend-api/codex/alpha/search", strings.NewReader(`{"model":"gpt-test","query":"fixture"}`))
		req.Header.Set("Authorization", "Bearer "+secret)
		recorder := httptest.NewRecorder()
		s.engine.ServeHTTP(recorder, req)
		if recorder.Code != want {
			t.Fatalf("raw search status %d, want %d: %s", recorder.Code, want, recorder.Body.String())
		}
	}
	e.mu.Lock()
	calls := append([]string(nil), e.calls...)
	e.mu.Unlock()
	if len(calls) != 1 || calls[0] != "http-a" {
		t.Fatalf("raw scope calls: %v", calls)
	}
	logs, err := s.controlPlane.RequestLogs(context.Background(), controlplane.LogFilter{APIKey: key.ID})
	if err != nil || len(logs) != 2 {
		t.Fatalf("raw telemetry: %#v %v", logs, err)
	}
	var execution, rejection int
	for _, entry := range logs {
		if entry.Status == 200 && entry.Account == account {
			execution++
		}
		if entry.Status == 429 && entry.Phase == "http_rejection" && entry.Account == "" && entry.Route == "POST /backend-api/codex/alpha/search" {
			rejection++
		}
	}
	if execution != 1 || rejection != 1 {
		t.Fatalf("raw execution/rejection evidence: %#v", logs)
	}
	if s.controlPlane.InFlight(account) != 0 {
		t.Fatal("raw search leaked in-flight lease")
	}
}

func controlPlaneHTTPFixture(t *testing.T) (*Server, *controlPlaneHTTPExecutor) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("CP_TEST_ADMIN", strings.Repeat("fixture-admin-", 4))
	dir := t.TempDir()
	m := auth.NewManager(nil, &auth.RoundRobinSelector{}, auth.NoopHook{})
	e := &controlPlaneHTTPExecutor{}
	m.RegisterExecutor(e)
	for _, id := range []string{"http-a", "http-b"} {
		if _, err := m.Register(context.Background(), &auth.Auth{ID: id, Provider: "codex", Label: id, Attributes: map[string]string{auth.AttributeAuthKind: auth.AuthKindOAuth}, Metadata: map[string]any{"account_id": id}}); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: "gpt-test"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
	dashboard, err := filepath.Abs(filepath.Join("..", "..", "web", "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if override := os.Getenv("CP_BROWSER_DASHBOARD"); override != "" {
		dashboard = override
	}
	cfg := &config.Config{AuthDir: filepath.Join(dir, "auth"), ControlPlane: config.ControlPlaneConfig{Enabled: true, Database: filepath.Join(dir, "state.sqlite"), AdminSecretEnv: "CP_TEST_ADMIN", DashboardFile: dashboard}}
	s := NewServer(cfg, m, sdkaccess.NewManager(), filepath.Join(dir, "config.yaml"))
	if s.controlPlaneErr != nil {
		t.Fatal(s.controlPlaneErr)
	}
	t.Cleanup(func() {
		if err := s.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return s, e
}

// Opt-in browser fixture has no real credentials and binds only the requested
// local/container bridge address. It is never part of the normal test suite.
func TestControlPlaneBrowserFixture(t *testing.T) {
	address := os.Getenv("CP_BROWSER_SMOKE")
	if address == "" {
		t.Skip("set CP_BROWSER_SMOKE to a local listener address")
	}
	s, _ := controlPlaneHTTPFixture(t)
	finished := make(chan struct{})
	var once sync.Once
	s.engine.POST("/test/finish", func(c *gin.Context) { once.Do(func() { close(finished) }); c.Status(204) })
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(s.engine)
	server.Listener = listener
	server.Start()
	defer server.Close()
	t.Logf("browser fixture listening at %s/dashboard", server.URL)
	<-finished
}

func TestControlPlaneHTTPAdmissionScopeAndAdminSeparation(t *testing.T) {
	s, e := controlPlaneHTTPFixture(t)
	server := httptest.NewServer(s.engine)
	defer server.Close()
	stateReq, _ := http.NewRequest("GET", server.URL+"/api/control-plane/state", nil)
	stateReq.Header.Set("Authorization", "Bearer "+os.Getenv("CP_TEST_ADMIN"))
	response, err := server.Client().Do(stateReq)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Accounts []controlplane.Account
		Pools    []controlplane.Pool
	}
	if err = json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 || len(state.Accounts) != 2 {
		t.Fatalf("state status %d", response.StatusCode)
	}
	var account string
	for _, a := range state.Accounts {
		if a.Label == "http-a" {
			account = a.ID
		}
	}
	key, secret, err := s.controlPlane.CreateKey(controlplane.APIKey{Name: "http-key", RequestLimit: 1, Bindings: map[string]controlplane.Binding{"codex": {Accounts: []string{account}}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{200, 429} {
		req, _ := http.NewRequest("POST", server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"Hi"}`))
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")
		r, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if r.StatusCode != want {
			t.Fatalf("request %d status %d, want %d: %s", i, r.StatusCode, want, body)
		}
	}
	searchReq, _ := http.NewRequest("POST", server.URL+"/backend-api/codex/alpha/search", strings.NewReader(`{"model":"gpt-test","query":"fixture"}`))
	searchReq.Header.Set("Authorization", "Bearer "+secret)
	searchResponse, err := server.Client().Do(searchReq)
	if err != nil {
		t.Fatal(err)
	}
	searchBody, _ := io.ReadAll(searchResponse.Body)
	_ = searchResponse.Body.Close()
	if searchResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("raw search bypassed invocation limit: %d %s", searchResponse.StatusCode, searchBody)
	}
	e.mu.Lock()
	calls := append([]string(nil), e.calls...)
	e.mu.Unlock()
	if len(calls) != 1 || calls[0] != "http-a" {
		t.Fatalf("scope calls: %v", calls)
	}
	stateReq.Header.Set("Authorization", "Bearer "+secret)
	r, err := server.Client().Do(stateReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("inference key authorized admin API")
	}
	key.Revoked = true
	if err = s.controlPlane.SaveKey(key); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"Hi"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	r, err = server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("revoked key admitted")
	}
}
