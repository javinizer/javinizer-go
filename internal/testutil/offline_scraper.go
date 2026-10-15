package testutil

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LocalScraperFixture serves synthetic metadata and optional generated artwork.
// Calls are recorded so command tests can prove they reached the scrape/download
// phases instead of passing after an empty scan or a swallowed scrape error.
type LocalScraperFixture struct {
	ImageData []byte
	mu        sync.Mutex
	searches  []string
	images    int
}

// SearchIDs returns a snapshot of the movie IDs requested from the fixture.
func (f *LocalScraperFixture) SearchIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.searches...)
}

// ImageRequests returns the number of artwork requests received by the fixture.
func (f *LocalScraperFixture) ImageRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.images
}

// ConfigureLocalScraper enables only LibreDMM against a numeric-loopback JSON
// fixture. It keeps real bootstrap/scan/scrape/apply code and all production
// defaults unchanged. No cover/poster URLs are emitted: poster generation can
// otherwise fetch artwork even when the apply-phase download flags are false.
// Tests using this helper must not call t.Parallel because config environment
// overrides and the per-user artifact sealing key are isolated with t.Setenv.
func ConfigureLocalScraper(t *testing.T, cfg *config.Config, withMedia bool) *LocalScraperFixture {
	t.Helper()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "false")
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_TEMP_DIR", "")
	t.Setenv("JAVINIZER_R18DEV_DUMP_PATH", "")
	t.Setenv("JAVINIZER_HOME", t.TempDir())

	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, jpeg.Encode(&encoded, img, nil))
	fixture := &LocalScraperFixture{ImageData: encoded.Bytes()}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.serveHTTP(t, withMedia, server.URL, w, r)
	}))
	t.Cleanup(server.Close)

	cfg.System.TempDir = t.TempDir()
	cfg.Database.LogLevel = "silent"
	cfg.Metadata.R18DevDump.Enabled = false
	cfg.Metadata.Translation.Enabled = false
	cfg.Matching.MinSizeMB = 0
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = []string{}
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = withMedia
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	reg := scraperutil.NewScraperRegistry()
	scraper.RegisterAll(reg)
	for name := range reg.GetAllDefaults() {
		settings := cfg.Scrapers.Override(name)
		settings.Enabled = name == "libredmm"
		settings.SetEnabledPresence(true)
	}
	cfg.Scrapers.Priority = []string{"libredmm"}
	cfg.Scrapers.Override("libredmm").BaseURL = server.URL
	cfg.Scrapers.Override("libredmm").RateLimit = 0
	cfg.Scrapers.ScrapeActress = false
	return fixture
}

// serveHTTP accepts an assertion reporter so fixture rejection behavior can be
// tested without deliberately failing its caller. ConfigureLocalScraper always
// supplies the real *testing.T, so unexpected requests still fail command tests.
func (f *LocalScraperFixture) serveHTTP(t assert.TestingT, withMedia bool, baseURL string, w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/search":
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "json", r.URL.Query().Get("format"))
		id := r.URL.Query().Get("q")
		assert.NotEmpty(t, id)
		f.mu.Lock()
		f.searches = append(f.searches, id)
		f.mu.Unlock()
		payload := map[string]any{
			"normalized_id": id,
			"title":         "Synthetic fixture " + id,
			"description":   "Synthetic fixture plot",
			"makers":        []string{"Synthetic fixture studio"},
		}
		if withMedia {
			payload["sample_image_urls"] = []string{baseURL + "/synthetic.jpg"}
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(payload))
	case "/synthetic.jpg":
		assert.True(t, withMedia, "unexpected media fetch for metadata-only fixture")
		assert.Contains(t, []string{http.MethodGet, http.MethodHead}, r.Method)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(f.ImageData)))
		if r.Method == http.MethodHead {
			return
		}
		f.mu.Lock()
		f.images++
		f.mu.Unlock()
		_, err := w.Write(f.ImageData)
		assert.NoError(t, err)
	default:
		t.Errorf("unexpected local scraper request: %s", r.URL)
		http.NotFound(w, r)
	}
}
