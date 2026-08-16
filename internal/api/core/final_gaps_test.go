package core

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockScraperNoMovie struct{}

func (m *mockScraperNoMovie) Name() string { return "mocknomovie" }
func (m *mockScraperNoMovie) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *mockScraperNoMovie) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockScraperNoMovie) IsEnabled() bool                                    { return true }
func (m *mockScraperNoMovie) Config() *models.ScraperSettings                    { return &models.ScraperSettings{} }
func (m *mockScraperNoMovie) Close() error                                       { return nil }

func TestCovFinal_EnsureActressSyncManagerStoppedAndCached(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	reg := scraperutil.NewScraperRegistry()
	deps := &APIDeps{CoreDeps: &commandutil.CoreDeps{DB: db, ScraperRegistry: reg}}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(config.DefaultConfig(nil, nil))

	m1 := rt.EnsureActressSyncManager()
	require.NotNil(t, m1)
	m2 := rt.EnsureActressSyncManager()
	assert.Equal(t, m1, m2)

	rt.stopActressSyncManager()
	assert.Nil(t, rt.EnsureActressSyncManager())
}

func TestCovFinal_ActressOnlyPriorityWarningsRecognized(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&mockScraperNoMovie{})
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Priority.Fields = map[string][]string{"actress": {"mocknomovie"}}
	warnings := actressOnlyPriorityWarnings(reg, cfg)
	assert.Empty(t, warnings)
}
