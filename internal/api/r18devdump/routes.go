package r18devdump

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes registers the r18.dev dump management routes. Read endpoints
// (status, search) use the protected group; mutating endpoints (download,
// update, clear) use the write-protected group which applies rate limiting.
func RegisterRoutes(protected *gin.RouterGroup, writeProtected *gin.RouterGroup, rt *core.APIRuntime) {
	dump := newDumpHandler(rt)
	protected.GET("/r18dev/dump/status", dump.getStatus)
	protected.GET("/r18dev/dump/search", dump.search)
	writeProtected.POST("/r18dev/dump/download", dump.startDownload)
	writeProtected.POST("/r18dev/dump/update", dump.startUpdate)
	writeProtected.DELETE("/r18dev/dump", dump.clearDump)
}
