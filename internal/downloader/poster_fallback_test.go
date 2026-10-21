package downloader

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPosterFallbackModesIntegration(t *testing.T) {
	source := twoToneSourceBytes(t)
	srv, _ := identityServer(t, source)
	for _, mode := range []string{"center", "fit"} {
		for _, intent := range []string{"automatic", "native", "manual", "manual-degenerate", "manual-mismatch"} {
			t.Run(mode+"/"+intent, func(t *testing.T) {
				fs := afero.NewMemMapFs()
				d := newGeometryDownloader(fs)
				d.config.PosterFallbackMode = mode
				var bounds *models.CropBounds
				if strings.HasPrefix(intent, "manual") {
					bounds = &models.CropBounds{X: 0, Y: 0, Width: 0.4, Height: 1, SourceAspect: 1000.0 / 600.0, SourceFingerprint: assetidentity.FromBytes(source).Fingerprint}
				}
				if intent == "manual-degenerate" {
					bounds.Width = 0.0001
					bounds.Height = 0.0001
				}
				if intent == "manual-mismatch" {
					bounds.SourceFingerprint = strings.Repeat("a", 64)
				}
				movie := geometryMovie(srv.URL, bounds, intent != "native")
				result, err := d.downloadPoster(context.Background(), movie, "/out", nil)
				if intent == "manual-mismatch" {
					require.ErrorIs(t, err, ErrPosterRecropRequired)
					require.False(t, result.Downloaded)
					return
				}
				require.NoError(t, err)
				require.True(t, result.Downloaded)
				img, w, h := decodeResultPoster(t, fs, result.LocalPath)
				switch intent {
				case "native":
					actual, err := afero.ReadFile(fs, result.LocalPath)
					require.NoError(t, err)
					require.Equal(t, source, actual, "direct/native path stays byte-identical")
				case "manual":
					require.Equal(t, 400, w)
					require.Equal(t, 600, h)
					require.Less(t, sampleLuma(img, 0.5, 0.5), 5.0, "explicit left-side crop wins")
				default:
					require.Equal(t, 3*w, 2*h)
					if mode == "center" {
						require.Equal(t, 400, w)
						require.Equal(t, 600, h)
						require.Less(t, sampleLuma(img, 0.1, 0.5), 5.0)
						require.Greater(t, sampleLuma(img, 0.9, 0.5), 250.0)
					} else {
						require.Equal(t, 1000, w)
						require.Equal(t, 1500, h)
						require.InDelta(t, 32, sampleLuma(img, 0.5, 0.1), 2)
						require.Less(t, sampleLuma(img, 0.1, 0.5), 5.0)
						require.Greater(t, sampleLuma(img, 0.9, 0.5), 250.0)
					}
				}
				files, err := afero.ReadDir(fs, "/out")
				require.NoError(t, err)
				require.Len(t, files, 1, "temporary files cleaned after install")
			})
		}
	}
}

func TestPosterFallbackFailurePreservesExisting(t *testing.T) {
	for _, mode := range []string{"center", "fit", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			raw := []byte("not an image")
			if mode == "invalid" {
				raw = twoToneSourceBytes(t)
			}
			srv, _ := identityServer(t, raw)
			fs := afero.NewMemMapFs()
			d := newGeometryDownloader(fs)
			d.config.PosterFallbackMode = mode
			require.NoError(t, fs.MkdirAll("/out", 0755))
			path := "/out/IPX-535-poster.jpg"
			require.NoError(t, afero.WriteFile(fs, path, []byte("old poster"), 0600))
			result, err := d.downloadPoster(context.Background(), geometryMovie(srv.URL, nil, true), "/out", nil, true, nil)
			require.Error(t, err)
			require.False(t, result.Downloaded)
			actual, err := afero.ReadFile(fs, path)
			require.NoError(t, err)
			require.Equal(t, []byte("old poster"), actual)
		})
	}
}

func TestPosterFallbackCoverOnly(t *testing.T) {
	source := twoToneSourceBytes(t)
	srv, _ := identityServer(t, source)
	for _, mode := range []string{"", "legacy", "center", "fit"} {
		for _, manual := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/manual=%t", mode, manual), func(t *testing.T) {
				fs := afero.NewMemMapFs()
				d := newGeometryDownloader(fs)
				d.config.PosterFallbackMode = mode
				movie := geometryMovie("", nil, false)
				movie.Poster.CoverURL = srv.URL
				if manual {
					movie.Poster.PosterCropBounds = &models.CropBounds{X: 0, Y: 0, Width: 0.4, Height: 1, SourceAspect: 1000.0 / 600.0, SourceFingerprint: assetidentity.FromBytes(source).Fingerprint}
					movie.Poster.PosterCropSourceFull = true
				}
				result, err := d.downloadPoster(context.Background(), movie, "/out", nil)
				require.NoError(t, err)
				require.True(t, result.Downloaded)
				img, w, h := decodeResultPoster(t, fs, result.LocalPath)
				if manual {
					require.Equal(t, 400, w)
					require.Equal(t, 600, h)
					require.Less(t, sampleLuma(img, 0.5, 0.5), 5.0)
				} else if mode == "" || mode == "legacy" {
					actual, err := afero.ReadFile(fs, result.LocalPath)
					require.NoError(t, err)
					require.Equal(t, source, actual)
				} else {
					require.Equal(t, 3*w, 2*h)
					if mode == "center" {
						require.Equal(t, 400, w)
					} else {
						require.Equal(t, 1000, w)
						require.InDelta(t, 32, sampleLuma(img, 0.5, 0.1), 2)
					}
				}
				require.Empty(t, movie.Poster.PosterURL, "fallback must not rewrite source metadata")
				require.False(t, movie.Poster.ShouldCropPoster)
			})
		}
	}
}
