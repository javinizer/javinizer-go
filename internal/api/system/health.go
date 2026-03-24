package system

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/version"
)

// healthCheck godoc
// @Summary Health check
// @Description Check API health status and list all enabled scrapers. Returns version information and build metadata.
// @Tags system
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func healthCheck(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Use getter to get current registry (respects config reloads)
		registry := deps.GetRegistry()
		scrapers := []string{}
		for _, s := range registry.GetEnabled() {
			scrapers = append(scrapers, s.Name())
		}
		c.JSON(200, HealthResponse{
			Status:    "ok",
			Scrapers:  scrapers,
			Version:   version.Short(),
			Commit:    version.Commit,
			BuildDate: version.BuildDate,
		})
	}
}
