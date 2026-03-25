package models

import (
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
)

// Rating represents rating information from scrapers
type Rating struct {
	Score float64 `json:"score"`
	Votes int     `json:"votes"`
}

// ScraperResult represents the raw data returned by a scraper
type ScraperResult struct {
	Source           string        `json:"source"`
	SourceURL        string        `json:"source_url"`
	Language         string        `json:"language"` // ISO 639-1 code: en, ja, zh, etc.
	ID               string        `json:"id"`
	ContentID        string        `json:"content_id"`
	Title            string        `json:"title"`
	OriginalTitle    string        `json:"original_title"` // Japanese/original language title
	Description      string        `json:"description"`
	ReleaseDate      *time.Time    `json:"release_date"`
	Runtime          int           `json:"runtime"`
	Director         string        `json:"director"`
	Maker            string        `json:"maker"`
	Label            string        `json:"label"`
	Series           string        `json:"series"`
	Rating           *Rating       `json:"rating"`
	Actresses        []ActressInfo `json:"actresses"`
	Genres           []string      `json:"genres"`
	PosterURL        string        `json:"poster_url"`         // Portrait/box art image
	CoverURL         string        `json:"cover_url"`          // Landscape/fanart image
	ShouldCropPoster bool          `json:"should_crop_poster"` // Whether poster needs cropping from cover
	ScreenshotURL    []string      `json:"screenshot_urls"`
	TrailerURL       string        `json:"trailer_url"`
}

// NormalizeMediaURLs applies post-scrape media URL normalization hooks.
//
// This currently upgrades DMM poster URLs ending in "ps.jpg" to "pl.jpg"
// to use higher-resolution assets when available.
func (r *ScraperResult) NormalizeMediaURLs() {
	if r == nil {
		return
	}

	r.PosterURL = normalizeDMMPosterURL(r.PosterURL)
	r.CoverURL = normalizeDMMPosterURL(r.CoverURL)
}

// normalizeDMMPosterURL rewrites known DMM poster URLs from ps.jpg to pl.jpg.
func normalizeDMMPosterURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	host := strings.ToLower(parsed.Hostname())
	if host != "pics.dmm.co.jp" &&
		host != "awsimgsrc.dmm.co.jp" &&
		host != "awsimgsrc.dmm.com" {
		return raw
	}

	base := strings.ToLower(path.Base(parsed.Path))
	if !strings.HasSuffix(base, "ps.jpg") {
		return raw
	}

	parsed.Path = replacePathSuffixIgnoreCase(parsed.Path, "ps.jpg", "pl.jpg")
	parsed.RawPath = ""

	return parsed.String()
}

func replacePathSuffixIgnoreCase(v, suffix, replacement string) string {
	lower := strings.ToLower(v)
	if !strings.HasSuffix(lower, suffix) {
		return v
	}
	return v[:len(v)-len(suffix)] + replacement
}

// ActressInfo represents actress information from a scraper
type ActressInfo struct {
	DMMID        int    `json:"dmm_id"` // DMM actress ID for unique identification
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	JapaneseName string `json:"japanese_name"`
	ThumbURL     string `json:"thumb_url"`
}

// FullName returns the actress's full name
func (a *ActressInfo) FullName() string {
	if a.LastName != "" && a.FirstName != "" {
		return a.LastName + " " + a.FirstName
	}
	if a.FirstName != "" {
		return a.FirstName
	}
	return a.JapaneseName
}

// Scraper defines the interface that all scrapers must implement
type Scraper interface {
	// Name returns the scraper's identifier (e.g., "r18dev", "dmm")
	Name() string

	// Search attempts to find and scrape metadata for the given movie ID
	Search(id string) (*ScraperResult, error)

	// GetURL attempts to find the URL for a given movie ID
	GetURL(id string) (string, error)

	// IsEnabled returns whether this scraper is enabled in configuration
	IsEnabled() bool

	// Config returns the scraper's configuration
	Config() *config.ScraperConfig

	// Close cleans up resources held by the scraper (e.g., HTTP clients, browsers)
	// Returns nil if no cleanup is needed
	Close() error
}

// Compile-time verification that nil Closer satisfies Scraper interface
var _ Scraper = (*Closer)(nil)

// Closer provides a no-op implementation for scrapers without cleanup.
// Scrapers that don't hold external resources can embed Closer to satisfy
// the Scraper interface without implementing Close().
type Closer struct{}

// Name returns "cloner" as the identifier for the Closer no-op scraper
func (c *Closer) Name() string { return "cloner" }

// Search returns an error indicating this is a no-op implementation
func (c *Closer) Search(_ string) (*ScraperResult, error) {
	return nil, fmt.Errorf("cloner: no-op scraper cannot search")
}

// GetURL returns an error indicating this is a no-op implementation
func (c *Closer) GetURL(_ string) (string, error) {
	return "", fmt.Errorf("cloner: no-op scraper cannot get url")
}

// IsEnabled returns false as this is a no-op implementation
func (c *Closer) IsEnabled() bool { return false }

// Config returns nil as this is a no-op implementation
func (c *Closer) Config() *config.ScraperConfig { return nil }

// Close is a no-op implementation for scrapers that don't require resource cleanup
func (c *Closer) Close() error { return nil }

// ScraperQueryResolver is an optional hook for scrapers to declare and normalize
// identifier formats they can handle (e.g., non-standard filename IDs).
//
// Implementations should return (normalizedQuery, true) when input matches a
// scraper-specific pattern, or ("", false) when it does not apply.
type ScraperQueryResolver interface {
	ResolveSearchQuery(input string) (string, bool)
}

// ScraperDownloadProxyResolver is an optional hook for scrapers to control
// media download proxy routing for scraper-specific media/CDN hosts.
//
// Implementations should return handled=false for unrelated hosts.
// When handled=true, downloader applies the same proxy precedence rules used by
// scraper download_proxy/proxy/global settings.
type ScraperDownloadProxyResolver interface {
	ResolveDownloadProxyForHost(host string) (downloadOverride *config.ProxyConfig, scraperProxy *config.ProxyConfig, handled bool)
}

// ScraperRegistry manages available scrapers
type ScraperRegistry struct {
	scrapers map[string]Scraper
}

// NewScraperRegistry creates a new scraper registry
func NewScraperRegistry() *ScraperRegistry {
	return &ScraperRegistry{
		scrapers: make(map[string]Scraper),
	}
}

// Register adds a scraper to the registry
func (r *ScraperRegistry) Register(scraper Scraper) {
	r.scrapers[scraper.Name()] = scraper
}

// Reset clears all registered scrapers from the registry.
// Primarily used for test isolation.
func (r *ScraperRegistry) Reset() {
	r.scrapers = make(map[string]Scraper)
}

// Get retrieves a scraper by name
func (r *ScraperRegistry) Get(name string) (Scraper, bool) {
	scraper, exists := r.scrapers[name]
	return scraper, exists
}

// GetAll returns all registered scrapers in sorted key order for deterministic iteration
func (r *ScraperRegistry) GetAll() []Scraper {
	keys := make([]string, 0, len(r.scrapers))
	for k := range r.scrapers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	scrapers := make([]Scraper, 0, len(r.scrapers))
	for _, k := range keys {
		scrapers = append(scrapers, r.scrapers[k])
	}
	return scrapers
}

// GetEnabled returns all enabled scrapers in sorted key order for deterministic iteration
func (r *ScraperRegistry) GetEnabled() []Scraper {
	keys := make([]string, 0, len(r.scrapers))
	for k := range r.scrapers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	scrapers := make([]Scraper, 0)
	for _, k := range keys {
		if r.scrapers[k].IsEnabled() {
			scrapers = append(scrapers, r.scrapers[k])
		}
	}
	return scrapers
}

// GetByPriority returns enabled scrapers in the specified priority order
// If priority list is empty or nil, returns all enabled scrapers
// Only returns scrapers that are both in the priority list AND enabled
func (r *ScraperRegistry) GetByPriority(priority []string) []Scraper {
	if len(priority) == 0 {
		return r.GetEnabled()
	}

	scrapers := make([]Scraper, 0)

	// Add scrapers in priority order (only if enabled)
	for _, name := range priority {
		if scraper, exists := r.scrapers[name]; exists && scraper.IsEnabled() {
			scrapers = append(scrapers, scraper)
		}
	}

	return scrapers
}

// GetByPriorityForInput returns enabled scrapers in priority order, but moves
// scrapers with matching query resolvers to the front for the provided input.
//
// If no scraper resolver matches, the original GetByPriority ordering is
// returned unchanged.
func (r *ScraperRegistry) GetByPriorityForInput(priority []string, input string) []Scraper {
	scrapers := r.GetByPriority(priority)
	input = strings.TrimSpace(input)
	if input == "" || len(scrapers) == 0 {
		return scrapers
	}

	matching := make([]Scraper, 0, len(scrapers))
	nonMatching := make([]Scraper, 0, len(scrapers))

	for _, scraper := range scrapers {
		if _, ok := ResolveSearchQueryForScraper(scraper, input); ok {
			matching = append(matching, scraper)
			continue
		}
		nonMatching = append(nonMatching, scraper)
	}

	if len(matching) == 0 {
		return scrapers
	}

	return append(matching, nonMatching...)
}

// ResolveSearchQueryForScraper resolves an input query using a scraper's
// optional ScraperQueryResolver hook.
func ResolveSearchQueryForScraper(scraper Scraper, input string) (string, bool) {
	resolver, ok := scraper.(ScraperQueryResolver)
	if !ok || resolver == nil {
		return "", false
	}

	query, matched := resolver.ResolveSearchQuery(input)
	query = strings.TrimSpace(query)
	if !matched || query == "" {
		return "", false
	}

	return query, true
}
