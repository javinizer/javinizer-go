package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigureLocalScraper(t *testing.T) {
	for _, withMedia := range []bool{false, true} {
		name := "metadata only"
		if withMedia {
			name = "generated artwork"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			fixture := ConfigureLocalScraper(t, cfg, withMedia)
			assert.Equal(t, []string{"libredmm"}, cfg.Scrapers.Priority)
			assert.DirExists(t, cfg.System.TempDir)
			assert.False(t, cfg.Metadata.R18DevDump.Enabled)
			assert.False(t, cfg.Metadata.Translation.Enabled)
			reg := scraperutil.NewScraperRegistry()
			scraper.RegisterAll(reg)
			for name := range reg.GetAllDefaults() {
				assert.Equal(t, name == "libredmm", cfg.Scrapers.Override(name).Enabled, name)
			}

			baseURL := cfg.Scrapers.Override("libredmm").BaseURL
			resp, err := http.Get(baseURL + "/search?format=json&q=TEST-001")
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			var metadata map[string]any
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&metadata))
			assert.Equal(t, "TEST-001", metadata["normalized_id"])
			assert.Equal(t, "Synthetic fixture TEST-001", metadata["title"])
			assert.NotContains(t, metadata, "cover_image_url")
			assert.NotContains(t, metadata, "thumbnail_image_url")
			assert.Equal(t, []string{"TEST-001"}, fixture.SearchIDs())
			if !withMedia {
				assert.NotContains(t, metadata, "sample_image_urls")
				assert.Zero(t, fixture.ImageRequests())
				return
			}
			assert.Equal(t, []any{baseURL + "/synthetic.jpg"}, metadata["sample_image_urls"])
			head, err := http.Head(baseURL + "/synthetic.jpg")
			require.NoError(t, err)
			require.NoError(t, head.Body.Close())
			assert.EqualValues(t, len(fixture.ImageData), head.ContentLength)
			assert.Zero(t, fixture.ImageRequests(), "HEAD is not an artwork download")
			artwork, err := http.Get(baseURL + "/synthetic.jpg")
			require.NoError(t, err)
			defer artwork.Body.Close()
			data, err := io.ReadAll(artwork.Body)
			require.NoError(t, err)
			assert.Equal(t, fixture.ImageData, data)
			assert.Equal(t, 1, fixture.ImageRequests())
		})
	}
}

type fixtureFailureRecorder struct {
	messages []string
}

func (r *fixtureFailureRecorder) Errorf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func TestLocalScraperFixture_RejectsUnexpectedRoute(t *testing.T) {
	fixture := &LocalScraperFixture{}
	reporter := &fixtureFailureRecorder{}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/unexpected?request=fixture", nil)

	fixture.serveHTTP(reporter, false, "http://127.0.0.1", response, request)

	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.Equal(t, []string{"unexpected local scraper request: /unexpected?request=fixture"}, reporter.messages,
		"unexpected requests must fail the owning test as well as returning 404")
	assert.Empty(t, fixture.SearchIDs())
	assert.Zero(t, fixture.ImageRequests())
}
