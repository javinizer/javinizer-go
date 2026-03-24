package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const sessionCookieName = "javinizer_session"

func requireAuthenticated(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps == nil || deps.Auth == nil {
			c.Next()
			return
		}

		if !deps.Auth.IsInitialized() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, ErrorResponse{
				Error: "authentication is not initialized",
			})
			return
		}

		sessionID, err := c.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(sessionID) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
				Error: "authentication required",
			})
			return
		}

		username, err := deps.Auth.AuthenticateSession(sessionID)
		if err != nil {
			if errors.Is(err, ErrAuthNotInitialized) {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, ErrorResponse{
					Error: "authentication is not initialized",
				})
				return
			}
			clearSessionCookie(c)
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
				Error: "authentication required",
			})
			return
		}

		c.Set("auth_username", username)
		c.Next()
	}
}

func getAuthStatus(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps == nil || deps.Auth == nil {
			c.JSON(http.StatusOK, AuthStatusResponse{
				Initialized:   true,
				Authenticated: true,
			})
			return
		}

		if !deps.Auth.IsInitialized() {
			c.JSON(http.StatusOK, AuthStatusResponse{
				Initialized:   false,
				Authenticated: false,
			})
			return
		}

		resp := AuthStatusResponse{
			Initialized:   true,
			Authenticated: false,
		}

		sessionID, err := c.Cookie(sessionCookieName)
		if err == nil && strings.TrimSpace(sessionID) != "" {
			username, authErr := deps.Auth.AuthenticateSession(sessionID)
			if authErr == nil {
				resp.Authenticated = true
				resp.Username = username
			} else if errors.Is(authErr, ErrInvalidSession) {
				clearSessionCookie(c)
			}
		}

		c.JSON(http.StatusOK, resp)
	}
}

func setupAuth(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps == nil || deps.Auth == nil {
			c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "authentication is unavailable"})
			return
		}

		if deps.Auth.IsInitialized() {
			c.JSON(http.StatusConflict, ErrorResponse{Error: "authentication is already initialized"})
			return
		}

		var req AuthCredentialsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid authentication payload"})
			return
		}

		if err := deps.Auth.Setup(req.Username, req.Password); err != nil {
			switch {
			case errors.Is(err, ErrAuthAlreadySet):
				c.JSON(http.StatusConflict, ErrorResponse{Error: "authentication is already initialized"})
			case errors.Is(err, ErrInvalidUsername), errors.Is(err, ErrWeakPassword):
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to initialize authentication"})
			}
			return
		}

		sessionID, err := deps.Auth.Login(req.Username, req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create authenticated session"})
			return
		}

		setSessionCookie(c, sessionID, deps.Auth.SessionTTL())
		c.JSON(http.StatusOK, AuthStatusResponse{
			Initialized:   true,
			Authenticated: true,
			Username:      strings.TrimSpace(req.Username),
		})
	}
}

func loginAuth(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps == nil || deps.Auth == nil {
			c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "authentication is unavailable"})
			return
		}

		var req AuthCredentialsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid authentication payload"})
			return
		}

		sessionID, err := deps.Auth.Login(req.Username, req.Password)
		if err != nil {
			switch {
			case errors.Is(err, ErrAuthNotInitialized):
				c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "authentication is not initialized"})
			case errors.Is(err, ErrInvalidCredentials):
				c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "invalid username or password"})
			case errors.Is(err, ErrLoginRateLimited):
				c.JSON(http.StatusTooManyRequests, ErrorResponse{Error: "too many login attempts, please try again later"})
			default:
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "authentication failed"})
			}
			return
		}

		setSessionCookie(c, sessionID, deps.Auth.SessionTTL())
		c.JSON(http.StatusOK, AuthStatusResponse{
			Initialized:   true,
			Authenticated: true,
			Username:      strings.TrimSpace(req.Username),
		})
	}
}

func logoutAuth(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps != nil && deps.Auth != nil {
			sessionID, err := c.Cookie(sessionCookieName)
			if err == nil && strings.TrimSpace(sessionID) != "" {
				deps.Auth.Logout(sessionID)
			}
		}
		clearSessionCookie(c)
		c.JSON(http.StatusOK, gin.H{"message": "logged out"})
	}
}

func setSessionCookie(c *gin.Context, sessionID string, ttl time.Duration) {
	secure := isSecureRequest(c.Request)
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().Add(ttl).UTC(),
	}
	http.SetCookie(c.Writer, cookie)
}

func clearSessionCookie(c *gin.Context) {
	secure := isSecureRequest(c.Request)
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	}
	http.SetCookie(c.Writer, cookie)
}

func isSecureRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return r.TLS != nil
}
