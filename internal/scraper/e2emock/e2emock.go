// Package e2emock provides a deterministic scraper module registered under the
// name "e2emock" for use by full-stack E2E tests. It returns deterministic
// metadata based on the movie ID so tests can drive the real scrape / apply /
// preview / rescrape pipelines end-to-end without network access or
// third-party scraper flakiness.
//
// Everything is real production code EXCEPT this single scraper module at the
// scraper seam — the same pattern real scrapers (r18dev, dmm, ...) use. The
// mock is intentionally registered via the same ScraperRegistrar/Register API
// as production scrapers so the full scraper lifecycle (registration →
// config finalization → constructor invocation → instance store → cache
// lookup → re-translation) is exercised.
//
// Behavior by movie ID prefix (matched case-insensitively):
//
//   - "GOOD-*"     → success. Returns a fully-populated ScraperResult with
//     title, maker, actresses, genres, poster URL, etc. so
//     movie cards / organize previews / thumbnails render
//     correctly.
//   - "MULTI-*"    → success. Same shape as GOOD-* but with a title that
//     includes the ID prefix, so the multipart spec can
//     assert multi-file grouping by shared MovieID with
//     distinctive metadata.
//   - "FAIL-*"     → returns an error whose message carries a per-scraper
//     substring ("e2emock:") so E2E tests can assert the
//     verbose per-scraper failure summary surfaces through
//     the API / DOM verbatim (commit 42d89e65 regression
//     class — the verbose "no result" message must not be
//     collapsed to a hardcoded "no result").
//   - anything else → returns an error so accidental misuse fails loud
//     rather than producing green results.
//
// The mock does NOT register URL handling, content-ID resolution, or query
// resolvers — the production fallback behavior for missing capabilities is
// what E2E tests want to exercise.
package e2emock

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// Name is the scraper's registration identifier. Tests pass "e2emock" in
// SelectedScrapers and rank it in cfg.Scrapers.Priority to ensure only the
// mock is queried.
const Name = "e2emock"

// Register adds the mock scraper registration metadata to the supplied
// registrar (mirrors r18dev.Register / dmm.Register). Callers must invoke
// scraper.NewDefaultScraperRegistryFrom afterwards to instantiate the scraper
// and populate the instance store — the same flow production uses.
func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        Name,
		Description: "Deterministic E2E mock scraper — never hits the network",
		Options:     nil,
		Defaults: models.ScraperSettings{
			Enabled: true,
		},
		ValidateFn: func(_ *models.ScraperSettings) error { return nil },
		Constructor: func(_ scraperutil.ScraperDeps) (models.Scraper, error) {
			return &Scraper{}, nil
		},
		Priority: 1, // high priority so E2E configs ranking only e2emock hit it first
	})
}

// ApplyToConfig sets the cfg.Scrapers fields required for the scraper
// subsystem to query only the mock scraper. E2E binaries call this after
// config.Prepare to ensure priority + enabled defaults are correct.
func ApplyToConfig(cfg *config.Config) {
	cfg.Scrapers.Priority = []string{Name}
}

// Scraper implements models.Scraper with deterministic behavior per movie ID.
type Scraper struct{}

// Name returns the scraper's identifier (matches Name constant).
func (s *Scraper) Name() string { return Name }

// IsEnabled reports whether the scraper is enabled. Always true — the E2E
// binary registers only this scraper, so its enabled flag controls whether
// scraping works at all.
func (s *Scraper) IsEnabled() bool { return true }

// Config returns the scraper's settings. Used by config-resolution code at
// startup; value is unimportant for E2E behavior.
func (s *Scraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}

// Close is a no-op. The mock holds no network or filesystem resources.
func (s *Scraper) Close() error { return nil }

// GetURL returns a fake URL for the given ID — used by scraper.URLHandler
// callers. Not implementing URLHandler so this is unused in E2E paths.
func (s *Scraper) GetURL(_ context.Context, id string) (string, error) {
	return fmt.Sprintf("https://e2e.invalid/%s", id), nil
}

// Search is the main entry point. Behavior is documented at package level.
//
// Returned errors carry a recognizable per-scraper substring "e2emock:" so
// tests pinning commit 42d89e65 (verbose no-result error must propagate
// verbatim through the API/DOM, not be collapsed to a hardcoded
// "no result") can assert the verbose message survives end-to-end.
func (s *Scraper) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	id = strings.TrimSpace(id)
	upper := strings.ToUpper(id)

	switch {
	case strings.HasPrefix(upper, "GOOD-"):
		return successResult(id), nil
	case strings.HasPrefix(upper, "MULTI-"):
		return successResult(id), nil
	case strings.HasPrefix(upper, "FAIL-"):
		return nil, fmt.Errorf("e2emock: movie %s not found — simulating a 404 from the source", id)
	default:
		return nil, fmt.Errorf("e2emock: unrecognized ID %q — use GOOD-* / MULTI-* prefix for success, FAIL-* for verbose failure (this guard prevents accidental green results)", id)
	}
}

// successResult builds a fully-populated ScraperResult for the given ID so
// every downstream consumer (movie card, organize preview, /jobs thumbnail,
// NFO generation) has fields to render. The poster / cover URLs point to a
// stable placeholder so any image-loading assertion fails with a clear
// "e2e image" marker rather than a broken-image icon.
func successResult(id string) *models.ScraperResult {
	return &models.ScraperResult{
		Source:     Name,
		SourceURL:  fmt.Sprintf("https://e2e.invalid/%s", id),
		Language:   "en",
		ID:         id,
		ContentID:  id,
		Title:      "E2E Movie " + id,
		Maker:      "E2E Test Studio",
		Label:      "E2E Test Label",
		Series:     "E2E Series",
		Director:   "Test Director",
		Runtime:    90,
		Actresses:  []models.ActressInfo{{FirstName: "Test", LastName: "Actor", DMMID: 1}},
		Genres:     []string{"Drama", "Test"},
		PosterURL:  fmt.Sprintf("https://e2e.invalid/poster-%s.jpg", id),
		CoverURL:   fmt.Sprintf("https://e2e.invalid/cover-%s.jpg", id),
		TrailerURL: "",
	}
}
