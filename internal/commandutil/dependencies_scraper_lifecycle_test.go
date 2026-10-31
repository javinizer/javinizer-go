package commandutil

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

type lifecycleScraper struct {
	models.Scraper
	name   string
	closes atomic.Int32
	err    error
}

func (s *lifecycleScraper) Name() string { return s.name }
func (s *lifecycleScraper) Close() error { s.closes.Add(1); return s.err }

type lifecycleCloser struct{ closes atomic.Int32 }

func (c *lifecycleCloser) Close() error { c.closes.Add(1); return nil }

func TestCoreDepsRetainsReloadedScrapersUntilClose(t *testing.T) {
	first, second := scraperutil.NewScraperRegistry(), scraperutil.NewScraperRegistry()
	closeErr := errors.New("close failed")
	a, b := &lifecycleScraper{name: "old", err: closeErr}, &lifecycleScraper{name: "new"}
	first.RegisterInstance(a)
	second.RegisterInstance(b)
	oldDump, newDump := &lifecycleCloser{}, &lifecycleCloser{}
	deps := &CoreDeps{ScraperRegistry: first, r18DumpCloser: oldDump}
	deps.RetireR18DevDumpCloser(newDump)
	deps.ReplaceReloadable(&config.Config{}, second)
	deps.ReplaceReloadable(&config.Config{}, second) // same instance must not double-close
	require.Zero(t, a.closes.Load(), "an in-flight workflow may still use the old registry")
	require.Zero(t, oldDump.closes.Load(), "old workflows may still read the dump")
	require.Same(t, second, deps.GetRegistry())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !errors.Is(deps.Close(), closeErr) {
				t.Error("lost cleanup error")
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, a.closes.Load())
	require.EqualValues(t, 1, b.closes.Load())
	require.EqualValues(t, 1, oldDump.closes.Load())
	require.EqualValues(t, 1, newDump.closes.Load())
	// Registry shutdown remains independently idempotent.
	require.ErrorIs(t, first.Close(), closeErr)
	require.EqualValues(t, 1, a.closes.Load())
}

func TestCoreDepsRejectsResourcesAfterClose(t *testing.T) {
	deps := &CoreDeps{}
	require.NoError(t, deps.Close())
	reg := scraperutil.NewScraperRegistry()
	s := &lifecycleScraper{name: "late"}
	reg.RegisterInstance(s)
	deps.ReplaceReloadable(&config.Config{}, reg)
	dump := &lifecycleCloser{}
	deps.RetireR18DevDumpCloser(dump)
	require.EqualValues(t, 1, s.closes.Load())
	require.EqualValues(t, 1, dump.closes.Load())
	legacy := &lifecycleCloser{}
	returned := deps.ReplaceR18DevDumpCloser(legacy)
	require.Same(t, legacy, returned, "closed dependencies must not adopt another handle")
	require.NoError(t, returned.Close())
	require.EqualValues(t, 1, legacy.closes.Load())
}

func TestQueryOnlyConstructionFailureClosesPartialRegistry(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	original := newScraperRegistryFrom
	t.Cleanup(func() { newScraperRegistryFrom = original })
	s := &lifecycleScraper{name: "partial"}
	want := errors.New("initialization failed")
	newScraperRegistryFrom = func(reg *scraperutil.ScraperRegistry, _ scraper.ScraperRegistryConfig, _ models.ContentIDMappingRepositoryInterface, _ models.R18DevDumpLookup) (*scraperutil.ScraperRegistry, error) {
		reg.RegisterInstance(s)
		return nil, want
	}
	cfg := &config.Config{}
	cfg.Scrapers.Priority = []string{"e2emock"}
	deps, err := NewQueryOnlyDependencies(cfg)
	require.Nil(t, deps)
	require.ErrorIs(t, err, want)
	require.EqualValues(t, 1, s.closes.Load())
}
