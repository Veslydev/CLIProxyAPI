package controlplane

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestHTTPRejectionsPersistWithoutBillingOrSensitiveEvidence(t *testing.T) {
	s, _ := testStore(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(s.ObserveInferenceHTTP(), s.InferenceMiddleware())
	engine.POST("/v1/chat/completions", func(c *gin.Context) { c.AbortWithStatus(400) })
	engine.GET("/v1/videos/:request_id", func(c *gin.Context) { c.AbortWithStatus(403) })
	engine.POST("/api/control-plane/example", func(c *gin.Context) { c.AbortWithStatus(401) })
	for _, path := range []string{"/v1/chat/completions?key=cp_sensitive-query", "/v1/videos/sensitive-resource", "/api/control-plane/example", "/v1/unknown"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"sensitive-model","messages":[{"content":"private prompt"}]}`))
		if strings.Contains(path, "/videos/") {
			req.Method = "GET"
		}
		req.Header.Set("Authorization", "Bearer cp_private-secret")
		engine.ServeHTTP(httptest.NewRecorder(), req)
	}
	rows, err := s.RequestLogs(context.Background(), LogFilter{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("observed rejections: %d %v", len(rows), err)
	}
	for _, row := range rows {
		if row.Phase != "http_rejection" || row.APIKey != "" || row.Account != "" || row.Provider != "" || row.Cost != nil || row.Total != 0 {
			t.Fatalf("invented upstream evidence: %+v", row)
		}
		data, errEncode := encode(row)
		if errEncode != nil {
			t.Fatal(errEncode)
		}
		for _, secret := range []string{"private-secret", "sensitive-query", "sensitive-resource", "sensitive-model", "private prompt"} {
			if strings.Contains(data, secret) {
				t.Fatalf("sensitive evidence persisted: %s", secret)
			}
		}
	}
	var requests, failures, tokens int
	if err = s.db.QueryRow("SELECT sum(requests),sum(failures),sum(tokens) FROM rollups").Scan(&requests, &failures, &tokens); err != nil || requests != 2 || failures != 2 || tokens != 0 {
		t.Fatalf("rejection rollups: %d %d %d %v", requests, failures, tokens, err)
	}
	var settlements int
	if err = s.db.QueryRow("SELECT count(*) FROM settlements").Scan(&settlements); err != nil || settlements != 0 {
		t.Fatalf("local rejection billed: %d %v", settlements, err)
	}
}

func TestHTTPObserverDoesNotDuplicatePublishedUsageOrChargeRejectedKey(t *testing.T) {
	s, auths := testStore(t)
	account := logical(s, auths[0])
	key, secret, err := s.CreateKey(APIKey{Name: "rejection", Bindings: map[string]Binding{"codex": {Accounts: []string{account}}}})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(s.ObserveInferenceHTTP(), s.InferenceMiddleware())
	engine.POST("/v1/rejected", func(c *gin.Context) { c.AbortWithStatus(429) })
	engine.POST("/v1/published", func(c *gin.Context) {
		s.HandleUsage(c.Request.Context(), usage.Record{RequestID: "published-failure", Provider: "codex", AuthID: auths[0].ID, APIKey: key.ID, Model: "test", Failed: true, Fail: usage.Failure{StatusCode: 502}})
		c.AbortWithStatus(502)
	})
	for _, path := range []string{"/v1/rejected", "/v1/published"} {
		req := httptest.NewRequest("POST", path, nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		engine.ServeHTTP(httptest.NewRecorder(), req)
	}
	rows, err := s.RequestLogs(context.Background(), LogFilter{APIKey: key.ID})
	if err != nil || len(rows) != 2 {
		t.Fatalf("duplicated HTTP usage: %d %v", len(rows), err)
	}
	if got := s.state.Load().Keys[key.ID]; got.Requests != 0 || got.Tokens != 0 || got.Cost != 0 {
		t.Fatalf("HTTP observer charged key: %+v", got)
	}
	if got := s.state.Load().Accounts[account].Requests; got != 1 {
		t.Fatalf("duplicate account billing: %d", got)
	}
	path := s.path
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	rows, err = restarted.RequestLogs(context.Background(), LogFilter{APIKey: key.ID})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rejections lost at restart: %d %v", len(rows), err)
	}
}
