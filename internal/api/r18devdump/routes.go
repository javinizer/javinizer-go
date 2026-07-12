package r18devdump

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes registers the r18.dev dump management routes on the given
// protected router group. These endpoints let the WebUI check dump status,
// download/update the dump sidecar, and search dvd_id ↔ content_id mappings
// without dropping to the CLI.
func RegisterRoutes(protected *gin.RouterGroup, rt *core.APIRuntime) {
	dump := newDumpHandler(rt)
	protected.GET("/r18dev/dump/status", dump.getStatus)
	protected.POST("/r18dev/dump/download", dump.startDownload)
	protected.POST("/r18dev/dump/update", dump.startUpdate)
	protected.GET("/r18dev/dump/search", dump.search)
}
