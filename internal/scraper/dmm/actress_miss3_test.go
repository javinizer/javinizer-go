package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- extractActresses: Actress row with both Japanese and English names ---

func TestMiss3_ExtractActresses_JapaneseAndEnglishFlip(t *testing.T) {
	// When actress has FirstName and LastName but no JapaneseName, swap them
	html := `<table><tr><td>Actress</td><td>
		<a href="?actress=9001">Smith Jane</a>
	</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	if assert.Len(t, actresses, 1) {
		assert.Equal(t, "Jane", actresses[0].FirstName)
		assert.Equal(t, "Smith", actresses[0].LastName)
	}
}

// --- extractActresses: Japanese name actress ---

func TestMiss3_ExtractActresses_JapaneseName(t *testing.T) {
	html := `<table><tr><td>出演者</td><td>
		<a href="?actress=9002">波多野結衣</a>
	</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	if assert.Len(t, actresses, 1) {
		assert.Equal(t, "波多野結衣", actresses[0].JapaneseName)
	}
}

// --- extractActresses: duplicate actress ID upsert ---

func TestMiss3_ExtractActresses_DuplicateUpsert(t *testing.T) {
	html := `<table><tr><td>Actress</td><td>
		<a href="?actress=9003">NameOne</a>
		<a href="?actress=9003">NameTwo</a>
	</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	assert.Len(t, actresses, 1)
	assert.Equal(t, "NameOne", actresses[0].FirstName)
}

// --- extractActressFromLink: single-word name (non-Japanese) ---

func TestMiss3_ExtractActressFromLink_SingleWordName(t *testing.T) {
	html := `<a href="?actress=9004">Mononym</a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	s := newTestDMMScraper()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 9004, actress.DMMID)
	assert.Equal(t, "Mononym", actress.FirstName)
	assert.Equal(t, "", actress.LastName)
}

// --- upsertActressInfo: adding new actress vs updating existing ---

func TestMiss3_UpsertActressInfo_NewAndExisting(t *testing.T) {
	actresses := make([]models.ActressInfo, 0)
	indexByID := make(map[int]int)

	// Add first actress
	added := upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 1, JapaneseName: "名前1"})
	assert.True(t, added)
	assert.Len(t, actresses, 1)

	// Add second actress
	added = upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 2, JapaneseName: "名前2"})
	assert.True(t, added)
	assert.Len(t, actresses, 2)

	// Update first actress with new thumb
	added = upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 1, ThumbURL: "http://example.com/thumb.jpg"})
	assert.False(t, added)
	assert.Len(t, actresses, 2)
	assert.Equal(t, "http://example.com/thumb.jpg", actresses[0].ThumbURL)
}

// --- upsertActressInfo: zero DMMID returns false ---

func TestMiss3_UpsertActressInfo_ZeroDMMID(t *testing.T) {
	actresses := make([]models.ActressInfo, 0)
	indexByID := make(map[int]int)

	added := upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 0})
	assert.False(t, added)
	assert.Len(t, actresses, 0)
}

// --- upsertActressInfo: fill in missing fields ---

func TestMiss3_UpsertActressInfo_FillMissingFields(t *testing.T) {
	actresses := []models.ActressInfo{{DMMID: 1, JapaneseName: "テスト"}}
	indexByID := map[int]int{1: 0}

	// Update with FirstName and LastName
	added := upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 1, FirstName: "Test", LastName: "User"})
	assert.False(t, added)
	assert.Equal(t, "Test", actresses[0].FirstName)
	assert.Equal(t, "User", actresses[0].LastName)
	assert.Equal(t, "テスト", actresses[0].JapaneseName)
}

// --- extractActresses: row with no content cell ---

func TestMiss3_ExtractActresses_NoContentCell(t *testing.T) {
	html := `<table><tr><td>Actress</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	assert.Len(t, actresses, 0)
}

// --- extractActresses: row with no label cell ---

func TestMiss3_ExtractActresses_NoLabelCell(t *testing.T) {
	html := `<table><tr><td></td><td><a href="?actress=9005">Test</a></td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	assert.Len(t, actresses, 0)
}

// --- extractActressFromLink: with thumbnail image ---

func TestMiss3_ExtractActressFromLink_WithThumb(t *testing.T) {
	html := `<a href="?actress=9006"><img src="http://pics.dmm.co.jp/thumb.jpg"/>Test Actress</a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	s := newTestDMMScraper()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 9006, actress.DMMID)
	assert.NotEmpty(t, actress.ThumbURL)
}

// --- extractActressFromLink: with parent thumbnail image ---

func TestMiss3_ExtractActressFromLink_ParentThumb(t *testing.T) {
	html := `<p><a href="?actress=9007">Actress Name</a></p>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	s := newTestDMMScraper()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 9007, actress.DMMID)
}

// --- extractActresses: 演者 label (short form) ---

func TestMiss3_ExtractActresses_ShortLabel(t *testing.T) {
	html := `<table><tr><td>演者</td><td>
		<a href="?actress=9008">テスト女優</a>
	</td></tr></table>`
	doc := docFromHTMLDMM(t, html)
	s := newTestDMMScraper()
	actresses := s.extractActresses(context.Background(), doc)
	if assert.Len(t, actresses, 1) {
		assert.Equal(t, "テスト女優", actresses[0].JapaneseName)
	}
}

// --- extractActressFromLink: Japanese name detection ---

func TestMiss3_ExtractActressFromLink_JapaneseNameDetection(t *testing.T) {
	html := `<a href="?actress=9009">さくらこ</a>`
	doc := docFromHTMLDMM(t, html)
	sel := doc.Find("a").First()
	s := newTestDMMScraper()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, "さくらこ", actress.JapaneseName)
	assert.Equal(t, "", actress.FirstName)
	assert.Equal(t, "", actress.LastName)
}

// --- shouldSkipActressName: 購入前 ---

func TestMiss3_ShouldSkipActressName_PurchasePrefix(t *testing.T) {
	assert.True(t, shouldSkipActressName("購入前チェック"))
	assert.True(t, shouldSkipActressName("レビュー投稿"))
	assert.True(t, shouldSkipActressName("ポイント"))
	assert.False(t, shouldSkipActressName("正常な名前"))
}

// --- extractActressThumbURL: img with data-src ---

func TestMiss3_ExtractActressThumbURL_DataSrc(t *testing.T) {
	html := `<a href="?actress=1"><img data-src="http://pics.dmm.co.jp/actress.jpg"/></a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	sel := doc.Find("a").First()
	thumbURL := extractActressThumbURL(sel)
	assert.Equal(t, "http://pics.dmm.co.jp/actress.jpg", thumbURL)
}

// --- extractActressThumbURL: source element srcset ---

func TestMiss3_ExtractActressThumbURL_SourceSrcset(t *testing.T) {
	html := `<a href="?actress=1"><source srcset="http://pics.dmm.co.jp/src.jpg"/></a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	sel := doc.Find("a").First()
	thumbURL := extractActressThumbURL(sel)
	assert.Equal(t, "http://pics.dmm.co.jp/src.jpg", thumbURL)
}
