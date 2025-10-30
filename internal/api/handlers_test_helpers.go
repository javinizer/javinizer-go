package api

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Test helpers for creating mock repositories

// mockScraperWithResults implements Scraper and returns predefined results
type mockScraperWithResults struct {
	name    string
	enabled bool
	result  *models.ScraperResult
	err     error
}

func (m *mockScraperWithResults) Name() string {
	return m.name
}

func (m *mockScraperWithResults) Search(id string) (*models.ScraperResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func (m *mockScraperWithResults) GetURL(id string) (string, error) {
	return "", nil
}

func (m *mockScraperWithResults) IsEnabled() bool {
	return m.enabled
}

// newMockMovieRepo creates a test movie repository with in-memory database
func newMockMovieRepo() *database.MovieRepository {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, _ := database.New(cfg)
	db.AutoMigrate()
	return database.NewMovieRepository(db)
}

// newMockActressRepo creates a test actress repository with in-memory database
func newMockActressRepo() *database.ActressRepository {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, _ := database.New(cfg)
	db.AutoMigrate()
	return database.NewActressRepository(db)
}
