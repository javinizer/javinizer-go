package httpclient

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const (
	DefaultTimeout    = 30 * time.Second
	DefaultRetryCount = 3
)

type ScraperClient struct {
	Client       *resty.Client
	FlareSolverr *FlareSolverr
	ProxyProfile *config.ProxyProfile
}

type ScraperOption func(*scraperConfig)

type scraperConfig struct {
	timeout            time.Duration
	retryCount         int
	globalProxy        config.ProxyConfig
	globalFlareSolverr config.FlareSolverrConfig
	scraperProxy       *config.ProxyConfig
	flareSolverr       bool
	headers            map[string]string
	cookies            map[string]string
	returnProxyProfile bool
}

func defaultConfig() scraperConfig {
	return scraperConfig{
		timeout:            DefaultTimeout,
		retryCount:         DefaultRetryCount,
		globalProxy:        config.ProxyConfig{},
		globalFlareSolverr: config.FlareSolverrConfig{},
		scraperProxy:       nil,
		flareSolverr:       false,
		headers:            make(map[string]string),
		cookies:            make(map[string]string),
		returnProxyProfile: false,
	}
}

func NewScraperClientBuilder() *ScraperClientBuilder {
	return &ScraperClientBuilder{
		config: defaultConfig(),
	}
}

type ScraperClientBuilder struct {
	config scraperConfig
}

func WithTimeout(timeout time.Duration) ScraperOption {
	return func(c *scraperConfig) {
		c.timeout = timeout
	}
}

func WithRetryCount(count int) ScraperOption {
	return func(c *scraperConfig) {
		c.retryCount = count
	}
}

func WithGlobalProxy(global config.ProxyConfig) ScraperOption {
	return func(c *scraperConfig) {
		c.globalProxy = global
	}
}

func WithGlobalFlareSolverr(cfg config.FlareSolverrConfig) ScraperOption {
	return func(c *scraperConfig) {
		c.globalFlareSolverr = cfg
	}
}

func WithScraperProxy(scraper *config.ProxyConfig) ScraperOption {
	return func(c *scraperConfig) {
		c.scraperProxy = scraper
	}
}

func WithFlareSolverr(enabled bool) ScraperOption {
	return func(c *scraperConfig) {
		c.flareSolverr = enabled
	}
}

func WithHeader(key, value string) ScraperOption {
	return func(c *scraperConfig) {
		c.headers[key] = value
	}
}

func WithHeaders(headers map[string]string) ScraperOption {
	return func(c *scraperConfig) {
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

func WithCookies(cookies map[string]string) ScraperOption {
	return func(c *scraperConfig) {
		c.cookies = cookies
	}
}

func WithProxyProfileReturn(enabled bool) ScraperOption {
	return func(c *scraperConfig) {
		c.returnProxyProfile = enabled
	}
}

func (b *ScraperClientBuilder) Apply(opts ...ScraperOption) *ScraperClientBuilder {
	for _, opt := range opts {
		opt(&b.config)
	}
	return b
}

func (b *ScraperClientBuilder) Build() (*ScraperClient, error) {
	return b.build(b.config.returnProxyProfile)
}

func (b *ScraperClientBuilder) BuildClient() (*resty.Client, error) {
	sc, err := b.build(false)
	if err != nil {
		return nil, err
	}
	return sc.Client, nil
}

func (b *ScraperClientBuilder) BuildWithFlareSolverr() (*resty.Client, *FlareSolverr, error) {
	sc, err := b.build(false)
	if err != nil {
		return nil, nil, err
	}
	return sc.Client, sc.FlareSolverr, nil
}

func (b *ScraperClientBuilder) BuildWithProxy() (*resty.Client, *config.ProxyProfile, error) {
	sc, err := b.build(true)
	if err != nil {
		return nil, nil, err
	}
	return sc.Client, sc.ProxyProfile, nil
}

func (b *ScraperClientBuilder) build(returnProxyProfile bool) (*ScraperClient, error) {
	cfg := b.config

	proxyProfile := config.ResolveScraperProxy(cfg.globalProxy, cfg.scraperProxy)

	timeout := cfg.timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	retryCount := cfg.retryCount
	if retryCount == 0 {
		retryCount = DefaultRetryCount
	}

	var client *resty.Client
	var fs *FlareSolverr
	var err error

	if cfg.flareSolverr && cfg.globalFlareSolverr.Enabled {
		proxyForFS := proxyProfile
		if proxyProfile.URL == "" {
			globalProfile := config.ResolveGlobalProxy(cfg.globalProxy)
			if globalProfile != nil && globalProfile.URL != "" {
				proxyForFS = globalProfile
			}
		}

		client, fs, err = NewRestyClientWithFlareSolverr(
			proxyForFS,
			cfg.globalFlareSolverr,
			timeout,
			retryCount,
		)
		if err != nil {
			logging.Warnf("ScraperClientBuilder: FlareSolverr creation failed, falling back: %v", err)
			client, err = NewRestyClient(proxyProfile, timeout, retryCount)
			fs = nil
		}
	} else {
		client, err = NewRestyClient(proxyProfile, timeout, retryCount)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	for k, v := range cfg.headers {
		client.SetHeader(k, v)
	}

	if len(cfg.cookies) > 0 {
		cookieHeader := b.buildCookieHeader(cfg.cookies)
		if cookieHeader != "" {
			existing := client.Header.Get("Cookie")
			if existing != "" {
				cookieHeader = existing + "; " + cookieHeader
			}
			client.SetHeader("Cookie", cookieHeader)
		}
	}

	result := &ScraperClient{
		Client:       client,
		FlareSolverr: fs,
	}

	if returnProxyProfile {
		result.ProxyProfile = proxyProfile
	}

	return result, nil
}

func (b *ScraperClientBuilder) buildCookieHeader(cookies map[string]string) string {
	parts := make([]string, 0, len(cookies))
	for name, value := range cookies {
		if !IsValidCookieName(name) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", name, SanitizeCookieValue(value)))
	}
	return strings.Join(parts, "; ")
}

// IsValidCookieName validates a cookie name against RFC 6265 token rules.
// Cookie names must be valid tokens: alphanumeric, dash, underscore, and a few special chars.
func IsValidCookieName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !isTokenChar(r) {
			return false
		}
	}
	return true
}

// isTokenChar returns true if r is a valid token character per RFC 6265.
func isTokenChar(r rune) bool {
	return (r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		r == '-' || r == '_' ||
		r == '!' || r == '#' || r == '$' || r == '%' || r == '&' || r == '\'' ||
		r == '*' || r == '+' || r == '.' || r == '^' || r == '`' || r == '|' || r == '~'
}

// SanitizeCookieValue removes characters forbidden in RFC 6265 cookie values.
// Prevents header injection and ensures parsing stability.
func SanitizeCookieValue(value string) string {
	return strings.Map(func(r rune) rune {
		if r == ';' || r == '"' || r == '\\' || r == '\r' || r == '\n' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

func FromScraperSettings(settings *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig, opts ...ScraperOption) *ScraperClientBuilder {
	builder := NewScraperClientBuilder()

	if settings != nil {
		if settings.Timeout > 0 {
			builder.Apply(WithTimeout(time.Duration(settings.Timeout) * time.Second))
		}
		if settings.RetryCount > 0 {
			builder.Apply(WithRetryCount(settings.RetryCount))
		}
		if settings.Proxy != nil {
			builder.Apply(WithScraperProxy(settings.Proxy))
		}
		builder.Apply(WithFlareSolverr(settings.UseFlareSolverr))

		if len(settings.Cookies) > 0 {
			builder.Apply(WithCookies(settings.Cookies))
		}
	}

	if globalProxy != nil {
		builder.Apply(WithGlobalProxy(*globalProxy))
	}

	builder.Apply(WithGlobalFlareSolverr(globalFlareSolverr))
	builder.Apply(opts...)

	return builder
}
