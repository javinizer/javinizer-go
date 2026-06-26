package auth

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// getAuthStatus godoc
// @Summary Get authentication status
// @Description Check if authentication is initialized and if the current session is authenticated
// @Tags auth
// @Produce json
// @Success 200 {object} contracts.AuthStatusResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/auth/status [get]
func getAuthStatus(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "authentication is unavailable"})
			return
		}
		deps := rt.Deps()
		if deps == nil || deps.Auth == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "authentication is unavailable"})
			return
		}

		if !deps.Auth.IsInitialized() {
			c.JSON(http.StatusOK, contracts.AuthStatusResponse{
				Initialized:   false,
				Authenticated: false,
			})
			return
		}

		resp := contracts.AuthStatusResponse{
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
				clearSessionCookie(c, securityConfig(rt))
			}
		}

		c.JSON(http.StatusOK, resp)
	}
}

// setupAuth godoc
// @Summary Initialize authentication
// @Description Set up initial admin credentials. Only available from localhost or with bootstrap secret when auth is not yet initialized.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body contracts.AuthCredentialsRequest true "Admin credentials"
// @Param X-Setup-Secret header string false "Bootstrap secret for remote setup"
// @Success 200 {object} contracts.AuthStatusResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 403 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/auth/setup [post]
func setupAuth(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "authentication is unavailable"})
			return
		}
		deps := rt.Deps()
		if deps == nil || deps.Auth == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "authentication is unavailable"})
			return
		}

		bootstrapSecret := deps.Auth.GetEnv("JAVINIZER_SETUP_SECRET")
		clientIP := peerIP(c.Request.RemoteAddr)

		if bootstrapSecret != "" {
			headerSecret := c.GetHeader("X-Setup-Secret")
			if headerSecret != bootstrapSecret {
				logging.Warnf("Setup attempt rejected from %s: invalid bootstrap secret", clientIP)
				c.AbortWithStatusJSON(http.StatusForbidden, contracts.ErrorResponse{Error: "setup requires a bootstrap secret"})
				return
			}
		} else {
			if !isTrustedClient(clientIP, deps.Auth.GetEnv) {
				logging.Warnf("Setup attempt rejected from %s: remote access without bootstrap secret", clientIP)
				c.AbortWithStatusJSON(http.StatusForbidden, contracts.ErrorResponse{Error: "setup is only available from localhost or trusted networks"})
				return
			}
		}

		if deps.Auth.IsInitialized() {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "authentication is already initialized"})
			return
		}

		var req contracts.AuthCredentialsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "invalid authentication payload"})
			return
		}

		if err := deps.Auth.Setup(req.Username, req.Password); err != nil {
			switch {
			case errors.Is(err, ErrAuthAlreadySet):
				c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "authentication is already initialized"})
			case errors.Is(err, ErrInvalidUsername), errors.Is(err, ErrWeakPassword):
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "failed to initialize authentication"})
			}
			return
		}

		sessionID, err := deps.Auth.Login(req.Username, req.Password, true)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "failed to create authenticated session"})
			return
		}

		setSessionCookie(c, sessionID, deps.Auth.SessionTTL(), true, securityConfig(rt))
		c.JSON(http.StatusOK, contracts.AuthStatusResponse{
			Initialized:   true,
			Authenticated: true,
			Username:      strings.TrimSpace(req.Username),
		})
	}
}

// loginAuth godoc
// @Summary Login
// @Description Authenticate with username and password to create a session
// @Tags auth
// @Accept json
// @Produce json
// @Param request body contracts.AuthCredentialsRequest true "Login credentials"
// @Success 200 {object} contracts.AuthStatusResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 401 {object} contracts.ErrorResponse
// @Failure 429 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/auth/login [post]
func loginAuth(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "authentication is unavailable"})
			return
		}
		deps := rt.Deps()
		if deps == nil || deps.Auth == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "authentication is unavailable"})
			return
		}

		var req contracts.AuthCredentialsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "invalid authentication payload"})
			return
		}

		sessionID, err := deps.Auth.Login(req.Username, req.Password, req.RememberMe)
		if err != nil {
			switch {
			case errors.Is(err, ErrAuthNotInitialized):
				c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "authentication is not initialized"})
			case errors.Is(err, ErrInvalidCredentials):
				c.JSON(http.StatusUnauthorized, contracts.ErrorResponse{Error: "invalid username or password"})
			case errors.Is(err, ErrLoginRateLimited):
				c.JSON(http.StatusTooManyRequests, contracts.ErrorResponse{Error: "too many login attempts, please try again later"})
			default:
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "authentication failed"})
			}
			return
		}

		setSessionCookie(c, sessionID, deps.Auth.SessionTTL(), req.RememberMe, securityConfig(rt))
		c.JSON(http.StatusOK, contracts.AuthStatusResponse{
			Initialized:   true,
			Authenticated: true,
			Username:      strings.TrimSpace(req.Username),
		})
	}
}

// logoutAuth godoc
// @Summary Logout
// @Description End the current authenticated session
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/auth/logout [post]
func logoutAuth(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil {
			clearSessionCookie(c, securityConfig(rt))
			c.JSON(http.StatusOK, gin.H{"message": "logged out"})
			return
		}
		deps := rt.Deps()
		if deps != nil && deps.Auth != nil {
			sessionID, err := c.Cookie(sessionCookieName)
			if err == nil && strings.TrimSpace(sessionID) != "" {
				deps.Auth.Logout(sessionID)
			}
		}
		clearSessionCookie(c, securityConfig(rt))
		c.JSON(http.StatusOK, gin.H{"message": "logged out"})
	}
}

func setSessionCookie(c *gin.Context, sessionID string, ttl time.Duration, persistent bool, cfg *core.SecurityNarrowConfig) {
	secure := isSecureRequest(c.Request, cfg)
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	}
	if persistent {
		cookie.MaxAge = int(ttl.Seconds())
		cookie.Expires = time.Now().Add(ttl).UTC()
	}
	http.SetCookie(c.Writer, cookie)
}

func clearSessionCookie(c *gin.Context, cfg *core.SecurityNarrowConfig) {
	secure := isSecureRequest(c.Request, cfg)
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

func isSecureRequest(r *http.Request, cfg *core.SecurityNarrowConfig) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if cfg != nil && cfg.ForceSecureCookies {
		return true
	}
	if cfg != nil && len(cfg.TrustedProxies) > 0 {
		forwarded := r.Header.Get("X-Forwarded-Proto")
		if forwarded == "https" {
			clientIP := r.RemoteAddr
			if host, _, err := net.SplitHostPort(clientIP); err == nil {
				clientIP = host
			}
			for _, trusted := range cfg.TrustedProxies {
				if clientIP == trusted {
					return true
				}
			}
		}
	}
	return false
}

func peerIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return ip
}
