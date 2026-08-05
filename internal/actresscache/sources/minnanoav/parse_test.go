package minnanoavsource

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fullProfileFixture = `<html><head>
<meta property="og:image" content="/img/act/1001.jpg?v=2">
</head><body>
<h1>山田花子<span>やまだはなこ / Yamada Hanako</span></h1>
<div class="act-profile">
<table>
<tr><td><span>別名</span></td><td><p>花子（はなこ / Hanako）</p></td></tr>
<tr><td><span>別名</span></td><td><p>花子（はなこ / Hanako）</p></td></tr>
<tr><td><span>別名</span></td><td><p>（ のみ）</p></td></tr>
<tr><td><span>別名</span></td><td><p></p></td></tr>
<tr><td><span>生年月日</span></td><td><p>1995-01-01</p></td></tr>
</table>
</div>
<a href="https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abp123/?actress=54321">DMM</a>
<a href="https://example.com/?actress%3D99999">encoded</a>
</body></html>`

func TestParseProfileFullPage(t *testing.T) {
	profile, err := ParseProfile([]byte(fullProfileFixture), "https://www.minnano-av.com/actress123.html")
	require.NoError(t, err)
	assert.Equal(t, 54321, profile.DMMID)
	assert.Equal(t, "山田花子", profile.JapaneseName)
	assert.Equal(t, "Hanako", profile.FirstName)
	assert.Equal(t, "Yamada", profile.LastName)
	assert.Equal(t, []string{"花子"}, profile.Aliases)
	assert.Equal(t, "https://www.minnano-av.com/img/act/1001.jpg", profile.ThumbURL, "relative og:image must resolve against the page URL and drop the query")
}

func TestParseProfileFallsBackToH2(t *testing.T) {
	page := `<html><body><div class="act-profile"><h2>鈴木一郎（すずきいちろう / Suzuki Ichiro）</h2></div></body></html>`
	profile, err := ParseProfile([]byte(page), "https://www.minnano-av.com/x.html")
	require.NoError(t, err)
	assert.Equal(t, "鈴木一郎", profile.JapaneseName)
	assert.Equal(t, "Ichiro", profile.FirstName)
	assert.Equal(t, "Suzuki", profile.LastName)
	assert.Empty(t, profile.ThumbURL)
}

func TestParseProfileEmptyAndBroken(t *testing.T) {
	profile, err := ParseProfile(nil, "https://www.minnano-av.com/x.html")
	require.NoError(t, err)
	assert.Zero(t, profile.DMMID)
	assert.Empty(t, profile.JapaneseName)
	assert.Empty(t, profile.FirstName)
	profile, err = ParseProfile([]byte(`<html><body><h1><span>only reading</span></h1></body></html>`), "https://www.minnano-av.com/x.html")
	require.NoError(t, err)
	assert.Empty(t, profile.Aliases)
}

func TestParseNameEntryEdges(t *testing.T) {
	jp, reading, romaji := parseNameEntry("名前")
	assert.Equal(t, "名前", jp)
	assert.Empty(t, reading)
	assert.Empty(t, romaji)
	jp, reading, romaji = parseNameEntry("名前（よみ）")
	assert.Equal(t, "名前", jp)
	assert.Equal(t, "よみ", reading)
	assert.Empty(t, romaji)
	jp, _, _ = parseNameEntry("")
	assert.Empty(t, jp)
}

func TestStripQueryEdges(t *testing.T) {
	assert.Equal(t, "", stripQuery("  "))
	assert.Equal(t, "https://a.test/x.jpg", stripQuery("https://a.test/x.jpg?v=1"))
	assert.Equal(t, "http://a.test/x", stripQuery("http://a.test/x"))
}

func TestParseNameEntryReadingOnlyAndEmptyInner(t *testing.T) {
	jp, reading, romaji := parseNameEntry("名（よみ）")
	assert.Equal(t, "名", jp)
	assert.Equal(t, "よみ", reading)
	assert.Empty(t, romaji)

	jp, reading, romaji = parseNameEntry("名（）")
	assert.Equal(t, "名", jp)
	assert.Empty(t, reading)
	assert.Empty(t, romaji)
}

func TestSplitRomajiNameSingleField(t *testing.T) {
	first, last, ok := splitRomajiName("Mononym")
	assert.False(t, ok)
	assert.Empty(t, first)
	assert.Empty(t, last)
}

func TestDirectTextSkipsNodeWithoutChildren(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<h1><span>inner</span></h1>"))
	require.NoError(t, err)
	// h1 with only an element child (no direct text) yields empty.
	assert.Empty(t, directText(doc.Find("h1")))
	assert.Empty(t, directText(nil))
}

func TestParseDMMActressIDKeepsScanningPastBrokenLinks(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="https://x.test/?actress=abc">nonnumeric</a><a href="https://x.test/?a=b">none</a><a href="https://www.dmm.co.jp/?actress=42">hit</a>`))
	require.NoError(t, err)
	assert.Equal(t, 42, parseDMMActressID(doc))
	assert.Zero(t, parseDMMActressID(nil))
}

func TestParseDMMActressIDAcceptsArticleStyleLinks(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="https://al.dmm.co.jp/?lurl=https%3A%2F%2Fvideo.dmm.co.jp%2Fav%2Flist%2F%3Farticle%3Dactress%26id%3D778899%2F">encoded</a><a href="/mono/dvd/-/list/=/article=actress/id=5567/">direct path</a>`))
	require.NoError(t, err)
	assert.Equal(t, 5567, parseDMMActressID(doc))

	// The same article form inside a DMM affiliate redirect decodes.
	encoded, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="https://al.dmm.co.jp/?lurl=https%3A%2F%2Fvideo.dmm.co.jp%2Fav%2Flist%2F%3D%2Farticle%3Dactress%2Fid%3D778899%2F">wrapped</a>`))
	require.NoError(t, err)
	assert.Equal(t, 778899, parseDMMActressID(encoded))

	// A non-DMM domain carrying the same numeric params must NOT mint an ID
	// (proxied affiliate walls, other vendors, and mirrors otherwise get
	// journaled with a bogus authoritative DMM anchor).
	nonDMM, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="https://other-vendor.example/items?actress=42">no</a><a href="https://redirect.example/?u=https%3A%2F%2Fvideo.dmm.co.jp%2Fav%2Flist%2F%3D%2Farticle%3Dactress%2Fid%3D55%2F">enc-scam</a>`))
	require.NoError(t, err)
	assert.Zero(t, parseDMMActressID(nonDMM))
}

func TestParseProfileSkipsEmptyAndDuplicateAliases(t *testing.T) {
	page := `<html><body><h1>山田花子<span>やまだ / Yamada Hanako</span></h1><div class="act-profile"><table><tr><td><span>別名</span></td><td><p>山田花子（やまだ / dup）</p></td></tr></table></div></body></html>`
	profile, err := ParseProfile([]byte(page), "https://www.minnano-av.com/x.html")
	require.NoError(t, err)
	assert.Empty(t, profile.Aliases, "alias equal to the primary name is skipped")
}

func TestParseActressPageNilDocument(t *testing.T) {
	page := parseActressPage(nil, "https://x.test")
	assert.Empty(t, page.primaryName)
	assert.Empty(t, page.thumbURL)
}
