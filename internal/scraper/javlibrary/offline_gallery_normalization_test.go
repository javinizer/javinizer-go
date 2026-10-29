package javlibrary

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type galleryBody struct {
	io.Reader
	closes int
}

func (b *galleryBody) Close() error { b.closes++; return nil }

func TestSearchGalleryNormalizationExcludesPoster(t *testing.T) {
	const page = "https://www.javlibrary.com/en/javfixture001.html"
	const media = "https://awsimgsrc.dmm.com/dig/digital/amateur/fixture001/fixture001"
	const other = "https://images.example.test/sample-2.jpg"
	html := `<title>Fixture gallery - JAVLibrary</title><div id="video_info"></div>
 <div id="video_id"><td class="text">FIX-001</td></div>
 <img id="video_jacket_img" src="` + media + `pl.jpg">
 <img data-src="` + media + `PS.jpg"><img data-src="` + media + `PL.jpg">
 <img data-src="` + media + `jp-1.jpg"><img data-src="` + media + `ps.jpg">
 <img data-src="` + other + `">`
	s := newScraper(&models.ScraperSettings{Enabled: true, Language: "en"}, nil, models.FlareSolverrConfig{})
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	var requests []string
	var bodies []*galleryBody
	s.client.SetTransport(nestingLimitTransport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.String())
		status, content := http.StatusOK, html
		switch {
		case r.Method == http.MethodGet && r.URL.String() == page:
		case r.Method == http.MethodGet && r.URL.String() == media+"ps.jpg":
			status, content = http.StatusNotFound, "not found"
		default:
			return nil, fmt.Errorf("unexpected owned fixture request: %s %s", r.Method, r.URL)
		}
		body := &galleryBody{Reader: strings.NewReader(content)}
		bodies = append(bodies, body)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: body, Request: r}, nil
	}))
	result, err := s.Search(context.Background(), page)
	require.NoError(t, err)
	require.Equal(t, []string{media + "jp-1.jpg", other}, result.ScreenshotURL)
	require.Equal(t, "FIX-001", result.ID)
	require.Equal(t, "FIX-001", result.ContentID)
	require.Equal(t, "Fixture gallery", result.Title)
	require.Equal(t, "en", result.Language)
	require.Equal(t, page, result.SourceURL)
	require.Equal(t, media+"pl.jpg", result.CoverURL)
	require.Equal(t, result.CoverURL, result.PosterURL)
	require.True(t, result.ShouldCropPoster)
	require.Equal(t, []string{"GET " + page, "GET " + media + "ps.jpg"}, requests)
	for _, body := range bodies {
		require.Equal(t, 1, body.closes)
	}
}
