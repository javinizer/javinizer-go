package commandutil

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDependenciesWithOptions_NilOptions verifies backward compatibility.
// When opts is nil, behavior is identical to NewDependencies().
func TestNewDependenciesWithOptions_NilOptions(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: t.TempDir() + "/test.db",
		},
	}

	deps, err := NewDependenciesWithOptions(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.Config)
	assert.NotNil(t, deps.DB, "DB should be initialized when opts is nil")
	assert.NotNil(t, deps.ScraperRegistry, "ScraperRegistry should be initialized when opts is nil")
}

// TestNewDependenciesWithOptions_InjectedDB tests injecting a mock database.
func TestNewDependenciesWithOptions_InjectedDB(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	// Create a real DB to inject (simulating a test mock)
	mockDB, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	opts := &DependenciesOptions{
		DB: mockDB,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.Config)
	assert.Equal(t, mockDB, deps.DB, "Injected DB should be used")
	assert.NotNil(t, deps.ScraperRegistry, "ScraperRegistry should still be initialized")
}

// TestNewDependenciesWithOptions_InjectedRegistry tests injecting a mock registry.
func TestNewDependenciesWithOptions_InjectedRegistry(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: t.TempDir() + "/test.db",
		},
	}

	// Create a mock registry
	mockRegistry := models.NewScraperRegistry()

	opts := &DependenciesOptions{
		ScraperRegistry: mockRegistry,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.Config)
	assert.NotNil(t, deps.DB, "DB should still be initialized")
	assert.Equal(t, mockRegistry, deps.ScraperRegistry, "Injected registry should be used")
}

// TestNewDependenciesWithOptions_BothInjected tests injecting both DB and registry.
func TestNewDependenciesWithOptions_BothInjected(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	// Create mocks
	mockDB, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	mockRegistry := models.NewScraperRegistry()

	opts := &DependenciesOptions{
		DB:              mockDB,
		ScraperRegistry: mockRegistry,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.Config)
	assert.Equal(t, mockDB, deps.DB, "Injected DB should be used")
	assert.Equal(t, mockRegistry, deps.ScraperRegistry, "Injected registry should be used")
}

// TestNewDependenciesWithOptions_NilConfig verifies error handling.
func TestNewDependenciesWithOptions_NilConfig(t *testing.T) {
	opts := &DependenciesOptions{}

	deps, err := NewDependenciesWithOptions(nil, opts)
	assert.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "config cannot be nil")
}

// TestDependenciesInterface_Compliance verifies Dependencies implements the interface.
func TestDependenciesInterface_Compliance(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: t.TempDir() + "/test.db",
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Verify interface compliance by assigning to interface type
	var _ DependenciesInterface = deps

	// Verify interface methods work
	assert.Equal(t, cfg, deps.GetConfig())
	assert.NotNil(t, deps.GetDB())
	assert.NotNil(t, deps.GetScraperRegistry())
}

// TestGetConfig verifies GetConfig returns the correct config.
func TestGetConfig(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: t.TempDir() + "/test.db",
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.Equal(t, cfg, deps.GetConfig())
}

// TestGetDB verifies GetDB returns the correct database.
func TestGetDB(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: t.TempDir() + "/test.db",
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	db := deps.GetDB()
	assert.NotNil(t, db)
	assert.Equal(t, deps.DB, db)
}

// TestGetScraperRegistry verifies GetScraperRegistry returns the correct registry.
func TestGetScraperRegistry(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: t.TempDir() + "/test.db",
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	registry := deps.GetScraperRegistry()
	assert.NotNil(t, registry)
	assert.Equal(t, deps.ScraperRegistry, registry)
}

// TestNewDependencies_BackwardCompatibility verifies NewDependencies still works unchanged.
func TestNewDependencies_BackwardCompatibility(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: t.TempDir() + "/test.db",
		},
	}

	// This should behave exactly as before Epic 6 refactoring
	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.Config)
	assert.NotNil(t, deps.DB)
	assert.NotNil(t, deps.ScraperRegistry)
}

// TestNewDependenciesWithOptions_EmptyOptions tests behavior with empty (but non-nil) options.
func TestNewDependenciesWithOptions_EmptyOptions(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: t.TempDir() + "/test.db",
		},
	}

	// Empty options should initialize real dependencies
	opts := &DependenciesOptions{}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.Config)
	assert.NotNil(t, deps.DB, "DB should be initialized when opts fields are nil")
	assert.NotNil(t, deps.ScraperRegistry, "ScraperRegistry should be initialized when opts fields are nil")
}
