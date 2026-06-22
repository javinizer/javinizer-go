package scanner

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromAppConfig_Scanner(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		assert.Nil(t, ConfigFromAppConfig(nil))
	})

	t.Run("extracts scanner settings", func(t *testing.T) {
		cfg := &config.Config{
			Matching: config.MatchingConfig{
				Extensions:      []string{".mp4", ".mkv"},
				MinSizeMB:       100,
				ExcludePatterns: []string{"sample"},
			},
		}
		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.Equal(t, []string{".mp4", ".mkv"}, result.Extensions)
		assert.Equal(t, 100, result.MinSizeMB)
		assert.Equal(t, []string{"sample"}, result.ExcludePatterns)
	})
}
