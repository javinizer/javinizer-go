package javlibrary

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestExtractTitleDeep2_WithSuffix(t *testing.T) {
	html := `<title>IPX-123 Test Movie Title - JAVLibrary</title>`
	s := &scraper{}
	title := s.extractTitle(html, "IPX-123")
	assert.Equal(t, "Test Movie Title", title)
}

func TestExtractTitleDeep2_WithoutSuffix(t *testing.T) {
	html := `<title>Just a title</title>`
	s := &scraper{}
	title := s.extractTitle(html, "IPX-123")
	assert.Equal(t, "Just a title", title)
}

func TestExtractTitleDeep2_NoTitle(t *testing.T) {
	html := `<html><body></body></html>`
	s := &scraper{}
	title := s.extractTitle(html, "IPX-123")
	assert.Equal(t, "", title)
}

func TestExtractCoverURLDeep2_JacketImg(t *testing.T) {
	html := `<img id="video_jacket_img" src="https://example.com/cover.jpg">`
	s := &scraper{}
	url := s.extractCoverURL(html)
	assert.Equal(t, "https://example.com/cover.jpg", url)
}

func TestExtractCoverURLDeep2_JacketLink(t *testing.T) {
	html := `<a id="video_jacket" href="//pics.dmm.co.jp/cover.jpg">`
	s := &scraper{}
	url := s.extractCoverURL(html)
	assert.Equal(t, "https://pics.dmm.co.jp/cover.jpg", url)
}

func TestExtractCoverURLDeep2_NoCover(t *testing.T) {
	s := &scraper{}
	assert.Equal(t, "", s.extractCoverURL("<html></html>"))
}

func TestExtractReleaseDateDeep2_Valid(t *testing.T) {
	html := `<div id="video_date"><span class="text">2024-03-15</span></div>`
	s := &scraper{}
	date := s.extractReleaseDate(html)
	assert.NotNil(t, date)
	assert.Equal(t, 2024, date.Year())
	assert.Equal(t, time.March, date.Month())
	assert.Equal(t, 15, date.Day())
}

func TestExtractReleaseDateDeep2_Fallback(t *testing.T) {
	html := `Release Date: 2024-06-01`
	s := &scraper{}
	date := s.extractReleaseDate(html)
	assert.NotNil(t, date)
	assert.Equal(t, 2024, date.Year())
}

func TestExtractReleaseDateDeep2_NoDate(t *testing.T) {
	s := &scraper{}
	assert.Nil(t, s.extractReleaseDate("<html></html>"))
}

func TestExtractRuntimeDeep2_Valid(t *testing.T) {
	html := `<div id="video_length"><span class="text">120</span></div>`
	s := &scraper{}
	runtime := s.extractRuntime(html)
	assert.Equal(t, 120, runtime)
}

func TestExtractRuntimeDeep2_Fallback(t *testing.T) {
	html := `Length: 90 min`
	s := &scraper{}
	runtime := s.extractRuntime(html)
	assert.Equal(t, 90, runtime)
}

func TestExtractRuntimeDeep2_NoRuntime(t *testing.T) {
	s := &scraper{}
	assert.Equal(t, 0, s.extractRuntime("<html></html>"))
}

func TestExtractFieldDeep2_Director(t *testing.T) {
	html := `<div id="video_director"><a>Test Director</a></div>`
	s := &scraper{}
	assert.Equal(t, "Test Director", s.extractField(html, "video_director"))
}

func TestExtractFieldDeep2_Maker(t *testing.T) {
	html := `<div id="video_maker"><a>Test Maker</a></div>`
	s := &scraper{}
	assert.Equal(t, "Test Maker", s.extractField(html, "video_maker"))
}

func TestExtractFieldDeep2_NotFound(t *testing.T) {
	s := &scraper{}
	assert.Equal(t, "", s.extractField("<html></html>", "video_director"))
}

func TestExtractGenresDeep2(t *testing.T) {
	html := `<span class="genre"><a rel="tag">Genre1</a></span><span class="genre"><a rel="tag">Genre2</a></span><span class="genre"><a rel="tag">Genre1</a></span>`
	s := &scraper{}
	genres := s.extractGenres(html)
	assert.Equal(t, []string{"Genre1", "Genre2"}, genres)
}

func TestExtractActressesDeep2(t *testing.T) {
	html := `<span class="star"><a rel="tag">Jane Smith</a></span><span class="star"><a rel="tag">田中麻美</a></span>`
	s := &scraper{}
	actresses := s.extractActresses(html)
	assert.Len(t, actresses, 2)
	assert.Equal(t, "Jane", actresses[0].FirstName)
	assert.Equal(t, "Smith", actresses[0].LastName)
	assert.Equal(t, "田中麻美", actresses[1].FirstName) // single Japanese name goes to FirstName
}

func TestExtractSeriesDeep2(t *testing.T) {
	html := `<div id="video_series"><a>Test Series</a></div>`
	s := &scraper{}
	assert.Equal(t, "Test Series", s.extractSeries(html))
}

func TestExtractSeriesDeep2_Fallback(t *testing.T) {
	html := `Series: <a>Test Series Fallback</a>`
	s := &scraper{}
	assert.Equal(t, "Test Series Fallback", s.extractSeries(html))
}

func TestExtractRatingDeep2_Valid(t *testing.T) {
	html := `<div id="video_rating"><span class="num">4.5</span> / 5.0</div>`
	s := &scraper{}
	rating := s.extractRating(html, mustParseDoc(t, html))
	assert.NotNil(t, rating)
	assert.Equal(t, 4.5, rating.Score)
}

func TestExtractRatingDeep2_Fallback(t *testing.T) {
	// The goquery fallback extracts rating from #video_rating span.num
	html := `<div id="video_rating"><span class="num">3.5</span> / 5.0</div>`
	s := &scraper{}
	rating := s.extractRating(html, mustParseDoc(t, html))
	assert.NotNil(t, rating)
	assert.Equal(t, 3.5, rating.Score)
}

func TestExtractRatingDeep2_NoRating(t *testing.T) {
	s := &scraper{}
	assert.Nil(t, s.extractRating("<html></html>", mustParseDoc(t, "<html></html>")))
}

func TestExtractTrailerURLDeep2_Mp4(t *testing.T) {
	html := `<a href="https://example.com/sample_movie.mp4">Trailer</a>`
	s := &scraper{}
	url := s.extractTrailerURL(html)
	assert.Contains(t, url, "sample_movie.mp4")
}

func TestExtractTrailerURLDeep2_NoTrailer(t *testing.T) {
	s := &scraper{}
	assert.Equal(t, "", s.extractTrailerURL("<html></html>"))
}

func TestExtractDescriptionDeep2_MetaDescription(t *testing.T) {
	html := `<meta name="description" content="This is a movie description">`
	s := &scraper{}
	desc := s.extractDescription(html)
	assert.Equal(t, "This is a movie description", desc)
}

func TestExtractDescriptionDeep2_NoDescription(t *testing.T) {
	s := &scraper{}
	assert.Equal(t, "", s.extractDescription("<html></html>"))
}

func TestExtractMovieURLFromHTMLDeep2_VideoThumbDiv(t *testing.T) {
	html := `<div class="video" id="vid_javliat76u"><div class="id">ONED-025</div></div>`
	s := &scraper{}
	url := s.extractMovieURLFromHTML(html, "ONED-025")
	assert.Contains(t, url, "?v=javliat76u")
}

func TestExtractMovieURLFromHTMLDeep2_NoMatch(t *testing.T) {
	s := &scraper{}
	url := s.extractMovieURLFromHTML("<html></html>", "NONEXISTENT-999")
	assert.Equal(t, "", url)
}

func TestIsValidLanguageDeep2(t *testing.T) {
	assert.True(t, isValidLanguage("en"))
	assert.True(t, isValidLanguage("ja"))
	assert.True(t, isValidLanguage("cn"))
	assert.True(t, isValidLanguage("tw"))
	assert.False(t, isValidLanguage("fr"))
	assert.False(t, isValidLanguage(""))
}

func TestResolveDownloadProxyForHostDeep2(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	_, _, ok := s.ResolveDownloadProxyForHost("javlibrary.com")
	assert.True(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("c.impact.jp")
	assert.True(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
}

func TestExtractScreenshotURLsDeep2_FiltersLoading(t *testing.T) {
	html := `<img src="https://example.com/loading.jpg"><img src="https://c.impact.jp/abc/01.jpg">`
	s := &scraper{}
	urls := s.extractScreenshotURLs(html)
	for _, u := range urls {
		assert.NotContains(t, u, "loading")
	}
}

func TestExtractScreenshotURLsDeep2_FiltersCover(t *testing.T) {
	html := `<img src="https://pics.dmm.co.jp/digital/video/abc/abc-pl.jpg">`
	s := &scraper{}
	urls := s.extractScreenshotURLs(html)
	for _, u := range urls {
		assert.NotContains(t, u, "pl.jpg")
		assert.NotContains(t, u, "ps.jpg")
	}
}
