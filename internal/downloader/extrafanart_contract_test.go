package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDownload_ExtrafanartNilOverrideRespectsConfig is the regression test for
// issue #79: `javinizer sort` (without --extrafanart) was silently skipping
// extrafanart downloads even with download_extrafanart: true in the config.
//
// Root cause: CLIApplyOptions.ToApplyPhaseConfig wrapped the unset --extrafanart
// flag (false) into a non-nil *bool, and the downloader treats any non-nil
// override as authoritative — so &false disabled extrafanart despite the config
// default. The contract is "nil = use config default"; the sort path violated it.
//
// This test pins the contract at the Download() seam: with config true and a
// nil override (what sort produces post-fix), extrafanart downloads.
func TestDownload_ExtrafanartNilOverrideRespectsConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("screenshot"))
	}))
	defer server.Close()

	movie := createTestMovie()
	movie.Screenshots = []string{
		server.URL + "/1.jpg",
		server.URL + "/2.jpg",
	}

	cfg := &Config{
		DownloadExtrafanart: true,
		DownloadCover:       false,
		DownloadPoster:      false,
		MediaFormatConfig: organizer.MediaFormatConfig{
			ScreenshotFolder:  "extrafanart",
			ScreenshotFormat:  "fanart<INDEX:2>.jpg",
			ScreenshotPadding: 2,
		},
	}
	fs := afero.NewMemMapFs()
	dl := NewDownloader(http.DefaultClient, fs, cfg, nil)

	outcome, err := dl.Download(context.Background(), DownloadCmd{
		Movie:               movie,
		DestDir:             "/movie",
		DownloadExtrafanart: nil, // sort/update with no --extrafanart flag
	})
	require.NoError(t, err)
	require.NotNil(t, outcome)

	var extrafanartPaths []string
	for _, r := range outcome.Results {
		if r.Type == MediaTypeExtrafanart && r.Downloaded {
			extrafanartPaths = append(extrafanartPaths, r.LocalPath)
		}
	}
	assert.Len(t, extrafanartPaths, 2, "extrafanart must download when config is true and the override is nil")
	for _, p := range extrafanartPaths {
		exists, _ := afero.Exists(fs, p)
		assert.True(t, exists, "extrafanart file should exist on disk: %s", p)
	}
}

// TestDownload_ExtrafanartExplicitFalseOverridesConfig documents the other half
// of the *bool contract: a non-nil &false is an explicit disable and wins over
// the config default. This is the intentional escape hatch (e.g. a future
// --no-extrafanart flag), not the unset-flag path.
func TestDownload_ExtrafanartExplicitFalseOverridesConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("screenshot"))
	}))
	defer server.Close()

	movie := createTestMovie()
	movie.Screenshots = []string{server.URL + "/1.jpg"}

	cfg := &Config{
		DownloadExtrafanart: true,
		DownloadCover:       false,
		DownloadPoster:      false,
		MediaFormatConfig: organizer.MediaFormatConfig{
			ScreenshotFolder:  "extrafanart",
			ScreenshotFormat:  "fanart<INDEX:2>.jpg",
			ScreenshotPadding: 2,
		},
	}
	dl := NewDownloader(http.DefaultClient, afero.NewMemMapFs(), cfg, nil)

	falseVal := false
	outcome, err := dl.Download(context.Background(), DownloadCmd{
		Movie:               movie,
		DestDir:             "/movie",
		DownloadExtrafanart: &falseVal, // explicit disable
	})
	require.NoError(t, err)
	require.NotNil(t, outcome)

	for _, r := range outcome.Results {
		assert.NotEqual(t, MediaTypeExtrafanart, r.Type, "explicit &false must suppress extrafanart")
	}
}
