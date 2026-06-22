package libredmm

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests use the documented PayloadToResult seam to verify JSON→Result conversion
// instead of reaching into unexported helper functions.

func TestPayloadToResult_NilPayload(t *testing.T) {
	client := &http.Client{}
	result := PayloadToResult(nil, "https://www.libredmm.com/movies/abc123/", "fallback", client)
	assert.Equal(t, "libredmm", result.Source)
	assert.Equal(t, "ja", result.Language)
	assert.Equal(t, "abc123", result.ID)
}

func TestPayloadToResult_FullPayload(t *testing.T) {
	client := &http.Client{}
	payload := &moviePayload{
		Title:             "Test Movie",
		NormalizedID:      "IPX-535",
		Date:              "2023-01-15T00:00:00Z",
		Volume:            7200,
		Directors:         []string{"Director A"},
		Makers:            []string{"Maker A"},
		Labels:            []string{"Label A"},
		Genres:            []string{"Drama", "Romance"},
		Description:       "A test description",
		CoverImageURL:     "https://pics.dmm.co.jp/cover.jpg",
		ThumbnailImageURL: "https://pics.dmm.co.jp/thumb.jpg",
		Review:            4.5,
		Actresses: []actressPayload{
			{Name: "波多野結衣", ImageURL: "https://example.com/actress1.jpg"},
			{Name: "Jane Smith", ImageURL: "https://example.com/actress2.jpg"},
		},
		SampleImageURLs: []string{"https://pics.dmm.co.jp/sample1.jpg", "https://pics.dmm.co.jp/sample2.jpg"},
	}
	result := PayloadToResult(payload, "https://www.libredmm.com/movies/ipx535/", "IPX-535", client)
	assert.Equal(t, "Test Movie", result.Title)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, 120, result.Runtime)
	assert.Equal(t, "Director A", result.Director)
	assert.Equal(t, "Maker A", result.Maker)
	assert.Equal(t, "Label A", result.Label)
	assert.Equal(t, []string{"Drama", "Romance"}, result.Genres)
	assert.Equal(t, "A test description", result.Description)
	assert.NotNil(t, result.ReleaseDate)
	assert.NotNil(t, result.Rating)
	assert.Equal(t, 4.5, result.Rating.Score)
	assert.Len(t, result.Actresses, 2)
}

func TestPayloadToResult_VolumeZero(t *testing.T) {
	client := &http.Client{}
	payload := &moviePayload{NormalizedID: "TEST-001", Volume: 0}
	result := PayloadToResult(payload, "https://www.libredmm.com/movies/test/", "TEST-001", client)
	assert.Equal(t, 0, result.Runtime)
}

func TestMoviePayloadJSONParsing(t *testing.T) {
	jsonData := `{
		"err": "",
		"actresses": [{"name": "Actress1", "image_url": "https://example.com/a.jpg"}],
		"cover_image_url": "https://pics.dmm.co.jp/cover.jpg",
		"date": "2023-06-15",
		"description": "Test description",
		"directors": ["Director1"],
		"genres": ["Drama"],
		"labels": ["Label1"],
		"makers": ["Maker1"],
		"normalized_id": "IPX-535",
		"review": 4.2,
		"subtitle": "",
		"thumbnail_image_url": "https://pics.dmm.co.jp/thumb.jpg",
		"title": "Test Movie Title",
		"url": "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
		"volume": 5400,
		"sample_image_urls": ["https://pics.dmm.co.jp/sample1.jpg"]
	}`

	var payload moviePayload
	err := json.Unmarshal([]byte(jsonData), &payload)
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", payload.NormalizedID)
	assert.Equal(t, "Test Movie Title", payload.Title)
	assert.Equal(t, 5400, payload.Volume)
	assert.Equal(t, 4.2, payload.Review)
	assert.Len(t, payload.Actresses, 1)
	assert.Equal(t, "Actress1", payload.Actresses[0].Name)
}

func TestExtractActressThumbFromHTML(t *testing.T) {
	html := `<div>
		<a href="?actress=123"><img src="https://pics.dmm.co.jp/actress1.jpg">Name1</a>
		<a href="?actress=456"><img data-src="https://pics.dmm.co.jp/actress2.jpg">Name2</a>
	</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	count := 0
	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		img := sel.Find("img").First()
		if img.Length() > 0 {
			src, _ := img.Attr("src")
			dataSrc, _ := img.Attr("data-src")
			if src != "" || dataSrc != "" {
				count++
			}
		}
	})
	assert.Equal(t, 2, count)
}
