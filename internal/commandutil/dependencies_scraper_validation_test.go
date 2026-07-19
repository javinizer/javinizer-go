package commandutil

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewQueryOnlyDependencies_InvalidEffectiveScraperFails(t *testing.T) {
	tmpDir := t.TempDir()
	src := strings.Join([]string{
		"config_version: 3",
		"database:",
		"  type: sqlite",
		"  dsn: " + filepath.Join(tmpDir, "javinizer.db"),
		"scrapers:",
		"    r18dev:",
		"        rate_limit: -1",
	}, "\n")
	cfg := config.DefaultConfig(nil, nil)
	require.NoError(t, yaml.Unmarshal([]byte(src), cfg))
	cfg.Metadata.R18DevDump.Path = filepath.Join(tmpDir, "nonexistent_dump.db")

	_, err := NewQueryOnlyDependencies(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit")
}

func TestNewQueryOnlyDependencies_ExplicitFalseInvalidScraperPasses(t *testing.T) {
	tmpDir := t.TempDir()
	src := strings.Join([]string{
		"config_version: 3",
		"database:",
		"  type: sqlite",
		"  dsn: " + filepath.Join(tmpDir, "javinizer.db"),
		"scrapers:",
		"    r18dev:",
		"        enabled: false",
		"        rate_limit: -1",
	}, "\n")
	cfg := config.DefaultConfig(nil, nil)
	require.NoError(t, yaml.Unmarshal([]byte(src), cfg))
	cfg.Metadata.R18DevDump.Path = filepath.Join(tmpDir, "nonexistent_dump.db")

	deps, err := NewQueryOnlyDependencies(cfg)
	require.NoError(t, err, "explicit enabled:false must skip validation and construct successfully")
	assert.NotNil(t, deps.ScraperRegistry)
	assert.NoError(t, deps.Close())
}

func TestNewQueryOnlyDependencies_ValidOmittedEnabledPasses(t *testing.T) {
	tmpDir := t.TempDir()
	src := strings.Join([]string{
		"config_version: 3",
		"database:",
		"  type: sqlite",
		"  dsn: " + filepath.Join(tmpDir, "javinizer.db"),
		"scrapers:",
		"    r18dev:",
		"        rate_limit: 500",
	}, "\n")
	cfg := config.DefaultConfig(nil, nil)
	require.NoError(t, yaml.Unmarshal([]byte(src), cfg))
	cfg.Metadata.R18DevDump.Path = filepath.Join(tmpDir, "nonexistent_dump.db")

	deps, err := NewQueryOnlyDependencies(cfg)
	require.NoError(t, err, "a valid sparse override omitting enabled must construct successfully")
	assert.NotNil(t, deps.ScraperRegistry)
	assert.NoError(t, deps.Close())
}

func newInjectedRegistryDB(t *testing.T) (*database.DB, *scraperutil.ScraperRegistry) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, scraperutil.NewScraperRegistry()
}

func TestNewDependenciesWithOptions_InjectedRegistry_InvalidBaseFails(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"}}
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true, RateLimit: -1},
	}
	db, reg := newInjectedRegistryDB(t)
	reg.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})

	deps, err := NewDependenciesWithOptions(cfg, &DependenciesOptions{DB: db, ScraperRegistry: reg})
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "rate_limit")
}

func TestNewDependenciesWithOptions_InjectedRegistry_ScraperSpecificValidatorFails(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"}}
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "bad-lang"},
	}
	db, reg := newInjectedRegistryDB(t)
	sentinel := errors.New("r18dev: language must be 'en' or 'ja'")
	reg.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
		ValidateFn: func(ss *models.ScraperSettings) error {
			if ss.Language != "en" && ss.Language != "ja" {
				return sentinel
			}
			return nil
		},
	})

	deps, err := NewDependenciesWithOptions(cfg, &DependenciesOptions{DB: db, ScraperRegistry: reg})
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.ErrorIs(t, err, sentinel)
}

func TestNewDependenciesWithOptions_InjectedRegistry_ValidPasses(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"}}
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true, RateLimit: 500},
	}
	db, reg := newInjectedRegistryDB(t)
	reg.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})

	deps, err := NewDependenciesWithOptions(cfg, &DependenciesOptions{DB: db, ScraperRegistry: reg})
	require.NoError(t, err)
	require.NotNil(t, deps)
	assert.Same(t, reg, deps.ScraperRegistry)
}
