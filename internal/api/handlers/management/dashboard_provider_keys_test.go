package management

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func dashboardKeyRequest(h *Handler, method, target, body, hash string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "hash", Value: hash}}
	h.DashboardProviderKeys(c)
	return w
}

func TestDashboardProviderKeysRedactsAndPreservesExecutionFields(t *testing.T) {
	key := "fixture-upstream-secret-0001"
	h := &Handler{cfg: &config.Config{CodexKey: []config.CodexKey{{APIKey: key, Priority: 9, BaseURL: "https://fixture.invalid", Websockets: true}}}, configFilePath: writeTestConfigFile(t)}
	w := dashboardKeyRequest(h, http.MethodGet, "/?provider=codex", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), key) || !strings.Contains(w.Body.String(), dashboardKeyHash("codex", key)) {
		t.Fatal("provider key listing must contain only redacted identity")
	}
	w = dashboardKeyRequest(h, http.MethodPost, "/", `{"provider":"codex","apiKey":"fixture-new"}`, "")
	if w.Code != 200 || len(h.cfg.CodexKey) != 2 || h.cfg.CodexKey[0].Priority != 9 || !h.cfg.CodexKey[0].Websockets {
		t.Fatal("adding a key discarded original execution fields")
	}
	w = dashboardKeyRequest(h, http.MethodPost, "/", `{"provider":"codex","apiKey":"fixture-new"}`, "")
	if w.Code != 409 || len(h.cfg.CodexKey) != 2 {
		t.Fatal("duplicate key accepted")
	}
	w = dashboardKeyRequest(h, http.MethodDelete, "/?provider=codex", "", dashboardKeyHash("codex", "fixture-new"))
	if w.Code != 200 || len(h.cfg.CodexKey) != 1 || h.cfg.CodexKey[0].APIKey != key {
		t.Fatal("deletion did not preserve unrelated key")
	}
}

func TestDashboardProviderKeysRollsBackFailedPersistenceAndRejectsAmbiguity(t *testing.T) {
	cfg := &config.Config{CodexKey: []config.CodexKey{{APIKey: "fixture-existing"}}}
	h := &Handler{cfg: cfg, configFilePath: filepath.Join(t.TempDir(), "missing", "config.yaml")}
	w := dashboardKeyRequest(h, http.MethodPost, "/", `{"provider":"codex","apiKey":"fixture-new"}`, "")
	if w.Code != 500 || h.cfg != cfg || len(cfg.CodexKey) != 1 {
		t.Fatal("failed save changed runtime keys")
	}
	h.cfg.CodexKey = append(h.cfg.CodexKey, h.cfg.CodexKey[0])
	w = dashboardKeyRequest(h, http.MethodDelete, "/?provider=codex", "", dashboardKeyHash("codex", "fixture-existing"))
	if w.Code != 409 || len(h.cfg.CodexKey) != 2 {
		t.Fatal("ambiguous deletion changed keys")
	}
}

func TestDashboardProviderKeysConcurrentAdditionsAreNotLost(t *testing.T) {
	h := &Handler{cfg: &config.Config{}, configFilePath: writeTestConfigFile(t)}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := dashboardKeyRequest(h, http.MethodPost, "/", fmt.Sprintf(`{"provider":"codex","apiKey":"fixture-key-%d"}`, i), "")
			if w.Code != 200 {
				t.Errorf("addition %d: status %d", i, w.Code)
			}
		}(i)
	}
	wg.Wait()
	if len(h.cfg.CodexKey) != 8 {
		t.Fatal("concurrent addition lost keys")
	}
}
