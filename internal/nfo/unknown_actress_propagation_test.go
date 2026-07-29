package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnknownActressMode_PropagationThroughBridges guards the fix for the
// @Unknown insertion bug: when unknown_actress_mode is "skip" (the config
// default) and group_actress is enabled, the mode must propagate from the
// app config through NFONameConfigFromAppConfig -> NFONameConfig -> each
// downstream ConfigFromAppConfig bridge so that <ACTORS>/<ACTRESSES>
// rendering suppresses @Unknown. A regression in any bridge wiring would
// let @Unknown leak back into folder/filename/poster paths.
func TestUnknownActressMode_PropagationThroughBridges(t *testing.T) {
	skipCfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				GroupActress:            true,
				GroupActressName:        "@Group",
				GroupUnknownActressName: "@Unknown",
			},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{
					UnknownActressMode: models.UnknownActressModeSkip,
					UnknownActressText: "Unknown",
				},
			},
		},
	}

	fallbackCfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				GroupActress:            true,
				GroupActressName:        "@Group",
				GroupUnknownActressName: "@Unknown",
			},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{
					UnknownActressMode: models.UnknownActressModeFallback,
					UnknownActressText: "Unknown",
				},
			},
		},
	}

	t.Run("NFONameConfigFromAppConfig propagates mode", func(t *testing.T) {
		skipName := NFONameConfigFromAppConfig(skipCfg)
		assert.Equal(t, models.UnknownActressModeSkip, skipName.UnknownActressMode, "skip must propagate to NFONameConfig")
		assert.True(t, skipName.GroupActress, "group_actress must propagate")

		fallbackName := NFONameConfigFromAppConfig(fallbackCfg)
		assert.Equal(t, models.UnknownActressModeFallback, fallbackName.UnknownActressMode, "fallback must propagate to NFONameConfig")
	})

	t.Run("nfo.ConfigFromAppConfig propagates mode", func(t *testing.T) {
		nameCfg := NFONameConfigFromAppConfig(skipCfg)
		nfoCfg := ConfigFromAppConfig(skipCfg, nameCfg)
		require.NotNil(t, nfoCfg)
		assert.Equal(t, models.UnknownActressModeSkip, nfoCfg.UnknownActressMode, "skip must propagate to nfo.Config")
	})

	t.Run("end-to-end: skip suppresses @Unknown in <ACTORS> render", func(t *testing.T) {
		nameCfg := NFONameConfigFromAppConfig(skipCfg)
		nameCfg.FilenameTemplate = "<ACTORS>"
		movie := &models.Movie{ID: "IPX-123"} // no actresses
		eng := template.NewEngine()
		got := ResolveNFOFilename(eng, movie, nameCfg)
		assert.NotContains(t, got, "@Unknown", "skip mode must suppress @Unknown in rendered <ACTORS>")
		// An empty <ACTORS> render falls back to movie.ID per ResolveNFOFilename's
		// empty-filename handling; the key assertion is that @Unknown is absent.
	})

	t.Run("end-to-end: fallback inserts @Unknown in <ACTORS> render", func(t *testing.T) {
		nameCfg := NFONameConfigFromAppConfig(fallbackCfg)
		nameCfg.FilenameTemplate = "<ACTORS>"
		movie := &models.Movie{ID: "IPX-123"} // no actresses
		eng := template.NewEngine()
		got := ResolveNFOFilename(eng, movie, nameCfg)
		assert.Contains(t, got, "@Unknown", "fallback mode must insert @Unknown in rendered <ACTORS>")
	})
}
