package scrape

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type actressOverrideScraper struct {
	name          string
	globalDefault bool
	settings      models.ScraperSettings
}

func (s *actressOverrideScraper) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{Source: s.name, ID: id, ContentID: id, Title: "Override evidence"}
	if s.settings.ShouldScrapeActress(s.globalDefault) {
		result.Actresses = []models.ActressInfo{{DMMID: 926001, FirstName: "Override", LastName: "Candidate"}}
	}
	return result, nil
}
func (s *actressOverrideScraper) Name() string                                   { return s.name }
func (s *actressOverrideScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *actressOverrideScraper) IsEnabled() bool                                { return true }
func (s *actressOverrideScraper) Config() *models.ScraperSettings                { return &s.settings }
func (s *actressOverrideScraper) Close() error                                   { return nil }

func TestPerScraperActressOverridePersistsScrapeOwnedCandidateAndCachesCredit(t *testing.T) {
	f := newFixture(t)
	globalDisabled := false
	overrideEnabled := true
	scraper := &actressOverrideScraper{
		name:          "override-actress",
		globalDefault: globalDisabled,
		settings:      models.ScraperSettings{Enabled: true, ScrapeActress: &overrideEnabled},
	}
	f.registry.RegisterInstance(scraper)
	f.withPriority([]string{scraper.name})
	engine := f.build()
	engine.cfg.ScrapeActress = globalDisabled
	verified := models.Actress{FirstName: "Override", LastName: "Candidate", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, f.db.Create(&verified).Error)

	fresh, err := engine.Scrape(t.Context(), ScrapeCmd{MovieID: "OVERRIDE-001", ForceRefresh: true})
	require.NoError(t, err)
	require.Len(t, fresh.Movie.Actresses, 1)
	require.Len(t, fresh.Movie.Credits, 1)
	require.Equal(t, string(models.CreditOriginScrape), fresh.Movie.Credits[0].Origin)

	persisted, err := f.movieRepo.Upsert(t.Context(), fresh.Movie)
	require.NoError(t, err)
	require.Len(t, persisted.Credits, 1)
	credit := persisted.Credits[0]
	require.False(t, credit.LegacyInferred)
	require.False(t, credit.UserOverride)
	require.False(t, credit.OrderPinned)
	require.Equal(t, string(models.CreditOriginScrape), credit.Origin)

	var candidate models.Actress
	require.NoError(t, f.db.First(&candidate, credit.ActressID).Error)
	require.False(t, candidate.Verified)
	require.True(t, candidate.AmbiguityQuarantined)
	require.Equal(t, database.ActressOriginScrape, candidate.Origin)

	cached, err := engine.Scrape(t.Context(), ScrapeCmd{MovieID: "OVERRIDE-001"})
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.Len(t, cached.Movie.Credits, 1)
	require.Equal(t, candidate.ID, cached.Movie.Credits[0].ActressID)
}

func TestGlobalDisabledWithoutPerScraperActressEvidenceCreatesNoCredits(t *testing.T) {
	f := newFixture(t)
	scraper := &actressOverrideScraper{name: "no-actress", globalDefault: false, settings: models.ScraperSettings{Enabled: true}}
	f.registry.RegisterInstance(scraper)
	f.withPriority([]string{scraper.name})
	engine := f.build()
	engine.cfg.ScrapeActress = false

	result, err := engine.Scrape(t.Context(), ScrapeCmd{MovieID: "OVERRIDE-EMPTY", ForceRefresh: true})
	require.NoError(t, err)
	require.Empty(t, result.Movie.Actresses)
	require.Nil(t, result.Movie.Credits)
}

func TestPostProcessPreservesExplicitCreditsWithoutDuplicatingAggregateEvidence(t *testing.T) {
	explicit := models.MovieCredit{CreditedName: "Explicit", Origin: string(models.CreditOriginUser), UserOverride: true}
	movie := &models.Movie{ID: "EXPLICIT-001", Actresses: []models.Actress{{FirstName: "Aggregate"}}, Credits: []models.MovieCredit{explicit}}
	raw := []*models.ScraperResult{{Source: "test", Actresses: []models.ActressInfo{{FirstName: "Aggregate"}}}}

	result, err := postProcessScraped(t.Context(), movie, raw, nil, &Config{ScrapeActress: false}, nil, nil, ScrapeCmd{MovieID: movie.ID}, time.Now())
	require.NoError(t, err)
	require.Equal(t, []models.MovieCredit{explicit}, result.Movie.Credits)
}

func TestScrapeResultsContainActressesVariants(t *testing.T) {
	require.False(t, scrapeResultsContainActresses(nil))
	require.False(t, scrapeResultsContainActresses([]*models.ScraperResult{nil, {Source: "empty"}}))
	require.True(t, scrapeResultsContainActresses([]*models.ScraperResult{{Actresses: []models.ActressInfo{{FirstName: "Evidence"}}}}))
}
