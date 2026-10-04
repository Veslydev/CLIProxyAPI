package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/controlplane"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// Standalone search bypasses executor usage reporters. Keep its request evidence
// without inventing token counts that the search endpoint does not report.
func (s *Server) trackControlPlaneHTTP(c *gin.Context, ctx context.Context, selected *auth.Auth, model string) (context.Context, func(http.Header)) {
	if s.controlPlane == nil {
		return ctx, func(http.Header) {}
	}
	started := time.Now()
	id := uuid.NewString()
	ctx = usage.WithExecutionRequestID(ctx, id)
	return ctx, func(headers http.Header) {
		status := c.Writer.Status()
		s.controlPlane.HandleUsage(ctx, usage.Record{RequestID: id, Provider: selected.Provider, AuthID: selected.ID, Model: model, APIKey: controlplane.RequestFromContext(ctx).KeyID, RequestedAt: started, Latency: time.Since(started), Failed: status >= 400, Fail: usage.Failure{StatusCode: status}, ResponseHeaders: headers})
	}
}

func (s *Server) initControlPlane(manager *auth.Manager) {
	cfg := s.cfg.ControlPlane
	if !cfg.Enabled {
		return
	}
	env := cfg.AdminSecretEnv
	if env == "" {
		env = "CONTROL_PLANE_ADMIN_SECRET"
	}
	secret := os.Getenv(env)
	if len(secret) < 32 {
		s.controlPlaneErr = fmt.Errorf("control plane requires a separate admin secret of at least 32 bytes in %s", env)
	}
	if s.cfg.Home.Enabled {
		s.controlPlaneErr = fmt.Errorf("local control plane and Home dispatch cannot be enabled together")
	}
	if manager == nil {
		s.controlPlaneErr = fmt.Errorf("control plane requires auth manager")
	}
	if s.controlPlaneErr == nil {
		path := cfg.Database
		if path == "" {
			path = "data/controlplane.sqlite"
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(s.configFilePath), path)
		}
		store, err := controlplane.Open(path)
		if err == nil {
			err = store.SyncAccounts(manager.List())
		}
		if err == nil {
			err = store.RecoverWarmups()
		}
		if err == nil {
			err = store.PlanWarmups()
		}
		if err != nil {
			if store != nil {
				_ = store.Close()
			}
			s.controlPlaneErr = fmt.Errorf("initialize control plane: %w", err)
		} else {
			s.controlPlane = store
		}
	}
	if s.controlPlaneErr != nil {
		// NewServer predates error returns. Fail closed even when its Handler is embedded.
		s.engine.Use(func(c *gin.Context) {
			c.AbortWithStatusJSON(503, gin.H{"error": "control-plane initialization failed"})
		})
		return
	}
	manager.SetCandidatePolicy(s.controlPlane)
	s.engine.Use(s.controlPlane.ObserveInferenceHTTP())
	s.engine.Use(s.controlPlane.InferenceMiddleware())
	g := s.controlPlane.RegisterRoutes(s.engine, secret)
	g.GET("/runtime", func(c *gin.Context) {
		c.JSON(200, gin.H{"admin_auth": "separate bearer credential", "admin_secret_source": env, "admin_secret_min_bytes": 32, "secret_rotation": "update environment and restart", "tls_enabled": s.cfg.TLS.Enable, "database": cfg.Database, "dashboard": cfg.DashboardFile, "retention_days": cfg.RetentionDays, "oauth_secrets_in_database": false, "home_enabled": s.cfg.Home.Enabled})
	})
	g.Use(func(c *gin.Context) {
		c.Next()
		if c.Writer.Status() < 400 {
			// Persist route patterns only; OAuth URLs and state query parameters are sensitive.
			if err := s.controlPlane.Audit("provider.operation", c.Request.Method+" "+c.FullPath()); err != nil {
				log.WithError(err).Error("control-plane provider operation audit failed")
			}
		}
	})
	launch := func(provider string, handler gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if target := c.Query("account"); target != "" {
				validate, err := s.controlPlane.ReauthValidator(target, provider)
				if err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				management.SetOAuthCompletionValidator(c, validate)
			}
			handler(c)
		}
	}
	g.GET("/oauth/codex", launch("codex", s.mgmt.RequestCodexToken))
	g.GET("/oauth/claude", launch("claude", s.mgmt.RequestAnthropicToken))
	g.GET("/oauth/antigravity", launch("antigravity", s.mgmt.RequestAntigravityToken))
	g.GET("/oauth/start", func(c *gin.Context) {
		provider := strings.ToLower(strings.TrimSpace(c.Query("provider")))
		// The v8 dispatcher also owns plugin login. Do not maintain a second
		// provider implementation or infer identity continuity for plugins.
		launch(provider, s.mgmt.StartOAuthV8)(c)
	})
	g.GET("/oauth/providers", s.mgmt.DashboardOAuthProviders)
	g.GET("/native/provider-keys", s.mgmt.DashboardProviderKeys)
	g.POST("/native/provider-keys", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		s.mgmt.DashboardProviderKeys(c)
	})
	g.DELETE("/native/provider-keys/:hash", s.mgmt.DashboardProviderKeys)
	g.POST("/native/credentials", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
		s.mgmt.UploadAuthFile(c)
	})
	g.GET("/native/logs", s.mgmt.DashboardLogs)
	g.PUT("/native/logging-to-file", s.mgmt.DashboardLoggingToFile)
	g.GET("/native/logging-to-file", s.mgmt.DashboardLoggingToFile)
	g.GET("/oauth/status", s.mgmt.GetAuthStatus)
	g.DELETE("/oauth/session", s.mgmt.CancelAuthSession)
	g.POST("/oauth/callback", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		s.mgmt.PostOAuthCallback(c)
	})
	g.POST("/accounts/:id/quota", func(c *gin.Context) {
		if err := s.controlPlane.RefreshQuota(c.Request.Context(), manager, c.Param("id")); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	serveDashboard := func(c *gin.Context, file string) {
		if file == "" {
			file = "dashboard/dist/index.html"
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(filepath.Dir(s.configFilePath), file)
		}
		if _, err := os.Stat(file); err != nil {
			c.String(http.StatusServiceUnavailable, "Dashboard bundle missing. Build the configured dashboard directory.")
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-cache")
		c.File(file)
	}
	s.engine.GET("/dashboard", func(c *gin.Context) { serveDashboard(c, cfg.DashboardFile) })
	s.engine.GET("/dashboard/*page", func(c *gin.Context) { serveDashboard(c, cfg.DashboardFile) })
	s.engine.GET("/control-plane", func(c *gin.Context) {
		file := cfg.OperationsDashboardFile
		if file == "" {
			file = "web/dist/index.html"
		}
		serveDashboard(c, file)
	})
	usage.DefaultManager().RegisterNamed("vesly-control-plane-"+strings.ReplaceAll(s.configFilePath, "/", "_"), s.controlPlane)
	ctx, cancel := context.WithCancel(context.Background())
	s.controlPlaneCancel = cancel
	s.controlPlaneDone = make(chan struct{})
	go func() { defer close(s.controlPlaneDone); s.controlPlane.Run(ctx, manager, cfg.RetentionDays) }()
}
