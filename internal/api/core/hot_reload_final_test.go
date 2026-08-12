package core

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

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

func TestHotReloadFinal_ActressOnlyPriorityWarningsNotMovieCapable(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&mockScraperMovieSearchFalse{})
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Priority.Fields = map[string][]string{"title": {"moviesearchfalse"}}
	warnings := actressOnlyPriorityWarnings(reg, cfg)
	assert.NotEmpty(t, warnings)
}
