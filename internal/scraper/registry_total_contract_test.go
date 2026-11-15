package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestPopulateScraperRegistryEmptyOverridesPreservesInstances(t *testing.T) {
	for _, overrides := range []map[string]models.ScraperSettings{nil, {}} {
		reg := scraperutil.NewScraperRegistry()
		existing := &testScraper{name: "existing", enabled: true}
		reg.RegisterInstance(existing)
		reg.Register(scraperutil.ScraperRegistration{
			Name: "unconfigured",
			Constructor: func(scraperutil.ScraperDeps) (models.Scraper, error) {
				t.Fatal("unconfigured scraper must not be constructed")
				return nil, nil
			},
		})
		got := PopulateScraperRegistry(reg, ScraperRegistryConfig{Overrides: overrides}, nil, nil)
		require.Same(t, reg, got)
		require.Equal(t, []models.Scraper{existing}, got.GetAllInstances())
		compatible, err := NewDefaultScraperRegistryFrom(reg, ScraperRegistryConfig{Overrides: overrides}, nil, nil)
		require.NoError(t, err)
		require.Same(t, got, compatible)
	}
}
