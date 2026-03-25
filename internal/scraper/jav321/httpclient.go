package jav321

import (
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

// NewHTTPClient creates a new HTTP client for the Jav321 scraper.
// HTTP-01: Per-scraper HTTP client ownership.
func NewHTTPClient(cfg *config.ScraperConfig, globalProxy *config.ProxyConfig) (*resty.Client, error) {
	proxyCfg := config.ResolveScraperProxy(*globalProxy, cfg.Proxy)

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	retryCount := cfg.RetryCount
	if retryCount == 0 {
		retryCount = 3
	}

	client, err := httpclient.NewRestyClient(proxyCfg, timeout, retryCount)
	if err != nil {
		return nil, err
	}

	userAgent := config.ResolveScraperUserAgent(cfg.UserAgent, cfg.UseFakeUserAgent, cfg.UserAgent)
	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client.SetHeader("Accept-Language", "ja,en-US;q=0.8,en;q=0.6")
	client.SetHeader("Connection", "keep-alive")
	client.SetHeader("Upgrade-Insecure-Requests", "1")

	return client, nil
}
