package auth

import "github.com/gin-gonic/gin"

func RequireAuthenticated(deps *ServerDependencies) gin.HandlerFunc {
	return requireAuthenticated(deps)
}

func RegisterPublicRoutes(v1 *gin.RouterGroup, deps *ServerDependencies) {
	v1.GET("/auth/status", getAuthStatus(deps))
	v1.POST("/auth/setup", setupAuth(deps))
	v1.POST("/auth/login", loginAuth(deps))
	v1.POST("/auth/logout", logoutAuth(deps))
}
