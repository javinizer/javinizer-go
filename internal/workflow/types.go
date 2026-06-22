package workflow

import "errors"

// Sentinel errors for the Compare workflow. The API layer uses errors.Is()
// to map these to HTTP status codes — never match on error message text.
var (
	// ErrInvalidPreset is returned when the preset string is not one of
	// the accepted values (conservative, gap-fill, aggressive).
	ErrInvalidPreset = errors.New("invalid preset")

	// ErrNFOParseFailed is returned when the NFO file exists but cannot be
	// parsed (e.g., malformed XML).
	ErrNFOParseFailed = errors.New("failed to parse NFO")

	// ErrScrapeFailed is returned when the scraper returns an error during
	// the compare pipeline.
	ErrScrapeFailed = errors.New("scrape failed")

	// ErrScrapeNoResult is returned when the scraper completes without error
	// but returns no data.
	ErrScrapeNoResult = errors.New("scrape returned no result")

	// ErrMergeFailed is returned when the NFO/scraper merge step fails.
	ErrMergeFailed = errors.New("merge failed")
)
