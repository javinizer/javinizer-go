package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPosterFallbackModeDefaultsAndValidation(t *testing.T) {
	assert.Equal(t, "legacy", DefaultConfig(nil, nil).Output.MediaFormat.PosterFallbackMode)
	for _, mode := range []string{"", "legacy", "center", "fit", "invalid", "CENTER"} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultConfig(nil, nil)
			cfg.Output.MediaFormat.PosterFallbackMode = mode
			for _, validate := range []func() error{cfg.Validate, func() error { return validateConfigExcludingTranslationCredentials(cfg) }} {
				err := validate()
				if mode == "invalid" || mode == "CENTER" {
					require.ErrorContains(t, err, "output.poster_fallback_mode")
				} else {
					require.NoError(t, err)
				}
			}
		})
	}
	for _, mode := range []string{"center", "fit"} {
		for _, height := range []int{-1, 0, 1, 2, 3, 500} {
			cfg := DefaultConfig(nil, nil)
			cfg.Output.MediaFormat.PosterFallbackMode = mode
			cfg.Output.MediaFormat.MaxPosterHeight = height
			if height != 0 && height < 3 {
				require.ErrorContains(t, cfg.Validate(), "output.max_poster_height")
			} else {
				require.NoError(t, cfg.Validate())
			}
		}
	}
}

func TestPosterFallbackModeRoundTrip(t *testing.T) {
	for _, mode := range []string{"", "legacy", "center", "fit"} {
		original := OutputConfig{MediaFormat: OutputMediaFormatConfig{PosterFallbackMode: mode}}
		for _, codec := range []struct {
			marshal   func(any) ([]byte, error)
			unmarshal func([]byte, any) error
		}{{json.Marshal, json.Unmarshal}, {yaml.Marshal, yaml.Unmarshal}} {
			raw, err := codec.marshal(original)
			require.NoError(t, err)
			assert.Contains(t, string(raw), "poster_fallback_mode")
			var restored OutputConfig
			require.NoError(t, codec.unmarshal(raw, &restored))
			assert.Equal(t, mode, restored.MediaFormat.PosterFallbackMode)
		}
	}
}
