package javstash

import (
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, error) {
	globalProxyVal := config.ProxyConfig{}
	if globalProxy != nil {
		globalProxyVal = *globalProxy
	}
	proxyCfg := config.ResolveScraperProxy(globalProxyVal, cfg.Proxy)

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	retryCount := cfg.RetryCount
	if retryCount == 0 {
		retryCount = 3
	}

	var client *resty.Client
	var err error

	if globalFlareSolverr.Enabled && cfg.UseFlareSolverr {
		client, _, err = httpclient.NewRestyClientWithFlareSolverr(
			proxyCfg,
			globalFlareSolverr,
			timeout,
			retryCount,
		)
	} else {
		client, err = httpclient.NewRestyClient(proxyCfg, timeout, retryCount)
	}

	if err != nil {
		return nil, err
	}

	userAgent := config.ResolveScraperUserAgent(cfg.UserAgent)
	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "application/json")
	client.SetHeader("Content-Type", "application/json")

	return client, nil
}
