package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

type ServerDependencies = core.ServerDependencies
type ErrorResponse = contracts.ErrorResponse
type ScrapeResponse = contracts.ScrapeResponse
type BatchScrapeRequest = contracts.BatchScrapeRequest

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.ServerDependencies {
	return testkit.CreateTestDeps(t, cfg, configFile)
}

func newMockMovieRepo() *database.MovieRepository { return testkit.NewMockMovieRepo() }
func newMockActressRepo() *database.ActressRepository {
	return testkit.NewMockActressRepo()
}

func cleanupServerHub(t *testing.T, deps *core.ServerDependencies) {
	testkit.CleanupServerHub(t, deps)
}

func setupAuthenticatedTestServer(t *testing.T) (*gin.Engine, *core.ServerDependencies) {
	t.Helper()
	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := auth.NewAuthManager(configFile, time.Hour)
	if err != nil {
		t.Fatalf("failed to init auth manager: %v", err)
	}
	deps.Auth = manager
	router := NewServer(deps)
	return router, deps
}

type mockScraperWithResults struct {
	name    string
	enabled bool
	result  *models.ScraperResult
	err     error
}

func (m *mockScraperWithResults) Name() string { return m.name }

func (m *mockScraperWithResults) Search(id string) (*models.ScraperResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := *m.result
	result.ID = id
	return &result, nil
}

func (m *mockScraperWithResults) GetURL(id string) (string, error) { return "", nil }
func (m *mockScraperWithResults) IsEnabled() bool                  { return m.enabled }
func (m *mockScraperWithResults) Close() error                     { return nil }
func (m *mockScraperWithResults) Config() *config.ScraperConfig {
	return &config.ScraperConfig{Enabled: m.enabled}
}

type mockScraper struct {
	name    string
	enabled bool
}

func (m *mockScraper) Name() string { return m.name }
func (m *mockScraper) Search(id string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *mockScraper) GetURL(id string) (string, error) { return "", nil }
func (m *mockScraper) IsEnabled() bool                  { return m.enabled }
func (m *mockScraper) Close() error                     { return nil }
func (m *mockScraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{Enabled: m.enabled}
}
