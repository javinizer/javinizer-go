package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestReloadRegistryPartialConstructionRetainsDumpUntilShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
	require.NoError(t, err)
	oldConfig := newHotReloadRaceConfig("old", 1000, 10)
	rt := newHotReloadRaceRuntime(t, oldConfig)
	t.Cleanup(func() { require.NoError(t, rt.deps.CoreDeps.Close()) })
	oldRegistry := rt.deps.CoreDeps.GetRegistry()
	oldScraper := &lifecycleCountingScraper{}
	oldRegistry.RegisterInstance(oldScraper)
	var stores []*r18devdump.Store
	var scrapers []*lifecycleCountingScraper
	for _, port := range []int{2000, 3000} {
		cfg := newHotReloadRaceConfig("new", port, 20)
		cfg.Metadata.R18DevDump = config.R18DevDumpConfig{Enabled: true, Path: path}
		reg := scraperutil.NewScraperRegistry()
		good := &lifecycleCountingScraper{}
		scrapers = append(scrapers, good)
		attempted := false
		reg.Register(scraperutil.ScraperRegistration{
			Name: "broken",
			Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
				attempted = true
				require.NotNil(t, deps.R18DevDump)
				return nil, errors.New("constructor unavailable")
			},
		})
		reg.Register(scraperutil.ScraperRegistration{
			Name: "lifecycle",
			Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
				store, ok := deps.R18DevDump.(*r18devdump.Store)
				require.True(t, ok)
				stores = append(stores, store)
				return good, nil
			},
		})
		require.NoError(t, rt.WithReloadLock(func() error { return rt.reloadConfigLocked(cfg, reg) }))
		require.True(t, attempted)
		require.Same(t, cfg, rt.deps.CoreDeps.GetConfig())
		require.Same(t, reg, rt.deps.CoreDeps.GetRegistry())
		require.Equal(t, port, rt.GetAPIConfig().Port)
		require.Len(t, reg.GetAllInstances(), 1)
		require.Zero(t, oldScraper.closes.Load())
		for _, scraper := range scrapers {
			require.Zero(t, scraper.closes.Load())
		}
		for _, store := range stores {
			stats, err := store.Stats(context.Background())
			require.NoError(t, err)
			require.EqualValues(t, 1, stats.RowCount)
		}
	}
	require.NoError(t, rt.deps.CoreDeps.Close())
	require.EqualValues(t, 1, oldScraper.closes.Load())
	for _, scraper := range scrapers {
		require.EqualValues(t, 1, scraper.closes.Load())
	}
	for _, store := range stores {
		_, err := store.Stats(context.Background())
		require.ErrorContains(t, err, "database is closed")
	}
}
