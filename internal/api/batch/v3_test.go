package batch

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/stretchr/testify/assert"
)

// TestRegisterRoutesV3_DoesNotPanic tests that RegisterRoutes doesn't panic with nil group
func TestRegisterRoutesV3_DoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("/api")

	// RegisterRoutes should not panic even with nil deps (handlers will fail at request time)
	assert.NotPanics(t, func() {
		RegisterRoutes(protected, (*core.APIRuntime)(nil))
	})
}

// TestRegisterRoutesV3_RouteCount tests that the expected number of routes are registered
func TestRegisterRoutesV3_RouteCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("/api")

	RegisterRoutes(protected, (*core.APIRuntime)(nil))

	routes := router.Routes()
	// We expect 15 batch routes
	assert.GreaterOrEqual(t, len(routes), 15, "should register at least 15 batch routes")
}
