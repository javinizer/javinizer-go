package javlibrary

import (
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	httpclient "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// NewHTTPClient creates an HTTP client and FlareSolverr for the javlibrary scraper.
// HTTP-01, HTTP-03: Per-scraper HTTP client and FlareSolverr ownership.
// The bool parameter (useFlareSolverr) indicates whether to enable FlareSolverr
// based on scraperCfg.UseFlareSolverr from the javlibrary config.
// Returns client, flaresolverr, and error.
func NewHTTPClient(cfg *config.ScraperConfig, globalProxy *config.ProxyConfig, useFlareSolverr bool) (*resty.Client, *httpclient.FlareSolverr, error) {
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

	// When cfg.FlareSolverr.Enabled is true, the ScraperConfig has been set up
	// with FlareSolverr.Enabled=true (via useFlareSolverr bool in javlibrary.go).
	// In this case, proxy must be enabled so buildFlareSolverrRequestProxy sets
	// fs.requestProxy for HTTP CONNECT tunneling to the FlareSolverr proxy server.
	var client *resty.Client
	var fs *httpclient.FlareSolverr
	var err error

	if cfg.FlareSolverr.Enabled {
		// When using FlareSolverr, we need to ensure:
		// 1. proxyWithFlareSolverr.Enabled is true so buildFlareSolverrRequestProxy works
		// 2. The FlareSolverr server has access to the global proxy to reach target sites
		// Use global proxy URL if scraper-specific proxy URL is empty but global proxy has one
		flareProxyURL := proxyCfg.URL
		flareUsername := proxyCfg.Username
		flarePassword := proxyCfg.Password
		if flareProxyURL == "" && globalProxy.URL != "" {
			// Scraper has no proxy override; use global proxy for FlareSolverr
			flareProxyURL = globalProxy.URL
			flareUsername = globalProxy.Username
			flarePassword = globalProxy.Password
		}

		proxyWithFlareSolverr := &config.ProxyConfig{
			Enabled:      true, // Must be true so buildFlareSolverrRequestProxy sets fs.requestProxy
			URL:          flareProxyURL,
			Username:     flareUsername,
			Password:     flarePassword,
			FlareSolverr: cfg.FlareSolverr,
		}
		client, fs, err = httpclient.NewRestyClientWithFlareSolverr(
			proxyWithFlareSolverr,
			timeout,
			retryCount,
		)
		if err != nil {
			logging.Errorf("JavLibrary: Failed to create FlareSolverr: %v", err)
			// Fall through to create client without FlareSolverr
			client, err = httpclient.NewRestyClient(proxyCfg, timeout, retryCount)
		}
	} else {
		client, err = httpclient.NewRestyClient(proxyCfg, timeout, retryCount)
	}
	if err != nil {
		return nil, nil, err
	}

	return client, fs, nil
}
