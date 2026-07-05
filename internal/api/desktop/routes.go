package desktop

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes wires the desktop bundle upgrade endpoints onto the protected
// router group. The handlers read the bootstrap-injected install environment
// and BundleUpdater via CoreDeps; on non-desktop builds every route returns 404.
func RegisterRoutes(protected *gin.RouterGroup, deps *core.APIDeps) {
	protected.POST("/desktop/upgrade", upgrade(deps.CoreDeps))
	protected.GET("/desktop/upgrade/status", upgradeStatus(deps.CoreDeps))
}
