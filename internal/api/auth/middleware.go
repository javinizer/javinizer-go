package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/token"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const sessionCookieName = "javinizer_session"

func securityConfig(rt *core.APIRuntime) *core.SecurityNarrowConfig {
	if rt == nil {
		return nil
	}
	return rt.GetAPIConfig().SecurityConfig()
}

//nolint:unused // used by same-package tests
func requireAuthenticated(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil {
			c.Next()
			return
		}
		deps := rt.Deps()
		if deps == nil || deps.Auth == nil {
			c.Next()
			return
		}

		if !deps.Auth.IsInitialized() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, contracts.ErrorResponse{
				Error: "authentication is not initialized",
			})
			return
		}

		sessionID, err := c.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(sessionID) == "" {
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
		if rt == nil {
			c.Next()
			return
		}
		deps := rt.Deps()
		if deps == nil || deps.Auth == nil {
			c.Next()
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

		sessionID, err := c.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(sessionID) == "" {
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
