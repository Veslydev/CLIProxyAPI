package management

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// DashboardProviderKeys exposes redacted upstream keys, never client access keys.
// Atomic read/modify/save keeps concurrent additions from replacing each other.
func (h *Handler) DashboardProviderKeys(c *gin.Context) {
	var body struct {
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
	}
	provider := c.Query("provider")
	if c.Request.Method == http.MethodPost {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		provider = body.Provider
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	before := h.cfg
	next := *before
	switch provider {
	case "claude":
		if !dashboardKeyList(c, provider, &next.ClaudeKey, body.APIKey, func(k config.ClaudeKey) string { return k.APIKey }, func(key string) config.ClaudeKey { return config.ClaudeKey{APIKey: key} }) {
			return
		}
		next.SanitizeClaudeKeys()
	case "gemini":
		if !dashboardKeyList(c, provider, &next.GeminiKey, body.APIKey, func(k config.GeminiKey) string { return k.APIKey }, func(key string) config.GeminiKey { return config.GeminiKey{APIKey: key} }) {
			return
		}
		next.SanitizeGeminiKeys()
	case "codex", "xai":
		keys := &next.CodexKey
		if provider == "xai" {
			keys = &next.XAIKey
		}
		if !dashboardKeyList(c, provider, keys, body.APIKey, func(k config.CodexKey) string { return k.APIKey }, func(key string) config.CodexKey { return config.CodexKey{APIKey: key} }) {
			return
		}
	case "openai-compatibility":
		if c.Request.Method == http.MethodGet {
			c.JSON(200, gin.H{"keys": []any{}})
		} else {
			c.JSON(400, gin.H{"error": "use native OpenAI compatibility configuration"})
		}
		return
	default:
		c.JSON(400, gin.H{"error": "unsupported API-key provider"})
		return
	}
	h.cfg = &next
	c.Set(ConfigV8ContextKey, true)
	if !h.persistLocked(c) {
		h.cfg = before
	}
}

func dashboardKeyHash(provider, key string) string {
	digest := sha256.Sum256([]byte(provider + "\x00" + key))
	return hex.EncodeToString(digest[:])
}

func dashboardKeyList[T any](c *gin.Context, provider string, keys *[]T, added string, keyOf func(T) string, create func(string) T) bool {
	if c.Request.Method == http.MethodGet {
		rows := make([]gin.H, 0, len(*keys))
		for _, item := range *keys {
			key := keyOf(item)
			mask := "********"
			if len(key) > 12 {
				mask += key[len(key)-4:]
			}
			rows = append(rows, gin.H{"keyHash": dashboardKeyHash(provider, key), "maskedKey": mask, "provider": provider, "ownerUsername": nil, "ownerUserId": nil, "isOwn": false})
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(200, gin.H{"keys": rows})
		return false
	}
	items := append([]T(nil), (*keys)...)
	if c.Request.Method == http.MethodPost {
		added = strings.TrimSpace(added)
		if added == "" || len(added) > 8192 {
			c.JSON(400, gin.H{"error": "invalid API key"})
			return false
		}
		for _, item := range items {
			if keyOf(item) == added {
				c.JSON(409, gin.H{"error": "key already configured"})
				return false
			}
		}
		*keys = append(items, create(added))
		return true
	}
	index := -1
	for i, item := range items {
		if dashboardKeyHash(provider, keyOf(item)) == c.Param("hash") {
			if index != -1 {
				c.JSON(409, gin.H{"error": "ambiguous key; use native configuration"})
				return false
			}
			index = i
		}
	}
	if index == -1 {
		c.JSON(404, gin.H{"error": "unknown key"})
		return false
	}
	*keys = append(items[:index], items[index+1:]...)
	return true
}
