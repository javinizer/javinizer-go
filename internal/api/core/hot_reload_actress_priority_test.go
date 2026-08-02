package core

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

type noMovieSearchStub struct{ name string }

func (s *noMovieSearchStub) Name() string    { return s.name }
func (s *noMovieSearchStub) IsEnabled() bool { return true }
func (s *noMovieSearchStub) Config() *models.ScraperSettings {
	return &models.ScraperSettings{}
}
func (s *noMovieSearchStub) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *noMovieSearchStub) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *noMovieSearchStub) Close() error                                   { return nil }
func (s *noMovieSearchStub) SupportsMovieSearch() bool                      { return false }

// Boot-time warning must cover YAML-authored exclusively-actress-only
// overrides, which bypass the API save-time rejection.
func TestActressOnlyPriorityWarning(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&noMovieSearchStub{name: "minnanoav"})

	cfg := config.DefaultConfig(nil, nil)
	require.Empty(t, actressOnlyPriorityWarning(registry, cfg))
	cfg.Metadata.Priority.Fields = map[string][]string{"actress": {"minnanoav"}}
	require.NotEmpty(t, actressOnlyPriorityWarning(registry, cfg))
	cfg.Metadata.Priority.Fields["actress"] = []string{"__skip__"}
	require.Empty(t, actressOnlyPriorityWarning(registry, cfg))
	require.Empty(t, actressOnlyPriorityWarning(nil, cfg))
	require.Empty(t, actressOnlyPriorityWarning(registry, nil))
}
