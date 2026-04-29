package dmm

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
)

const (
	baseURL             = "https://www.dmm.co.jp"
	newBaseURL          = "https://video.dmm.co.jp"
	searchURL           = baseURL + "/search/=/searchstr=%s/"
	digitalURL          = baseURL + "/digital/videoa/-/detail/=/cid=%s/"
	physicalURL         = baseURL + "/mono/dvd/-/detail/=/cid=%s/"
	newDigitalURL       = newBaseURL + "/av/content/?id=%s"
	newAmateurURL       = newBaseURL + "/amateur/content/?id=%s"
	actressLinkSelector = `a[href*='?actress='], a[href*='&actress='], a[href*='/article=actress/id=']`
)

var (
	normalizeIDRegex        = regexp.MustCompile(`^([a-z]+)(\d+)(.*)$`)
	normalizeContentIDRegex = regexp.MustCompile(`^([a-z]+)(\d+)(.*)$`)
	contentIDUnpadRegex     = regexp.MustCompile(`^([a-z]+)0*(\d+.*)$`)
	cleanPrefixRegex        = regexp.MustCompile(`^(?:\d+|h_\d+)?([a-z]+\d+.*)$`)
	actressIDRegex          = regexp.MustCompile(`[?&]actress=(\d+)`)
	actressArticleIDRegex   = regexp.MustCompile(`/article=actress/id=(\d+)`)
	actressParenRegex       = regexp.MustCompile(`\(.*\)|（.*）`)
	actressJapaneseCharRe   = regexp.MustCompile(`\p{Hiragana}|\p{Katakana}|\p{Han}`)
	dmmCIDRegex             = regexp.MustCompile(`cid=([^/?&]+)`)
	dmmIDRegex              = regexp.MustCompile(`[?&]id=([^/?&]+)`)
)

type Scraper struct {
	client        *resty.Client
	enabled       bool
	scrapeActress bool
	useBrowser    bool
	browserConfig config.BrowserConfig
	contentIDRepo *database.ContentIDMappingRepository
	proxyProfile  *config.ProxyProfile
	proxyOverride *config.ProxyConfig
	downloadProxy *config.ProxyConfig
	rateLimiter   *ratelimit.Limiter
	settings      config.ScraperSettings
}

func resolveTimeout(scraperTimeout, globalTimeout int) int {
	if scraperTimeout > 0 {
		return scraperTimeout
	}
	if globalTimeout > 0 {
		return globalTimeout
	}
	return 30
}

func New(settings config.ScraperSettings, globalConfig *config.ScrapersConfig, contentIDRepo *database.ContentIDMappingRepository) *Scraper {
	if globalConfig == nil {
		globalConfig = &config.ScrapersConfig{}
	}

	resolvedTimeout := resolveTimeout(settings.Timeout, globalConfig.TimeoutSeconds)
	settings.Timeout = resolvedTimeout

	result := httpclient.InitScraperClient(&settings, &globalConfig.Proxy, globalConfig.FlareSolverr,
		httpclient.WithScraperHeaders(httpclient.CombineHeaders(
			httpclient.DMMHeaders(),
			httpclient.UserAgentHeader(settings.UserAgent),
		)),
		httpclient.WithProxyProfile(),
	)
	client := result.Client
	proxyProfile := result.ProxyProfile

	return &Scraper{
		client:        client,
		enabled:       settings.Enabled,
		scrapeActress: settings.ShouldScrapeActress(globalConfig.ScrapeActress),
		useBrowser:    settings.ShouldUseBrowser(globalConfig.Browser.Enabled),
		browserConfig: globalConfig.Browser,
		contentIDRepo: contentIDRepo,
		proxyProfile:  proxyProfile,
		proxyOverride: settings.Proxy,
		downloadProxy: settings.DownloadProxy,
		rateLimiter:   ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		settings:      settings,
	}
}

func (s *Scraper) Name() string {
	return "dmm"
}

func (s *Scraper) IsEnabled() bool {
	return s.enabled
}

func (s *Scraper) Config() *config.ScraperSettings {
	return s.settings.DeepCopy()
}

func (s *Scraper) Close() error {
	return nil
}

func (s *Scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasPrefix(host, "pics.") || strings.HasPrefix(host, "awsimgsrc.") {
		return false
	}
	return host == "dmm.co.jp" || strings.HasSuffix(host, ".dmm.co.jp") ||
		host == "dmm.com" || strings.HasSuffix(host, ".dmm.com")
}

func (s *Scraper) ExtractIDFromURL(urlStr string) (string, error) {
	matches := dmmCIDRegex.FindStringSubmatch(urlStr)
	if len(matches) > 1 {
		return matches[1], nil
	}

	matches = dmmIDRegex.FindStringSubmatch(urlStr)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("failed to extract content ID from DMM URL")
}

func (s *Scraper) ValidateConfig(cfg *config.ScraperSettings) error {
	if cfg == nil {
		return fmt.Errorf("dmm: config is nil")
	}
	if !cfg.Enabled {
		return nil
	}
	if cfg.RateLimit < 0 {
		return fmt.Errorf("dmm: rate_limit must be non-negative, got %d", cfg.RateLimit)
	}
	if cfg.RetryCount < 0 {
		return fmt.Errorf("dmm: retry_count must be non-negative, got %d", cfg.RetryCount)
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("dmm: timeout must be non-negative, got %d", cfg.Timeout)
	}
	return nil
}

func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if host == "libredmm.com" || strings.HasSuffix(host, ".libredmm.com") {
		return nil, nil, false
	}
	if host == "dmm.co.jp" || strings.HasSuffix(host, ".dmm.co.jp") ||
		host == "dmm.com" || strings.HasSuffix(host, ".dmm.com") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}
