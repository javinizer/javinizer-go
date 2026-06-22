package matcher

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatcherInterface(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	var iface MatcherInterface = m

	files := []models.FileMatchInfo{{Name: "IPX-535.mp4", Extension: ".mp4"}}
	results := iface.Match(files)
	require.Len(t, results, 1)
	require.Equal(t, "IPX-535", results[0].ID)

	result := iface.MatchFile(files[0])
	require.NotNil(t, result)
	require.Equal(t, "IPX-535", result.ID)

	require.Equal(t, "IPX-535", iface.MatchString("IPX-535.mp4"))
}

func TestConfigFromAppConfig(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		assert.Nil(t, ConfigFromAppConfig(nil))
	})

	t.Run("extracts regex settings", func(t *testing.T) {
		cfg := &config.Config{
			Matching: config.MatchingConfig{
				RegexEnabled: true,
				RegexPattern: "([A-Z]+-\\d+)",
			},
		}
		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.True(t, result.RegexEnabled)
		assert.Equal(t, "([A-Z]+-\\d+)", result.RegexPattern)
	})

	t.Run("defaults when not set", func(t *testing.T) {
		cfg := &config.Config{}
		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.False(t, result.RegexEnabled)
		assert.Equal(t, "", result.RegexPattern)
	})
}
