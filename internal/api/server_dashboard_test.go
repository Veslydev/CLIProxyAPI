package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardNativeProviderDiscoveryAndAdminSeparation(t *testing.T) {
	s, _ := controlPlaneHTTPFixture(t)
	for _, token := range []string{"", "not-the-admin", os.Getenv("CP_TEST_ADMIN")} {
		req := httptest.NewRequest(http.MethodGet, "/api/control-plane/oauth/providers", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.engine.ServeHTTP(w, req)
		if token != os.Getenv("CP_TEST_ADMIN") {
			if w.Code != 401 {
				t.Fatal("provider discovery accepted non-admin")
			}
			continue
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"antigravity"`) || strings.Contains(w.Body.String(), `"id":"gemini-cli"`) {
			t.Fatal("provider discovery does not match native/plugin boundary")
		}
	}
	for _, path := range []string{"/api/control-plane/oauth/antigravity?account=unknown", "/api/control-plane/oauth/start?provider=antigravity&account=unknown"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+os.Getenv("CP_TEST_ADMIN"))
		w := httptest.NewRecorder()
		s.engine.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal("targeted OAuth was dispatched without validating the account")
		}
	}
	for _, path := range []string{"/api/control-plane/native/provider-keys?provider=codex", "/api/control-plane/native/credentials", "/api/control-plane/native/logs"} {
		w := httptest.NewRecorder()
		s.engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if w.Code != 401 && w.Code != 404 {
			t.Fatal("native route bypassed admin auth")
		}
	}
}

func TestDashboardDeepLinksServeConfiguredBundle(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dashboard.html")
	const bundle = "<!doctype html><title>Dashboard fixture</title>"
	if err := os.WriteFile(file, []byte(bundle), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CP_BROWSER_DASHBOARD", file)
	s, _ := controlPlaneHTTPFixture(t)
	for _, path := range []string{"/dashboard", "/dashboard/providers", "/dashboard/quota?provider=codex"} {
		w := httptest.NewRecorder()
		s.engine.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.String() != bundle || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("dashboard route %q did not serve only the configured bundle: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, httptest.NewRequest("GET", "/dashboard/../dashboard/usage", nil))
	if w.Code != 400 {
		t.Fatal("path traversal was not rejected by the request boundary")
	}
}
