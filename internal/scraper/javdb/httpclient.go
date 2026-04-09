package javdb

import (
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

// NewHTTPClient creates an HTTP client and FlareSolverr for the JavDB scraper.
// HTTP-01, HTTP-03: Per-scraper HTTP client and FlareSolverr ownership.
// Uses builder pattern for consistent HTTP client construction.
// Returns client, flaresolverr, and error.
func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, *httpclient.FlareSolverr, error) {
	builder := httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
		httpclient.WithHeaders(httpclient.StandardHTMLHeaders()),
		httpclient.WithHeaders(httpclient.UserAgentHeader(cfg.UserAgent)),
	)
	return builder.BuildWithFlareSolverr()
}
