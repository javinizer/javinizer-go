package apperrors

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/system"
)

type envContextKey struct{}

var installEnvironmentKey = envContextKey{}

// WithInstallEnvironment returns a copy of ctx carrying the install environment
// so downstream error handlers can surface environment-aware operator guidance
// without the error site itself needing to know how javinizer was installed.
// The middleware layer (which has the CoreDeps reader) stamps this once per
// request; path-validation errors are re-stamped at the write seam.
func WithInstallEnvironment(ctx context.Context, env system.Environment) context.Context {
	return context.WithValue(ctx, installEnvironmentKey, env)
}

// InstallEnvironmentFromContext extracts the install environment stamped by
// WithInstallEnvironment. Returns ok=false when unset (callers fall back to
// the sentinel's default OperatorMessage).
func InstallEnvironmentFromContext(ctx context.Context) (system.Environment, bool) {
	env, ok := ctx.Value(installEnvironmentKey).(system.Environment)
	return env, ok
}

// AllowedDirsEmptyOperatorMessage returns install-environment-aware guidance for
// the "no allowed directories configured" failure. Desktop users can fix this
// from the UI (Settings → Security), CLI users edit config.yaml, and Docker
// users must mount a directory and reference it. The default CLI guidance is
// kept on the ErrAllowedDirsEmpty sentinel for callers without an environment.
func AllowedDirsEmptyOperatorMessage(env system.Environment) string {
	switch env {
	case system.EnvironmentDesktop:
		return "Open Settings → Security to add directories to the allowed list"
	case system.EnvironmentDocker:
		return "Mount a directory into the container and add it to api.security.allowed_directories in your configuration file"
	default:
		return ErrAllowedDirsEmpty.OperatorMessage
	}
}

// operatorMessageFor returns the operator-facing guidance for a path error,
// substituting environment-aware text when the request carried an install
// environment and the error is the allowlist-empty failure. For all other
// errors the sentinel's OperatorMessage is returned unchanged.
func operatorMessageFor(err *PathError, ctx context.Context) string {
	if ctx == nil {
		return err.OperatorMessage
	}
	if err.Code == CodeAllowedDirsEmpty {
		if env, ok := InstallEnvironmentFromContext(ctx); ok {
			return AllowedDirsEmptyOperatorMessage(env)
		}
	}
	return err.OperatorMessage
}

// requestContext safely extracts the request context from a gin.Context,
// returning nil when the request is unset (e.g. unit tests that build a
// context via gin.CreateTestContext without assigning c.Request).
func requestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return nil
	}
	return c.Request.Context()
}
