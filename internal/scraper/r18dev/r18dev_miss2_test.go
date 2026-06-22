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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapeURL: success with full JSON response ---

func TestScrapeURL_Miss2_SuccessFullJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"dvd_id": "SSIS-100",
			"content_id": "1ssis00100",
			"title_ja": "フルタイトル",
			"title_en": "Full Title",
			"description": "日本語説明",
			"description_en": "English description",
			"release_date": "2021-06-15",
			"runtime_mins": 150,
			"jacket_full_url": "https://pics.dmm.co.jp/digital/video/ssis00100/ssis00100pl.jpg",
			"gallery": [
				{"image_full": "https://pics.dmm.co.jp/digital/video/ssis00100/ssis00100jp-1.jpg"},
				{"image_full": "https://pics.dmm.co.jp/digital/video/ssis00100/ssis00100jp-2.jpg"}
			],
			"sample_url": "https://cc3001.dmm.co.jp/litevideo/freepv/s/ssis/ssis00100/ssis00100_mhb_w.mp4",
			"directors": [{"name_romaji": "Director R", "name_kanji": "監督名"}],
			"maker_name_en": "S1 NO.1 STYLE",
			"maker_name_ja": "S1 NO.1 STYLE",
			"label_name_en": "S1 NO.1 STYLE",
			"label_name_ja": "S1 NO.1 STYLE",
			"series_name_en": "Test Series",
			"series_name_ja": "テストシリーズ",
			"categories": [{"name_en": "Drama", "name_ja": "ドラマ"}],
			"actresses": [{"id": 1, "name_romaji": "Momo Sakura", "name_kanji": "桜もも", "image_url": "https://pics.dmm.co.jp/mono/actjpgs/sakura_momo.jpg"}]
		}`))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined=1ssis00100")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "SSIS-100", result.ID)
	assert.Equal(t, "Full Title", result.Title)
	assert.Equal(t, 150, result.Runtime)
	assert.Len(t, result.Actresses, 1)
	assert.Len(t, result.ScreenshotURL, 2)
	assert.Equal(t, "Director R", result.Director)
	assert.NotNil(t, result.ReleaseDate)
	assert.Equal(t, "2021-06-15", result.ReleaseDate.Format("2006-01-02"))
}

// --- ScrapeURL: success with Japanese language ---

func TestScrapeURL_Miss2_JapaneseLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"dvd_id": "JA-001",
			"content_id": "1ja00001",
			"title_ja": "日本語タイトル",
			"title_en": "English Title",
			"maker_name_en": "English Maker",
			"maker_name_ja": "日本語メーカー",
			"actresses": [],
			"categories": []
		}`))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "ja")
	result, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined=1ja00001")
	require.NoError(t, err)
	assert.Equal(t, "日本語タイトル", result.Title)
	assert.Equal(t, "日本語メーカー", result.Maker)
}

// --- ScrapeURL: context cancelled ---

func TestScrapeURL_Miss2_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.ScrapeURL(ctx, "https://r18.dev/videos/vod/movies/detail/-/combined=test")
	require.Error(t, err)
}

// --- Search: dvd_id lookup returns mismatched dvd_id ---

func TestSearch_Miss2_MismatchedDVDID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		if strings.Contains(path, "dvd_id=") {
			w.WriteHeader(http.StatusOK)
			// Return a different dvd_id than what was searched
			_, _ = w.Write([]byte(`{
				"content_id": "1other00100",
				"dvd_id": "other100"
			}`))
			return
		}

		if strings.Contains(path, "combined=") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "MIS-001",
				"content_id": "1mis00001",
				"title_en": "Mismatch Result",
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "MIS-001")
	require.NoError(t, err)
	require.NotNil(t, result)
}

// --- Search: dvd_id lookup returns empty dvd_id but matching content_id ---

func TestSearch_Miss2_EmptyDVDIDMatchingContentID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		if strings.Contains(path, "dvd_id=") {
			if strings.Contains(path, "dvd_id=emt001") {
				w.WriteHeader(http.StatusOK)
				// Empty dvd_id but matching content_id
				_, _ = w.Write([]byte(`{
					"content_id": "1emt00001",
					"dvd_id": ""
				}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if strings.Contains(path, "combined=") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "EMT-001",
				"content_id": "1emt00001",
				"title_en": "Empty DVD ID Result",
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "EMT-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "EMT-001", result.ID)
}

// --- Search: html response on dvd_id lookup ---

func TestSearch_Miss2_HTMLOnDVDLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if strings.Contains(path, "dvd_id=") {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html>not found</html>"))
			return
		}

		// For combined= lookups, return valid JSON
		if strings.Contains(path, "combined=") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "HTM-001",
				"content_id": "1htm00001",
				"title_en": "HTML Fallback",
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "HTM-001")
	require.NoError(t, err)
	assert.Equal(t, "HTML Fallback", result.Title)
}

// --- Search: context cancelled ---

func TestSearch_Miss2_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Search(ctx, "CANCEL-001")
	require.Error(t, err)
}

// --- validateScraperSettings tests ---

func TestValidateScraperSettings_Miss2_InvalidLanguage(t *testing.T) {
	err := validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: "fr"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "language")
}

func TestValidateScraperSettings_Miss2_ValidLanguage(t *testing.T) {
	err := validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: "en", RateLimit: 100, RetryCount: 3, Timeout: 30})
	assert.NoError(t, err)
}

// --- contentIDCoreMatch: more edge cases ---

func TestContentIDMatchesExpected_Miss2_EmptyContentID(t *testing.T) {
	assert.False(t, contentIDCoreMatch("", "ipx535"))
}

func TestContentIDMatchesExpected_Miss2_ShortIDs(t *testing.T) {
	assert.False(t, contentIDCoreMatch("ipx", "ipx535"))
}

// --- generateContentIDVariations edge cases ---

func TestGenerateAlternateContentIDs_Miss2_ShortID(t *testing.T) {
	// ID that doesn't match the regex
	result := generateContentIDVariations("short")
	assert.Nil(t, result)
}

func TestGenerateAlternateContentIDs_Miss2_ValidID(t *testing.T) {
	result := generateContentIDVariations("ipx00535")
	assert.NotEmpty(t, result)
	// Should contain prefixed variants using the prefix lookup for "ipx"
	found := false
	for _, id := range result {
		if strings.HasPrefix(id, "ipx") || strings.HasPrefix(id, "4ipx") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected prefixed alternate IDs")
}

// --- contentIDToID edge cases ---

func TestContentIDToID_Miss2_ShortInput(t *testing.T) {
	result := contentIDToID("ab")
	assert.Equal(t, "AB", result)
}

func TestContentIDToID_Miss2_WithSuffix(t *testing.T) {
	result := contentIDToID("118abw00001")
	assert.Equal(t, "ABW-001", result)
}

// --- normalizeIDWithoutStripping edge cases ---

func TestNormalizeIDWithoutStripping_Miss2_PreservesPrefix(t *testing.T) {
	result := normalizeIDWithoutStripping("4sone860")
	assert.Equal(t, "4sone860", result)
}

func TestNormalizeIDWithoutStripping_Miss2_UnicodeWhitespace(t *testing.T) {
	result := normalizeIDWithoutStripping("IPX\u00a0-535") // non-breaking space
	assert.Equal(t, "ipx535", result)
}

// --- normalizeID edge cases ---

func TestNormalizeID_Miss2_StripsDMMToPrefix(t *testing.T) {
	result := normalizeID("4sone860")
	assert.Equal(t, "sone860", result)
}

func TestNormalizeID_Miss2_NoPrefix(t *testing.T) {
	result := normalizeID("ipx535")
	assert.Equal(t, "ipx535", result)
}

// --- stripDMMPrefix edge cases ---

func TestStripDMMPrefix_Miss2_NoDigits(t *testing.T) {
	result := stripDMMPrefix("sone860")
	assert.Equal(t, "sone860", result)
}

func TestStripDMMPrefix_Miss2_WithPrefix(t *testing.T) {
	result := stripDMMPrefix("4sone860")
	assert.Equal(t, "sone860", result)
}

func TestStripDMMPrefix_Miss2_MultipleDigits(t *testing.T) {
	result := stripDMMPrefix("118abw001")
	assert.Equal(t, "abw001", result)
}

// --- ResolveDownloadProxyForHost edge cases ---

func TestResolveDownloadProxyForHost_Miss2_EmptyHost(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")
	_, _, ok := s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
}

func TestResolveDownloadProxyForHost_Miss2_Subdomain(t *testing.T) {
	downloadProxy := &models.ProxyConfig{Enabled: true}
	overrideProxy := &models.ProxyConfig{Enabled: true}
	s := &scraper{
		downloadProxy: downloadProxy,
		proxyOverride: overrideProxy,
		settings:      models.ScraperSettings{DownloadProxy: downloadProxy, Proxy: overrideProxy},
	}
	dp, op, ok := s.ResolveDownloadProxyForHost("api.r18.dev")
	assert.True(t, ok)
	assert.Equal(t, downloadProxy, dp)
	assert.Equal(t, overrideProxy, op)
}

func TestResolveDownloadProxyForHost_Miss2_UnknownHost(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")
	_, _, ok := s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
}

// --- Close returns nil ---

func TestClose_Miss2(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")
	assert.NoError(t, s.Close())
}

// --- doRequestWithRetryCtx: success on first try ---

func TestDoRequestWithRetryCtx_Miss2_SuccessFirstTry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"dvd_id": "TEST-001"}`))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		maxRetries:  3,
		rateLimiter: ratelimit.NewLimiter(0),
	}

	resp, err := s.doRequestWithRetryCtx(context.Background(), "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
}

// --- doRequestWithRetryCtx: request error (no retry) ---

func TestDoRequestWithRetryCtx_Miss2_RequestError(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		maxRetries:  3,
		rateLimiter: ratelimit.NewLimiter(0),
	}

	_, err := s.doRequestWithRetryCtx(context.Background(), "http://127.0.0.1:1/unreachable")
	require.Error(t, err)
}

// --- doRequestWithRetryCtx: respect Retry-After header ---

func TestDoRequestWithRetryCtx_Miss2_RespectRetryAfter(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt <= 1 {
			w.Header().Set("Retry-After", "0") // 0 seconds
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"dvd_id": "TEST-003"}`))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:            client,
		maxRetries:        2,
		respectRetryAfter: true,
		rateLimiter:       ratelimit.NewLimiter(0),
	}

	resp, err := s.doRequestWithRetryCtx(context.Background(), "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.True(t, attempt >= 2)
}

// --- parseResponse: actress with single-word romaji name ---

func TestParseResponse_Miss2_ActressSingleName(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:     "SN-001",
		ContentID: "1sn00001",
		TitleJA:   "テスト",
		TitleEn:   "Test",
		Runtime:   60,
		Actresses: []struct {
			ID         int    `json:"id"`
			ImageURL   string `json:"image_url"`
			NameKana   string `json:"name_kana"`
			NameKanji  string `json:"name_kanji"`
			NameRomaji string `json:"name_romaji"`
		}{
			{ID: 5, NameRomaji: "Mononame", NameKanji: "単名"},
		},
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	require.Len(t, result.Actresses, 1)
	assert.Equal(t, "Mononame", result.Actresses[0].FirstName)
	assert.Empty(t, result.Actresses[0].LastName)
}

// --- parseResponse: actress with relative image_url ---

func TestParseResponse_Miss2_ActressRelativeImageURL(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:     "RI-001",
		ContentID: "1ri00001",
		TitleJA:   "テスト",
		TitleEn:   "Test",
		Runtime:   60,
		Actresses: []struct {
			ID         int    `json:"id"`
			ImageURL   string `json:"image_url"`
			NameKana   string `json:"name_kana"`
			NameKanji  string `json:"name_kanji"`
			NameRomaji string `json:"name_romaji"`
		}{
			{ID: 6, NameRomaji: "Test Actress", NameKanji: "テスト", ImageURL: "test_actress.jpg"},
		},
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	require.Len(t, result.Actresses, 1)
	assert.Contains(t, result.Actresses[0].ThumbURL, "pics.dmm.co.jp")
}

// --- parseResponse: legacy cover image (large only, no large2) ---

func TestParseResponse_Miss2_LegacyCoverLarge(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:     "LC-001",
		ContentID: "1lc00001",
		TitleJA:   "テスト",
		TitleEn:   "Test",
		Runtime:   60,
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
				Large: "https://pics.dmm.co.jp/digital/video/lc00001/lc00001pl.jpg",
			},
		},
		Actresses:  nil,
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Contains(t, result.CoverURL, "lc00001pl.jpg")
}

// --- parseResponse: legacy trailer (low quality only) ---

func TestParseResponse_Miss2_LegacyTrailerLow(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:     "LT-001",
		ContentID: "1lt00001",
		TitleJA:   "テスト",
		TitleEn:   "Test",
		Runtime:   60,
		Sample: struct {
			High string `json:"high"`
			Low  string `json:"low"`
		}{
			Low: "https://example.com/trailer_low.mp4",
		},
		Actresses:  nil,
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/trailer_low.mp4", result.TrailerURL)
}

// --- parseResponse: series fallback to nested and flat fields ---

func TestParseResponse_Miss2_SeriesFallback(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:     "SF-001",
		ContentID: "1sf00001",
		TitleJA:   "テスト",
		TitleEn:   "Test",
		Runtime:   60,
		Series: struct {
			Name string `json:"name"`
		}{Name: "Nested Series"},
		SeriesName: "Flat Series Name",
		Actresses:  nil,
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Equal(t, "Nested Series", result.Series)
}

// --- parseResponse: no cover image ---

func TestParseResponse_Miss2_NoCoverImage(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")

	data := &r18Response{
		DVDID:      "NC-001",
		ContentID:  "1nc00001",
		TitleJA:    "テスト",
		TitleEn:    "Test",
		Runtime:    60,
		Actresses:  nil,
		Categories: nil,
	}

	result, err := s.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)
	assert.Empty(t, result.CoverURL)
}

// --- ExtractIDFromURL: combined parameter ---

func TestExtractIDFromURL_Miss2_CombinedParam(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")
	id, err := s.ExtractIDFromURL("https://r18.dev/videos/vod/movies/detail/-/combined=ipx00535")
	require.NoError(t, err)
	assert.Equal(t, "ipx00535", id)
}

// --- getURLCtx: normalizes ID ---

func TestGetURLCtx_Miss2_NormalizesID(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")
	url, err := s.getURLCtx(context.Background(), "IPX-535")
	require.NoError(t, err)
	assert.Contains(t, url, "ipx535")
}

// --- getURLCtx: handles whitespace in ID ---

func TestGetURLCtx_Miss2_WhitespaceInID(t *testing.T) {
	s := newR18TestScraper(httptest.NewServer(nil), true, "en")
	url, err := s.getURLCtx(context.Background(), "IPX\u00a0-535") // non-breaking space
	require.NoError(t, err)
	assert.Contains(t, url, "ipx535")
}

// --- selectLocalizedString edge cases ---

func TestSelectLocalizedString_Miss2(t *testing.T) {
	assert.Equal(t, "English", selectLocalizedString("en", "English", "日本語"))
	assert.Equal(t, "日本語", selectLocalizedString("ja", "English", "日本語"))
	assert.Equal(t, "日本語", selectLocalizedString("en", "", "日本語"))         // fallback to Japanese when English empty
	assert.Equal(t, "English", selectLocalizedString("ja", "English", "")) // fallback to English when Japanese empty
}

// --- getPreferredString edge cases ---

func TestGetPreferredString_Miss2(t *testing.T) {
	assert.Equal(t, "preferred", getPreferredString("preferred", "fallback"))
	assert.Equal(t, "fallback", getPreferredString("", "fallback"))
}
