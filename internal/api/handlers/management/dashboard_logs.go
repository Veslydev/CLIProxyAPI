package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// DashboardLoggingToFile shares configuration ownership with dashboard key writes.
func (h *Handler) DashboardLoggingToFile(c *gin.Context) {
	var body struct {
		Value *bool `json:"value"`
	}
	if c.Request.Method == http.MethodPut {
		if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.Request.Method == http.MethodGet {
		c.JSON(200, gin.H{"logging-to-file": h.cfg.LoggingToFile})
		return
	}
	before := h.cfg
	next := *before
	next.LoggingToFile = *body.Value
	h.cfg = &next
	c.Set(ConfigV8ContextKey, true)
	if !h.persistLocked(c) {
		h.cfg = before
	}
}

func (h *Handler) DashboardLogs(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.GetLogs(c)
}
