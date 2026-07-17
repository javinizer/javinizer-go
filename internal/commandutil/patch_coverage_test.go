package commandutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQueryOnlyDependencies_FinalizeError(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	orig := finalizeScrapersConfig
	t.Cleanup(func() { finalizeScrapersConfig = orig })
	finalizeScrapersConfig = func(c *config.ScrapersConfig, reg *scraperutil.ScraperRegistry) error {
		return errors.New("finalize boom")
	}
	tmpDir := t.TempDir()
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(tmpDir, "javinizer.db")}}
	deps, err := NewQueryOnlyDependencies(cfg)
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "failed to finalize scraper config")
}

func TestNewQueryOnlyDependencies_DumpLookupWarning(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	corruptDump := filepath.Join(tmpDir, "corrupt.db")
	require.NoError(t, os.WriteFile(corruptDump, []byte("not a sqlite database"), 0o600))
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(tmpDir, "javinizer.db")}}
	cfg.Metadata.R18DevDump.Enabled = true
	cfg.Metadata.R18DevDump.Path = corruptDump
	cfg.Scrapers.Priority = []string{"e2emock"}
	deps, err := NewQueryOnlyDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps)
	assert.NoError(t, deps.Close())
}

func TestNewQueryOnlyDependencies_RegistryInitError(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	orig := newScraperRegistryFrom
	t.Cleanup(func() { newScraperRegistryFrom = orig })
	newScraperRegistryFrom = func(reg *scraperutil.ScraperRegistry, cfg scraper.ScraperRegistryConfig, contentIDRepo models.ContentIDMappingRepositoryInterface, r18DevDump models.R18DevDumpLookup) (*scraperutil.ScraperRegistry, error) {
		return nil, errors.New("registry boom")
	}
	tmpDir := t.TempDir()
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(tmpDir, "javinizer.db")}}
	cfg.Scrapers.Priority = []string{"e2emock"}
	deps, err := NewQueryOnlyDependencies(cfg)
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "failed to initialize scraper registry")
}

func TestNewQueryOnlyDependencies_RegistryInitError_ClosesDump(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	orig := newScraperRegistryFrom
	t.Cleanup(func() { newScraperRegistryFrom = orig })
	newScraperRegistryFrom = func(reg *scraperutil.ScraperRegistry, cfg scraper.ScraperRegistryConfig, contentIDRepo models.ContentIDMappingRepositoryInterface, r18DevDump models.R18DevDumpLookup) (*scraperutil.ScraperRegistry, error) {
		return nil, errors.New("registry boom")
	}
	dumpPath := seedDumpDB(t)
	tmpDir := t.TempDir()
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(tmpDir, "javinizer.db")}}
	cfg.Metadata.R18DevDump.Enabled = true
	cfg.Metadata.R18DevDump.Path = dumpPath
	cfg.Scrapers.Priority = []string{"e2emock"}
	deps, err := NewQueryOnlyDependencies(cfg)
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "failed to initialize scraper registry")
}

func TestMemoryContentIDRepository_GetAllPaginated_CancelledContext(t *testing.T) {
	repo := newMemoryContentIDRepository()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repo.GetAllPaginated(ctx, 1, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
