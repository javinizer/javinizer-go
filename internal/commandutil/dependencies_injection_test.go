package commandutil

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDependenciesWithOptions_NilOptions verifies backward compatibility.
// When opts is nil, behavior is identical to NewDependencies().
func TestNewDependenciesWithOptions_NilOptions(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependenciesWithOptions(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.GetConfig())
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
	mockDB, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	opts := &DependenciesOptions{
		DB: mockDB,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.GetConfig())
	assert.Equal(t, mockDB, deps.DB, "Injected DB should be used")
	assert.NotNil(t, deps.ScraperRegistry, "ScraperRegistry should still be initialized")
}

// TestNewDependenciesWithOptions_InjectedRegistry tests injecting a mock registry.
func TestNewDependenciesWithOptions_InjectedRegistry(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	// Create a mock registry
	mockRegistry := scraperutil.NewScraperRegistry()

	opts := &DependenciesOptions{
		ScraperRegistry: mockRegistry,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.GetConfig())
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
	mockDB, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	mockRegistry := scraperutil.NewScraperRegistry()

	opts := &DependenciesOptions{
		DB:              mockDB,
		ScraperRegistry: mockRegistry,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.GetConfig())
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

// TestGetConfig verifies GetConfig returns the correct config.
func TestGetConfig(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
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
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	db := deps.DB
	assert.NotNil(t, db)
}

// TestGetRegistry verifies GetRegistry returns the correct registry.
func TestGetRegistry(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	registry := deps.GetRegistry()
	assert.NotNil(t, registry)
	assert.Equal(t, deps.ScraperRegistry, registry)
}

// TestNewDependencies_BackwardCompatibility verifies NewDependencies still works unchanged.
func TestNewDependencies_BackwardCompatibility(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	// This should behave exactly as before Epic 6 refactoring
	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.GetConfig())
	assert.NotNil(t, deps.DB)
	assert.NotNil(t, deps.ScraperRegistry)
}

// TestNewDependenciesWithOptions_EmptyOptions tests behavior with empty (but non-nil) options.
func TestNewDependenciesWithOptions_EmptyOptions(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	// Empty options should initialize real dependencies
	opts := &DependenciesOptions{}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.GetConfig())
	assert.NotNil(t, deps.DB, "DB should be initialized when opts fields are nil")
	assert.NotNil(t, deps.ScraperRegistry, "ScraperRegistry should be initialized when opts fields are nil")
}

// TestDependencies_GetConfig_AtomicPointer tests that GetConfig returns the stored config.
func TestDependencies_GetConfig_AtomicPointer(t *testing.T) {
	cfg := &config.Config{ConfigVersion: 1}
	deps := &CoreDeps{}
	deps.config.Store(cfg)

	// GetConfig returns the stored config
	assert.Equal(t, cfg, deps.GetConfig())

	// After SetConfig, GetConfig returns the new config
	cfg2 := &config.Config{ConfigVersion: 2}
	deps.SetConfig(cfg2)
	assert.Equal(t, cfg2, deps.GetConfig())
}

func TestDependencies_GetConfig_PanicsOnNil(t *testing.T) {
	deps := &CoreDeps{}
	assert.Panics(t, func() {
		deps.GetConfig()
	}, "GetConfig should panic when no config has been set")
}

func TestDependencies_SetConfig_PanicsOnNil(t *testing.T) {
	deps := &CoreDeps{}
	assert.Panics(t, func() {
		deps.SetConfig(nil)
	}, "SetConfig should panic when called with nil config")
}

// TestDependencies_GetRegistry_PanicsOnNil tests that GetRegistry panics when ScraperRegistry is nil.
// This indicates a construction bug — NewDependenciesWithOptions always initializes the registry.
func TestDependencies_GetRegistry_PanicsOnNil(t *testing.T) {
	deps := &CoreDeps{}
	assert.Panics(t, func() {
		deps.GetRegistry()
	}, "GetRegistry should panic when ScraperRegistry is nil")
}

// TestDependencies_ReplaceReloadable tests atomic swap of reloadable components.
func TestDependencies_ReplaceReloadable(t *testing.T) {
	cfg := &config.Config{ConfigVersion: 1}
	registry := scraperutil.NewScraperRegistry()

	deps := &CoreDeps{}
	deps.ReplaceReloadable(cfg, registry)

	assert.Equal(t, cfg, deps.GetConfig(), "Config should be replaced")
	assert.Equal(t, registry, deps.GetRegistry(), "Registry should be replaced")
}

// TestDependencies_ConcurrentAccess tests thread-safe config access.
func TestDependencies_ConcurrentAccess(t *testing.T) {
	deps := &CoreDeps{}
	cfg1 := &config.Config{ConfigVersion: 1}
	cfg2 := &config.Config{ConfigVersion: 2}

	deps.SetConfig(cfg1)

	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			_ = deps.GetConfig()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			deps.SetConfig(cfg2)
		}
		done <- true
	}()

	for i := 0; i < 2; i++ {
		<-done
	}

	finalCfg := deps.GetConfig()
	assert.Contains(t, []int{1, 2}, finalCfg.ConfigVersion, "Final config should be one of the set values")
}

// TestDependencies_APISpecificFields tests that API fields default to nil for CLI usage.
func TestDependencies_APISpecificFields(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// CoreDeps should only have the 3 core fields populated
	assert.NotNil(t, deps.GetConfig())
	assert.NotNil(t, deps.DB)
	assert.NotNil(t, deps.ScraperRegistry)
}
