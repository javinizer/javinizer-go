package dmm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dmmSearchSuccessRoundTripper struct {
	responses map[string]struct {
		status int
		body   string
	}
	requested []string
}

func (rt *dmmSearchSuccessRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	rt.requested = append(rt.requested, url)

	resp, ok := rt.responses[url]
	if !ok {
		resp = struct {
			status int
			body   string
		}{
			status: http.StatusNotFound,
			body:   "<html><body>not found</body></html>",
		}
	}

	return &http.Response{
		StatusCode: resp.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Request:    req,
	}, nil
}

func newDMMTestRepo(t *testing.T) *database.ContentIDMappingRepository {
	t.Helper()

	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(dbCfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})
	require.NoError(t, db.AutoMigrate())

	return database.NewContentIDMappingRepository(db)
}

func TestGetURLAndSearch_SuccessWithCachedContentID(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(&models.ContentIDMapping{
		SearchID:  "IPX-535",
		ContentID: "ipx00535",
		Source:    "dmm",
	}))

	settings := config.ScraperSettings{
		Enabled:       true,
		ScrapeActress: ptrBool(true),
	}

	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, true, false), repo)

	searchPage := `<html><body>
		<a href="/digital/videoa/-/detail/=/cid=ipx00535/">IPX-535 result</a>
	</body></html>`
	detailPage := `
<!DOCTYPE html>
<html>
<body>
	<h1 id="title" class="item">IPX-535 Successful DMM Search</h1>
	<div class="mg-b20 lh4">
		<p class="mg-b20">Detailed description from search success test.</p>
	</div>
	<table>
		<tr><td>Release: 2024/02/03</td></tr>
		<tr><td>Runtime: 125 minutes</td></tr>
		<tr><td>Actress:</td><td><a href="?actress=111">Test Actress</a></td></tr>
		<tr><td>Genre:</td><td><a href="/genre/1">Drama</a><a href="/genre/2">Featured</a></td></tr>
	</table>
	<a href="?director=123">Director Name</a>
	<a href="?maker=456">Maker Name</a>
	<a href="?label=789">Label Name</a>
	<img src="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535ps.jpg" />
	<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg" /></a>
	<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-2.jpg" /></a>
</body>
</html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("ipx535"): {
				status: http.StatusOK,
				body:   searchPage,
			},
			digitalURLFor("ipx00535"): {
				status: http.StatusOK,
				body:   detailPage,
			},
		},
	}
	scraper.client.SetTransport(transport)

	foundURL, err := scraper.GetURL("IPX-535")
	require.NoError(t, err)
	assert.Equal(t, digitalURLFor("ipx00535"), foundURL)

	result, err := scraper.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "ipx00535", result.ContentID)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "IPX-535 Successful DMM Search", result.Title)
	assert.Equal(t, "Detailed description from search success test.", result.Description)
	assert.Equal(t, 125, result.Runtime)
	assert.Equal(t, "Director Name", result.Director)
	assert.Equal(t, "Maker Name", result.Maker)
	assert.Equal(t, "Label Name", result.Label)
	assert.Equal(t, "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg", result.CoverURL)
	assert.Len(t, result.ScreenshotURL, 2)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 1)
	require.NotNil(t, result.ReleaseDate)
	assert.Equal(t, "2024-02-03", result.ReleaseDate.Format("2006-01-02"))
}

func TestSearch_ReturnsStatusErrorForDetailPage(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(&models.ContentIDMapping{
		SearchID:  "IPX-777",
		ContentID: "ipx00777",
		Source:    "dmm",
	}))

	settings := config.ScraperSettings{
		Enabled: true,
	}

	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), repo)

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("ipx777"): {
				status: http.StatusOK,
				body:   `<html><body><a href="/digital/videoa/-/detail/=/cid=ipx00777/">IPX-777 result</a></body></html>`,
			},
			digitalURLFor("ipx00777"): {
				status: http.StatusBadGateway,
				body:   "bad gateway",
			},
		},
	}
	scraper.client.SetTransport(transport)

	result, err := scraper.Search(context.Background(), "IPX-777")
	require.Error(t, err)
	assert.Nil(t, result)

	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadGateway, scraperErr.StatusCode)
}

func TestGetURL_PrefersSearchResultOverDirectURL(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(&models.ContentIDMapping{
		SearchID:  "MDB-087",
		ContentID: "61mdb087",
		Source:    "dmm",
	}))

	settings := config.ScraperSettings{
		Enabled: true,
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), repo)

	searchPage := `<html><body>
		<a href="/monthly/standard/-/detail/=/cid=61mdb087/">Low priority monthly result</a>
	</body></html>`
	monthlyPage := `
<!DOCTYPE html>
<html>
<body>
	<h1 id="title" class="item">MDB-087 Monthly Result</h1>
	<div class="mg-b20 lh4"><p class="mg-b20">Found through search.</p></div>
	<table>
		<tr><td>Release: 2024/04/05</td></tr>
		<tr><td>Runtime: 140 minutes</td></tr>
	</table>
	<a href="?maker=456">Monthly Maker</a>
</body>
</html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("mdb087"): {
				status: http.StatusOK,
				body:   searchPage,
			},
			searchURLFor("61mdb087"): {
				status: http.StatusOK,
				body:   searchPage,
			},
			"https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/": {
				status: http.StatusOK,
				body:   monthlyPage,
			},
		},
	}
	scraper.client.SetTransport(transport)

	foundURL, err := scraper.GetURL("MDB-087")
	require.NoError(t, err)
	assert.Equal(t, "https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/", foundURL)

	result, err := scraper.Search(context.Background(), "MDB-087")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "MDB-087 Monthly Result", result.Title)
	assert.Equal(t, "Monthly Maker", result.Maker)
	assert.Equal(t, "Found through search.", result.Description)
}

func searchURLFor(query string) string {
	return "https://www.dmm.co.jp/search/=/searchstr=" + query + "/"
}

func digitalURLFor(contentID string) string {
	return "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=" + contentID + "/"
}

func physicalURLFor(contentID string) string {
	return "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=" + contentID + "/"
}

func rentalURLFor(contentID string) string {
	return "https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=" + contentID + "/"
}

func TestTryDirectURLs_RentalOnly(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(&models.ContentIDMapping{
		SearchID:  "FSDSS-879",
		ContentID: "fsdss879",
		Source:    "dmm",
	}))

	settings := config.ScraperSettings{Enabled: true}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), repo)

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("fsdss879"):   {status: http.StatusOK, body: "<html><body>no results</body></html>"},
			searchURLFor("fsdss-879"):  {status: http.StatusOK, body: "<html><body>no results</body></html>"},
			physicalURLFor("fsdss879"): {status: http.StatusNotFound, body: ""},
			digitalURLFor("fsdss879"):  {status: http.StatusNotFound, body: ""},
			rentalURLFor("1fsdss879r"): {status: http.StatusOK, body: "<html><body>rental page</body></html>"},
		},
	}
	scraper.client.SetTransport(transport)

	foundURL, err := scraper.GetURL("FSDSS-879")
	require.NoError(t, err)
	assert.Equal(t, rentalURLFor("1fsdss879r"), foundURL)
}

func TestTryDirectURLs_NonRentalPriority(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(&models.ContentIDMapping{
		SearchID:  "IPX-535",
		ContentID: "ipx00535",
		Source:    "dmm",
	}))

	settings := config.ScraperSettings{Enabled: true}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), repo)

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("ipx535"):     {status: http.StatusOK, body: "<html><body>no results</body></html>"},
			searchURLFor("ipx-535"):    {status: http.StatusOK, body: "<html><body>no results</body></html>"},
			searchURLFor("ipx00535"):   {status: http.StatusOK, body: "<html><body>no results</body></html>"},
			physicalURLFor("ipx00535"): {status: http.StatusNotFound, body: ""},
			digitalURLFor("ipx00535"):  {status: http.StatusOK, body: "<html><body>digital page</body></html>"},
			rentalURLFor("1ipx00535r"): {status: http.StatusOK, body: "<html><body>rental page</body></html>"},
		},
	}
	scraper.client.SetTransport(transport)

	foundURL, err := scraper.GetURL("IPX-535")
	require.NoError(t, err)
	assert.Equal(t, digitalURLFor("ipx00535"), foundURL)
}
