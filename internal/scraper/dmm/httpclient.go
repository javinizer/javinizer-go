package dmm

import (
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// NewHTTPClient creates an HTTP client for the DMM scraper.
// HTTP-01: Per-scraper HTTP client ownership.
// Returns client, effective proxyConfig (for browser use), and error.
func NewHTTPClient(cfg *config.ScraperConfig, globalProxy *config.ProxyConfig) (*resty.Client, *config.ProxyConfig, error) {
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

	client, err := httpclient.NewRestyClient(proxyCfg, timeout, retryCount)
	if err != nil {
		return nil, nil, err
	}

	// Apply UserAgent from ScraperConfig
	userAgent := config.ResolveScraperUserAgent(
		cfg.UserAgent,
		cfg.UseFakeUserAgent,
		cfg.UserAgent,
	)
	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client.SetHeader("Accept-Language", "en-US,en;q=0.9,ja;q=0.8")
	client.SetHeader("Accept-Encoding", "gzip, deflate, br")
	client.SetHeader("Connection", "keep-alive")
	client.SetHeader("Upgrade-Insecure-Requests", "1")
	client.SetHeader("Cookie", "age_check_done=1; cklg=ja")

	return client, proxyCfg, nil
}

// NewHTTPClientWithDefaults creates an HTTP client using DMM-specific defaults.
// This is a convenience wrapper for the common case where DMMConfig is available.
func NewHTTPClientWithDefaults(cfg *config.Config) (*resty.Client, *config.ProxyConfig, error) {
	scraperCfg := &config.ScraperConfig{
		Enabled:          cfg.Scrapers.DMM.Enabled,
		Timeout:          30, // default (seconds)
		RateLimit:        0,  // DMM doesn't have per-request delay in its config
		RetryCount:       3,  // default
		UseFakeUserAgent: cfg.Scrapers.DMM.UseFakeUserAgent,
		UserAgent:        cfg.Scrapers.DMM.FakeUserAgent,
		Proxy:            cfg.Scrapers.DMM.Proxy,
		DownloadProxy:    cfg.Scrapers.DMM.DownloadProxy,
		FlareSolverr:     cfg.Scrapers.Proxy.FlareSolverr, // inherit global if not overridden
	}

	client, proxyConfig, err := NewHTTPClient(scraperCfg, &cfg.Scrapers.Proxy)
	if err != nil {
		logging.Errorf("DMM: Failed to create HTTP client: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
		proxyConfig = nil
	}

	return client, proxyConfig, nil
}
