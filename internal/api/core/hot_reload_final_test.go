package core

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

type mockScraperNoMovieSearch struct{}

func (m *mockScraperNoMovieSearch) Name() string { return "nomoviesearch" }
func (m *mockScraperNoMovieSearch) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *mockScraperNoMovieSearch) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockScraperNoMovieSearch) IsEnabled() bool                 { return true }
func (m *mockScraperNoMovieSearch) Config() *models.ScraperSettings { return &models.ScraperSettings{} }
func (m *mockScraperNoMovieSearch) Close() error                    { return nil }

type mockScraperMovieSearchFalse struct{}

func (m *mockScraperMovieSearchFalse) Name() string { return "moviesearchfalse" }
func (m *mockScraperMovieSearchFalse) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *mockScraperMovieSearchFalse) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockScraperMovieSearchFalse) IsEnabled() bool { return true }
func (m *mockScraperMovieSearchFalse) Config() *models.ScraperSettings {
	return &models.ScraperSettings{}
}
func (m *mockScraperMovieSearchFalse) Close() error              { return nil }
func (m *mockScraperMovieSearchFalse) SupportsMovieSearch() bool { return false }

func TestHotReloadFinal_NotMovieSearchCapableSetsCapable(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&mockScraperNoMovieSearch{})
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Priority.Fields = map[string][]string{"title": {"nomoviesearch"}}
	warnings := actressOnlyPriorityWarnings(reg, cfg)
	assert.Empty(t, warnings)
}

func TestHotReloadFinal_ReloadConfigLockedWithWarnings(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&mockScraperMovieSearchFalse{})
	cfg := newHotReloadRaceConfig("host", 1, 10)
	cfg.Metadata.Priority.Fields = map[string][]string{"title": {"moviesearchfalse"}}
	rt := newHotReloadRaceRuntime(t, cfg)
	rt.deps.CoreDeps.ScraperRegistry = reg
	err := rt.reloadConfigLocked(cfg, reg)
	assert.NoError(t, err)
}
