package commandutil

import (
	"errors"
	"os"
	"path/filepath"
	"regexp/syntax"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

// A matcher configuration error must leave no workflow or open SQLite handle.

func TestBootstrap_InvalidRegex_InMatcher(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}
	// Enable custom regex with an invalid pattern — this passes config validation
	// but will fail when the matcher tries to compile the regex in NewFactoryConfigFromRepos
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "[invalid"

	result, err := Bootstrap(cfg)
	assertBootstrapMatcherFailure(t, cfg.Database.DSN, result, err)
}

func TestBootstrapScrapeOnly_InvalidRegex_InMatcher(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}
	// Enable custom regex with an invalid pattern
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "[invalid"

	result, err := BootstrapScrapeOnly(cfg)
	assertBootstrapMatcherFailure(t, cfg.Database.DSN, result, err)
}

func assertBootstrapMatcherFailure(t *testing.T, dsn string, result *bootstrapResult, err error) {
	t.Helper()
	require.Nil(t, result)
	var syntaxErr *syntax.Error
	require.ErrorAs(t, err, &syntaxErr)
	// Preserve the concrete first-stage wrapper, not an errors.Join wrapper or
	// a subsequent factory validation error.
	require.Same(t, syntaxErr, errors.Unwrap(err))
	require.ErrorContains(t, err, "buildMatcher:")

	// The database was opened and migrated before the error. Closing its last
	// connection must remove the WAL, as in the dependency cleanup tests.
	require.FileExists(t, dsn)
	_, err = os.Stat(dsn + "-wal")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBootstrap_ValidRegex_StillSucceeds(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = `^([A-Z]+-\d+)` // Valid regex

	result, err := Bootstrap(cfg)
	require.NoError(t, err, "Bootstrap should succeed with valid regex")
	require.NotNil(t, result)
	require.NoError(t, result.Close())
}

func TestBootstrapScrapeOnly_ValidRegex_StillSucceeds(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = `^([A-Z]+-\d+)` // Valid regex

	result, err := BootstrapScrapeOnly(cfg)
	require.NoError(t, err, "BootstrapScrapeOnly should succeed with valid regex")
	require.NotNil(t, result)
	require.NoError(t, result.Close())
}
