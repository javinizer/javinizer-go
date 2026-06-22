package downloader

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromAppConfig(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		assert.Nil(t, ConfigFromAppConfig(nil, nfo.NFONameConfig{}))
	})

	t.Run("extracts settings from app config", func(t *testing.T) {
		cfg := &config.Config{
			Output: config.OutputConfig{
				MediaFormat: config.OutputMediaFormatConfig{
					PosterFormat:      "jpg",
					FanartFormat:      "webp",
					TrailerFormat:     "mp4",
					ScreenshotFormat:  "jpg",
					ScreenshotFolder:  "screenshots",
					ScreenshotPadding: 4,
					ActressFolder:     "actress",
					ActressFormat:     "{name}",
				},
				Download: config.OutputDownloadConfig{
					DownloadCover:       true,
					DownloadPoster:      true,
					DownloadExtrafanart: false,
					DownloadTrailer:     true,
					DownloadActress:     false,
					DownloadTimeout:     30,
				},
			},
		}
		result := ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
		require.NotNil(t, result)
		assert.Equal(t, "jpg", result.PosterFormat)
		assert.True(t, result.DownloadCover)
		assert.Equal(t, 30, result.DownloadTimeout)
	})
}

func TestDownloadPartialError(t *testing.T) {
	err := &DownloadPartialError{Attempted: 3, Succeeded: 1}
	assert.Contains(t, err.Error(), "3 critical media attempted")
	assert.Contains(t, err.Error(), "1 succeeded")
}
