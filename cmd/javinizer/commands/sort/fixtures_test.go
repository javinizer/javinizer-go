package sort_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createOfflineSortConfig(t *testing.T, customize func(*config.Config)) (string, *config.Config) {
	t.Helper()
	path, cfg, _ := createSortFixture(t, false, customize)
	return path, cfg
}

func createLocalMediaSortConfig(t *testing.T) (string, []byte) {
	t.Helper()
	path, _, imageData := createSortFixture(t, true, nil)
	return path, imageData
}

// createSortFixture exercises the real CLI bootstrap, scan, scrape, organize,
// NFO and summary paths. Only LibreDMM is enabled and its JSON endpoint is a
// loopback server. Optional artwork uses an image generated in memory on that
// same server; video fixtures contain only dummy bytes. No real media is used.
//
// Do not use the e2emock scraper here: its reserved e2e.invalid artwork URLs
// still cause DNS lookups during scrape-phase poster generation, even when
// apply-phase downloads are disabled. These fixtures instead omit poster URLs.
func createSortFixture(t *testing.T, withMedia bool, customize func(*config.Config)) (string, *config.Config, []byte) {
	t.Helper()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "false")
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_TEMP_DIR", "")
	t.Setenv("JAVINIZER_R18DEV_DUMP_PATH", "")
	// Keep artifact staging's per-user sealing key in the fixture too.
	t.Setenv("JAVINIZER_HOME", t.TempDir())

	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, jpeg.Encode(&encoded, img, nil))
	imageData := encoded.Bytes()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			assert.Equal(t, "json", r.URL.Query().Get("format"))
			id := r.URL.Query().Get("q")
			payload := map[string]any{
				"normalized_id": id,
				"title":         "Synthetic sort fixture " + id,
			}
			if withMedia {
				payload["sample_image_urls"] = []string{server.URL + "/synthetic.jpg"}
			}
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(payload))
		case "/synthetic.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, err := w.Write(imageData)
			assert.NoError(t, err)
		default:
			t.Errorf("unexpected fixture request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	tempDir := t.TempDir()
	path, cfg := testutil.CreateTestConfig(t, func(c *config.Config) {
		c.Database.LogLevel = "silent"
		c.System.TempDir = tempDir
		c.Metadata.R18DevDump.Enabled = false
		c.Metadata.Translation.Enabled = false
		c.Matching.MinSizeMB = 0
		c.Output.Template.FolderFormat = "<ID>"
		c.Output.Template.SubfolderFormat = []string{}
		c.Output.Template.FileFormat = "<ID>"
		c.Output.Download.DownloadCover = false
		c.Output.Download.DownloadPoster = false
		c.Output.Download.DownloadExtrafanart = false
		c.Output.Download.DownloadTrailer = false
		c.Output.Download.DownloadActress = false
		reg := scraperutil.NewScraperRegistry()
		scraper.RegisterAll(reg)
		for name := range reg.GetAllDefaults() {
			settings := c.Scrapers.Override(name)
			settings.Enabled = name == "libredmm"
			settings.SetEnabledPresence(true)
		}
		c.Scrapers.Priority = []string{"libredmm"}
		c.Scrapers.Override("libredmm").BaseURL = server.URL
		c.Scrapers.ScrapeActress = false
		if customize != nil {
			customize(c)
		}
	})
	return path, cfg, imageData
}

func assertSortCompleted(t *testing.T, output, dest, id string) {
	t.Helper()
	assert.Contains(t, output, "Metadata found: 1")
	assert.Contains(t, output, "Files organized: 1")
	assert.Contains(t, output, "Sort complete!")
	assert.FileExists(t, filepath.Join(dest, id, id+".mp4"))
}

func assertSyntheticVideo(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "dummy video content", string(data))
}
