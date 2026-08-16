package system

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

type mockScraperNoMovieSys struct{}

func (m *mockScraperNoMovieSys) Name() string { return "dmm" }
func (m *mockScraperNoMovieSys) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *mockScraperNoMovieSys) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockScraperNoMovieSys) IsEnabled() bool                                    { return true }
func (m *mockScraperNoMovieSys) Config() *models.ScraperSettings                    { return &models.ScraperSettings{} }
func (m *mockScraperNoMovieSys) Close() error                                       { return nil }
func (m *mockScraperNoMovieSys) SupportsMovieSearch() bool                          { return false }
func (m *mockScraperNoMovieSys) ResolveActressMetadata(_ context.Context, _ models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{}, nil
}

func TestCovFinal_ValidatePriorityScraperNotFound(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&mockScraperNoMovieSys{})
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: reg}}
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Priority.Fields = map[string][]string{"actress": {"dmm", "nonexistent"}}
	err := validatePriorityFieldCapabilities(deps, cfg)
	assert.NoError(t, err)
}
