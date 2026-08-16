package scrape

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// claimFailScraper claims to handle every URL (CanHandleURL=true) but fails to
// extract an ID, driving matcher.ParseInput's claimedButFailed path — the
// parse-fail fallback that resolveScrapeInput routes to cmd.MovieID = RawInput.
type claimFailScraper struct {
	name    string
	enabled bool
}

func (s *claimFailScraper) Name() string { return s.name }
func (s *claimFailScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *claimFailScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *claimFailScraper) IsEnabled() bool                                { return s.enabled }
func (s *claimFailScraper) Config() *models.ScraperSettings                { return nil }
func (s *claimFailScraper) Close() error                                   { return nil }
func (s *claimFailScraper) CanHandleURL(string) bool                       { return true }
func (s *claimFailScraper) ExtractIDFromURL(string) (string, error) {
	return "", errors.New("extraction failed")
}
func (s *claimFailScraper) ScrapeURL(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}

// redactStubResolver satisfies ScraperInstanceResolver with only GetAllInstances
// populated (the sole method matcher.ParseInput / resolveScrapeInput reads).
type redactStubResolver struct{ instances []models.Scraper }

func (r *redactStubResolver) GetInstance(string) (models.Scraper, bool) { return nil, false }
func (r *redactStubResolver) GetInstancesByPriorityForInput([]string, string) []models.Scraper {
	return r.instances
}
func (r *redactStubResolver) GetAllInstances() []models.Scraper { return r.instances }
func (r *redactStubResolver) Names() []string                   { return nil }

func TestResolveScrapeInput_RedactsQueryOnParseFailFallback(t *testing.T) {
	registry := &redactStubResolver{instances: []models.Scraper{
		&claimFailScraper{name: "test", enabled: true},
	}}
	cmd := ScrapeCmd{RawInput: "https://example.com/v/123?token=secret"}

	got, err := resolveScrapeInput(context.Background(), cmd, registry, &Config{})

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/v/123", got.MovieID,
		"parse-fail fallback MovieID must be query-redacted (security F2)")
	assert.NotContains(t, got.MovieID, "token=secret")
	assert.Contains(t, got.ParseWarning, "could not be parsed",
		"ParseWarning surfaces the parse failure (backend F5 follow-on)")
}
