package javstash

import (
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

// NewHTTPClient creates a new HTTP client for the JavStash scraper.
// HTTP-01: Per-scraper HTTP client ownership.
// Uses builder pattern for consistent HTTP client construction.
func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, error) {
	builder := httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
		httpclient.WithHeaders(httpclient.JSONAPIHeaders()),
		httpclient.WithHeaders(httpclient.UserAgentHeader(cfg.UserAgent)),
	)
	return builder.BuildClient()
}
