package libredmm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig(baseURL string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Scrapers.LibreDMM.Enabled = true
	cfg.Scrapers.LibreDMM.RequestDelay = 0
	cfg.Scrapers.LibreDMM.BaseURL = baseURL
	cfg.Scrapers.Proxy.Enabled = false
	return cfg
}

func TestScraperInterfaceCompliance(t *testing.T) {
	cfg := testConfig("https://www.libredmm.com")
	scraper := New(cfg)

	var _ models.Scraper = scraper
	var _ models.ScraperQueryResolver = scraper
}

func TestNameAndEnabled(t *testing.T) {
	cfg := testConfig("https://www.libredmm.com")
	scraper := New(cfg)

	assert.Equal(t, "libredmm", scraper.Name())
	assert.True(t, scraper.IsEnabled())
}

func TestGetURL(t *testing.T) {
	cfg := testConfig("https://www.libredmm.com")
	scraper := New(cfg)

	url, err := scraper.GetURL("IPX535")
	require.NoError(t, err)
	assert.Equal(t, "https://www.libredmm.com/search?q=IPX535&format=json", url)

	url, err = scraper.GetURL("https://www.libredmm.com/movies/IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "https://www.libredmm.com/movies/IPX-535.json", url)
}

func TestSearchSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			assert.Equal(t, "IPX535", r.URL.Query().Get("q"))
			assert.Equal(t, "json", r.URL.Query().Get("format"))
			http.Redirect(w, r, "/movies/IPX-535.json", http.StatusFound)
		case "/movies/IPX-535.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{
				"actresses": [{"name": "Momo Sakura", "image_url": "http://images.example.com/momo.jpg"}],
				"cover_image_url": "https://images.example.com/cover.jpg",
				"date": "2020-09-14T03:00:00.000-07:00",
				"description": "Test description",
				"directors": ["Director A"],
				"genres": ["Genre A", "Genre B"],
				"labels": ["Label A"],
				"makers": ["Maker A"],
				"normalized_id": "IPX-535",
				"review": 8.2,
				"subtitle": "k9ipx535",
				"thumbnail_image_url": "https://images.example.com/poster.jpg",
				"title": "Movie Title",
				"url": "https://www.libredmm.com/movies/IPX-535",
				"volume": 7200,
				"sample_image_urls": ["https://images.example.com/1.jpg", "https://images.example.com/2.jpg"]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	scraper := New(cfg)

	result, err := scraper.Search("IPX535")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "libredmm", result.Source)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "k9ipx535", result.ContentID)
	assert.Equal(t, "Movie Title", result.Title)
	assert.Equal(t, "Movie Title", result.OriginalTitle)
	assert.Equal(t, "Test description", result.Description)
	assert.Equal(t, 120, result.Runtime)
	assert.Equal(t, "Director A", result.Director)
	assert.Equal(t, "Maker A", result.Maker)
	assert.Equal(t, "Label A", result.Label)
	assert.Equal(t, "https://images.example.com/cover.jpg", result.CoverURL)
	assert.Equal(t, "https://images.example.com/cover.jpg", result.PosterURL)
	assert.False(t, result.ShouldCropPoster)
	assert.Equal(t, []string{"Genre A", "Genre B"}, result.Genres)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "Momo", result.Actresses[0].FirstName)
	assert.Equal(t, "Sakura", result.Actresses[0].LastName)
	assert.Equal(t, "https://images.example.com/momo.jpg", result.Actresses[0].ThumbURL)
	assert.Len(t, result.ScreenshotURL, 2)
	require.NotNil(t, result.ReleaseDate)
	require.NotNil(t, result.Rating)
	assert.Equal(t, 8.2, result.Rating.Score)
}

func TestSearchNormalizesDMMScreenshotURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			http.Redirect(w, r, "/movies/ABP-880.json", http.StatusFound)
		case "/movies/ABP-880.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{
				"cover_image_url": "https://images.example.com/cover.jpg",
				"normalized_id": "ABP-880",
				"subtitle": "abp880",
				"title": "Movie Title",
				"url": "https://www.libredmm.com/movies/ABP-880",
				"sample_image_urls": [
					"https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg?foo=bar",
					"https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880jp-2.jpg"
				]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	scraper := New(cfg)

	result, err := scraper.Search("ABP-880")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.ScreenshotURL, 2)

	assert.Equal(
		t,
		"https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		result.ScreenshotURL[0],
	)
	assert.Equal(
		t,
		"https://pics.dmm.co.jp/digital/video/118abp880/118abp880jp-2.jpg",
		result.ScreenshotURL[1],
	)
}

func TestSearchProcessingTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			http.Redirect(w, r, "/movies/ZZZ-99999.json", http.StatusFound)
		case "/movies/ZZZ-99999.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{"err":"processing"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	scraper := New(cfg)
	scraper.maxPollAttempts = 2
	scraper.pollInterval = 1 * time.Millisecond

	result, err := scraper.Search("ZZZ-99999")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "still processing")
}

func TestSearchHostUnavailable502(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			http.Redirect(w, r, "/movies/ABW-102.json", http.StatusFound)
		case "/movies/ABW-102.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprint(w, `{"err":"bad gateway"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	scraper := New(cfg)

	result, err := scraper.Search("ABW-102")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "temporarily unavailable")
	assert.Contains(t, err.Error(), "host may be down")
	assert.Contains(t, err.Error(), "502")
}

func TestResolveSearchQuery(t *testing.T) {
	cfg := testConfig("https://www.libredmm.com")
	scraper := New(cfg)

	query, ok := scraper.ResolveSearchQuery("https://www.libredmm.com/movies/IPX-535")
	assert.True(t, ok)
	assert.Equal(t, "https://www.libredmm.com/movies/IPX-535.json", query)

	query, ok = scraper.ResolveSearchQuery("IPX-535")
	assert.False(t, ok)
	assert.Empty(t, query)
}

func TestPayloadToResultPreservesCoverURL(t *testing.T) {
	const coverURL = "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880pl.jpg"
	payload := &moviePayload{
		CoverImageURL:     coverURL,
		ThumbnailImageURL: "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880ps.jpg",
		NormalizedID:      "ABP-880",
		Subtitle:          "118abp00880",
		Title:             "Movie Title",
	}

	// Force poster probe to fail immediately so payloadToResult uses cover URL fallback
	// without making a real network request.
	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network disabled")
		}),
	}

	result := payloadToResult(payload, "https://www.libredmm.com/movies/ABP-880.json", "ABP-880", client)
	require.NotNil(t, result)
	assert.Equal(t, coverURL, result.CoverURL)
	assert.Equal(t, coverURL, result.PosterURL)
}

func TestNormalizeLibredmmScreenshotURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "adds jp marker for dmm sample image",
			in:   "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg?foo=bar",
			want: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		},
		{
			name: "normalizes prefixed content id segments",
			in:   "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880jp-2.jpg",
			want: "https://pics.dmm.co.jp/digital/video/118abp880/118abp880jp-2.jpg",
		},
		{
			name: "keeps at least three digits in prefixed content id segments",
			in:   "https://pics.dmm.co.jp/digital/video/118abw00013/118abw00013jp-1.jpg",
			want: "https://pics.dmm.co.jp/digital/video/118abw013/118abw013jp-1.jpg",
		},
		{
			name: "keeps single digit prefixed content id segments",
			in:   "https://pics.dmm.co.jp/digital/video/1mgnl00134/1mgnl00134jp-1.jpg",
			want: "https://pics.dmm.co.jp/digital/video/1mgnl00134/1mgnl00134jp-1.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeLibredmmScreenshotURL(tt.in))
		})
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
