package dmm

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestOfflineMetadataCastRejectsInvalidIDsAndMergesDuplicates(t *testing.T) {
	const invalid = `<a href="?actress=0">Zero ID</a><a href="?actress=invalid">Invalid ID</a>`
	const first = `<a href="?actress=123"><img src="https://pics.dmm.co.jp/mono/actjpgs/fixture.jpg">Yui Hatano</a>`
	const duplicate = `<a href="?actress=123"><img src="https://pics.dmm.co.jp/mono/actjpgs/fixture.jpg">波多野結衣</a>`
	for _, tc := range []struct {
		name, body string
		legacy     bool
	}{
		{"legacy table header", `<table><tr><th>Details</th></tr><tr><td>出演者</td><td>` + invalid + first + duplicate + `</td></tr></table>`, true},
		{"explicit streaming cast", `<div data-e2eid="actress-information">` + invalid + first + duplicate + `</div>`, false},
		{"heading streaming cast", `<section><h2>Unrelated information</h2></section><section><h2>この商品に出演しているAV女優</h2>` + invalid + first + duplicate + `</section>`, false},
		{"metadata fallback", `<table><tr><td>出演者</td><td>` + invalid + first + duplicate + `</td></tr></table>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestDMMScraper(t)
			requests := 0
			s.client.SetTransport(dmmFixtureTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				t.Errorf("cast with supplied thumbnails must not fetch %s", r.URL)
				return nil, fmt.Errorf("unexpected cast request")
			}))
			s.thumbnailClient.SetTransport(s.client.GetClient().Transport)
			doc := boundaryDocument(t, tc.body)
			var got []models.ActressInfo
			want := models.ActressInfo{DMMID: 123, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/fixture.jpg"}
			if tc.legacy {
				got = s.extractActresses(context.Background(), doc)
				want.FirstName, want.LastName = want.LastName, want.FirstName
			} else {
				got = s.extractActressesFromStreamingPage(context.Background(), doc)
			}
			require.Equal(t, []models.ActressInfo{want}, got, "invalid identities are discarded and duplicate metadata enriches one cast entry")
			require.Zero(t, requests)
		})
	}
}

func TestOfflineMetadataCoverFallbackPrecedence(t *testing.T) {
	s := &scraper{}
	doc := boundaryDocument(t, `<div style="background-image: none"></div><div style="background-image: url('//pics.dmm.co.jp/digital/video/abc00123/abc00123pl.jpg')"></div><div style="background-image: url('//pics.dmm.co.jp/digital/video/xyz00456/xyz00456pl.jpg')"></div><img src="https://pics.dmm.co.jp/digital/video/other001/other001pl.jpg">`)
	require.Equal(t, "https://awsimgsrc.dmm.com/dig/digital/video/abc00123/abc00123pl.jpg", s.extractCoverURLNewSite(doc, "ignored001"))

	doc = boundaryDocument(t, `<img src="logo.png"><img src="avatar.png"><img src="icon.png"><img src="banner.png"><img src="button.png"><img src="other.png">`)
	require.Equal(t, "https://pics.dmm.co.jp/digital/amateur/oreco183/oreco183jp.jpg", s.extractCoverURLNewSite(doc, "ORECO183"), "unrelated images must not replace the content-ID cover")
	require.Empty(t, s.extractCoverURLNewSite(doc, ""), "without an identity, unrelated images are not covers")
}

func TestOfflineMetadataTrailerFirstValidAlternativeWins(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"onclick", `<a onclick="video('not-a-url')"></a><a onclick="video('https://media.invalid/first.mp4?token=fixture')"></a><a onclick="video('https://media.invalid/second.mp4')"></a>`},
		{"script", `<script>{"sampleUrl":"not-a-url"}</script><script>{"sampleUrl":"https://media.invalid/first.mp4?token=fixture"}</script><script>{"videoUrl":"https://media.invalid/second.mp4"}</script>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, "https://media.invalid/first.mp4", (&scraper{}).extractTrailerURLNewSite(boundaryDocument(t, tc.body)))
		})
	}
}

func TestOfflineMetadataStructuredScreenshotsAndPortraitWin(t *testing.T) {
	const sourceURL = "https://video.dmm.co.jp/av/content/?id=abc00123"
	const posterURL = "https://awsimgsrc.dmm.com/dig/digital/video/abc00123/abc00123ps.jpg"
	const screenshot1 = "https://pics.dmm.co.jp/digital/video/abc00123/abc00123jp-1.jpg"
	const screenshot2 = "https://pics.dmm.co.jp/digital/video/abc00123/abc00123jp-2.jpg"
	const body = `<html><head><script type="application/ld+json">{"@type":"Product","name":"Fixture title","image":["https://pics.dmm.co.jp/digital/video/abc00123/abc00123pl.jpg","https://pics.dmm.co.jp/digital/video/abc00123/abc00123-1.jpg","https://pics.dmm.co.jp/digital/video/abc00123/abc00123-2.jpg"],"subjectOf":{"contentUrl":"https://media.invalid/structured.mp4"}}</script><meta property="og:image" content="https://media.invalid/html-cover.jpg"></head><body><img src="https://awsimgsrc.dmm.co.jp/pics_dig/digital/video/other001/other001-1.jpg"><video><source src="https://media.invalid/html.mp4"></video></body></html>`
	var portrait bytes.Buffer
	require.NoError(t, png.Encode(&portrait, image.NewRGBA(image.Rect(0, 0, 600, 800))))
	s := newTestDMMScraper(t)
	var requests []string
	s.client.SetTransport(dmmFixtureTransport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.String())
		switch {
		case r.Method == http.MethodGet && r.URL.String() == sourceURL:
			return dmmFixtureResponse(r, http.StatusOK, body), nil
		case r.Method == http.MethodGet && r.URL.String() == posterURL:
			resp := dmmFixtureResponse(r, http.StatusOK, "")
			resp.Body = io.NopCloser(bytes.NewReader(portrait.Bytes()))
			return resp, nil
		case r.Method == http.MethodHead && (r.URL.String() == screenshot1 || r.URL.String() == screenshot2):
			resp := dmmFixtureResponse(r, http.StatusOK, "")
			resp.Header.Set("Content-Length", "20000")
			return resp, nil
		default:
			t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
			return nil, fmt.Errorf("unexpected metadata request")
		}
	}))
	result, err := s.ScrapeURL(context.Background(), sourceURL)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "ABC-123", result.ID)
	require.Equal(t, "https://awsimgsrc.dmm.com/dig/digital/video/abc00123/abc00123pl.jpg", result.CoverURL)
	require.Equal(t, posterURL, result.PosterURL)
	require.False(t, result.ShouldCropPoster, "a usable portrait must not be cropped from the landscape cover")
	require.Equal(t, []string{screenshot1, screenshot2}, result.ScreenshotURL)
	require.Equal(t, "https://media.invalid/structured.mp4", result.TrailerURL)
	require.Equal(t, []string{"GET " + sourceURL, "GET " + posterURL, "HEAD " + screenshot1, "HEAD " + screenshot2}, requests, "HTML media alternatives must not be probed when structured metadata wins")
}

func TestOfflineMetadataLegacySeriesPreservesLinkText(t *testing.T) {
	doc := boundaryDocument(t, `<table><tr><td>シリーズ</td><td><a href="/mono/dvd/-/list/=/article=series/id=123/">  Fixture Series  </a></td></tr></table>`)
	result, err := newTestDMMScraper(t).parseHTML(context.Background(), doc, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abc00123/")
	require.NoError(t, err)
	require.Equal(t, "Fixture Series", result.Series)
}
