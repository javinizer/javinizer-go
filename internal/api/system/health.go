package system

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/version"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// healthCheck godoc
// @Summary Health check
// @Description Check API health status and list all enabled scrapers. Returns version information and build metadata.
// @Tags system
// @Produce json
// @Success 200 {object} contracts.HealthResponse
// @Router /health [get]
func healthCheck(deps *core.APIDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Use accessor to get current registry (respects config reloads)
		registry := deps.GetScraperLister()
		scrapers := []string{}
		for _, s := range registry.GetEnabledInstances() {
			scrapers = append(scrapers, s.Name())
		}
		c.JSON(http.StatusOK, contracts.HealthResponse{
			Status:    "ok",
			Scrapers:  scrapers,
			Version:   version.Short(),
			Commit:    version.Commit,
			BuildDate: version.BuildDate,
		})
	}
}
