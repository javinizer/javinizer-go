package commandutil

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestReplaceReloadableRejectsNilRegistryWithoutChangingState(t *testing.T) {
	original := &config.Config{Database: config.DatabaseConfig{DSN: filepath.Join(t.TempDir(), "reload.db")}}
	originalRegistry := scraperutil.NewScraperRegistry()
	deps, err := NewDependenciesWithOptions(original, &DependenciesOptions{ScraperRegistry: originalRegistry})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, deps.Close()) })

	replacement := original.Clone()
	replacement.ConfigVersion++
	require.PanicsWithValue(t, "commandutil: ReplaceReloadable() called with nil registry — this is a programming error", func() {
		deps.ReplaceReloadable(replacement, nil)
	})
	require.Same(t, original, deps.GetConfig(), "rejected reload must not publish the replacement config")
	require.Same(t, originalRegistry, deps.GetRegistry(), "rejected reload must retain the working registry")

	replacementRegistry := scraperutil.NewScraperRegistry()
	deps.ReplaceReloadable(replacement, replacementRegistry)
	require.Same(t, replacement, deps.GetConfig(), "valid reload must still work after rejection")
	require.Same(t, replacementRegistry, deps.GetRegistry())
	sqlDB, err := deps.DB.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Ping(), "reload rejection must not close the existing database")
}
