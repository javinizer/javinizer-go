package system

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/ssrf"
)

// testProxy godoc
// @Summary Test proxy connectivity
// @Description Test direct proxy or FlareSolverr access to a target URL using provided proxy settings
// @Tags system
// @Accept json
// @Produce json
// @Param request body ProxyTestRequest true "Proxy test request"
// @Success 200 {object} ProxyTestResponse
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/proxy/test [post]
func testProxy(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ProxyTestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid proxy test request"})
			return
		}

		targetURL := strings.TrimSpace(req.TargetURL)
		if targetURL == "" {
			targetURL = defaultProxyTestURL
		}
		if !isValidHTTPURL(targetURL) {
			c.JSON(400, ErrorResponse{Error: "target_url must be a valid http(s) URL"})
			return
		}

		if err := ssrf.CheckURL(targetURL); err != nil {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: err.Error()})
			return
		}

		start := time.Now()
		resp := ProxyTestResponse{
			Mode:      req.Mode,
			TargetURL: targetURL,
		}

		switch req.Mode {
		case "direct":
			// Resolve proxy profile from the global config
			globalProxy := deps.GetConfig().Scrapers.Proxy
			proxyProfile := config.ResolveScraperProxy(globalProxy, &req.Proxy)

			if !req.Proxy.Enabled || strings.TrimSpace(proxyProfile.URL) == "" {
				c.JSON(400, ErrorResponse{Error: "proxy.enabled=true and proxy profile with url are required for direct proxy test"})
				return
			}
			resp.ProxyURL = httpclient.SanitizeProxyURL(proxyProfile.URL)

			client, err := httpclient.NewRestyClient(proxyProfile, 30*time.Second, 0)
			if err != nil {
				resp.Success = false
				resp.DurationMS = time.Since(start).Milliseconds()
				resp.Message = fmt.Sprintf("failed to create proxy client: %v", err)
				c.JSON(200, resp)
				return
			}

			userAgent := deps.GetConfig().Scrapers.UserAgent
			if userAgent == "" {
				userAgent = config.DefaultUserAgent
			}

			httpResp, err := client.R().
				SetHeader("User-Agent", userAgent).
				SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8").
				Get(targetURL)

			resp.DurationMS = time.Since(start).Milliseconds()
			if err != nil {
				resp.Success = false
				resp.Message = formatDirectProxyError(err)
				c.JSON(200, resp)
				return
			}

			resp.StatusCode = httpResp.StatusCode()
			resp.Success = httpResp.StatusCode() >= 200 && httpResp.StatusCode() < 400
			if resp.Success {
				resp.Message = fmt.Sprintf("direct proxy request succeeded with status %d", httpResp.StatusCode())
				// Issue verification token for successful test
				if deps.TokenStore != nil {
					vt := deps.TokenStore.Create("global", core.HashProxyConfig(req.Proxy))
					resp.VerificationToken = vt.Token
					resp.TokenExpiresAt = vt.ExpiresAt.Unix()
				}
			} else {
				resp.Message = fmt.Sprintf("direct proxy request returned status %d", httpResp.StatusCode())
			}
			c.JSON(200, resp)
		case "flaresolverr":
			if !req.FlareSolverr.Enabled || strings.TrimSpace(req.FlareSolverr.URL) == "" {
				c.JSON(400, ErrorResponse{Error: "flaresolverr.enabled=true and flaresolverr.url are required for flaresolverr test"})
				return
			}

			// Resolve proxy profile for FlareSolverr request proxy
			globalProxy := deps.GetConfig().Scrapers.Proxy
			proxyProfile := config.ResolveScraperProxy(globalProxy, &req.Proxy)

			resp.ProxyURL = httpclient.SanitizeProxyURL(proxyProfile.URL)
			resp.FlareSolverrURL = req.FlareSolverr.URL

			_, fs, err := httpclient.NewRestyClientWithFlareSolverr(proxyProfile, req.FlareSolverr, 45*time.Second, 0)
			if err != nil {
				resp.Success = false
				resp.DurationMS = time.Since(start).Milliseconds()
				resp.Message = fmt.Sprintf("failed to create flaresolverr client: %v", err)
				c.JSON(200, resp)
				return
			}
			if fs == nil {
				resp.Success = false
				resp.DurationMS = time.Since(start).Milliseconds()
				resp.Message = "flaresolverr client is not enabled in proxy config"
				c.JSON(200, resp)
				return
			}

			html, cookies, err := fs.ResolveURL(targetURL)
			resp.DurationMS = time.Since(start).Milliseconds()
			if err != nil {
				resp.Success = false
				resp.Message = fmt.Sprintf("flaresolverr request failed: %v", err)
				c.JSON(200, resp)
				return
			}

			resp.Success = true
			resp.Message = fmt.Sprintf("flaresolverr resolved page successfully (%d bytes, %d cookies)", len(html), len(cookies))
			// Issue verification token for successful test
			if deps.TokenStore != nil {
				vt := deps.TokenStore.Create("flaresolverr", core.HashProxyConfig(req.FlareSolverr))
				resp.VerificationToken = vt.Token
				resp.TokenExpiresAt = vt.ExpiresAt.Unix()
			}
			c.JSON(200, resp)
		default:
			c.JSON(400, ErrorResponse{Error: "mode must be 'direct' or 'flaresolverr'"})
		}
	}
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
