package models

import (
	"context"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Rating represents rating information from scrapers
type Rating struct {
	Score float64 `json:"score"`
	Votes int     `json:"votes"`
}

// ScraperResult represents the raw data returned by a scraper
type ScraperResult struct {
	Source           string             `json:"source"`
	SourceURL        string             `json:"source_url"`
	Language         string             `json:"language"` // ISO 639-1 code: en, ja, zh, etc.
	ID               string             `json:"id"`
	ContentID        string             `json:"content_id"`
	Title            string             `json:"title"`
	OriginalTitle    string             `json:"original_title"` // Japanese/original language title
	Description      string             `json:"description"`
	ReleaseDate      *time.Time         `json:"release_date"`
	Runtime          int                `json:"runtime"`
	Director         string             `json:"director"`
	Maker            string             `json:"maker"`
	Label            string             `json:"label"`
	Series           string             `json:"series"`
	Rating           *Rating            `json:"rating"`
	Actresses        []ActressInfo      `json:"actresses"`
	Genres           []string           `json:"genres"`
	PosterURL        string             `json:"poster_url"`         // Portrait/box art image
	CoverURL         string             `json:"cover_url"`          // Landscape/fanart image
	ShouldCropPoster bool               `json:"should_crop_poster"` // Whether poster needs cropping from cover
	ScreenshotURL    []string           `json:"screenshot_urls"`
	TrailerURL       string             `json:"trailer_url"`
	Translations     []MovieTranslation `json:"translations,omitempty"` // Additional language translations (optional)
}

// Clone returns a deep copy of the ScraperResult, including all slice and
// pointer fields. Used by ProvenanceData.Clone to isolate the raw per-scraper
// results retained for the review-page source viewer from the scrape path's
// in-flight values.
func (r *ScraperResult) Clone() *ScraperResult {
	if r == nil {
		return nil
	}
	copied := *r
	if r.ReleaseDate != nil {
		t := *r.ReleaseDate
		copied.ReleaseDate = &t
	}
	if r.Rating != nil {
		rating := *r.Rating
		copied.Rating = &rating
	}
	if r.Actresses != nil {
		copied.Actresses = make([]ActressInfo, len(r.Actresses))
		copy(copied.Actresses, r.Actresses)
	}
	if r.Genres != nil {
		copied.Genres = make([]string, len(r.Genres))
		copy(copied.Genres, r.Genres)
	}
	if r.ScreenshotURL != nil {
		copied.ScreenshotURL = make([]string, len(r.ScreenshotURL))
		copy(copied.ScreenshotURL, r.ScreenshotURL)
	}
	if r.Translations != nil {
		copied.Translations = make([]MovieTranslation, len(r.Translations))
		copy(copied.Translations, r.Translations)
	}
	return &copied
}

// NormalizeMediaURLs applies post-scrape media URL normalization hooks.
//
// The cover (landscape jacket) is upgraded to pl.jpg when available, as that
// is the highest-resolution cover variant.
//
// The poster (PortraitURL) is intentionally NOT upgraded: ps.jpg is the
// portrait poster (e.g. 1032x1467) and pl.jpg is the landscape jacket
// (e.g. 2184x1467), so upgrading ps.jpg -> pl.jpg would replace the poster
// with the jacket and downstream code (e.g. sort) would download the jacket
// as poster.jpg.
func (r *ScraperResult) NormalizeMediaURLs() {
	if r == nil {
		return
	}

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
	return formatActressNameSimple(a.LastName, a.FirstName, a.JapaneseName)
}

// ScraperConfigResolverInterface provides scraper registry lookups without
// importing scraperutil. Defined in models so both config and scraperutil
// can reference it without circular imports. Implementations live in
// scraperutil (ScraperRegistry) and are injected by callers.
type ScraperConfigResolverInterface interface {
	IsRegistered(name string) bool
	GetAllDefaults() map[string]ScraperSettings
	GetValidateFn(name string) func(*ScraperSettings) error
}

// Scraper defines the core interface that all scrapers must implement.
//
// URL handling and download proxy resolution are separate optional interfaces
// (URLHandler, DownloadProxyResolver). Consumers that need those capabilities
// should use type assertions, following the same pattern as ScraperQueryResolver
// and ContentIDResolver.
type Scraper interface {
	// Name returns the scraper's identifier (e.g., "r18dev", "dmm")
	Name() string

	// Search attempts to find and scrape metadata for the given movie ID.
	// Context enables cancellation and timeout propagation through rate limiters and HTTP requests.
	Search(ctx context.Context, id string) (*ScraperResult, error)

	// GetURL attempts to find the URL for a given movie ID
	GetURL(ctx context.Context, id string) (string, error)

	// IsEnabled returns whether this scraper is enabled in configuration
	IsEnabled() bool

	// Config returns the scraper's configuration
	Config() *ScraperSettings

	// Close cleans up resources held by the scraper (e.g., HTTP clients, browsers)
	// Returns nil if no cleanup is needed
	Close() error
}

// URLHandler is an optional interface for scrapers that support URL-based
// scraping. Scrapers that implement this interface can handle direct URL input,
// extract movie IDs from URLs, and scrape metadata from a URL.
//
// Consumers should use type assertion: handler, ok := scraper.(URLHandler)
type URLHandler interface {
	// CanHandleURL returns true if this scraper can handle the given URL.
	CanHandleURL(url string) bool

	// ExtractIDFromURL extracts the movie ID from a URL this scraper can handle.
	// Returns (id, nil) on success or ("", error) if extraction fails.
	ExtractIDFromURL(url string) (string, error)

	// ScrapeURL directly scrapes metadata from a URL.
	// Returns ScraperResult on success, or error with typed ScraperError on failure.
	// Context enables cancellation and timeout propagation through rate limiters and HTTP requests.
	ScrapeURL(ctx context.Context, url string) (*ScraperResult, error)
}

// DownloadProxyResolver is an optional interface for scrapers that can resolve
// download proxy configuration for media downloads from scraper-specific CDN hosts.
//
// Consumers should use type assertion: resolver, ok := scraper.(DownloadProxyResolver)
type DownloadProxyResolver interface {
	// ResolveDownloadProxyForHost returns proxy configuration for media downloads
	// from scraper-specific CDN hosts. Implementations should return
	// (nil, nil, false) for unrelated hosts.
	ResolveDownloadProxyForHost(host string) (downloadOverride *ProxyConfig, scraperProxy *ProxyConfig, handled bool)
}

// ScraperQueryResolver is an optional hook for scrapers to declare and normalize
// identifier formats they can handle (e.g., non-standard filename IDs).
//
// Implementations should return (normalizedQuery, true) when input matches a
// scraper-specific pattern, or ("", false) when it does not apply.
type ScraperQueryResolver interface {
	ResolveSearchQuery(input string) (string, bool)
}

// ContentIDResolver is an optional interface for scrapers that can resolve
// a JAV ID to its DMM content-ID format (e.g., "ipx-123" -> "118BDP-00118").
//
// This is primarily used by DMM to normalize IDs before querying other scrapers,
// since many scrapers share the same DMM content-ID format.
//
// Implementations should return (resolvedID, nil) on success or ("", error) on failure.
// If a scraper does not support content-ID resolution, it should return (input, false).
type ContentIDResolver interface {
	ResolveContentID(id string) (string, error)
}

// ContentIDResolverCtx is the context-aware variant of ContentIDResolver.
// Scrapers that can honor cancellation/timeouts during content-ID resolution
// (e.g. when the lookup issues HTTP) should implement this in addition to
// ContentIDResolver. Callers should type-assert this first and fall back to
// ContentIDResolver.ResolveContentID for scrapers that only implement the
// non-context interface.
type ContentIDResolverCtx interface {
	ResolveContentIDCtx(ctx context.Context, id string) (string, error)
}

// HTMLParser is an optional interface for scrapers that can parse a pre-fetched
// HTML document into a ScraperResult. Scrapers implement this so their parsing
// logic can be tested with a static HTML fixture (goquery.Document) without
// HTTP mocking.
//
// Consumers should use type assertion: parser, ok := scraper.(HTMLParser)
type HTMLParser interface {
	// ParseHTML parses the given HTML document and returns scraper results.
	// sourceURL is the URL the document was fetched from, used for resolving
	// relative links and populating SourceURL on the result.
	ParseHTML(doc *goquery.Document, sourceURL string) (*ScraperResult, error)
}

// ResolveSearchQueryForScraper resolves an input query using a scraper's
// optional ScraperQueryResolver hook.
func ResolveSearchQueryForScraper(scraper Scraper, input string) (string, bool) {
	resolver, ok := scraper.(ScraperQueryResolver)
	if !ok {
		return "", false
	}

	query, matched := resolver.ResolveSearchQuery(input)
	query = strings.TrimSpace(query)
	if !matched || query == "" {
		return "", false
	}

	return query, true
}

// ScraperOption represents a configurable option for a scraper
type ScraperOption struct {
	Key         string          `json:"key" example:"scrape_actress"`
	Label       string          `json:"label" example:"Scrape Actress Information"`
	Description string          `json:"description" example:"Enable detailed actress data scraping from DMM (may be slower)"`
	Type        string          `json:"type" example:"boolean"`
	Default     any             `json:"default,omitempty"`
	Min         *int            `json:"min,omitempty" example:"5"`
	Max         *int            `json:"max,omitempty" example:"120"`
	Unit        string          `json:"unit,omitempty" example:"seconds"`
	Choices     []ScraperChoice `json:"choices,omitempty"`
}

// ScraperChoice represents a choice for a select-type scraper option
type ScraperChoice struct {
	Value string `json:"value" example:"en"`
	Label string `json:"label" example:"English"`
}
