package commandutil

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Bootstrap error paths via invalid regex (lines 250-258) ---

func TestBootstrap_InvalidRegex_InMatcher(t *testing.T) {
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
	assert.Error(t, err, "Bootstrap should fail when matcher regex is invalid")
	assert.Nil(t, result)
}

// --- BootstrapScrapeOnly error paths via invalid regex (lines 270-278) ---

func TestBootstrapScrapeOnly_InvalidRegex_InMatcher(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}
	// Enable custom regex with an invalid pattern
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "[invalid"

	result, err := BootstrapScrapeOnly(cfg)
	assert.Error(t, err, "BootstrapScrapeOnly should fail when matcher regex is invalid")
	assert.Nil(t, result)
}

// --- Bootstrap: NewWorkflowFactory error (line 255-258) ---
// This is hard to trigger without mocking, but we can try with a config that
// has valid regex but causes NewWorkflowFactory to fail.

func TestBootstrap_ValidRegex_StillSucceeds(t *testing.T) {
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

// --- BootstrapScrapeOnly: NewWorkflowFactory error ---
// Similar to above — the scrape-only path should also fail with invalid regex
// since NewFactoryConfigFromRepos is called before NewWorkflowFactory

func TestBootstrapScrapeOnly_ValidRegex_StillSucceeds(t *testing.T) {
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
