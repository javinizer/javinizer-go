package system

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// ProxyTestResult holds the outcome of a proxy connectivity test,
// decoupled from the HTTP response layer so it can be tested independently.
type ProxyTestResult struct {
	Success           bool
	StatusCode        int
	DurationMS        int64
	Message           string
	ProxyURL          string
	FlareSolverrURL   string
	VerificationToken string
	TokenExpiresAt    int64
}

const defaultProxyTestURL = "https://httpbin.org/ip"

// testProxy godoc
// @Summary Test proxy connectivity
// @Description Test direct proxy or FlareSolverr access to a target URL using provided proxy settings
// @Tags system
// @Accept json
// @Produce json
// @Param request body contracts.ProxyTestRequest true "Proxy test request"
// @Success 200 {object} contracts.ProxyTestResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Router /api/v1/proxy/test [post]
func testProxy(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.ProxyTestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Invalid proxy test request"})
			return
		}

		targetURL := strings.TrimSpace(req.TargetURL)
		if targetURL == "" {
			targetURL = defaultProxyTestURL
		}
		if !isValidHTTPURL(targetURL) {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "target_url must be a valid http(s) URL"})
			return
		}
		if err := ssrf.CheckURL(targetURL); err != nil {
			c.JSON(http.StatusForbidden, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		var result ProxyTestResult
		switch req.Mode {
		case "direct":
			apiCfg := rt.GetAPIConfig()
			globalProxy := &apiCfg.ProxyConfig
			scraperProxy := &req.Proxy
			proxyProfile := models.ResolveScraperProxy(*globalProxy, scraperProxy)

			if !req.Proxy.Enabled || strings.TrimSpace(proxyProfile.URL) == "" {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "proxy.enabled=true and proxy profile with url are required for direct proxy test"})
				return
			}
			result = TestDirectProxy(c.Request.Context(), targetURL, proxyProfile, apiCfg.ScraperUserAgent)

		case "flaresolverr":
			if !req.FlareSolverr.Enabled || strings.TrimSpace(req.FlareSolverr.URL) == "" {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "flaresolverr.enabled=true and flaresolverr.url are required for flaresolverr test"})
				return
			}
			apiCfg := rt.GetAPIConfig()
			globalProxy := &apiCfg.ProxyConfig
			scraperProxy := &req.Proxy
			proxyProfile := models.ResolveScraperProxy(*globalProxy, scraperProxy)

			result = TestFlareSolverr(targetURL, req.FlareSolverr, proxyProfile)

		default:
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "mode must be 'direct' or 'flaresolverr'"})
			return
		}

		// Issue verification token for successful tests
		if result.Success {
			deps := rt.Deps()
			if deps.TokenStore != nil {
				var configHash string
				var hashErr error
				if req.Mode == "direct" {
					// Normalize: the frontend sends the default profile name in the
					// Profile field of the test request, but the saved config stores
					// it in DefaultProfile. Copy Profile → DefaultProfile (and clear
					// Profile) so the hash matches what validateProxySaveConfig
					// computes from newCfg.Scrapers.Proxy.
					normalized := req.Proxy
					if normalized.DefaultProfile == "" && normalized.Profile != "" {
						normalized.DefaultProfile = normalized.Profile
						normalized.Profile = ""
					}
					configHash, hashErr = core.HashProxyConfig(normalized)
				} else {
					configHash, hashErr = core.HashProxyConfig(req.FlareSolverr)
				}
				if hashErr != nil {
					logging.Warnf("failed to hash %s config: %v", req.Mode, hashErr)
				} else {
					// Direct proxy tests validate the global proxy configuration, so the
					// token is issued with scope "global" to match what config-save
					// validation expects (config.go validates "global" and "flaresolverr").
					tokenScope := req.Mode
					if req.Mode == "direct" {
						tokenScope = "global"
					}
					token, expiresAt, createErr := deps.TokenStore.Create(tokenScope, configHash)
					if createErr != nil {
						logging.Warnf("failed to generate verification token: %v", createErr)
					} else {
						result.VerificationToken = token
						result.TokenExpiresAt = expiresAt.Unix()
					}
				}
			}
		}

		// Map domain result to HTTP response
		c.JSON(http.StatusOK, contracts.ProxyTestResponse{
			Success:           result.Success,
			Mode:              req.Mode,
			TargetURL:         targetURL,
			StatusCode:        result.StatusCode,
			DurationMS:        result.DurationMS,
			Message:           result.Message,
			ProxyURL:          result.ProxyURL,
			FlareSolverrURL:   result.FlareSolverrURL,
			VerificationToken: result.VerificationToken,
			TokenExpiresAt:    result.TokenExpiresAt,
		})
	}
}

// TestDirectProxy tests direct proxy connectivity to a target URL.
// It creates a transport, sends an HTTP GET, and returns a ProxyTestResult
// with the outcome. This function is side-effect-free and testable in isolation.
func TestDirectProxy(ctx context.Context, targetURL string, proxyProfile *models.ProxyProfile, userAgent string) ProxyTestResult {
	start := time.Now()
	result := ProxyTestResult{
		ProxyURL: httpclient.SanitizeProxyURL(proxyProfile.URL),
	}

	transport, err := httpclient.NewTransport(proxyProfile)
	if err != nil {
		result.DurationMS = time.Since(start).Milliseconds()
		result.Message = fmt.Sprintf("failed to create proxy transport: %v", err)
		return result
	}
	defer transport.CloseIdleConnections()
	ssrf.WrapTransportWithSSRFCheck(transport)

	client := resty.New()
	client.SetTimeout(30 * time.Second)
	client.SetTransport(transport)
	client.SetRedirectPolicy(resty.NoRedirectPolicy())

	if userAgent == "" {
		userAgent = config.DefaultUserAgent
	}

	httpResp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", userAgent).
		SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8").
		Get(targetURL)

	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Message = formatDirectProxyError(err)
		return result
	}

	result.StatusCode = httpResp.StatusCode()
	result.Success = httpResp.StatusCode() >= 200 && httpResp.StatusCode() < 400
	if result.Success {
		result.Message = fmt.Sprintf("direct proxy request succeeded with status %d", httpResp.StatusCode())
	} else {
		result.Message = fmt.Sprintf("direct proxy request returned status %d", httpResp.StatusCode())
	}
	return result
}

// TestFlareSolverr tests FlareSolverr connectivity to a target URL.
// It creates a FlareSolverr client, resolves the URL, and returns a ProxyTestResult
// with the outcome. This function is side-effect-free and testable in isolation.
func TestFlareSolverr(targetURL string, flareCfg models.FlareSolverrConfig, proxyProfile *models.ProxyProfile) ProxyTestResult {
	start := time.Now()
	result := ProxyTestResult{
		ProxyURL:        httpclient.SanitizeProxyURL(proxyProfile.URL),
		FlareSolverrURL: flareCfg.URL,
	}

	restyResult, err := httpclient.NewRestyClientWithFlareSolverr(proxyProfile, flareCfg, 45*time.Second, 0)
	if err != nil {
		result.DurationMS = time.Since(start).Milliseconds()
		result.Message = fmt.Sprintf("failed to create flaresolverr client: %v", err)
		return result
	}
	if restyResult.FlareSolverr == nil {
		result.DurationMS = time.Since(start).Milliseconds()
		result.Message = "flaresolverr client is not enabled in proxy config"
		return result
	}

	html, cookies, err := restyResult.FlareSolverr.ResolveURL(targetURL)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Message = fmt.Sprintf("flaresolverr request failed: %v", err)
		return result
	}

	result.Success = true
	result.Message = fmt.Sprintf("flaresolverr resolved page successfully (%d bytes, %d cookies)", len(html), len(cookies))
	return result
}

func isValidHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func formatDirectProxyError(err error) string {
	base := fmt.Sprintf("direct proxy request failed: %v", err)
	if err == nil {
		return base
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "method not allowed") || strings.Contains(msg, "proxyconnect") {
		return base + ". The proxy URL appears to be a regular HTTP endpoint, not a forward proxy. Use an HTTP/SOCKS5 proxy host:port; use FlareSolverr only in FlareSolverr test mode."
	}

	return base
}
