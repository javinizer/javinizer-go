package token

import (
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(protected *gin.RouterGroup, writeProtected *gin.RouterGroup, svc *TokenService) {
	protected.GET("/tokens", listTokens(svc))
	writeProtected.POST("/tokens", createToken(svc))
	writeProtected.DELETE("/tokens/:id", revokeToken(svc))
	writeProtected.POST("/tokens/:id/regenerate", regenerateToken(svc))
}
