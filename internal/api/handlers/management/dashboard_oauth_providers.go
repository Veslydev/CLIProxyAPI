package management

import "github.com/gin-gonic/gin"

// DashboardOAuthProviders reports the native dispatcher and registered plugins.
// Uninstalled provider plugins are not silently promoted to native providers.
func (h *Handler) DashboardOAuthProviders(c *gin.Context) {
	providers := []gin.H{}
	for _, id := range []string{"claude", "codex", "antigravity", "kimi", "kimi-ai", "xai", "devin", "meta"} {
		providers = append(providers, gin.H{"id": id, "name": id, "native": true})
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pluginHost != nil && h.cfg.Plugins.Enabled {
		for _, plugin := range h.pluginHost.RegisteredPlugins() {
			if plugin.SupportsOAuth && plugin.OAuthProvider != "" && pluginInstanceEnabled(h.cfg.Plugins.Configs[plugin.ID]) {
				providers = append(providers, gin.H{"id": plugin.OAuthProvider, "name": plugin.Metadata.Name, "native": false})
			}
		}
	}
	c.JSON(200, gin.H{"providers": providers})
}
