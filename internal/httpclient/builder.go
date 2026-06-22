package httpclient

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const (
	DefaultTimeout    = 30 * time.Second
	DefaultRetryCount = 3
)

type ScraperClient struct {
	Client       *resty.Client
	FlareSolverr *FlareSolverr
	ProxyProfile *models.ProxyProfile
}

type ScraperOption func(*scraperConfig)

type scraperConfig struct {
	timeout            time.Duration
	retryCount         int
	globalProxy        models.ProxyConfig
	globalFlareSolverr models.FlareSolverrConfig
	scraperProxy       *models.ProxyConfig
	flareSolverr       bool
	headers            map[string]string
	cookies            map[string]string
	returnProxyProfile bool
}

func defaultConfig() scraperConfig {
	return scraperConfig{
		timeout:            DefaultTimeout,
		retryCount:         DefaultRetryCount,
		globalProxy:        models.ProxyConfig{},
		globalFlareSolverr: models.FlareSolverrConfig{},
		scraperProxy:       nil,
		flareSolverr:       false,
		headers:            make(map[string]string),
		cookies:            make(map[string]string),
		returnProxyProfile: false,
	}
}

func newScraperClientBuilder() *ScraperClientBuilder {
	return &ScraperClientBuilder{
		config: defaultConfig(),
	}
}

type ScraperClientBuilder struct {
	config scraperConfig
}

func withTimeout(timeout time.Duration) ScraperOption {
	return func(c *scraperConfig) {
		c.timeout = timeout
	}
}

func withRetryCount(count int) ScraperOption {
	return func(c *scraperConfig) {
		c.retryCount = count
	}
}

func withGlobalProxy(global models.ProxyConfig) ScraperOption {
	return func(c *scraperConfig) {
		c.globalProxy = global
	}
}

func withGlobalFlareSolverr(cfg models.FlareSolverrConfig) ScraperOption {
	return func(c *scraperConfig) {
		c.globalFlareSolverr = cfg
	}
}

func withScraperProxy(scraper *models.ProxyConfig) ScraperOption {
	return func(c *scraperConfig) {
		c.scraperProxy = scraper
	}
}

func withFlareSolverr(enabled bool) ScraperOption {
	return func(c *scraperConfig) {
		c.flareSolverr = enabled
	}
}

func WithHeaders(headers map[string]string) ScraperOption {
	return func(c *scraperConfig) {
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

func withCookies(cookies map[string]string) ScraperOption {
	return func(c *scraperConfig) {
		c.cookies = cookies
	}
}

func (b *ScraperClientBuilder) Apply(opts ...ScraperOption) *ScraperClientBuilder {
	for _, opt := range opts {
		opt(&b.config)
	}
	return b
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

func (b *ScraperClientBuilder) BuildWithProxy() (*resty.Client, *models.ProxyProfile, error) {
	sc, err := b.build(true)
	if err != nil {
		return nil, nil, err
	}
	return sc.Client, sc.ProxyProfile, nil
}

func (b *ScraperClientBuilder) build(returnProxyProfile bool) (*ScraperClient, error) {
	cfg := b.config

	proxyProfile := models.ResolveScraperProxy(cfg.globalProxy, cfg.scraperProxy)

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
		result, fsErr := NewRestyClientWithFlareSolverr(
			proxyProfile,
			cfg.globalFlareSolverr,
			timeout,
			retryCount,
		)
		if fsErr != nil {
			logging.Warnf("ScraperClientBuilder: FlareSolverr creation failed, falling back: %v", fsErr)
			client, err = NewRestyClient(proxyProfile, timeout, retryCount)
			fs = nil
		} else {
			client = result.Client
			fs = result.FlareSolverr
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
		if !isValidCookieName(name) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", name, sanitizeCookieValue(value)))
	}
	return strings.Join(parts, "; ")
}

// isValidCookieName validates a cookie name against RFC 6265 token rules.
// Cookie names must be valid tokens: alphanumeric, dash, underscore, and a few special chars.
func isValidCookieName(name string) bool {
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

// sanitizeCookieValue removes characters forbidden in RFC 6265 cookie values.
// Prevents header injection and ensures parsing stability.
func sanitizeCookieValue(value string) string {
	return strings.Map(func(r rune) rune {
		if r == ';' || r == '"' || r == '\\' || r == '\r' || r == '\n' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

func FromScraperSettings(settings *models.ScraperSettings, globalProxy *models.ProxyConfig, globalFlareSolverr models.FlareSolverrConfig, opts ...ScraperOption) *ScraperClientBuilder {
	builder := newScraperClientBuilder()

	if settings != nil {
		if settings.Timeout > 0 {
			builder.Apply(withTimeout(time.Duration(settings.Timeout) * time.Second))
		}
		if settings.RetryCount > 0 {
			builder.Apply(withRetryCount(settings.RetryCount))
		}
		if settings.Proxy != nil {
			builder.Apply(withScraperProxy(settings.Proxy))
		}
		builder.Apply(withFlareSolverr(settings.UseFlareSolverr))

		if len(settings.Cookies) > 0 {
			builder.Apply(withCookies(settings.Cookies))
		}
	}

	if globalProxy != nil {
		builder.Apply(withGlobalProxy(*globalProxy))
	}

	builder.Apply(withGlobalFlareSolverr(globalFlareSolverr))
	builder.Apply(opts...)

	return builder
}
