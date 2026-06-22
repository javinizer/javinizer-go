package downloader

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// NewHTTPClient creates an HTTP client for the downloader using pre-resolved configuration.
// The bridge function resolves all proxy profiles so this function never imports internal/config.
func NewHTTPClient(cfg HTTPClientConfig) (httpclient.HTTPClient, error) {
	adaptiveClient := &adaptiveDownloaderHTTPClient{
		timeout:        cfg.Timeout,
		httpCfg:        cfg,
		clients:        make(map[string]httpclient.HTTPClient),
		proxyResolvers: cfg.ProxyResolvers,
	}

	// Explicit download proxy override still takes precedence when configured.
	if cfg.DownloadProxy != nil && cfg.DownloadProxy.URL != "" {
		client, err := httpclient.NewHTTPClient(cfg.DownloadProxy, cfg.Timeout)
		if err != nil {
			logging.Errorf("Downloader: Failed to create download proxy client: %v, using adaptive routing", err)
		} else {
			logging.Infof("Downloader: Using download proxy %s", httpclient.SanitizeProxyURL(cfg.DownloadProxy.URL))
			adaptiveClient.forceClient = client
			return adaptiveClient, nil
		}
	}

	// Default direct client
	directClient, err := httpclient.NewHTTPClient(nil, cfg.Timeout)
	if err != nil {
		logging.Errorf("Downloader: Failed to create direct HTTP client: %v, using standard http client", err)
		directClient = &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
				MaxIdleConnsPerHost: 2,
			},
		}
	}
	adaptiveClient.directClient = directClient

	return adaptiveClient, nil
}

// adaptiveDownloaderHTTPClient routes media downloads through per-scraper proxies when needed.
type adaptiveDownloaderHTTPClient struct {
	timeout        time.Duration
	httpCfg        HTTPClientConfig
	forceClient    httpclient.HTTPClient // forced proxy client for all downloads
	directClient   httpclient.HTTPClient
	proxyResolvers []models.DownloadProxyResolver
	mu             sync.Mutex
	clients        map[string]httpclient.HTTPClient // keyed by proxy fingerprint
}

func (c *adaptiveDownloaderHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// If a force client is configured, always use it.
	if c.forceClient != nil {
		return c.forceClient.Do(req)
	}

	proxyProfile := c.selectProxyForRequest(req)
	if proxyProfile == nil || proxyProfile.URL == "" {
		return c.directClient.Do(req)
	}

	client, err := c.getOrCreateProxyClient(proxyProfile)
	if err != nil {
		logging.Warnf("Downloader: Failed to create proxy client for %s: %v; falling back to direct", req.URL.Host, err)
		return c.directClient.Do(req)
	}

	return client.Do(req)
}

func (c *adaptiveDownloaderHTTPClient) selectProxyForRequest(req *http.Request) *models.ProxyProfile {
	if req == nil || req.URL == nil {
		return nil
	}

	host := strings.ToLower(req.URL.Hostname())
	if host == "" {
		return nil
	}

	for _, resolver := range c.proxyResolvers {
		downloadOverride, scraperProxy, handled := resolver.ResolveDownloadProxyForHost(host)
		if handled {
			return c.resolveScraperDownloadProxy(downloadOverride, scraperProxy)
		}
	}

	// Fallback to global scraper proxy, if enabled.
	if c.httpCfg.GlobalProxy != nil && c.httpCfg.GlobalProxy.URL != "" {
		return c.httpCfg.GlobalProxy
	}
	return nil
}

// resolveScraperDownloadProxy resolves per-scraper proxy settings using the global proxy config
// provided in HTTPClientConfig. This mirrors models.ResolveScraperProxy but uses models.ProxyConfig
// so the downloader does not import internal/config.
//
// Resolution order:
//  1. If downloadOverride is enabled with a specific profile → use that profile
//  2. If downloadOverride is enabled with no profile (Inherit) → use global proxy
//  3. If no downloadOverride applies, check scraperProxy:
//     a. If scraperProxy is enabled with a specific profile → use that profile
//     b. If scraperProxy is enabled with no profile (Inherit) → use global proxy
//     c. If scraperProxy is nil or not enabled → use global proxy
func (c *adaptiveDownloaderHTTPClient) resolveScraperDownloadProxy(downloadOverride, scraperProxy *models.ProxyConfig) *models.ProxyProfile {
	globalCfg := c.httpCfg.GlobalProxyConfig
	if globalCfg == nil || !globalCfg.Enabled {
		// Global proxy disabled → direct mode
		return nil
	}

	// Step 1-2: Check download override
	if downloadOverride != nil {
		if !downloadOverride.Enabled {
			// Explicitly disabled → direct mode (no proxy)
			return nil
		}
		if downloadOverride.Profile != "" {
			// Specific profile → use it
			return c.resolveProfile(downloadOverride.Profile, globalCfg)
		}
		// Inherit mode → use global proxy (NOT scraper-level proxy)
		return c.httpCfg.GlobalProxy
	}

	// Step 3: Check scraper proxy
	if scraperProxy != nil {
		if !scraperProxy.Enabled {
			// Explicitly disabled → direct mode (no proxy)
			return nil
		}
		if scraperProxy.Profile != "" {
			// Specific profile → use it
			return c.resolveProfile(scraperProxy.Profile, globalCfg)
		}
		// Inherit mode → use global proxy
		return c.httpCfg.GlobalProxy
	}

	// No override applies → return global proxy
	return c.httpCfg.GlobalProxy
}

// resolveProfile looks up a named profile from the global config and fills in
// missing credentials from the pre-resolved global proxy.
func (c *adaptiveDownloaderHTTPClient) resolveProfile(profileName string, globalCfg *models.ProxyConfig) *models.ProxyProfile {
	profile, ok := globalCfg.Profiles[profileName]
	if !ok {
		// Unknown profile → fall back to global proxy
		return c.httpCfg.GlobalProxy
	}
	resolved := profile
	// Inherit credentials from global if omitted
	if resolved.URL == "" && c.httpCfg.GlobalProxy != nil {
		resolved.URL = c.httpCfg.GlobalProxy.URL
	}
	if resolved.Username == "" && c.httpCfg.GlobalProxy != nil {
		resolved.Username = c.httpCfg.GlobalProxy.Username
	}
	if resolved.Password == "" && c.httpCfg.GlobalProxy != nil {
		resolved.Password = c.httpCfg.GlobalProxy.Password
	}
	return &resolved
}

func (c *adaptiveDownloaderHTTPClient) getOrCreateProxyClient(proxyProfile *models.ProxyProfile) (httpclient.HTTPClient, error) {
	keyBytes := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", proxyProfile.URL, proxyProfile.Username, proxyProfile.Password)))
	key := fmt.Sprintf("%x", keyBytes)

	c.mu.Lock()
	defer c.mu.Unlock()

	if client, ok := c.clients[key]; ok {
		return client, nil
	}

	client, err := httpclient.NewHTTPClient(proxyProfile, c.timeout)
	if err != nil {
		return nil, err
	}
	c.clients[key] = client
	logging.Infof("Downloader: Using scraper-level proxy for media host via %s", httpclient.SanitizeProxyURL(proxyProfile.URL))
	return client, nil
}
