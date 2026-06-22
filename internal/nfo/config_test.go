package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromAppConfig_NFO(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		assert.Nil(t, ConfigFromAppConfig(nil, NFONameConfig{}))
	})

	t.Run("extracts NFO config", func(t *testing.T) {
		cfg := &config.Config{
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					Format: config.NFOFormatConfig{
						FilenameTemplate: "{id}",
						FirstNameOrder:   true,
					},
				},
			},
		}
		result := ConfigFromAppConfig(cfg, NFONameConfigFromAppConfig(cfg))
		require.NotNil(t, result)
		assert.Equal(t, "{id}", result.FilenameTemplate)
		assert.True(t, result.FirstNameOrder)
	})
}

func TestConfig_ToNFONameConfig(t *testing.T) {
	c := &Config{
		FilenameTemplate: "{id}",
		GroupActress:     true,
		GroupActressName: "actress",
		PerFile:          true,
		FirstNameOrder:   true,
	}
	result := c.ToNFONameConfig(true, "-A")
	assert.Equal(t, "{id}", result.FilenameTemplate)
	assert.True(t, result.GroupActress)
	assert.True(t, result.IsMultiPart)
	assert.Equal(t, "-A", result.PartSuffix)
	assert.True(t, result.FirstNameOrder)
}
