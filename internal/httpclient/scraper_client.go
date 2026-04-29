package httpclient

import (
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

type ScraperHTTPClientOption func(*scraperHTTPConfig)

type scraperHTTPConfig struct {
	headers   map[string]string
	cookies   map[string]string
	withProxy bool
}

func WithScraperHeaders(headers map[string]string) ScraperHTTPClientOption {
	return func(c *scraperHTTPConfig) {
		if c.headers == nil {
			c.headers = make(map[string]string)
		}
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

func WithScraperCookies(cookies map[string]string) ScraperHTTPClientOption {
	return func(c *scraperHTTPConfig) {
		c.cookies = cookies
	}
}

func WithProxyProfile() ScraperHTTPClientOption {
	return func(c *scraperHTTPConfig) {
		c.withProxy = true
	}
}

func NewScraperHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig, opts ...ScraperHTTPClientOption) (*resty.Client, error) {
	httpOpts := &scraperHTTPConfig{}
	for _, opt := range opts {
		opt(httpOpts)
	}

	var scraperOpts []ScraperOption
	if len(httpOpts.headers) > 0 {
		scraperOpts = append(scraperOpts, WithHeaders(httpOpts.headers))
	}
	if len(httpOpts.cookies) > 0 {
		scraperOpts = append(scraperOpts, WithCookies(httpOpts.cookies))
	}

	builder := FromScraperSettings(cfg, globalProxy, globalFlareSolverr, scraperOpts...)

	if httpOpts.withProxy {
		client, _, err := builder.BuildWithProxy()
		return client, err
	}

	return builder.BuildClient()
}

type ScraperClientResult struct {
	Client       *resty.Client
	ProxyProfile *config.ProxyProfile
	ProxyEnabled bool
}

func InitScraperClient(settings *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig, opts ...ScraperHTTPClientOption) *ScraperClientResult {
	scraperCfg := &config.ScraperSettings{
		Enabled:       settings.Enabled,
		Timeout:       settings.Timeout,
		RateLimit:     settings.RateLimit,
		RetryCount:    settings.RetryCount,
		UserAgent:     settings.UserAgent,
		Proxy:         settings.Proxy,
		DownloadProxy: settings.DownloadProxy,
	}

	globalProxyVal := config.ProxyConfig{}
	if globalProxy != nil {
		globalProxyVal = *globalProxy
	}

	proxyEnabled := globalProxyVal.Enabled
	if settings.Proxy != nil && settings.Proxy.Enabled {
		proxyEnabled = true
	}

	proxyConfig := config.ResolveScraperProxy(globalProxyVal, settings.Proxy)

	allOpts := append([]ScraperHTTPClientOption{WithProxyProfile()}, opts...)

	client, err := NewScraperHTTPClient(scraperCfg, globalProxy, globalFlareSolverr, allOpts...)
	if err != nil {
		logging.Errorf("InitScraperClient: Failed to create HTTP client: %v, using no-proxy fallback", err)
		client = NewRestyClientNoProxy(time.Duration(settings.Timeout)*time.Second, settings.RetryCount)
	}

	if proxyEnabled && proxyConfig.URL != "" {
		logging.Infof("InitScraperClient: Using proxy %s", SanitizeProxyURL(proxyConfig.URL))
	}

	return &ScraperClientResult{
		Client:       client,
		ProxyProfile: proxyConfig,
		ProxyEnabled: proxyEnabled,
	}
}
