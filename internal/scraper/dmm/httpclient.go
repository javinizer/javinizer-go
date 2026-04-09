package dmm

import (
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

// NewHTTPClient creates an HTTP client for the DMM scraper.
// HTTP-01: Per-scraper HTTP client ownership.
// Returns client, effective proxyProfile (for browser use), and error.
func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, *config.ProxyProfile, error) {
	return httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
		httpclient.WithHeaders(httpclient.CombineHeaders(
			httpclient.DMMHeaders(),
			httpclient.UserAgentHeader(cfg.UserAgent),
		)),
	).BuildWithProxy()
}
