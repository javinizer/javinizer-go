package r18dev

import (
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

// NewHTTPClient creates a new HTTP client for the R18.dev scraper.
// It accepts *ScraperConfig to enable generic HTTP client setup without scraper-name branching.
// HTTP-01: Per-scraper HTTP client ownership.
func NewHTTPClient(cfg *config.ScraperConfig, globalProxy *config.ProxyConfig) (*resty.Client, error) {
	// Resolve proxy per-scraper (HTTP-02)
	proxyCfg := config.ResolveScraperProxy(*globalProxy, cfg.Proxy)

	// Use timeout from ScraperConfig, default to 30s
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Use RetryCount from ScraperConfig, default to 3
	retryCount := cfg.RetryCount
	if retryCount == 0 {
		retryCount = 3
	}

	// HTTP-03: Check for FlareSolverr on ScraperConfig directly (not via scraper-name branching)
	var client *resty.Client
	var err error

	if cfg.FlareSolverr.Enabled {
		// Build a ProxyConfig with FlareSolverr for the factory function
		proxyWithFlareSolverr := &config.ProxyConfig{
			Enabled:      proxyCfg.Enabled,
			URL:          proxyCfg.URL,
			Username:     proxyCfg.Username,
			Password:     proxyCfg.Password,
			FlareSolverr: cfg.FlareSolverr,
		}
		client, _, err = httpclient.NewRestyClientWithFlareSolverr(
			proxyWithFlareSolverr,
			timeout,
			retryCount,
		)
	} else {
		client, err = httpclient.NewRestyClient(proxyCfg, timeout, retryCount)
	}

	if err != nil {
		return nil, err
	}

	// Apply UserAgent from ScraperConfig
	userAgent := config.ResolveScraperUserAgent("", cfg.UseFakeUserAgent, cfg.UserAgent)
	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "application/json, text/html, */*")
	client.SetHeader("Referer", "https://r18.dev/")

	return client, nil
}
