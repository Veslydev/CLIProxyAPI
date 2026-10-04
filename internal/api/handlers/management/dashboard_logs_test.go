package management

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func dashboardLoggingRequest(h *Handler, method string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/", strings.NewReader(`{"value":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.DashboardLoggingToFile(c)
	return w
}

func TestDashboardLoggingRollsBackFailedSave(t *testing.T) {
	cfg := &config.Config{}
	h := &Handler{cfg: cfg, configFilePath: filepath.Join(t.TempDir(), "missing", "config.yaml")}
	if w := dashboardLoggingRequest(h, "PUT"); w.Code != 500 || h.cfg != cfg || h.cfg.LoggingToFile {
		t.Fatal("failed logging save changed runtime configuration")
	}
}

func TestDashboardLoggingAndKeyWritesShareOwnership(t *testing.T) {
	h := &Handler{cfg: &config.Config{}, configFilePath: writeTestConfigFile(t)}
	var wg sync.WaitGroup
	for _, method := range []string{"PUT", "GET"} {
		wg.Add(1)
		go func(method string) {
			defer wg.Done()
			if w := dashboardLoggingRequest(h, method); w.Code != 200 {
				t.Errorf("logging %s: status %d", method, w.Code)
			}
		}(method)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if w := dashboardKeyRequest(h, "POST", "/", `{"provider":"codex","apiKey":"fixture-key"}`, ""); w.Code != 200 {
			t.Errorf("key: status %d", w.Code)
		}
	}()
	wg.Wait()
	if !h.cfg.LoggingToFile || len(h.cfg.CodexKey) != 1 {
		t.Fatal("concurrent logging/key mutation lost configuration")
	}
}
