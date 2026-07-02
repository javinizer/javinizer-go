package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// missRoundTripper routes requests to the test server regardless of the URL host.
type missRoundTripper struct {
	server *httptest.Server
}

func (rt *missRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyReq := req.Clone(req.Context())
	proxyReq.URL.Scheme = "http"
	proxyReq.URL.Host = rt.server.Listener.Addr().String()
	proxyReq.Host = rt.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(proxyReq)
}

// newR18TestScraper creates a scraper wired to a httptest.Server
func newR18TestScraper(server *httptest.Server, enabled bool, language string) *scraper {
	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	return &scraper{
		client:            client,
		enabled:           enabled,
		language:          language,
		rateLimiter:       ratelimit.NewLimiter(0),
		maxRetries:        0,
		respectRetryAfter: false,
		settings:          models.ScraperSettings{Enabled: enabled, Language: language},
	}
}

// --- ScrapeURL tests ---

func TestScrapeURL_Disabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request when disabled")
	}))
	defer server.Close()

	s := newR18TestScraper(server, false, "en")
	_, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined=ipx535/json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestScrapeURL_WrongHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request for non-R18 URL")
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.ScrapeURL(context.Background(), "https://example.com/something")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestScrapeURL_CannotExtractID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request")
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.ScrapeURL(context.Background(), "https://r18.dev/no-id-here")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extract ID")
}

func TestScrapeURL_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/id=ipx535")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, scraperErr.StatusCode)
}

func TestScrapeURL_ReturnsHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>Not found</body></html>"))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/id=ipx535")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestScrapeURL_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/id=ipx535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse R18.dev response")
}

func TestScrapeURL_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"dvd_id": "IPX-535",
			"content_id": "1ipx00535",
			"title_ja": "テストタイトル",
			"title_en": "Test Title",
			"release_date": "2020-08-13",
			"runtime_mins": 120,
			"maker_name_en": "Test Maker",
			"label_name_en": "Test Label",
			"series_name_en": "Test Series",
			"categories": [{"name": "Drama"}],
			"actresses": []
		}`))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/id=ipx535")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "Test Title", result.Title)
	assert.Equal(t, "Test Maker", result.Maker)
}

// --- Search with httptest ---

func TestSearch_WithTestServer_DVDLookupReturnsContentID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		if strings.Contains(path, "dvd_id=") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "1ipx00535",
				"dvd_id": "ipx535"
			}`))
			return
		}

		if strings.Contains(path, "combined=") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "IPX-535",
				"content_id": "1ipx00535",
				"title_ja": "検索テスト",
				"title_en": "Search Test",
				"release_date": "2020-08-13",
				"runtime_mins": 90,
				"maker_name_en": "Test Maker",
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "Search Test", result.Title)
}

func TestSearch_WithTestServer_NoContentIDFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		if strings.Contains(path, "dvd_id=") {
			if strings.Contains(path, "dvd_id=ipx535") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html>not found</html>"))
			return
		}

		if strings.Contains(path, "combined=ipx535") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "IPX-535",
				"content_id": "ipx00535",
				"title_en": "Fallback Result",
				"release_date": "2021-01-01",
				"runtime_mins": 60,
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "Fallback Result", result.Title)
}

func TestSearch_WithTestServer_AlternateContentIDs(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		if strings.Contains(path, "dvd_id=") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if strings.Contains(path, "combined=abw001") && callCount <= 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if strings.Contains(path, "combined=") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "ABW-001",
				"content_id": "118abw00001",
				"title_en": "Alternate Match",
				"release_date": "2021-06-15",
				"runtime_mins": 80,
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "ABW-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABW-001", result.ID)
}

func TestSearch_WithTestServer_AllFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.Search(context.Background(), "NOTFOUND-999")
	require.Error(t, err)
}

func TestSearch_WithTestServer_NilResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "dvd_id=") {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html></html>"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.Search(context.Background(), "IPX-999")
	require.Error(t, err)
}

// --- doRequestWithRetryCtx tests ---

func TestDoRequestWithRetryCtx_RateLimited429(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"dvd_id": "TEST-001"}`))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:            client,
		maxRetries:        3,
		respectRetryAfter: true,
		rateLimiter:       ratelimit.NewLimiter(0),
	}

	resp, err := s.doRequestWithRetryCtx(context.Background(), "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.True(t, attempt >= 3, "should retry on 429")
}

func TestDoRequestWithRetryCtx_RateLimited503(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt <= 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"dvd_id": "TEST-002"}`))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:            client,
		maxRetries:        2,
		respectRetryAfter: false,
		rateLimiter:       ratelimit.NewLimiter(0),
	}

	resp, err := s.doRequestWithRetryCtx(context.Background(), "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
}

func TestDoRequestWithRetryCtx_MaxRetriesExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:            client,
		maxRetries:        1,
		respectRetryAfter: false,
		rateLimiter:       ratelimit.NewLimiter(0),
	}

	_, err := s.doRequestWithRetryCtx(context.Background(), "https://r18.dev/test")
	require.Error(t, err)
	// The error may be wrapped; just verify it contains rate-limit context
	assert.Contains(t, err.Error(), "rate limited")
}

func TestDoRequestWithRetryCtx_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &scraper{
		client:            client,
		maxRetries:        2,
		respectRetryAfter: false,
		rateLimiter:       ratelimit.NewLimiter(0),
	}

	_, err := s.doRequestWithRetryCtx(ctx, "https://r18.dev/test")
	require.Error(t, err)
}

// --- parseResponse edge cases ---

func TestParseResponse_JapaneseLanguage(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "ja")

	data := &r18Response{
		DVDID:         "IPX-535",
		ContentID:     "1ipx00535",
		TitleJA:       "日本語タイトル",
		TitleEn:       "English Title",
		Description:   "日本語説明",
		DescriptionEn: "English Description",
		ReleaseDate:   "2020-08-13",
		Runtime:       120,
		MakerNameEn:   "English Maker",
		MakerNameJa:   "日本語メーカー",
		LabelNameEn:   "English Label",
		LabelNameJa:   "日本語レーベル",
		SeriesNameEn:  "English Series",
		SeriesNameJa:  "日本語シリーズ",
		Director:      "日本語監督",
		DirectorEn:    "English Director",
		Categories: []struct {
			ID                         int    `json:"id"`
			Name                       string `json:"name"`
			NameEn                     string `json:"name_en"`
			NameJa                     string `json:"name_ja"`
			NameEnIsMachineTranslation bool   `json:"name_en_is_machine_translation"`
		}{
			{Name: "Genre", NameEn: "English Genre", NameJa: "日本語ジャンル"},
		},
		Actresses: []struct {
			ID         int    `json:"id"`
			ImageURL   string `json:"image_url"`
			NameKana   string `json:"name_kana"`
			NameKanji  string `json:"name_kanji"`
			NameRomaji string `json:"name_romaji"`
		}{
			{ID: 1, NameKanji: "女優名", NameRomaji: "Yūjo Mei"},
		},
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, "日本語タイトル", result.Title)
	assert.Equal(t, "日本語メーカー", result.Maker)
	assert.Equal(t, "日本語レーベル", result.Label)
	assert.Equal(t, "日本語監督", result.Director)
	assert.Equal(t, "日本語ジャンル", result.Genres[0])
}

func TestParseResponse_LegacyFormat(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:       "IPX-535",
		ContentID:   "1ipx00535",
		TitleJA:     "日本語タイトル",
		TitleEn:     "English Title",
		ReleaseDate: "2020-08-13",
		Runtime:     120,
		Maker: struct {
			Name string `json:"name"`
		}{Name: "Legacy Maker"},
		Label: struct {
			Name string `json:"name"`
		}{Name: "Legacy Label"},
		Series: struct {
			Name string `json:"name"`
		}{Name: "Legacy Series"},
		Director: "Legacy Director",
		Categories: []struct {
			ID                         int    `json:"id"`
			Name                       string `json:"name"`
			NameEn                     string `json:"name_en"`
			NameJa                     string `json:"name_ja"`
			NameEnIsMachineTranslation bool   `json:"name_en_is_machine_translation"`
		}{
			{Name: "Legacy Genre"},
		},
		Actresses: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, "Legacy Maker", result.Maker)
	assert.Equal(t, "Legacy Label", result.Label)
	assert.Equal(t, "Legacy Director", result.Director)
	assert.Equal(t, "Legacy Genre", result.Genres[0])
}

func TestParseResponse_DirectorsArray(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:       "IPX-535",
		ContentID:   "1ipx00535",
		TitleJA:     "テスト",
		TitleEn:     "Test",
		ReleaseDate: "2020-08-13",
		Runtime:     120,
		Directors: []struct {
			ID         int    `json:"id"`
			NameKana   string `json:"name_kana"`
			NameKanji  string `json:"name_kanji"`
			NameRomaji string `json:"name_romaji"`
		}{
			{NameRomaji: "Director Romaji", NameKanji: "監督漢字"},
		},
		Actresses:  nil,
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, "Director Romaji", result.Director)
}

func TestParseResponse_CoverAndScreenshots(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:         "IPX-535",
		ContentID:     "1ipx00535",
		TitleJA:       "テスト",
		TitleEn:       "Test",
		ReleaseDate:   "2020-08-13",
		Runtime:       120,
		JacketFullURL: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
		Actresses:     nil,
		Categories:    nil,
		Gallery: []struct {
			ImageFull  string `json:"image_full"`
			ImageThumb string `json:"image_thumb"`
		}{
			{ImageFull: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg"},
			{ImageFull: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-2.jpg"},
		},
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Contains(t, result.CoverURL, "ipx00535pl.jpg")
	assert.Len(t, result.ScreenshotURL, 2)
}

func TestParseResponse_LegacyCoverAndScreenshots(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:       "IPX-535",
		ContentID:   "1ipx00535",
		TitleJA:     "テスト",
		TitleEn:     "Test",
		ReleaseDate: "2020-08-13",
		Runtime:     120,
		Images: struct {
			JacketImage struct {
				Large  string `json:"large"`
				Large2 string `json:"large2"`
			} `json:"jacket_image"`
			SampleImages []string `json:"sample_images"`
		}{
			JacketImage: struct {
				Large  string `json:"large"`
				Large2 string `json:"large2"`
			}{
				Large2: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
			},
			SampleImages: []string{
				"https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
			},
		},
		Actresses:  nil,
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Contains(t, result.CoverURL, "ipx00535pl.jpg")
	assert.Len(t, result.ScreenshotURL, 1)
}

func TestParseResponse_TrailerSampleURL(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:       "IPX-535",
		ContentID:   "1ipx00535",
		TitleJA:     "テスト",
		TitleEn:     "Test",
		ReleaseDate: "2020-08-13",
		Runtime:     120,
		SampleURL:   "https://cc3001.dmm.co.jp/litevideo/freepv/i/ipx/ipx00535/ipx00535_mhb_w.mp4",
		Actresses:   nil,
		Categories:  nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, "https://cc3001.dmm.co.jp/litevideo/freepv/i/ipx/ipx00535/ipx00535_mhb_w.mp4", result.TrailerURL)
}

func TestParseResponse_LegacyTrailer(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:       "IPX-535",
		ContentID:   "1ipx00535",
		TitleJA:     "テスト",
		TitleEn:     "Test",
		ReleaseDate: "2020-08-13",
		Runtime:     120,
		Sample: struct {
			High string `json:"high"`
			Low  string `json:"low"`
		}{
			High: "https://example.com/trailer_high.mp4",
			Low:  "https://example.com/trailer_low.mp4",
		},
		Actresses:  nil,
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/trailer_high.mp4", result.TrailerURL)
}

func TestParseResponse_ActressWithRomajiName(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:       "IPX-535",
		ContentID:   "1ipx00535",
		TitleJA:     "テスト",
		TitleEn:     "Test",
		ReleaseDate: "2020-08-13",
		Runtime:     120,
		Actresses: []struct {
			ID         int    `json:"id"`
			ImageURL   string `json:"image_url"`
			NameKana   string `json:"name_kana"`
			NameKanji  string `json:"name_kanji"`
			NameRomaji string `json:"name_romaji"`
		}{
			{ID: 1, NameRomaji: "Momo Sakura", NameKanji: "桜もも", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/sakura_momo.jpg"},
		},
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	require.Len(t, result.Actresses, 1)
	assert.Equal(t, "Momo", result.Actresses[0].FirstName)
	assert.Equal(t, "Sakura", result.Actresses[0].LastName)
	assert.Equal(t, "桜もも", result.Actresses[0].JapaneseName)
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/sakura_momo.jpg", result.Actresses[0].ThumbURL)
}

func TestParseResponse_ActressThumbConstructedFromRomaji(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:       "IPX-535",
		ContentID:   "1ipx00535",
		TitleJA:     "テスト",
		TitleEn:     "Test",
		ReleaseDate: "2020-08-13",
		Runtime:     120,
		Actresses: []struct {
			ID         int    `json:"id"`
			ImageURL   string `json:"image_url"`
			NameKana   string `json:"name_kana"`
			NameKanji  string `json:"name_kanji"`
			NameRomaji string `json:"name_romaji"`
		}{
			{ID: 2, NameRomaji: "Yui Hatano", NameKanji: "波多野結衣", ImageURL: ""},
		},
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	require.Len(t, result.Actresses, 1)
	assert.Contains(t, result.Actresses[0].ThumbURL, "hatano_yui.jpg")
}

func TestParseResponse_ContentIDOnly_NoDVDID(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		ContentID:   "118abw00001",
		TitleJA:     "テスト",
		TitleEn:     "Test",
		ReleaseDate: "2021-01-01",
		Runtime:     60,
		Actresses:   nil,
		Categories:  nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, "ABW-001", result.ID)
}

func TestParseResponse_Translations(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:         "IPX-535",
		ContentID:     "1ipx00535",
		TitleJA:       "日本語タイトル",
		TitleEn:       "English Title",
		Description:   "日本語説明",
		DescriptionEn: "English Description",
		ReleaseDate:   "2020-08-13",
		Runtime:       120,
		MakerNameEn:   "English Maker",
		MakerNameJa:   "日本語メーカー",
		LabelNameEn:   "English Label",
		LabelNameJa:   "日本語レーベル",
		SeriesNameEn:  "English Series",
		SeriesNameJa:  "日本語シリーズ",
		Actresses:     nil,
		Categories:    nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	require.Len(t, result.Translations, 2)

	enTrans := result.Translations[0]
	assert.Equal(t, "en", enTrans.Language)
	assert.Equal(t, "English Title", enTrans.Title)

	jaTrans := result.Translations[1]
	assert.Equal(t, "ja", jaTrans.Language)
	assert.Equal(t, "日本語タイトル", jaTrans.Title)
}

func TestParseResponse_InvalidReleaseDate(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:       "IPX-535",
		ContentID:   "1ipx00535",
		TitleJA:     "テスト",
		TitleEn:     "Test",
		ReleaseDate: "not-a-date",
		Runtime:     120,
		Actresses:   nil,
		Categories:  nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Nil(t, result.ReleaseDate)
}

// --- CanHandleURL edge case ---

func TestCanHandleURL_InvalidURL(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")
	assert.False(t, s.CanHandleURL("://not-a-valid-url"))
}

// --- contentIDCoreMatch edge cases ---

func TestContentIDMatchesExpected_MatchingIDs(t *testing.T) {
	assert.True(t, contentIDCoreMatch("ipx00535", "ipx535"))
}

func TestContentIDMatchesExpected_NonMatchingSeries(t *testing.T) {
	assert.False(t, contentIDCoreMatch("abc01234", "xyz5678"))
}

func TestContentIDMatchesExpected_NonMatchingNumbers(t *testing.T) {
	assert.False(t, contentIDCoreMatch("ipx00535", "ipx99999"))
}

// --- Module Register ---

func TestRegister(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	assert.NotPanics(t, func() {
		Register(reg)
	})
}

// --- New scraper edge cases ---

func TestNewScraper_JapaneseLanguage(t *testing.T) {
	cfg := models.ScraperSettings{
		Enabled:  true,
		Language: "ja",
	}
	scraper := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	require.NotNil(t, scraper)
	assert.Equal(t, "ja", scraper.language)
}

func TestNewScraper_WithRateLimit(t *testing.T) {
	cfg := models.ScraperSettings{
		Enabled:   true,
		Language:  "en",
		RateLimit: 100,
	}
	scraper := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	require.NotNil(t, scraper)
}

// --- buildTranslations edge cases ---

func TestBuildTranslations_JapaneseWithDirectors(t *testing.T) {
	s := &scraper{language: "ja", settings: models.ScraperSettings{Enabled: true}}

	data := &r18Response{
		TitleJA:      "日本語タイトル",
		TitleEn:      "English Title",
		Description:  "日本語説明",
		MakerNameJa:  "日本語メーカー",
		LabelNameJa:  "日本語レーベル",
		SeriesNameJa: "日本語シリーズ",
		Directors: []struct {
			ID         int    `json:"id"`
			NameKana   string `json:"name_kana"`
			NameKanji  string `json:"name_kanji"`
			NameRomaji string `json:"name_romaji"`
		}{
			{NameKanji: "監督名", NameRomaji: "Kantoku Mei"},
		},
	}

	translations := s.buildTranslations(data, "IPX-535")
	require.Len(t, translations, 2)

	jaTrans := translations[1]
	assert.Equal(t, "監督名", jaTrans.Director)

	enTrans := translations[0]
	assert.Equal(t, "Kantoku Mei", enTrans.Director)
}

// --- missRoundTripper helper test ---

func TestMissRoundTripper(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	rt := &missRoundTripper{server: server}
	client := resty.New()
	client.SetTransport(rt)
	client.SetTimeout(5 * time.Second)

	resp, err := client.R().Get("https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
}
