package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/token"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const sessionCookieName = "javinizer_session"

// sessionIDFromRequest reads the session ID from the cookie, falling back to
// the X-Session-ID header and then the ?session= query parameter. macOS
// WKWebView does not reliably store Set-Cookie responses from the Wails
// asset server's custom URL scheme handler, so the desktop app sends the
// session ID via a header (and <img> tags append it as a query param) instead.
func sessionIDFromRequest(c *gin.Context) string {
	if sid, err := c.Cookie(sessionCookieName); err == nil && strings.TrimSpace(sid) != "" {
		return sid
	}
	if sid := c.GetHeader("X-Session-ID"); strings.TrimSpace(sid) != "" {
		return sid
	}
	if sid := c.Query("session"); strings.TrimSpace(sid) != "" {
		return sid
	}
	return ""
}

// authDisabler is an optional capability of AuthProvider implementations that
// explicitly bypasses authentication. Only the test-only testkit.NoOpAuth
// implements it; the production *AuthManager does not, so this path is
// unreachable in production. This keeps the test pass-through out of the
// production AuthProvider contract and free of any config-level bypass flag.
type authDisabler interface {
	IsDisabled() bool
}

func authBypassed(auth commandutil.AuthProvider) bool {
	d, ok := auth.(authDisabler)
	return ok && d.IsDisabled()
}

func resolveAuth(c *gin.Context, rt *core.APIRuntime) (deps *core.APIDeps, handled bool) {
	if rt == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, contracts.ErrorResponse{
			Error: "authentication is unavailable",
		})
		return nil, true
	}
	deps = rt.Deps()
	if deps == nil || deps.Auth == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, contracts.ErrorResponse{
			Error: "authentication is unavailable",
		})
		return nil, true
	}
	if authBypassed(deps.Auth) {
		c.Next()
		return deps, true
	}
	return deps, false
}

func securityConfig(rt *core.APIRuntime) *core.SecurityNarrowConfig {
	if rt == nil {
		return nil
	}
	return rt.GetAPIConfig().SecurityConfig()
}

//nolint:unused // used by same-package tests
func requireAuthenticated(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps, handled := resolveAuth(c, rt)
		if handled {
			return
		}

		if !deps.Auth.IsInitialized() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, contracts.ErrorResponse{
				Error: "authentication is not initialized",
			})
			return
		}

		sessionID := sessionIDFromRequest(c)
		if sessionID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, contracts.ErrorResponse{
				Error: "authentication required",
			})
			return
		}

		username, err := deps.Auth.AuthenticateSession(sessionID)
		if err != nil {
			if errors.Is(err, ErrAuthNotInitialized) {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, contracts.ErrorResponse{
					Error: "authentication is not initialized",
				})
				return
			}
			clearSessionCookie(c, securityConfig(rt))
			c.AbortWithStatusJSON(http.StatusUnauthorized, contracts.ErrorResponse{
				Error: "authentication required",
			})
			return
		}

		c.Set("auth_username", username)
		c.Next()
	}
}

func requireTokenOrSession(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps, handled := resolveAuth(c, rt)
		if handled {
			return
		}

		if !deps.Auth.IsInitialized() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, contracts.ErrorResponse{
				Error: "authentication is not initialized",
			})
			return
		}

		authHeader := c.GetHeader("Authorization")
		// Auth schemes are case-insensitive (RFC 7235); accept "bearer"/"BEARER" etc.,
		// not just "Bearer ", so valid variants don't fall through to session auth.
		const bearerPrefix = "Bearer "
		if len(authHeader) >= len(bearerPrefix) && strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
			rawToken := authHeader[len(bearerPrefix):]
			if !strings.HasPrefix(rawToken, token.TokenPrefix) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, contracts.ErrorResponse{
					Error: "invalid or revoked token",
				})
				return
			}

			hash := token.HashToken(rawToken)
			tokenID, err := deps.Auth.ValidateToken(c.Request.Context(), hash)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, contracts.ErrorResponse{
					Error: "invalid or revoked token",
				})
				return
			}

			if err := deps.Auth.UpdateTokenLastUsed(c.Request.Context(), tokenID); err != nil {
				logging.Warnf("failed to update token last_used_at for %s: %v", tokenID, err)
			}

			c.Set("auth_method", "token")
			c.Set("token_id", tokenID)
			c.Set("auth_username", "api_token")
			c.Next()
			return
		}

		sessionID := sessionIDFromRequest(c)
		if sessionID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, contracts.ErrorResponse{
				Error: "authentication required",
			})
			return
		}

		username, err := deps.Auth.AuthenticateSession(sessionID)
		if err != nil {
			if errors.Is(err, ErrAuthNotInitialized) {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, contracts.ErrorResponse{
					Error: "authentication is not initialized",
				})
				return
			}
			clearSessionCookie(c, securityConfig(rt))
			c.AbortWithStatusJSON(http.StatusUnauthorized, contracts.ErrorResponse{
				Error: "authentication required",
			})
			return
		}

		c.Set("auth_method", "session")
		c.Set("auth_username", username)
		c.Next()
	}
}
