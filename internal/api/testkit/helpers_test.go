package testkit

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockScraperWithResults_Search(t *testing.T) {
	result := &models.ScraperResult{Title: "Test Movie", Source: "mock"}
	scraper := NewMockScraperWithResults("mock", true, result, nil)

	assert.Equal(t, "mock", scraper.Name())
	assert.True(t, scraper.IsEnabled())

	sr, err := scraper.Search(context.Background(), "TEST-001")
	require.NoError(t, err)
	assert.Equal(t, "TEST-001", sr.ID)
	assert.Equal(t, "Test Movie", sr.Title)
}

func TestMockScraperWithResults_SearchError(t *testing.T) {
	scraper := NewMockScraperWithResults("err", true, nil, assert.AnError)
	_, err := scraper.Search(context.Background(), "TEST-001")
	assert.Error(t, err)
}

func TestMockScraperWithResults_GetURL(t *testing.T) {
	scraper := NewMockScraperWithResults("mock", true, nil, nil)
	url, err := scraper.GetURL(context.Background(), "TEST-001")
	assert.NoError(t, err)
	assert.Empty(t, url)
}

func TestMockScraperWithResults_Config(t *testing.T) {
	scraper := NewMockScraperWithResults("mock", true, nil, nil)
	cfg := scraper.Config()
	assert.NotNil(t, cfg)
}

func TestMockScraperWithResults_Close(t *testing.T) {
	scraper := NewMockScraperWithResults("mock", true, nil, nil)
	assert.NoError(t, scraper.Close())
}

func TestNewMockMovieRepo(t *testing.T) {
	repo := NewMockMovieRepo()
	assert.NotNil(t, repo)
}

func TestNewMockActressRepo(t *testing.T) {
	repo := NewMockActressRepo()
	assert.NotNil(t, repo)
}

func TestCreateTestDeps(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"

	deps := CreateTestDeps(t, cfg, "")
	assert.NotNil(t, deps)
	assert.NotNil(t, deps.Repos)
	assert.NotNil(t, deps.JobStore)

	// Verify we can get config
	retrievedCfg := deps.CoreDeps.GetConfig()
	assert.NotNil(t, retrievedCfg)
}

func TestInitTestWebSocket(t *testing.T) {
	rt := core.NewRuntimeState()
	InitTestWebSocket(t, rt)
}

func TestCleanupServerHub(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	deps := CreateTestDeps(t, cfg, "")
	CleanupServerHub(t, GetTestRuntime(deps))
}

func TestCleanupServerHub_NilDeps(t *testing.T) {
	// Should not panic
	CleanupServerHub(t, (*core.APIRuntime)(nil))
}
