package scrape

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunMissingArgumentRecoversWithoutCreatingConfig(t *testing.T) {
	for _, args := range [][]string{nil, {}} {
		configPath := filepath.Join(t.TempDir(), "not-created.yaml")
		movie, results, err := Run(t.Context(), NewCommand(), args, configPath, nil)
		require.Nil(t, movie)
		require.Nil(t, results)
		require.ErrorContains(t, err, "panic: runtime error: index out of range")
		require.NotContains(t, err.Error(), "goroutine ", "callers must not receive the recovery stack")
		_, statErr := os.Stat(configPath)
		require.ErrorIs(t, statErr, os.ErrNotExist, "invalid input must fail before configuration initialization")
	}
}
