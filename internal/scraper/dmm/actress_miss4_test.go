package dmm

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// --- extractActresses: label with "演者" variant ---

func TestMiss4_ExtractActresses_EnshaLabel(t *testing.T) {
	html := `<table><tr><td>演者</td><td>
		<a href="?actress=9101">Test Actress</a>
	</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	if assert.Len(t, actresses, 1) {
		assert.Equal(t, "Actress", actresses[0].FirstName) // Single word = first name only
	}
}

// --- extractActresses: no content cell in row ---

func TestMiss4_ExtractActresses_NoContentCell(t *testing.T) {
	html := `<table><tr><td>Actress</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	assert.Empty(t, actresses)
}

// --- extractActresses: actress row with empty label cell ---

func TestMiss4_ExtractActresses_EmptyLabelCell(t *testing.T) {
	html := `<table><tr><td></td><td><a href="?actress=9102">Someone</a></td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	assert.Empty(t, actresses) // Empty label doesn't match actress keywords
}

// --- extractActresses: skip actress with 購入前 in name ---

func TestMiss4_ExtractActresses_SkipPurchasePrefixName(t *testing.T) {
	html := `<table><tr><td>Actress</td><td>
		<a href="?actress=9103">購入前チェック</a>
	</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	assert.Empty(t, actresses)
}

// --- extractActresses: skip actress with レビュー in name ---

func TestMiss4_ExtractActresses_SkipReviewName(t *testing.T) {
	html := `<table><tr><td>Actress</td><td>
		<a href="?actress=9104">レビュー投稿</a>
	</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	assert.Empty(t, actresses)
}

// --- extractActresses: skip actress with ポイント in name ---

func TestMiss4_ExtractActresses_SkipPointsName(t *testing.T) {
	html := `<table><tr><td>Actress</td><td>
		<a href="?actress=9105">ポイント確認</a>
	</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	assert.Empty(t, actresses)
}

// --- extractActressFromLink: no href attribute ---

func TestMiss4_ExtractActressFromLink_NoHref(t *testing.T) {
	html := `<a>No Href</a>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	sel := doc.Find("a").First()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, models.ActressInfo{}, actress)
}

// --- extractActressFromLink: href without actress ID ---

func TestMiss4_ExtractActressFromLink_NoActressIDInHref(t *testing.T) {
	html := `<a href="/some/other/page">Name</a>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	sel := doc.Find("a").First()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, models.ActressInfo{}, actress)
}

// --- extractActressFromLink: actress with article=actress/id= URL format ---

func TestMiss4_ExtractActressFromLink_ArticleIDFormat(t *testing.T) {
	html := `<a href="/article=actress/id=9200/">テスト女優</a>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	sel := doc.Find("a").First()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 9200, actress.DMMID)
	assert.Equal(t, "テスト女優", actress.JapaneseName)
}

// --- extractActressFromLink: name with parentheses stripped ---

func TestMiss4_ExtractActressFromLink_ParenthesesStripped(t *testing.T) {
	html := `<a href="?actress=9201">Name (Alias)</a>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	sel := doc.Find("a").First()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 9201, actress.DMMID)
	// Parentheses should be stripped
	assert.Equal(t, "Name", actress.FirstName)
}

// --- extractActressFromLink: name with fullwidth parentheses stripped ---

func TestMiss4_ExtractActressFromLink_FullwidthParenStripped(t *testing.T) {
	html := `<a href="?actress=9202">名前（別名）</a>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	sel := doc.Find("a").First()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 9202, actress.DMMID)
	assert.Equal(t, "名前", actress.JapaneseName)
}

// --- extractActressesFromStreamingPage: heading with この商品に出演しているAV女優 ---

func TestMiss4_ExtractActressesFromStreamingPage_HeadingMatch(t *testing.T) {
	html := `<div>
		<h2>この商品に出演しているAV女優</h2>
		<div>
			<a href="?actress=9300">テスト女優</a>
		</div>
	</div>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActressesFromStreamingPage(context.Background(), doc)
	if assert.Len(t, actresses, 1) {
		assert.Equal(t, 9300, actresses[0].DMMID)
	}
}

// --- extractActressesFromStreamingPage: metadata selectors fallback ---

func TestMiss4_ExtractActressesFromStreamingPage_MetadataFallback(t *testing.T) {
	html := `<dl>
		<a href="?actress=9301">Fallback Actress</a>
	</dl>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActressesFromStreamingPage(context.Background(), doc)
	// The dl selector pattern should match
	if assert.NotEmpty(t, actresses) {
		assert.Equal(t, 9301, actresses[0].DMMID)
	}
}

// --- upsertActressInfo: merge ThumbURL from new into existing ---

func TestMiss4_UpsertActressInfo_MergeThumbURL(t *testing.T) {
	actresses := []models.ActressInfo{
		{DMMID: 100, FirstName: "Original", ThumbURL: ""},
	}
	indexByID := map[int]int{100: 0}

	newActress := models.ActressInfo{DMMID: 100, ThumbURL: "https://example.com/thumb.jpg"}
	result := upsertActressInfo(&actresses, indexByID, newActress)

	assert.False(t, result) // false because it's an update, not insert
	assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)
}

// --- upsertActressInfo: merge JapaneseName from new into existing ---

func TestMiss4_UpsertActressInfo_MergeJapaneseName(t *testing.T) {
	actresses := []models.ActressInfo{
		{DMMID: 101, FirstName: "Test", JapaneseName: ""},
	}
	indexByID := map[int]int{101: 0}

	newActress := models.ActressInfo{DMMID: 101, JapaneseName: "テスト"}
	result := upsertActressInfo(&actresses, indexByID, newActress)

	assert.False(t, result)
	assert.Equal(t, "テスト", actresses[0].JapaneseName)
}

// --- upsertActressInfo: merge FirstName from new into existing ---

func TestMiss4_UpsertActressInfo_MergeFirstName(t *testing.T) {
	actresses := []models.ActressInfo{
		{DMMID: 102, JapaneseName: "テスト", FirstName: ""},
	}
	indexByID := map[int]int{102: 0}

	newActress := models.ActressInfo{DMMID: 102, FirstName: "TestFirst"}
	result := upsertActressInfo(&actresses, indexByID, newActress)

	assert.False(t, result)
	assert.Equal(t, "TestFirst", actresses[0].FirstName)
}

// --- upsertActressInfo: merge LastName from new into existing ---

func TestMiss4_UpsertActressInfo_MergeLastName(t *testing.T) {
	actresses := []models.ActressInfo{
		{DMMID: 103, JapaneseName: "テスト", LastName: ""},
	}
	indexByID := map[int]int{103: 0}

	newActress := models.ActressInfo{DMMID: 103, LastName: "TestLast"}
	result := upsertActressInfo(&actresses, indexByID, newActress)

	assert.False(t, result)
	assert.Equal(t, "TestLast", actresses[0].LastName)
}

// --- shouldSkipActressName: coverage of all branches ---

func TestMiss4_ShouldSkipActressName_AllCases(t *testing.T) {
	assert.True(t, shouldSkipActressName(""))
	assert.True(t, shouldSkipActressName("購入前チェック"))
	assert.True(t, shouldSkipActressName("レビュー投稿"))
	assert.True(t, shouldSkipActressName("ポイント確認"))
	assert.False(t, shouldSkipActressName("Valid Name"))
	assert.False(t, shouldSkipActressName("波多野結衣"))
}

// --- extractActressID: both regex patterns ---

func TestMiss4_ExtractActressID_Patterns(t *testing.T) {
	// Standard ?actress= pattern
	assert.Equal(t, 12345, extractActressID("https://www.dmm.co.jp/actress/?actress=12345"))
	// Article ID pattern
	assert.Equal(t, 67890, extractActressID("https://www.dmm.co.jp/article=actress/id=67890/"))
	// No match
	assert.Equal(t, 0, extractActressID("https://www.dmm.co.jp/some/other/url"))
}

// --- cleanActressName: paren removal ---

func TestMiss4_CleanActressName_ParenRemoval(t *testing.T) {
	assert.Equal(t, "Test", cleanActressName("Test (alias)"))
	assert.Equal(t, "テスト", cleanActressName("テスト（別名）"))
	assert.Equal(t, "Clean Name", cleanActressName("  Clean Name  "))
}

// --- normalizeActressThumbURL: various cases ---

func TestMiss4_NormalizeActressThumbURL_Cases(t *testing.T) {
	// Empty
	assert.Equal(t, "", normalizeActressThumbURL(""))
	// Absolute DMM URL passes through
	assert.Equal(t, "https://pics.dmm.co.jp/img.jpg", normalizeActressThumbURL("https://pics.dmm.co.jp/img.jpg"))
	// Non-DMM host is rejected (SSRF/allowed-host guard): returns empty
	assert.Equal(t, "", normalizeActressThumbURL("https://example.com/img.jpg"))
	// Comma-separated srcset (DMM host)
	assert.Equal(t, "https://pics.dmm.co.jp/img.jpg", normalizeActressThumbURL("https://pics.dmm.co.jp/img.jpg, https://pics.dmm.co.jp/img2x.jpg 2x"))
	// Whitespace in URL (DMM host)
	assert.Equal(t, "https://pics.dmm.co.jp/img.jpg", normalizeActressThumbURL("https://pics.dmm.co.jp/img.jpg\t"))
	// Root-relative path resolves to a DMM host
	assert.Equal(t, "https://video.dmm.co.jp/path/img.jpg", normalizeActressThumbURL("/path/img.jpg"))
	// Protocol-relative non-DMM host is rejected by the allowed-host guard
	assert.Equal(t, "", normalizeActressThumbURL("//example.com/img.jpg"))
	// Protocol-relative DMM host is upgraded to https and kept
	assert.Equal(t, "https://pics.dmm.co.jp/img.jpg", normalizeActressThumbURL("//pics.dmm.co.jp/img.jpg"))
	// file:// scheme is rejected
	assert.Equal(t, "", normalizeActressThumbURL("file:///etc/passwd"))
}

// --- findNearestActressContainer: nil input ---

func TestMiss4_FindNearestActressContainer_NilInput(t *testing.T) {
	result := findNearestActressContainer(nil)
	assert.Nil(t, result)
}

// --- findNearestActressContainer: no actress links in ancestors ---

func TestMiss4_FindNearestActressContainer_NoLinksInAncestors(t *testing.T) {
	html := `<div><h2>Heading</h2></div>`
	doc := docFromHTMLDMM(t, html)
	heading := doc.Find("h2").First()
	result := findNearestActressContainer(heading)
	// Should return nil since no actress links found within 8 levels
	assert.Nil(t, result)
}

// --- extractActressThumbURL: with img inside link ---

func TestMiss4_ExtractActressThumbURL_ImgInsideLink(t *testing.T) {
	html := `<a href="?actress=9400"><img data-src="https://pics.dmm.co.jp/actress/9400.jpg"/></a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	thumbURL := extractActressThumbURL(sel)
	assert.Contains(t, thumbURL, "9400.jpg")
}

// --- extractActressThumbURL: with source element inside ---

func TestMiss4_ExtractActressThumbURL_SourceElement(t *testing.T) {
	html := `<a href="?actress=9401"><source srcset="https://pics.dmm.co.jp/actress/9401.webp"/></a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	thumbURL := extractActressThumbURL(sel)
	assert.Contains(t, thumbURL, "9401.webp")
}

// --- extractActressThumbURL: with img in parent ---

func TestMiss4_ExtractActressThumbURL_ImgInParent(t *testing.T) {
	html := `<div><img src="https://pics.dmm.co.jp/actress/9402.jpg"/><a href="?actress=9402">Name</a></div>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	thumbURL := extractActressThumbURL(sel)
	assert.Contains(t, thumbURL, "9402.jpg")
}

// --- extractActressFromLink: actress with Japanese name should not flip first/last ---

func TestMiss4_ExtractActressFromLink_JapaneseNameNoFlip(t *testing.T) {
	html := `<a href="?actress=9500">山田花子</a>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	sel := doc.Find("a").First()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, "山田花子", actress.JapaneseName)
	assert.Empty(t, actress.FirstName)
	assert.Empty(t, actress.LastName)
}
