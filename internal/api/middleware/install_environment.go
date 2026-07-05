package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/system"
)

// InstallEnvironmentInjector stamps the bootstrap-detected install environment
// (desktop/docker/cli) into each request's context so error handlers can tailor
// operator guidance to how javinizer is running. The environment is a
// process-level constant read from CoreDeps; nil deps fall back to CLI.
func InstallEnvironmentInjector(deps *commandutil.CoreDeps) gin.HandlerFunc {
	var env system.Environment
	if deps != nil {
		env = deps.InstallEnvironment()
	} else {
		env = system.EnvironmentCLI
	}
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(apperrors.WithInstallEnvironment(c.Request.Context(), env))
		c.Next()
	}
}
