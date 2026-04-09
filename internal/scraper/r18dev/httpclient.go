package r18dev

import (
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

// NewHTTPClient creates a new HTTP client for the R18.dev scraper.
// HTTP-01: Per-scraper HTTP client ownership.
// Uses builder pattern for consistent HTTP client construction.
func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, error) {
	builder := httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
		httpclient.WithHeaders(httpclient.R18DevHeaders()),
		httpclient.WithHeaders(httpclient.RefererHeader("https://r18.dev/")),
		httpclient.WithHeaders(httpclient.UserAgentHeader(cfg.UserAgent)),
	)
	return builder.BuildClient()
}
