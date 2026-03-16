package httpclient_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	httpclient "github.com/javinizer/javinizer-go/internal/httpclient"
)

// This file demonstrates the FlareSolverr integration pattern for scrapers
// It is in the httpclient_test package so it will only be built when tests are run

// ExampleScraper demonstrates how a scraper can use FlareSolverr
// This pattern should be used by scrapers that need to bypass Cloudflare protection
type ExampleScraper struct {
	client       *resty.Client
	flaresolverr *httpclient.FlareSolverr // May be nil if not enabled
	enabled      bool
}

// NewExampleScraper creates a new example scraper with optional FlareSolverr support
func NewExampleScraper(cfg *config.Config) (*ExampleScraper, error) {
	// Create HTTP client with FlareSolverr support
	client, fs, err := httpclient.NewRestyClientWithFlareSolverr(
		&cfg.Scrapers.Proxy,
		30*time.Second,
		3,
	)
	if err != nil {
		return nil, err
	}

	return &ExampleScraper{
		client:       client,
		flaresolverr: fs, // Will be nil if FlareSolverr is not enabled in config
		enabled:      true,
	}, nil
}

// fetchPage demonstrates the pattern for fetching a page with optional FlareSolverr
func (s *ExampleScraper) fetchPage(url string) (string, error) {
	// Try FlareSolverr if available and enabled
	if s.flaresolverr != nil {
		html, cookies, err := s.flaresolverr.ResolveURL(url)
		if err == nil {
			// Apply cookies to the client for subsequent requests
			for _, c := range cookies {
				s.client.SetCookie(&c)
			}
			return html, nil
		}
		// Log warning and fall back to direct request
		// In production, use the actual logging package
		_ = err // Placeholder for logging
	}

	// Fallback to direct HTTP request
	resp, err := s.client.R().Get(url)
	if err != nil {
		return "", err
	}
	return string(resp.Body()), nil
}

// Example with session persistence for multi-page scraping
func (s *ExampleScraper) fetchMultiplePages(urls []string) (map[string]string, error) {
	results := make(map[string]string)

	if s.flaresolverr != nil {
		// Create a session for cookie persistence
		sessionID, err := s.flaresolverr.CreateSession()
		if err != nil {
			// Fall back to individual requests
			return s.fetchMultiplePagesDirect(urls)
		}
		defer func() { _ = s.flaresolverr.DestroySession(sessionID) }()

		// Fetch all pages using the same session
		for _, url := range urls {
			html, cookies, err := s.flaresolverr.ResolveURLWithSession(url, sessionID)
			if err != nil {
				return nil, err
			}
			results[url] = html

			// Apply cookies to client
			for _, c := range cookies {
				s.client.SetCookie(&c)
			}
		}
		return results, nil
	}

	// Fallback to direct requests
	return s.fetchMultiplePagesDirect(urls)
}

// fetchMultiplePagesDirect fetches multiple pages without FlareSolverr
func (s *ExampleScraper) fetchMultiplePagesDirect(urls []string) (map[string]string, error) {
	results := make(map[string]string)
	for _, url := range urls {
		resp, err := s.client.R().Get(url)
		if err != nil {
			return nil, err
		}
		results[url] = string(resp.Body())
	}
	return results, nil
}

// Example test to verify FlareSolverr configuration
func TestExampleScraper_FlareSolverrIntegration(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Proxy: config.ProxyConfig{
				FlareSolverr: config.FlareSolverrConfig{
					Enabled:    true,
					URL:        "http://localhost:8191/v1",
					Timeout:    30,
					MaxRetries: 3,
					SessionTTL: 300,
				},
			},
		},
	}

	scraper, err := NewExampleScraper(cfg)
	if err != nil {
		t.Fatalf("Failed to create scraper: %v", err)
	}

	if scraper.flaresolverr == nil {
		t.Error("FlareSolverr should be enabled but is nil")
	}
}

func TestExampleScraper_NoFlareSolverr(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Proxy: config.ProxyConfig{
				FlareSolverr: config.FlareSolverrConfig{
					Enabled: false,
				},
			},
		},
	}

	scraper, err := NewExampleScraper(cfg)
	if err != nil {
		t.Fatalf("Failed to create scraper: %v", err)
	}

	if scraper.flaresolverr != nil {
		t.Error("FlareSolverr should be disabled but is not nil")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	_, err = scraper.fetchPage(server.URL)
	if err != nil {
		t.Fatalf("fetchPage failed: %v", err)
	}

	pages, err := scraper.fetchMultiplePages([]string{server.URL})
	if err != nil {
		t.Fatalf("fetchMultiplePages failed: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 page result, got %d", len(pages))
	}
}
