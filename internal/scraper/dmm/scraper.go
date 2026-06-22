package dmm

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/spf13/afero"
)

const (
	baseURL             = "https://www.dmm.co.jp"
	newBaseURL          = "https://video.dmm.co.jp"
	searchURL           = baseURL + "/search/=/searchstr=%s/"
	digitalURL          = baseURL + "/digital/videoa/-/detail/=/cid=%s/"
	physicalURL         = baseURL + "/mono/dvd/-/detail/=/cid=%s/"
	newDigitalURL       = newBaseURL + "/av/content/?id=%s"
	newAmateurURL       = newBaseURL + "/amateur/content/?id=%s"
	rentalURL           = baseURL + "/rental/ppr/-/detail/=/cid=%s/"
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

// scraper implements the DMM scraper.
type scraper struct {
	client        *resty.Client
	enabled       bool
	scrapeActress bool
	useBrowser    bool
	browserConfig models.BrowserConfig
	contentIDRepo models.ContentIDMappingRepositoryInterface
	proxyProfile  *models.ProxyProfile
	proxyOverride *models.ProxyConfig
	downloadProxy *models.ProxyConfig
	rateLimiter   *ratelimit.Limiter
	settings      models.ScraperSettings
	envLookup     func(string) string
	fs            afero.Fs
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

// dmmOptions holds DMM-specific construction parameters that differ from
// the standard (settings, proxy, flaresolverr) pattern used by other scrapers.
type dmmOptions struct {
	TimeoutSeconds int
	ScrapeActress  bool
	Browser        models.BrowserConfig
	ContentIDRepo  models.ContentIDMappingRepositoryInterface
}

// newScraper creates a new DMM scraper.
func newScraper(settings *models.ScraperSettings, globalProxy *models.ProxyConfig, globalFlareSolverr models.FlareSolverrConfig, opts dmmOptions) *scraper {
	resolvedTimeout := resolveTimeout(settings.Timeout, opts.TimeoutSeconds)
	settings.Timeout = resolvedTimeout

	result := httpclient.InitScraperClient(settings, globalProxy, globalFlareSolverr,
		httpclient.WithScraperHeaders(httpclient.CombineHeaders(
			httpclient.DMMHeaders(),
			httpclient.UserAgentHeader(settings.UserAgent),
		)),
		httpclient.WithProxyProfile(),
	)
	client := result.Client
	proxyProfile := result.ProxyProfile

	return &scraper{
		client:        client,
		enabled:       settings.Enabled,
		scrapeActress: settings.ShouldScrapeActress(opts.ScrapeActress),
		useBrowser:    settings.ShouldUseBrowser(opts.Browser.Enabled),
		browserConfig: opts.Browser,
		contentIDRepo: opts.ContentIDRepo,
		proxyProfile:  proxyProfile,
		proxyOverride: settings.Proxy,
		downloadProxy: settings.DownloadProxy,
		rateLimiter:   ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		settings:      *settings,
		envLookup:     os.Getenv,
		fs:            afero.NewOsFs(),
	}
}

func (s *scraper) Name() string {
	return "dmm"
}

// getEnvLookup returns the injected env lookup function or os.Getenv as fallback.
func (s *scraper) getEnvLookup() func(string) string {
	if s.envLookup != nil {
		return s.envLookup
	}
	return os.Getenv
}

// getFs returns the injected filesystem or afero.NewOsFs() as fallback.
func (s *scraper) getFs() afero.Fs {
	if s.fs != nil {
		return s.fs
	}
	return afero.NewOsFs()
}

func (s *scraper) IsEnabled() bool {
	return s.enabled
}

func (s *scraper) Config() *models.ScraperSettings {
	cloned := s.settings.Clone()
	return &cloned
}

func (s *scraper) Close() error {
	return nil
}

func (s *scraper) CanHandleURL(rawURL string) bool {
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

func (s *scraper) ExtractIDFromURL(urlStr string) (string, error) {
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

func (s *scraper) ResolveDownloadProxyForHost(host string) (*models.ProxyConfig, *models.ProxyConfig, bool) {
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
