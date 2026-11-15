package scraper

import (
	"errors"
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

// Both entry points retain successful instances when another constructor fails.
// That failure is reported by logging, not by a partially initialized registry
// error, and it must not close the healthy scraper before the registry owner.
func TestRegistryPopulationKeepsHealthyScrapersAfterConstructorFailure(t *testing.T) {
	for _, compatible := range []bool{false, true} {
		name := "total initializer"
		if compatible {
			name = "compatibility wrapper"
		}
		t.Run(name, func(t *testing.T) {
			reg := scraperutil.NewScraperRegistry()
			good := &populationLifetimeScraper{testScraper: testScraper{name: "healthy", enabled: true}}
			calls := map[string]int{}
			reg.Register(scraperutil.ScraperRegistration{
				Name: "healthy",
				Constructor: func(scraperutil.ScraperDeps) (models.Scraper, error) {
					calls["healthy"]++
					return good, nil
				},
			})
			reg.Register(scraperutil.ScraperRegistration{
				Name: "unavailable",
				Constructor: func(scraperutil.ScraperDeps) (models.Scraper, error) {
					calls["unavailable"]++
					return nil, errors.New("source initialization unavailable")
				},
			})
			cfg := ScraperRegistryConfig{Overrides: map[string]models.ScraperSettings{
				"healthy": {Enabled: true}, "unavailable": {Enabled: true},
			}}
			var got *scraperutil.ScraperRegistry
			if compatible {
				var err error
				got, err = NewDefaultScraperRegistryFrom(reg, cfg, nil, nil)
				require.NoError(t, err)
			} else {
				got = PopulateScraperRegistry(reg, cfg, nil, nil)
			}
			require.Same(t, reg, got)
			require.Equal(t, map[string]int{"healthy": 1, "unavailable": 1}, calls)
			require.Equal(t, []models.Scraper{good}, got.GetAllInstances())
			require.Zero(t, good.closes)
			require.NoError(t, got.Close())
			require.NoError(t, got.Close())
			require.Equal(t, 1, good.closes)
		})
	}
}

type populationLifetimeScraper struct {
	testScraper
	closes int
}

func (s *populationLifetimeScraper) Close() error {
	s.closes++
	return nil
}
