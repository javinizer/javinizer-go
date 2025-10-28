package models

import "time"

// Rating represents rating information from scrapers
type Rating struct {
	Score float64 `json:"score"`
	Votes int     `json:"votes"`
}

// ScraperResult represents the raw data returned by a scraper
type ScraperResult struct {
	Source        string        `json:"source"`
	SourceURL     string        `json:"source_url"`
	Language      string        `json:"language"` // ISO 639-1 code: en, ja, zh, etc.
	ID            string        `json:"id"`
	ContentID     string        `json:"content_id"`
	Title         string        `json:"title"`
	OriginalTitle string        `json:"original_title"` // Japanese/original language title
	Description   string        `json:"description"`
	ReleaseDate   *time.Time    `json:"release_date"`
	Runtime       int           `json:"runtime"`
	Director      string        `json:"director"`
	Maker         string        `json:"maker"`
	Label         string        `json:"label"`
	Series        string        `json:"series"`
	Rating        *Rating       `json:"rating"`
	Actresses     []ActressInfo `json:"actresses"`
	Genres        []string      `json:"genres"`
	PosterURL     string        `json:"poster_url"` // Portrait/box art image
	CoverURL      string        `json:"cover_url"`  // Landscape/fanart image
	ScreenshotURL []string      `json:"screenshot_urls"`
	TrailerURL    string        `json:"trailer_url"`
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

// Get retrieves a scraper by name
func (r *ScraperRegistry) Get(name string) (Scraper, bool) {
	scraper, exists := r.scrapers[name]
	return scraper, exists
}

// GetAll returns all registered scrapers
func (r *ScraperRegistry) GetAll() []Scraper {
	scrapers := make([]Scraper, 0, len(r.scrapers))
	for _, scraper := range r.scrapers {
		scrapers = append(scrapers, scraper)
	}
	return scrapers
}

// GetEnabled returns all enabled scrapers
func (r *ScraperRegistry) GetEnabled() []Scraper {
	scrapers := make([]Scraper, 0)
	for _, scraper := range r.scrapers {
		if scraper.IsEnabled() {
			scrapers = append(scrapers, scraper)
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
