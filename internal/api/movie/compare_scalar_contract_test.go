package movie

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareNFO_ScalarDiffAndBareCodeTitleContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	nfoPath := filepath.Join(dir, "IPX-535.nfo")
	const nfoContent = `<movie><id>IPX-535</id><title>IPX-535</title><director>NFO Director</director><runtime>90</runtime><releasedate>not-a-date</releasedate></movie>`
	require.NoError(t, os.WriteFile(nfoPath, []byte(nfoContent), 0600))

	// Configure the runtime once, before registering the sole synthetic source.
	deps := createTestDeps(t, &config.Config{
		API:      config.APIConfig{Security: config.SecurityConfig{AllowedDirectories: []string{dir}}},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}, "")
	released := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)
	deps.CoreDeps.GetRegistry().RegisterInstance(&mockScraperWithResults{
		name: "r18dev", enabled: true,
		result: &models.ScraperResult{
			ID: "IPX-535", ContentID: "IPX-535", Source: "r18dev",
			Title: "Fresh source title", Director: "Scraper Director", Runtime: 120,
			ReleaseDate: &released,
		},
	})
	runtime := testkit.GetTestRuntime(deps)
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), NewMovieDeps(deps.Repos.MovieRepo,
		WithWorkflow(runtime.GetWorkflow), WithAllowedDirs(runtime.GetAPIConfig().AllowedDirectories)))

	for _, tt := range []struct {
		strategy string
		title    string
		director string
		runtime  int
		source   string
	}{
		{"prefer-nfo", "IPX-535", "NFO Director", 90, "nfo"},
		{"prefer-scraper", "Fresh source title", "Scraper Director", 120, "scraper"},
	} {
		t.Run(tt.strategy, func(t *testing.T) {
			body, err := json.Marshal(contracts.NFOComparisonRequest{
				NFOPath: nfoPath, ScalarStrategy: tt.strategy, ArrayStrategy: "replace",
			})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/movies/IPX-535/compare-nfo", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			var resp contracts.NFOComparisonResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, "IPX-535", resp.MovieID)
			assert.True(t, resp.NFOExists)
			assert.Equal(t, filepath.Base(nfoPath), resp.NFOPath)
			require.NotNil(t, resp.NFOData)
			require.NotNil(t, resp.ScrapedData)
			require.NotNil(t, resp.MergedData)

			// The comparison preserves each original input, including a bad date's
			// otherwise valid NFO fields. Parse warnings are not part of this API.
			assert.Equal(t, "IPX-535", resp.NFOData.Title)
			assert.Empty(t, resp.NFOData.DisplayTitle)
			assert.Equal(t, "NFO Director", resp.NFOData.Director)
			assert.Equal(t, 90, resp.NFOData.Runtime)
			assert.Nil(t, resp.NFOData.ReleaseDate)
			assert.Equal(t, "Fresh source title", resp.ScrapedData.Title)
			assert.Equal(t, "Scraper Director", resp.ScrapedData.Director)
			assert.Equal(t, 120, resp.ScrapedData.Runtime)
			require.NotNil(t, resp.ScrapedData.ReleaseDate)
			assert.True(t, released.Equal(*resp.ScrapedData.ReleaseDate))

			assert.Equal(t, tt.title, resp.MergedData.Title)
			assert.Empty(t, resp.MergedData.DisplayTitle, "a bare code is a base title, not a generated display title")
			assert.Equal(t, tt.director, resp.MergedData.Director)
			assert.Equal(t, tt.runtime, resp.MergedData.Runtime)
			for _, field := range []string{"title", "director", "runtime"} {
				require.Contains(t, resp.Provenance, field)
				assert.Equal(t, tt.source, resp.Provenance[field].Source, field)
			}

			differences := make(map[string]contracts.FieldDifference)
			for _, diff := range resp.Differences {
				require.NotContains(t, differences, diff.Field, "duplicate field difference")
				differences[diff.Field] = diff
			}
			require.Contains(t, differences, "director")
			assert.Equal(t, contracts.FieldDifference{
				Field: "director", NFOValue: "NFO Director", ScrapedValue: "Scraper Director", MergedValue: tt.director,
			}, differences["director"])
			require.Contains(t, differences, "runtime")
			// JSON numbers in any-valued differences decode as float64, unlike
			// the strongly typed MovieView.Runtime fields checked above.
			assert.Equal(t, contracts.FieldDifference{
				Field: "runtime", NFOValue: float64(90), ScrapedValue: float64(120), MergedValue: float64(tt.runtime),
			}, differences["runtime"])

			after, err := os.ReadFile(nfoPath)
			require.NoError(t, err)
			assert.Equal(t, nfoContent, string(after), "comparison must not rewrite the original NFO")
		})
	}
}
