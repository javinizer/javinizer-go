package tokyohot

import (
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

// NewHTTPClient creates a new HTTP client for the TokyoHot scraper.
// HTTP-01: Per-scraper HTTP client ownership.
func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, error) {
	return httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
		httpclient.WithHeaders(httpclient.CombineHeaders(
			httpclient.StandardHTMLHeaders(),
			httpclient.UserAgentHeader(cfg.UserAgent),
			map[string]string{"Accept-Language": "ja,en-US;q=0.8,en;q=0.6,zh;q=0.5"},
		)),
	).BuildClient()
}
