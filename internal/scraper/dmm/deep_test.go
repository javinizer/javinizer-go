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

// --- urlPriority deep tests ---

func TestURLPriorityDeep_AllBranches(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		expected int
	}{
		{"mono dvd", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abc123/", 350},
		{"digital videoa", "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=abc123/", 300},
		{"digital videoc", "https://www.dmm.co.jp/digital/videoc/-/detail/=/cid=abc123/", 300},
		{"amateur", "https://video.dmm.co.jp/amateur/content/?id=abc123", 250},
		{"av streaming", "https://video.dmm.co.jp/av/content/?id=abc123", 200},
		{"monthly premium", "https://www.dmm.co.jp/monthly/premium/-/detail/=/cid=abc123/", 150},
		{"monthly standard", "https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=abc123/", 100},
		{"rental", "https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=abc123/", 0},
		{"unknown path", "https://www.dmm.co.jp/unknown/path/", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, urlPriority(tt.rawURL))
		})
	}
}

// --- maxPriority tests ---

func TestMaxPriorityDeepUncovered(t *testing.T) {
	tests := []struct {
		name       string
		candidates []urlCandidate
		expected   int
	}{
		{"empty", nil, 0},
		{"single", []urlCandidate{{priority: 100}}, 100},
		{"multiple", []urlCandidate{{priority: 50}, {priority: 300}, {priority: 100}}, 300},
		{"all zero", []urlCandidate{{priority: 0}, {priority: 0}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, maxPriority(tt.candidates))
		})
	}
}

// --- sortCandidates deep tests ---

func TestSortCandidatesDeepUncovered(t *testing.T) {
	candidates := []urlCandidate{
		{url: "a", priority: 100, idLength: 10},
		{url: "b", priority: 300, idLength: 5},
		{url: "c", priority: 300, idLength: 8},
		{url: "d", priority: 200, idLength: 3},
	}
	sortCandidates(candidates)
	assert.Equal(t, 300, candidates[0].priority)
	assert.Equal(t, 300, candidates[1].priority)
	// Same priority: shorter idLength first
	assert.Equal(t, 5, candidates[0].idLength)
	assert.Equal(t, 8, candidates[1].idLength)
	assert.Equal(t, 200, candidates[2].priority)
	assert.Equal(t, 100, candidates[3].priority)
}

// --- extractContentIDCandidates deep tests ---

func TestExtractContentIDCandidatesDeep_CidPattern(t *testing.T) {
	html := `<html><body>
		<a href="/digital/videoa/-/detail/=/cid=ipx00535/">IPX-535</a>
		<a href="/mono/dvd/-/detail/=/cid=4sone860/">SONE-860</a>
		<a href="/monthly/standard/-/detail/=/cid=abp00420r/">ABP-420 rental</a>
		<a href="https://video.dmm.co.jp/av/content/?id=oreco183">FC2</a>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	candidates := extractContentIDCandidates(doc, []string{"ipx535", "sone860"})
	// Should find at least the cid= matches
	assert.NotEmpty(t, candidates)
}

func TestExtractContentIDCandidatesDeep_NilDoc(t *testing.T) {
	candidates := extractContentIDCandidates(nil, []string{"test"})
	assert.Empty(t, candidates)
}

func TestExtractContentIDCandidatesDeep_EmptySearchIDs(t *testing.T) {
	html := `<html><body><a href="/digital/videoa/-/detail/=/cid=ipx00535/">Link</a></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	candidates := extractContentIDCandidates(doc, nil)
	assert.Empty(t, candidates)
}

func TestExtractContentIDCandidatesDeep_VariantSuffixMatch(t *testing.T) {
	html := `<html><body>
		<a href="/digital/videoa/-/detail/=/cid=akdl229a/">AKDL-229a</a>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	candidates := extractContentIDCandidates(doc, []string{"akdl229"})
	assert.NotEmpty(t, candidates)
}

// --- normalizeContentID more edge cases ---

func TestNormalizeContentIDDeep_AmateurDetection(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected string
	}{
		{"oreco amateur", "oreco183", "oreco183"},
		{"luxu amateur", "luxu456", "luxu456"},
		{"maan amateur", "maan789", "maan789"},
		{"standard ABP no hyphen", "abp420", "abp00420"},
		{"standard with hyphen", "ABP-420", "abp00420"},
		{"standard IPX", "IPX-535", "ipx00535"},
		{"already padded", "ipx00535", "ipx00535"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeContentID(tt.id))
		})
	}
}

// --- normalizeID more edge cases ---

func TestNormalizeIDDeep_FiveDigitNumber(t *testing.T) {
	// t28123 -> T-28123 (5-digit number preserved)
	assert.Equal(t, "T-28123", normalizeID("t28123"))
}

func TestNormalizeIDDeep_AllZeros(t *testing.T) {
	assert.Equal(t, "ABP-000", normalizeID("abp00000"))
}

func TestNormalizeIDDeep_PrefixStripping(t *testing.T) {
	assert.Equal(t, "SMKCX-003", normalizeID("h_1472smkcx003"))
	assert.Equal(t, "MDB-087", normalizeID("61mdb087"))
}

// --- stripRentalSuffix more edge cases ---

func TestStripRentalSuffixDeep_Uncovered(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"digits+r suffix", "123r", "123"},
		{"letter+r no strip", "abcr", "abcr"},
		{"single char", "r", "r"},
		{"two letters", "ar", "ar"},
		{"uppercase R", "123R", "123"},
		{"no suffix", "abc123", "abc123"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripRentalSuffix(tt.input))
		})
	}
}

// --- uniqueNonEmptyStrings more tests ---

func TestUniqueNonEmptyStringsDeep(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{"all empty", []string{"", " ", "  "}, []string{}},
		{"duplicates", []string{"a", "a", "b"}, []string{"a", "b"}},
		{"whitespace trim", []string{" a ", "a", " b "}, []string{"a", "b"}},
		{"nil input", nil, []string{}},
		{"preserves order", []string{"c", "b", "a"}, []string{"c", "b", "a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, uniqueNonEmptyStrings(tt.input))
		})
	}
}

// --- buildResolveContentIDSearchQueries ---

func TestBuildResolveContentIDSearchQueriesDeep(t *testing.T) {
	queries := buildResolveContentIDSearchQueries("ABP-420", "abp00420")
	assert.NotEmpty(t, queries)
	// Should include both the stripped and padded forms
	found := false
	for _, q := range queries {
		if q == "abp420" {
			found = true
		}
	}
	assert.True(t, found)
}

// --- extractContentIDFromURL more tests ---

func TestExtractContentIDFromURLDeep(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"cid param", "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/", "ipx00535"},
		{"id param", "https://video.dmm.co.jp/av/content/?id=oreco183", "oreco183"},
		{"no match", "https://www.dmm.co.jp/top/", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractContentIDFromURL(tt.url))
		})
	}
}

// --- matchesWithVariantSuffix more tests ---

func TestMatchesWithVariantSuffixDeep(t *testing.T) {
	tests := []struct {
		name    string
		urlCID  string
		ids     []string
		matches bool
	}{
		{"exact match", "abc123", []string{"abc123"}, true},
		{"variant a", "abc123a", []string{"abc123"}, true},
		{"variant z", "abc123z", []string{"abc123"}, true},
		{"variant A uppercase no match", "abc123A", []string{"abc123"}, false}, // uppercase shouldn't match
		{"two char suffix no match", "abc123ab", []string{"abc123"}, false},
		{"no match", "xyz789", []string{"abc123"}, false},
		{"multiple ids", "abc123b", []string{"xyz789", "abc123"}, true},
		{"empty urlCID", "", []string{"abc123"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.matches, matchesWithVariantSuffix(tt.urlCID, tt.ids...))
		})
	}
}

// --- hiraganaToRomaji deep tests ---

func TestHiraganaToRomajiDeep_CombinedChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple kya", "きゃ", "kya"},
		{"simple sha", "しゃ", "sya"}, // Nihon-shiki: si->s, ya
		{"simple chu", "ちゅ", "tyu"}, // Nihon-shiki: ti->t, yu
		{"small tsu gemination", "かった", "katta"},
		{"mixed sentence", "しらかみ", "sirakami"},
		{"empty", "", ""},
		{"unknown char", "Z", "Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hiraganaToRomaji(tt.input))
		})
	}
}

// --- normalizedContentIDWithoutPadding ---

func TestNormalizedContentIDWithoutPaddingDeep(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"padded id", "abp00420", "abp420"},
		{"no padding", "oreco183", "oreco183"},
		{"empty", "", ""},
		{"whitespace", "  ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizedContentIDWithoutPadding(tt.input))
		})
	}
}

// --- extractActressID ---

func TestExtractActressIDDeep(t *testing.T) {
	tests := []struct {
		name     string
		href     string
		expected int
	}{
		{"actress param", "?actress=12345", 12345},
		{"actress param with prefix", "/something?actress=67890", 67890},
		{"article actress", "/article=actress/id=11111", 11111},
		{"no match", "/some/path", 0},
		{"empty", "", 0},
		{"non-numeric", "?actress=abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractActressID(tt.href))
		})
	}
}

// --- cleanActressName ---

func TestCleanActressNameDeep(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"parenthetical", "Yui (Actress)", "Yui"},
		{"japanese parenthetical", "あい（女優）", "あい"},
		{"normal", "Yui Hatano", "Yui Hatano"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanActressName(tt.input))
		})
	}
}

// --- shouldSkipActressName ---

func TestShouldSkipActressNameDeep(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty", "", true},
		{"purchase pre-check", "購入前確認", true},
		{"review", "レビュー投稿", true},
		{"points", "ポイント", true},
		{"valid name", "Yui Hatano", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shouldSkipActressName(tt.input))
		})
	}
}

// --- upsertActressInfo deep tests ---

func TestUpsertActressInfoDeep(t *testing.T) {
	t.Run("merge existing actress", func(t *testing.T) {
		actresses := []models.ActressInfo{
			{DMMID: 1, JapaneseName: "あい", ThumbURL: ""},
		}
		indexByID := map[int]int{1: 0}

		// Upsert same ID with new thumbURL
		result := upsertActressInfo(&actresses, indexByID, models.ActressInfo{
			DMMID:    1,
			ThumbURL: "https://example.com/thumb.jpg",
		})
		assert.False(t, result) // false = updated existing
		assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)
		assert.Equal(t, "あい", actresses[0].JapaneseName)
	})

	t.Run("add new actress", func(t *testing.T) {
		actresses := []models.ActressInfo{}
		indexByID := map[int]int{}

		result := upsertActressInfo(&actresses, indexByID, models.ActressInfo{
			DMMID:        2,
			JapaneseName: "ゆい",
		})
		assert.True(t, result) // true = new entry
		assert.Len(t, actresses, 1)
		assert.Equal(t, 2, actresses[0].DMMID)
	})

	t.Run("zero DMMID ignored", func(t *testing.T) {
		actresses := []models.ActressInfo{}
		indexByID := map[int]int{}

		result := upsertActressInfo(&actresses, indexByID, models.ActressInfo{
			DMMID: 0,
		})
		assert.False(t, result)
		assert.Empty(t, actresses)
	})

	t.Run("merge multiple fields", func(t *testing.T) {
		actresses := []models.ActressInfo{
			{DMMID: 1, FirstName: "Yui", ThumbURL: ""},
		}
		indexByID := map[int]int{1: 0}

		result := upsertActressInfo(&actresses, indexByID, models.ActressInfo{
			DMMID:        1,
			LastName:     "Hatano",
			ThumbURL:     "https://example.com/thumb.jpg",
			JapaneseName: "波多野結衣",
		})
		assert.False(t, result)
		assert.Equal(t, "Yui", actresses[0].FirstName)
		assert.Equal(t, "Hatano", actresses[0].LastName)
		assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)
		assert.Equal(t, "波多野結衣", actresses[0].JapaneseName)
	})
}

// --- normalizeActressThumbURL deep tests ---

func TestNormalizeActressThumbURLDeep(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"relative path", "/pics/act/abc.jpg", "https://video.dmm.co.jp/pics/act/abc.jpg"},
		{"already absolute", "https://pics.dmm.co.jp/abc.jpg", "https://pics.dmm.co.jp/abc.jpg"},
		{"srcset with comma", "https://pics.dmm.co.jp/abc.jpg 2x, https://pics.dmm.co.jp/abc2.jpg 1x", "https://pics.dmm.co.jp/abc.jpg"},
		{"whitespace in url", "https://pics.dmm.co.jp/abc.jpg ", "https://pics.dmm.co.jp/abc.jpg"},
		{"amp escape", "https://pics.dmm.co.jp/abc.jpg&amp;x=1", "https://pics.dmm.co.jp/abc.jpg&x=1"},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeActressThumbURL(tt.input)
			if tt.expected == "" {
				assert.Equal(t, "", result)
			} else {
				assert.Contains(t, result, strings.TrimPrefix(tt.expected, "https://"))
			}
		})
	}
}

// --- extractBackgroundImageURL deep tests ---

func TestExtractBackgroundImageURLDeep(t *testing.T) {
	tests := []struct {
		name     string
		style    string
		expected string
	}{
		{"standard url", "background-image: url(https://pics.dmm.co.jp/abc.jpg)", "https://pics.dmm.co.jp/abc.jpg"},
		{"quoted url", `background-image: url("https://pics.dmm.co.jp/abc.jpg")`, "https://pics.dmm.co.jp/abc.jpg"},
		{"single quoted", `background-image: url('https://pics.dmm.co.jp/abc.jpg')`, "https://pics.dmm.co.jp/abc.jpg"},
		{"protocol relative", "background-image: url(//pics.dmm.co.jp/abc.jpg)", "//pics.dmm.co.jp/abc.jpg"},
		{"no background-image", "color: red", ""},
		{"no url()", "background-image: none", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractBackgroundImageURL(tt.style))
		})
	}
}

// --- normalizeTrailerURL deep tests ---

func TestNormalizeTrailerURLDeep(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"protocol relative", "//example.com/video.mp4", "https://example.com/video.mp4"},
		{"already absolute", "https://example.com/video.mp4", "https://example.com/video.mp4"},
		{"escaped slashes", `https:\/\/example.com\/video.mp4`, "https://example.com/video.mp4"},
		{"query params", "https://example.com/video.mp4?t=1", "https://example.com/video.mp4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeTrailerURL(tt.input))
		})
	}
}

// --- buildScopedActressSelector ---

func TestBuildScopedActressSelectorDeep(t *testing.T) {
	result := buildScopedActressSelector("table")
	assert.Contains(t, result, "table a[href*='?actress=']")
	assert.Contains(t, result, "table a[href*='&actress=']")
	assert.Contains(t, result, "table a[href*='/article=actress/id=']")
}

// --- findNearestActressContainer ---

func TestFindNearestActressContainerDeep(t *testing.T) {
	html := `<div><h2>test</h2><p><a href="?actress=1">Actress</a></p></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	heading := doc.Find("h2")
	container := findNearestActressContainer(heading)
	assert.NotNil(t, container)

	// Test nil input
	result := findNearestActressContainer(nil)
	assert.Nil(t, result)
}

// --- parseHTML extraction from structured HTML ---

func TestParseHTMLDeep_MonthlyPageSkipsActress(t *testing.T) {
	html := `<html><body>
		<div id="title" class="item">Test Movie</div>
		<table><tr><td>Genre:</td><td><a href="#">Drama</a></td></tr></table>
		<tr><td>Actress</td><td><a href="?actress=123">Test Actress</a></td></tr>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	trueVal := true
	s := newScraper(&models.ScraperSettings{Enabled: true, RateLimit: 100, ScrapeActress: &trueVal}, nil, models.FlareSolverrConfig{}, dmmOptions{})
	result, err := s.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=test123/")
	require.NoError(t, err)
	assert.Empty(t, result.Actresses, "monthly pages should not extract actresses")
}
