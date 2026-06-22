package system

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/stretchr/testify/assert"
)

func TestRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	protected := r.Group("/api/v1")

	deps := &core.APIDeps{}

	// Should not panic
	assert.NotPanics(t, func() {
		RegisterRoutes(protected, testkit.GetTestRuntime(deps))
	})
}

func TestRegisterCoreRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	deps := &core.APIDeps{}

	// Should not panic
	assert.NotPanics(t, func() {
		RegisterCoreRoutes(r, testkit.GetTestRuntime(deps))
	})
}
