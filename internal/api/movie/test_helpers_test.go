package movie

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.ServerDependencies {
	return testkit.CreateTestDeps(t, cfg, configFile)
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
