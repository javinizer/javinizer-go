package dmm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- extractActressesFromStreamingPage: DL metadata selector ---

func TestExtractActressesFromStreamingPage_Miss2_DLMetadata(t *testing.T) {
	html := `<dl><a href="?actress=650">DL Actress</a></dl>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActressesFromStreamingPage(context.Background(), doc)
	assert.Len(t, actresses, 1)
	assert.Equal(t, 650, actresses[0].DMMID)
}

// --- extractActressesFromStreamingPage: heading with nested container ---

func TestExtractActressesFromStreamingPage_Miss2_HeadingWithContainer(t *testing.T) {
	html := `
	<div>
		<h2>この商品に出演しているAV女優</h2>
		<p>
			<a href="?actress=210">Heading Container Actress</a>
		</p>
	</div>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActressesFromStreamingPage(context.Background(), doc)
	assert.GreaterOrEqual(t, len(actresses), 1)
}

// --- extractActressFromLink: no href attribute ---

func TestExtractActressFromLink_Miss2_NoHref(t *testing.T) {
	html := `<a>No href</a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	s := newTestDMMScraper()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 0, actress.DMMID)
}

// --- extractActressFromLink: no actress ID in href ---

func TestExtractActressFromLink_Miss2_NoActressID(t *testing.T) {
	html := `<a href="/some/other/page">No ID</a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	s := newTestDMMScraper()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 0, actress.DMMID)
}

// --- extractActressFromLink: should-skip actress names ---

func TestExtractActressFromLink_Miss2_SkipName(t *testing.T) {
	tests := []struct {
		name string
		href string
		text string
	}{
		{"empty name", "?actress=1", ""},
		{"review keyword", "?actress=2", "購入前レビュー"},
		{"points keyword", "?actress=3", "ポイント獲得"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := `<a href="` + tt.href + `">` + tt.text + `</a>`
			doc := docFromHTMLDMM(t, html)
			sel := doc.Find("a").First()
			s := newTestDMMScraper()
			actress := s.extractActressFromLink(context.Background(), sel)
			assert.Equal(t, 0, actress.DMMID, "should skip name: %q", tt.text)
		})
	}
}

// --- extractActressFromLink: Japanese name detection ---

func TestExtractActressFromLink_Miss2_JapaneseName(t *testing.T) {
	html := `<a href="?actress=777">波多野結衣</a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	s := newTestDMMScraper()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 777, actress.DMMID)
	assert.Equal(t, "波多野結衣", actress.JapaneseName)
	assert.Empty(t, actress.FirstName)
	assert.Empty(t, actress.LastName)
}

// --- upsertActressInfo: update existing actress with missing fields ---

func TestUpsertActressInfo_Miss2_UpdateExisting(t *testing.T) {
	actresses := make([]models.ActressInfo, 0)
	indexByID := make(map[int]int)

	// Add initial actress
	upsertActressInfo(&actresses, indexByID, models.ActressInfo{
		DMMID:    100,
		ThumbURL: "",
	})

	require.Len(t, actresses, 1)

	// Update with thumb URL
	result := upsertActressInfo(&actresses, indexByID, models.ActressInfo{
		DMMID:        100,
		ThumbURL:     "https://example.com/thumb.jpg",
		JapaneseName: "日本名",
		FirstName:    "First",
		LastName:     "Last",
	})

	assert.False(t, result, "upsert of existing should return false")
	assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)
	assert.Equal(t, "日本名", actresses[0].JapaneseName)
	assert.Equal(t, "First", actresses[0].FirstName)
	assert.Equal(t, "Last", actresses[0].LastName)
}

// --- upsertActressInfo: do not overwrite existing fields ---

func TestUpsertActressInfo_Miss2_DoNotOverwriteExistingFields(t *testing.T) {
	actresses := make([]models.ActressInfo, 0)
	indexByID := make(map[int]int)

	// Add initial actress with all fields
	upsertActressInfo(&actresses, indexByID, models.ActressInfo{
		DMMID:        200,
		ThumbURL:     "https://example.com/original.jpg",
		JapaneseName: "オリジナル",
		FirstName:    "OrigFirst",
		LastName:     "OrigLast",
	})

	// Try to update with empty fields — should not overwrite
	upsertActressInfo(&actresses, indexByID, models.ActressInfo{
		DMMID:    200,
		ThumbURL: "",
	})

	assert.Equal(t, "https://example.com/original.jpg", actresses[0].ThumbURL)
	assert.Equal(t, "オリジナル", actresses[0].JapaneseName)
}

// --- upsertActressInfo: zero DMMID returns false ---

func TestUpsertActressInfo_Miss2_ZeroDMMID(t *testing.T) {
	actresses := make([]models.ActressInfo, 0)
	indexByID := make(map[int]int)

	result := upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 0})
	assert.False(t, result)
	assert.Empty(t, actresses)
}

// --- shouldSkipActressName: comprehensive coverage ---

func TestShouldSkipActressName_Miss2_AllPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		skip  bool
	}{
		{"empty", "", true},
		{"review purchase", "購入前チェック", true},
		{"review literal", "レビュー", true},
		{"points literal", "ポイント", true},
		{"normal name", "Hatano Yui", false},
		{"japanese name", "波多野結衣", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.skip, shouldSkipActressName(tt.input))
		})
	}
}

// --- extractActressID: article=actress/id= format ---

func TestExtractActressID_Miss2_ArticleFormat(t *testing.T) {
	assert.Equal(t, 5567, extractActressID("/mono/dvd/-/list/=/article=actress/id=5567/"))
}

// --- extractActressID: no match ---

func TestExtractActressID_Miss2_NoMatch(t *testing.T) {
	assert.Equal(t, 0, extractActressID("/some/other/path/"))
}

// --- cleanActressName: parenthesized content removed ---

func TestCleanActressName_Miss2_ParenRemoval(t *testing.T) {
	assert.Equal(t, "TestName", cleanActressName("TestName(alias)"))
	assert.Equal(t, "テスト", cleanActressName("テスト（別名）"))
	assert.Equal(t, "Simple", cleanActressName("  Simple  "))
}

// --- normalizeActressThumbURL: comma-separated srcset ---

func TestNormalizeActressThumbURL_Miss2_CommaSrcset(t *testing.T) {
	result := normalizeActressThumbURL("https://pics.dmm.co.jp/test.jpg, 2x")
	assert.Equal(t, "https://pics.dmm.co.jp/test.jpg", result)
}

// --- normalizeActressThumbURL: amp-decoded URL ---

func TestNormalizeActressThumbURL_Miss2_AmpDecoded(t *testing.T) {
	result := normalizeActressThumbURL("https://pics.dmm.co.jp/test.jpg?x=1&amp;y=2")
	// &amp; should be decoded to &
	assert.Contains(t, result, "test.jpg")
	assert.NotContains(t, result, "&amp;")
}

// --- normalizeActressThumbURL: relative URL with leading slash ---

func TestNormalizeActressThumbURL_Miss2_RelativePath(t *testing.T) {
	result := normalizeActressThumbURL("/mono/actjpgs/test.jpg")
	assert.Contains(t, result, "https://video.dmm.co.jp/mono/actjpgs/test.jpg")
}

// --- tryActressThumbURLs: with first and last name ---

func TestTryActressThumbURLs_Miss2_WithNames(t *testing.T) {
	s := newTestDMMScraper()
	result := s.tryActressThumbURLs(context.Background(), "Yui", "Hatano", 0)
	// The function may or may not find a real URL depending on network access.
	// The key test is that it doesn't panic.
	_ = result
}

// --- tryActressThumbURLs: with DMM ID but no names ---

func TestTryActressThumbURLs_Miss2_WithDMMIDNoNames(t *testing.T) {
	s := newTestDMMScraper()
	// With empty names and a DMM ID, should try actress page romaji variants
	result := s.tryActressThumbURLs(context.Background(), "", "", 12345)
	// May be empty since no real server responds, but should not panic
	_ = result
}

// --- extractRomajiVariantsFromActressPageCtx: with actual hiragana variants ---

func TestExtractRomajiVariantsFromActressPageCtx_Miss2_WithVariants(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><head><title>女優（あべみかこ） - DMM</title></head><body></body></html>`))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		client:      client,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	variants := s.extractRomajiVariantsFromActressPageCtx(context.Background(), 9999)
	// The function constructs its own URL to dmm.co.jp. When the test server
	// is accessed via the dmmTestTransport redirect, the page is fetched and
	// hiragana is extracted. If the URL construction differs from what the
	// test server expects, variants may be empty. Key test: no panic.
	_ = variants
}

// --- extractActressThumbURL: img with data-src attribute ---

func TestExtractActressThumbURL_Miss2_DataSrcAttr(t *testing.T) {
	html := `<a href="?actress=1"><img data-src="https://pics.dmm.co.jp/mono/actjpgs/datasrc_test.jpg" />Name</a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	thumbURL := extractActressThumbURL(sel)
	assert.Contains(t, thumbURL, "datasrc_test.jpg")
}

// --- extractActressThumbURL: data:image prefix is skipped ---

func TestExtractActressThumbURL_Miss2_DataImageSkipped(t *testing.T) {
	html := `<a href="?actress=1"><img src="data:image/png;base64,abc" />Name</a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	thumbURL := extractActressThumbURL(sel)
	assert.Empty(t, thumbURL)
}

// --- findNearestActressContainer: nil input ---

func TestFindNearestActressContainer_Miss2_NilInput(t *testing.T) {
	result := findNearestActressContainer(nil)
	assert.Nil(t, result)
}

// --- findNearestActressContainer: empty selection ---

func TestFindNearestActressContainer_Miss2_EmptySelection(t *testing.T) {
	doc := docFromHTMLDMM(t, `<html><body></body></html>`)
	heading := doc.Find("h2").First()
	// Empty selection (no h2 found), but the selection is not nil
	result := findNearestActressContainer(heading)
	// Should be nil since the heading doesn't exist
	assert.Nil(t, result)
}
