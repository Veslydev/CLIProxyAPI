package controlplane

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func inferenceSecret(r *http.Request) string {
	if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	}
	for _, header := range []string{"X-Api-Key", "X-Goog-Api-Key"} {
		if v := r.Header.Get(header); v != "" {
			return v
		}
	}
	return r.URL.Query().Get("key")
}

// InferenceMiddleware recognizes only the reserved cp_ namespace. Legacy keys
// still pass through the existing access manager without changing its semantics.
func (s *Store) InferenceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/control-plane") || c.Request.URL.Path == "/dashboard" {
			c.Next()
			return
		}
		secret := inferenceSecret(c.Request)
		if !strings.HasPrefix(secret, "cp_") {
			c.Next()
			return
		}
		key, ok := s.Key(secret)
		if !ok || key.Revoked || (!key.ExpiresAt.IsZero() && !s.now().Before(key.ExpiresAt)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "invalid_inference_key", "message": "invalid or expired inference key"}})
			return
		}
		c.Set("userApiKey", key.ID)
		c.Request = c.Request.WithContext(WithRequest(c.Request.Context(), RequestContext{KeyID: key.ID}))
		c.Next()
	}
}

type loginAttempt struct {
	At       time.Time
	Failures int
}

func AdminMiddleware(secret string) gin.HandlerFunc {
	digest := sha256.Sum256([]byte(secret))
	var mu sync.Mutex
	attempts := map[string]loginAttempt{}
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()
		mu.Lock()
		attempt := attempts[ip]
		if now.Sub(attempt.At) > time.Minute {
			attempt = loginAttempt{At: now}
		}
		if attempt.Failures >= 20 {
			mu.Unlock()
			c.AbortWithStatusJSON(429, gin.H{"error": "too many authentication failures"})
			return
		}
		mu.Unlock()
		supplied := sha256.Sum256([]byte(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")))
		if len(secret) < 32 || subtle.ConstantTimeCompare(digest[:], supplied[:]) != 1 {
			mu.Lock()
			attempt = attempts[ip]
			if now.Sub(attempt.At) > time.Minute {
				attempt = loginAttempt{At: now}
			}
			attempt.Failures++
			for key, value := range attempts {
				if now.Sub(value.At) > 2*time.Minute {
					delete(attempts, key)
				}
			}
			if len(attempts) >= 4096 {
				mu.Unlock()
				c.AbortWithStatusJSON(429, gin.H{"error": "too many authentication failures"})
				return
			}
			attempts[ip] = attempt
			mu.Unlock()
			c.AbortWithStatusJSON(401, gin.H{"error": "admin authentication required"})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func input(c *gin.Context, v any) error {
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return errors.New("invalid JSON input")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid JSON input")
	}
	return nil
}
func respond(c *gin.Context, v any, err error) {
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func integer(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }

func (s *Store) RegisterRoutes(engine *gin.Engine, admin string) *gin.RouterGroup {
	g := engine.Group("/api/control-plane", AdminMiddleware(admin))
	g.GET("/executions/unresolved", func(c *gin.Context) { rows, err := s.UnresolvedExecutions(c.Request.Context()); respond(c, rows, err) })
	g.GET("/state", func(c *gin.Context) {
		st := s.state.Load()
		accounts := []Account{}
		pools := []Pool{}
		keys := []APIKey{}
		quotas := []QuotaWindow{}
		for _, v := range st.Accounts {
			accounts = append(accounts, v)
		}
		for _, v := range st.Pools {
			pools = append(pools, v)
		}
		for _, v := range st.Keys {
			keys = append(keys, v)
		}
		for _, v := range st.Quotas {
			quotas = append(quotas, v)
		}
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
		sort.Slice(pools, func(i, j int) bool { return pools[i].ID < pools[j].ID })
		sort.Slice(keys, func(i, j int) bool { return keys[i].ID < keys[j].ID })
		respond(c, gin.H{"accounts": accounts, "pools": pools, "keys": keys, "quotas": quotas, "settings": st.Settings, "strategies": Strategies, "schedules": s.WarmSchedules(), "warm_accounts": st.WarmAccounts}, nil)
	})
	g.PUT("/accounts/:id", func(c *gin.Context) {
		var a Account
		err := input(c, &a)
		a.ID = c.Param("id")
		if err == nil {
			err = s.SaveAccount(a)
		}
		if err == nil {
			err = s.PlanWarmups()
		}
		respond(c, gin.H{"ok": err == nil}, err)
	})
	g.POST("/accounts/:id/reassociate", func(c *gin.Context) {
		var body struct {
			Source       string `json:"source"`
			Confirmation string `json:"confirmation"`
		}
		err := input(c, &body)
		if err == nil {
			err = s.ReassociateCredential(c.Param("id"), body.Source, body.Confirmation)
		}
		respond(c, gin.H{"ok": err == nil}, err)
	})
	g.POST("/pools", func(c *gin.Context) {
		var p Pool
		err := input(c, &p)
		if err == nil {
			err = s.SavePool(p)
		}
		if err == nil {
			err = s.PlanWarmups()
		}
		respond(c, gin.H{"ok": err == nil}, err)
	})
	g.PUT("/pools/:id", func(c *gin.Context) {
		var p Pool
		err := input(c, &p)
		p.ID = c.Param("id")
		if err == nil {
			err = s.SavePool(p)
		}
		if err == nil {
			err = s.PlanWarmups()
		}
		respond(c, gin.H{"ok": err == nil}, err)
	})
	g.POST("/keys", func(c *gin.Context) {
		var k APIKey
		err := input(c, &k)
		var secret string
		if err == nil {
			k, secret, err = s.CreateKey(k)
		}
		respond(c, gin.H{"key": k, "secret": secret}, err)
	})
	g.PUT("/keys/:id", func(c *gin.Context) {
		var k APIKey
		err := input(c, &k)
		k.ID = c.Param("id")
		if err == nil {
			err = s.SaveKey(k)
		}
		respond(c, gin.H{"ok": err == nil}, err)
	})
	g.DELETE("/keys/:id", func(c *gin.Context) {
		k, ok := s.state.Load().Keys[c.Param("id")]
		if !ok {
			respond(c, nil, errors.New("unknown key"))
			return
		}
		k.Revoked = true
		err := s.SaveKey(k)
		respond(c, gin.H{"ok": err == nil}, err)
	})
	g.PUT("/settings", func(c *gin.Context) {
		var settings Settings
		err := input(c, &settings)
		if err == nil {
			err = s.SaveSettings(settings)
		}
		respond(c, gin.H{"ok": err == nil}, err)
	})
	g.GET("/requests", func(c *gin.Context) {
		rows, err := s.RequestLogs(c.Request.Context(), LogFilter{Provider: c.Query("provider"), Model: c.Query("model"), Account: c.Query("account"), Pool: c.Query("pool"), APIKey: c.Query("api_key"), Since: integer(c.Query("since")), Before: integer(c.Query("before")), BeforeID: c.Query("before_id"), Status: int(integer(c.Query("status"))), Limit: int(integer(c.Query("limit")))})
		respond(c, rows, err)
	})
	g.GET("/analytics", func(c *gin.Context) {
		rows, err := s.AnalyticsFiltered(c.Request.Context(), c.Query("since"), c.Query("until"), LogFilter{Provider: c.Query("provider"), Model: c.Query("model"), Account: c.Query("account"), Pool: c.Query("pool"), APIKey: c.Query("api_key")})
		respond(c, rows, err)
	})
	g.GET("/analytics/page", func(c *gin.Context) {
		page, err := s.AnalyticsPage(c.Request.Context(), c.Query("since"), c.Query("until"), LogFilter{Provider: c.Query("provider"), Model: c.Query("model"), Account: c.Query("account"), Pool: c.Query("pool"), APIKey: c.Query("api_key"), Limit: int(integer(c.Query("limit"))), Offset: int(integer(c.Query("offset")))})
		respond(c, page, err)
	})
	g.GET("/history/:kind", func(c *gin.Context) {
		rows, err := s.History(c.Request.Context(), c.Param("kind"), c.Query("account"))
		respond(c, rows, err)
	})
	g.GET("/history/:kind/page", func(c *gin.Context) {
		page, err := s.HistoryPage(c.Request.Context(), c.Param("kind"), c.Query("account"), integer(c.Query("before")), int(integer(c.Query("limit"))))
		respond(c, page, err)
	})
	g.POST("/accounts/:id/warm", func(c *gin.Context) { err := s.WarmNow(c.Param("id")); respond(c, gin.H{"ok": err == nil}, err) })
	g.POST("/backup", func(c *gin.Context) {
		path, err := s.Backup()
		if err == nil {
			err = s.mutate("maintenance.backup", path, func(*sql.Tx) error { return nil })
		}
		respond(c, gin.H{"file": path}, err)
	})
	g.POST("/compact", func(c *gin.Context) { path, err := s.Compact(); respond(c, gin.H{"backup": path}, err) })
	g.POST("/retention", func(c *gin.Context) {
		var v struct {
			Days int `json:"days"`
		}
		err := input(c, &v)
		if err == nil && v.Days <= 0 {
			err = errors.New("positive retention days required")
		}
		if err == nil {
			err = s.Retain(v.Days)
		}
		respond(c, gin.H{"ok": err == nil}, err)
	})
	return g
}
