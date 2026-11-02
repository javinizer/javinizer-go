package scraperutil_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestMetadataOnlyRegistrationIsDiscoverableButNotExecutable(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	t.Cleanup(func() { require.NoError(t, registry.Close()) })
	registry.Register(scraperutil.ScraperRegistration{
		Name: "metadata-only", Description: "Catalog metadata without an implementation", Priority: 10,
		Defaults: models.ScraperSettings{Enabled: false},
		Options:  []models.ScraperOption{{Key: "catalog_note", Label: "Catalog note", Type: "string"}},
	})
	dmm.Register(registry)

	metadata, ok := registry.Get("metadata-only")
	require.True(t, ok)
	require.Equal(t, "Catalog metadata without an implementation", metadata.Description)
	options, ok := registry.GetOptions("metadata-only")
	require.True(t, ok)
	require.Equal(t, metadata.Options, options.Options)
	require.Contains(t, registry.Names(), "metadata-only")

	constructor, ok := registry.GetScraperConstructor("metadata-only")
	require.False(t, ok, "registered metadata must not be advertised as an executable constructor")
	require.Nil(t, constructor)
	constructor, ok = registry.GetScraperConstructor("dmm")
	require.True(t, ok)
	require.NotNil(t, constructor, "a real registered implementation remains executable")
	require.NotContains(t, registry.GetScraperConstructors(), "metadata-only")
	require.Contains(t, registry.GetScraperConstructors(), "dmm")

	require.NoError(t, registry.InitInstances(map[string]scraperutil.ScraperDeps{
		"metadata-only": {Settings: models.ScraperSettings{Enabled: true}},
		"dmm":           {Settings: models.ScraperSettings{Enabled: true}, TimeoutSeconds: 30},
	}))
	instances := registry.GetAllInstances()
	require.Len(t, instances, 1, "metadata-only entries do not produce a nil or phantom runtime scraper")
	require.Equal(t, "dmm", instances[0].Name())
	require.True(t, registry.IsRegistered("metadata-only"), "runtime initialization preserves catalog metadata")
}
