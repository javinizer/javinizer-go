package core

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

type lifecycleCountingScraper struct {
	models.Scraper
	closes atomic.Int32
}

func (s *lifecycleCountingScraper) Name() string { return "lifecycle" }
func (s *lifecycleCountingScraper) Close() error { s.closes.Add(1); return nil }

func TestCloseAfterHTTPDrainJoinsLegacyTaskBeforeRetiredRegistryClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := scraperutil.NewScraperRegistry()
		s := &lifecycleCountingScraper{}
		old.RegisterInstance(s)
		deps := &commandutil.CoreDeps{ScraperRegistry: old}
		rt := NewAPIRuntime(&APIDeps{CoreDeps: deps})
		rt.EnsureRuntime()
		done := rt.TrackBackgroundTask()
		rt.ReplaceReloadable(&config.Config{}, scraperutil.NewScraperRegistry())
		stopped := make(chan error, 1)
		go func() { stopped <- rt.CloseAfterHTTPDrain(time.Minute) }()
		synctest.Wait()
		require.Zero(t, s.closes.Load())
		require.Error(t, rt.ServerCtx().Err())
		done()
		synctest.Wait()
		require.NoError(t, <-stopped)
		require.EqualValues(t, 1, s.closes.Load())
		require.NoError(t, rt.CloseAfterHTTPDrain(time.Minute))
		require.EqualValues(t, 1, s.closes.Load())
	})
}

func TestCloseAfterHTTPDrainTimeoutKeepsDependenciesAlive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := scraperutil.NewScraperRegistry()
		s := &lifecycleCountingScraper{}
		reg.RegisterInstance(s)
		rt := NewAPIRuntime(&APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: reg}})
		rt.EnsureRuntime()
		done := rt.TrackBackgroundTask()
		require.ErrorContains(t, rt.CloseAfterHTTPDrain(time.Second), "dependencies remain open")
		require.Zero(t, s.closes.Load())
		done()
		require.NoError(t, rt.CloseAfterHTTPDrain(time.Second))
		require.EqualValues(t, 1, s.closes.Load())
	})
}
