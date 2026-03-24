package system

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
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

		start := time.Now()
		resp := ProxyTestResponse{
			Mode:      req.Mode,
			TargetURL: targetURL,
		}

		switch req.Mode {
		case "direct":
			if !req.Proxy.Enabled || strings.TrimSpace(req.Proxy.URL) == "" {
				c.JSON(400, ErrorResponse{Error: "proxy.enabled=true and proxy.url are required for direct proxy test"})
				return
			}
			resp.ProxyURL = httpclient.SanitizeProxyURL(req.Proxy.URL)

			client, err := httpclient.NewRestyClient(&req.Proxy, 30*time.Second, 0)
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
				resp.Message = fmt.Sprintf("direct proxy request failed: %v", err)
				c.JSON(200, resp)
				return
			}

			resp.StatusCode = httpResp.StatusCode()
			resp.Success = httpResp.StatusCode() >= 200 && httpResp.StatusCode() < 400
			if resp.Success {
				resp.Message = fmt.Sprintf("direct proxy request succeeded with status %d", httpResp.StatusCode())
			} else {
				resp.Message = fmt.Sprintf("direct proxy request returned status %d", httpResp.StatusCode())
			}
			c.JSON(200, resp)
		case "flaresolverr":
			if !req.Proxy.FlareSolverr.Enabled || strings.TrimSpace(req.Proxy.FlareSolverr.URL) == "" {
				c.JSON(400, ErrorResponse{Error: "proxy.flaresolverr.enabled=true and proxy.flaresolverr.url are required for flaresolverr test"})
				return
			}

			resp.ProxyURL = httpclient.SanitizeProxyURL(req.Proxy.URL)
			resp.FlareSolverrURL = req.Proxy.FlareSolverr.URL

			_, fs, err := httpclient.NewRestyClientWithFlareSolverr(&req.Proxy, 45*time.Second, 0)
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
