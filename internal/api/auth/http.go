package auth

import (
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const sessionCookieName = "javinizer_session"

func securityConfig(deps *ServerDependencies) *config.SecurityConfig {
	if deps == nil {
		return nil
	}
	cfg := deps.GetConfig()
	if cfg == nil {
		return nil
	}
	return &cfg.API.Security
}

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
			clearSessionCookie(c, securityConfig(deps))
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
				Error: "authentication required",
			})
			return
		}

		c.Set("auth_username", username)
		c.Next()
	}
}

// getAuthStatus godoc
// @Summary Get authentication status
// @Description Check if authentication is initialized and if the current session is authenticated
// @Tags auth
// @Produce json
// @Success 200 {object} AuthStatusResponse
// @Failure 503 {object} ErrorResponse
// @Router /api/v1/auth/status [get]
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
				clearSessionCookie(c, securityConfig(deps))
			}
		}

		c.JSON(http.StatusOK, resp)
	}
}

var defaultTrustedCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
}

func parseCIDRList(raw string) []*net.IPNet {
	if raw == "" {
		return nil
	}
	var cidrs []*net.IPNet
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			logging.Warnf("Ignoring invalid CIDR in JAVINIZER_SETUP_TRUSTED_CIDRS: %s", s)
			continue
		}
		cidrs = append(cidrs, n)
	}
	return cidrs
}

var (
	trustedCIDRsOnce  sync.Once
	trustedCIDRSCache []*net.IPNet
)

func trustedCIDRs() []*net.IPNet {
	trustedCIDRsOnce.Do(func() {
		trustedCIDRSCache = computeTrustedCIDRs()
	})
	return trustedCIDRSCache
}

//nolint:unused
func resetTrustedCIDRsCache() {
	trustedCIDRsOnce = sync.Once{}
	trustedCIDRSCache = nil
}

func computeTrustedCIDRs() []*net.IPNet {
	var cidrs []*net.IPNet
	for _, s := range defaultTrustedCIDRs {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			continue
		}
		cidrs = append(cidrs, n)
	}
	if extra := os.Getenv("JAVINIZER_SETUP_TRUSTED_CIDRS"); extra != "" {
		cidrs = append(cidrs, parseCIDRList(extra)...)
	}
	return cidrs
}

func isTrustedClient(ipStr string) bool {
	ipStr = strings.TrimPrefix(strings.TrimSuffix(ipStr, "]"), "[")
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range trustedCIDRs() {
		if cidr.Contains(ip) {
			return true
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

// setupAuth godoc
// @Summary Initialize authentication
// @Description Set up initial admin credentials. Only available from localhost or with bootstrap secret when auth is not yet initialized.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body AuthCredentialsRequest true "Admin credentials"
// @Param X-Setup-Secret header string false "Bootstrap secret for remote setup"
// @Success 200 {object} AuthStatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /api/v1/auth/setup [post]
func setupAuth(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps == nil || deps.Auth == nil {
			c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "authentication is unavailable"})
			return
		}

		bootstrapSecret := os.Getenv("JAVINIZER_SETUP_SECRET")
		clientIP := peerIP(c.Request.RemoteAddr)

		if bootstrapSecret != "" {
			headerSecret := c.GetHeader("X-Setup-Secret")
			if headerSecret != bootstrapSecret {
				logging.Warnf("Setup attempt rejected from %s: invalid bootstrap secret", clientIP)
				c.AbortWithStatusJSON(http.StatusForbidden, ErrorResponse{Error: "setup requires a bootstrap secret"})
				return
			}
		} else {
			if !isTrustedClient(clientIP) {
				logging.Warnf("Setup attempt rejected from %s: remote access without bootstrap secret", clientIP)
				c.AbortWithStatusJSON(http.StatusForbidden, ErrorResponse{Error: "setup is only available from localhost or trusted networks"})
				return
			}
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

		sessionID, err := deps.Auth.Login(req.Username, req.Password, true)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create authenticated session"})
			return
		}

		setSessionCookie(c, sessionID, deps.Auth.SessionTTL(), true, securityConfig(deps))
		c.JSON(http.StatusOK, AuthStatusResponse{
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
// @Param request body AuthCredentialsRequest true "Login credentials"
// @Success 200 {object} AuthStatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /api/v1/auth/login [post]
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

		sessionID, err := deps.Auth.Login(req.Username, req.Password, req.RememberMe)
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

		setSessionCookie(c, sessionID, deps.Auth.SessionTTL(), req.RememberMe, securityConfig(deps))
		c.JSON(http.StatusOK, AuthStatusResponse{
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
func logoutAuth(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps != nil && deps.Auth != nil {
			sessionID, err := c.Cookie(sessionCookieName)
			if err == nil && strings.TrimSpace(sessionID) != "" {
				deps.Auth.Logout(sessionID)
			}
		}
		clearSessionCookie(c, securityConfig(deps))
		c.JSON(http.StatusOK, gin.H{"message": "logged out"})
	}
}

func setSessionCookie(c *gin.Context, sessionID string, ttl time.Duration, persistent bool, cfg *config.SecurityConfig) {
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

func clearSessionCookie(c *gin.Context, cfg *config.SecurityConfig) {
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

func isSecureRequest(r *http.Request, cfg *config.SecurityConfig) bool {
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
