package version

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/stretchr/testify/assert"
)

func TestRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	protected := r.Group("/api/v1")

	deps := &core.APIDeps{}

	assert.NotPanics(t, func() {
		RegisterRoutes(protected, deps)
	})
}
