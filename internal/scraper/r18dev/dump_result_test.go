package r18dev

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newScraperWithBlockedHTTP builds an enabled scraper whose HTTP client uses a
// recordingTransport. Any outgoing HTTP request increments the transport's hit
// counter and returns an error. Tests assert on count() to PROVE whether HTTP
// was (or was not) attempted — replacing the previous non-hermetic tests that
// hit real r18.dev and flaked on 429 rate-limiting.
func newScraperWithBlockedHTTP(t *testing.T, dump models.R18DevDumpLookup) (*scraper, *recordingTransport) {
	t.Helper()
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.RetryCount = 0 // no scraper-level retries so a blocked HTTP request fails fast
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, dump)
	// The httpclient builder floors RetryCount=0 up to DefaultRetryCount (3),
	// which would make every blocked request wait through resty's retry
	// backoff. Disable resty retries directly so the fallback path is fast.
	s.client.SetRetryCount(0)
	rt := &recordingTransport{err: errHTTPBlocked}
	s.client.SetTransport(rt)
	return s, rt
}

// TestSearchFromDump_ZeroHTTP verifies that Search returns a complete
// ScraperResult from the dump with zero r18.dev HTTP. A recordingTransport
// PROVES no request was issued: count() must remain 0 after a dump hit.
func TestSearchFromDump_ZeroHTTP(t *testing.T) {
	dump := &stubDumpLookup{}
	dump.lookupMovieResult = &models.DumpMovie{
		ContentID:      "118abw00013",
		DVDID:          "ABW-013",
		TitleEn:        "You Can't Make A Sound",
		TitleJa:        "Airi Suzumura",
		CommentEn:      "English description",
		CommentJa:      "日本語説明",
		Runtime:        182,
		ReleaseDate:    "2020-10-02",
		SampleURL:      "https://cc3001.dmm.co.jp/litevideo/freepv/1/118/118abw013/118abw013_dmb_w.mp4",
		JacketFullURL:  "digital/video/118abw00013/118abw00013pl",
		JacketThumbURL: "digital/video/118abw00013/118abw00013ps",
		GalleryFirst:   "digital/video/118abw00013/118abw00013jp-1",
		GalleryLast:    "digital/video/118abw00013/118abw00013jp-12",
		Maker:          &models.DumpNamedEntity{NameEn: "Prestige", NameJa: "プレステージ"},
		Label:          &models.DumpNamedEntity{NameEn: "ABSOLUTELY WONDERFUL", NameJa: "ABSOLUTELY WONDERFUL"},
		Series:         &models.DumpNamedEntity{NameEn: "When You Can't Scream Out...", NameJa: "声が出せない状況で…"},
		Director:       &models.DumpDirector{NameRomaji: "Kantoku Mei", NameKanji: "監督名"},
		Actresses: []models.DumpActress{{
			ID: "1019076", NameRomaji: "Airi Suzumura", NameKanji: "鈴村あいり",
			ImageURL: "suzumura_airi.jpg",
		}},
		Categories: []models.DumpNamedEntity{
			{NameEn: "Cheating Wife", NameJa: "寝取り・寝取られ・NTR"},
			{NameEn: "Squirting", NameJa: "潮吹き"},
		},
		TrailerURL: "https://cc3001.dmm.co.jp/litevideo/freepv/1/118/118abw013/118abw013_trailer.mp4",
	}

	s, rt := newScraperWithBlockedHTTP(t, dump)

	result, err := s.Search(context.Background(), "ABW-013")
	require.NoError(t, err, "Search should succeed from dump with zero HTTP")
	require.NotNil(t, result)

	// THE core assertion: zero HTTP requests to r18.dev.
	assert.Equal(t, 0, rt.count(), "dump hit must issue zero HTTP requests")

	assert.Equal(t, "r18dev", result.Source)
	assert.Equal(t, "ABW-013", result.ID)
	assert.Equal(t, "118abw00013", result.ContentID)
	assert.Equal(t, 182, result.Runtime)
	assert.Equal(t, "You Can't Make A Sound", result.Title)
	assert.Contains(t, result.CoverURL, "118abw00013pl.jpg")
	assert.Contains(t, result.PosterURL, "118abw00013pl.jpg")
	assert.True(t, result.ShouldCropPoster, "dump path should crop cover into portrait poster (can't probe awsimgsrc ps.jpg)")
	assert.Len(t, result.ScreenshotURL, 12, "gallery range 1-12 should expand to 12 screenshots")
	assert.Contains(t, result.ScreenshotURL[0], "118abw00013jp-1.jpg")
	assert.Contains(t, result.ScreenshotURL[11], "118abw00013jp-12.jpg")
	assert.Equal(t, "Prestige", result.Maker)
	assert.Equal(t, "ABSOLUTELY WONDERFUL", result.Label)
	assert.Equal(t, "When You Can't Scream Out...", result.Series)
	assert.Equal(t, "Kantoku Mei", result.Director)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "Airi", result.Actresses[0].FirstName)
	assert.Equal(t, "Suzumura", result.Actresses[0].LastName)
	assert.Equal(t, "鈴村あいり", result.Actresses[0].JapaneseName)
	assert.Contains(t, result.Actresses[0].ThumbURL, "suzumura_airi.jpg")
	assert.Len(t, result.Genres, 2)
	assert.Equal(t, "Cheating Wife", result.Genres[0])
	assert.Equal(t, "Squirting", result.Genres[1])
	assert.Equal(t, "https://cc3001.dmm.co.jp/litevideo/freepv/1/118/118abw013/118abw013_trailer.mp4", result.TrailerURL)

	// Translations: assert content, not just presence.
	require.Len(t, result.Translations, 2, "both en and ja translations should be built")
	assert.Equal(t, "en", result.Translations[0].Language)
	assert.Equal(t, "You Can't Make A Sound", result.Translations[0].Title)
	assert.Equal(t, "English description", result.Translations[0].Description)
	assert.Equal(t, "Prestige", result.Translations[0].Maker)
	assert.Equal(t, "Kantoku Mei", result.Translations[0].Director)
	assert.Equal(t, "ja", result.Translations[1].Language)
	assert.Equal(t, "Airi Suzumura", result.Translations[1].Title)
	assert.Equal(t, "日本語説明", result.Translations[1].Description)
	assert.Equal(t, "プレステージ", result.Translations[1].Maker)
	assert.Equal(t, "監督名", result.Translations[1].Director)
}

// TestSearchFromDump_MissFallsBackToHTTP verifies that a dump miss falls
// through to the HTTP path. The recordingTransport PROVES HTTP was attempted
// (count > 0) — hermetically, with no real network — and Search returns an
// error since the blocked transport fails every request.
func TestSearchFromDump_MissFallsBackToHTTP(t *testing.T) {
	dump := &stubDumpLookup{dvdToContent: map[string]string{}} // no entries -> miss
	s, rt := newScraperWithBlockedHTTP(t, dump)

	_, err := s.Search(context.Background(), "NOPE-999")
	assert.Error(t, err, "dump miss should fall back to HTTP which fails")
	assert.Greater(t, rt.count(), 0, "dump miss should attempt HTTP fallback")
}

// TestSearchFromDump_DumpErrorFallsBackToHTTP verifies that a real dump error
// (not a miss) is logged and falls back to HTTP rather than aborting.
func TestSearchFromDump_DumpErrorFallsBackToHTTP(t *testing.T) {
	dump := &stubDumpLookup{lookupErr: errors.New("simulated corrupt dump")}
	s, rt := newScraperWithBlockedHTTP(t, dump)

	_, err := s.Search(context.Background(), "ABW-013")
	assert.Error(t, err, "dump error should still fall back to HTTP")
	assert.Greater(t, rt.count(), 0, "dump error should attempt HTTP fallback")
}

// TestSearchFromDump_NilDumpFallsBackToHTTP verifies that with no dump
// configured, Search goes straight to HTTP.
func TestSearchFromDump_NilDumpFallsBackToHTTP(t *testing.T) {
	s, rt := newScraperWithBlockedHTTP(t, nil)

	_, err := s.Search(context.Background(), "ABW-013")
	assert.Error(t, err)
	assert.Greater(t, rt.count(), 0, "nil dump should attempt HTTP")
}

// TestResultFromDump_PosterFallback verifies that when JacketThumbURL is empty,
// the poster falls back to the cover URL and ShouldCropPoster is set so the
// frontend right-crops the landscape cover into a portrait (instead of
// letterboxing the full cover).
func TestResultFromDump_PosterFallback(t *testing.T) {
	cfg := createTestSettings(true)
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013", DVDID: "ABW-013",
		JacketFullURL: "digital/video/118abw00013/118abw00013pl",
		// JacketThumbURL intentionally empty.
	}
	result := s.resultFromDump(d)
	assert.NotEmpty(t, result.CoverURL)
	assert.Equal(t, result.CoverURL, result.PosterURL, "poster should fall back to cover when thumb is absent")
	assert.True(t, result.ShouldCropPoster, "cover-as-poster fallback must flag for cropping")
}

// TestResultFromDump_PosterFromThumbOnly verifies that when JacketFullURL is
// empty but JacketThumbURL is set, the poster is still resolved from the thumb.
// This is the reverse of PosterFallback — the poster resolution is independent
// of the cover so a thumb-only row still gets a poster.
func TestResultFromDump_PosterFromThumbOnly(t *testing.T) {
	cfg := createTestSettings(true)
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013", DVDID: "ABW-013",
		// JacketFullURL intentionally empty.
		JacketThumbURL: "digital/video/118abw00013/118abw00013ps",
	}
	result := s.resultFromDump(d)
	assert.Empty(t, result.CoverURL, "cover should be empty when JacketFullURL is absent")
	assert.Contains(t, result.PosterURL, "118abw00013ps.jpg", "poster should resolve from thumb even without a cover")
	assert.False(t, result.ShouldCropPoster, "portrait thumb poster should not be flagged for cropping")
}

// TestResultFromDump_TrailerFallback verifies that when TrailerURL is empty,
// the trailer falls back to the video's sample_url.
func TestResultFromDump_TrailerFallback(t *testing.T) {
	cfg := createTestSettings(true)
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013", DVDID: "ABW-013",
		SampleURL: "https://example.com/sample.mp4",
		// TrailerURL intentionally empty.
	}
	result := s.resultFromDump(d)
	assert.Equal(t, "https://example.com/sample.mp4", result.TrailerURL)
}

// TestResultFromDump_ContentIDToID verifies that when DVDID is empty but
// ContentID is set, the movie ID is derived from the content_id.
func TestResultFromDump_ContentIDToID(t *testing.T) {
	cfg := createTestSettings(true)
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013",
		// DVDID intentionally empty.
	}
	result := s.resultFromDump(d)
	// contentIDToID("118abw00013") -> "ABW-013" (strips DMM prefix, formats number).
	assert.Equal(t, "ABW-013", result.ID)
	assert.Equal(t, "118abw00013", result.ContentID)
}

// TestResultFromDump_ActressThumbSynthesis verifies that when an actress has no
// image_url, the thumb URL is synthesized from the romaji name.
func TestResultFromDump_ActressThumbSynthesis(t *testing.T) {
	cfg := createTestSettings(true)
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013", DVDID: "ABW-013",
		Actresses: []models.DumpActress{{
			ID: "1", NameRomaji: "Airi Suzumura",
			// ImageURL intentionally empty.
		}},
	}
	result := s.resultFromDump(d)
	require.Len(t, result.Actresses, 1)
	assert.Contains(t, result.Actresses[0].ThumbURL, "suzumura_airi.jpg",
		"thumb should be synthesized as lastname_firstname.jpg from romaji")
}

// TestResultFromDump_JapaneseLanguage verifies the dump result respects the
// configured language preference (Japanese names/genres/series selected),
// including the ja series branch.
func TestResultFromDump_JapaneseLanguage(t *testing.T) {
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.Language = "ja"

	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013", DVDID: "ABW-013",
		TitleEn: "English Title", TitleJa: "日本語タイトル",
		Maker:      &models.DumpNamedEntity{NameEn: "Prestige", NameJa: "プレステージ"},
		Series:     &models.DumpNamedEntity{NameEn: "English Series", NameJa: "日本語シリーズ"},
		Categories: []models.DumpNamedEntity{{NameEn: "Squirting", NameJa: "潮吹き"}},
	}
	result := s.resultFromDump(d)

	assert.Equal(t, "日本語タイトル", result.Title)
	assert.Equal(t, "プレステージ", result.Maker)
	assert.Equal(t, "日本語シリーズ", result.Series, "ja series branch should select the Japanese name")
	assert.Equal(t, "潮吹き", result.Genres[0])
}

// TestResultFromDump_EmptyMovie verifies resultFromDump does not panic on a
// minimal movie with no media/entities, and yields empty media URLs.
func TestResultFromDump_EmptyMovie(t *testing.T) {
	cfg := createTestSettings(true)
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{ContentID: "118abw00013", DVDID: "ABW-013"}

	result := s.resultFromDump(d)
	assert.Equal(t, "ABW-013", result.ID)
	assert.Empty(t, result.CoverURL)
	assert.Empty(t, result.PosterURL)
	assert.Empty(t, result.ScreenshotURL)
	assert.Empty(t, result.TrailerURL)
	assert.Empty(t, result.Actresses)
	assert.Empty(t, result.Genres)
}

// TestResultFromDump_DirectorJapaneseLanguage covers the ja-language director
// branch (line 122-124): when language is "ja" and a director is present, the
// result.Director prefers NameKanji over NameRomaji.
func TestResultFromDump_DirectorJapaneseLanguage(t *testing.T) {
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.Language = "ja"
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013", DVDID: "ABW-013",
		Director: &models.DumpDirector{NameKanji: "監督名", NameRomaji: "Kantoku Mei"},
	}
	result := s.resultFromDump(d)
	assert.Equal(t, "監督名", result.Director, "ja director branch should prefer NameKanji")
}

// TestResultFromDump_DirectorEnglishLanguage covers the else branch of the
// director language selection (NameRomaji preferred when language != "ja").
func TestResultFromDump_DirectorEnglishLanguage(t *testing.T) {
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.Language = "en"
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013", DVDID: "ABW-013",
		Director: &models.DumpDirector{NameKanji: "監督名", NameRomaji: "Kantoku Mei"},
	}
	result := s.resultFromDump(d)
	assert.Equal(t, "Kantoku Mei", result.Director, "en director branch should prefer NameRomaji")
}

// TestResultFromDump_ActressSingleWordName covers the single-word actress name
// branch (line 166-168): when NameRomaji has only one part, the synthesized
// filename uses that single word (not the two-word lastname_firstname format).
func TestResultFromDump_ActressSingleWordName(t *testing.T) {
	cfg := createTestSettings(true)
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	d := &models.DumpMovie{
		ContentID: "118abw00013", DVDID: "ABW-013",
		Actresses: []models.DumpActress{{
			ID: "1", NameRomaji: "Maki", // single word, no ImageURL
		}},
	}
	result := s.resultFromDump(d)
	require.Len(t, result.Actresses, 1)
	assert.Contains(t, result.Actresses[0].ThumbURL, "maki.jpg",
		"single-word name should synthesize as the word itself")
}

// TestSearchFromDump_ContextCanceled covers the context.Canceled branch of
// searchFromDump's error switch (line 261-264): a canceled context should
// fall back to HTTP at debug level (not warn), treated as benign.
func TestSearchFromDump_ContextCanceled(t *testing.T) {
	dump := &stubDumpLookup{lookupErr: context.Canceled}
	s, _ := newScraperWithBlockedHTTP(t, dump)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, ok := s.searchFromDump(ctx, "IPX-535")
	assert.False(t, ok, "canceled lookup should fall back to HTTP")
	assert.Nil(t, result, "no result on cancellation")
}

// TestSearchFromDump_ContextDeadlineExceeded covers the DeadlineExceeded arm
// of the same case.
func TestSearchFromDump_ContextDeadlineExceeded(t *testing.T) {
	dump := &stubDumpLookup{lookupErr: context.DeadlineExceeded}
	s, _ := newScraperWithBlockedHTTP(t, dump)
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	result, ok := s.searchFromDump(ctx, "IPX-535")
	assert.False(t, ok)
	assert.Nil(t, result)
}
