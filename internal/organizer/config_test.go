package organizer

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromAppConfig_Organizer(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		assert.Nil(t, ConfigFromAppConfig(nil, nfo.NFONameConfig{}))
	})

	t.Run("extracts organizer config", func(t *testing.T) {
		cfg := &config.Config{
			Output: config.OutputConfig{
				Template: config.OutputTemplateConfig{
					FolderFormat:     "<ID>",
					FileFormat:       "<ID>",
					ActressDelimiter: " - ",
				},
				Operation: config.OutputOperationConfig{
					RenameFile:  true,
					AllowRevert: true,
				},
			},
		}
		result := ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
		require.NotNil(t, result)
		assert.Equal(t, "<ID>", result.FolderFormat)
		assert.Equal(t, "<ID>", result.FileFormat)
		assert.True(t, result.RenameFile)
		assert.True(t, result.AllowRevert)
	})
}
