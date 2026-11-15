package dmm

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestStreamingAttributeSelectorsIgnoreMissingAndEmptyAttributes(t *testing.T) {
	s := &scraper{}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body>
 <div></div><div style=""></div><div style="background-image: url(https://example.com/cover.jpg)"></div>
 <img><img src=""><img src="https://awsimgsrc.dmm.co.jp/pics_dig/digital/video/abc/abc-1.jpg?size=large">
 <a>Trailer</a><a onclick="">Trailer</a><a onclick="video('https://example.com/trailer.mp4')">Trailer</a>
 </body></html>`))
	require.NoError(t, err)
	require.Equal(t, "https://example.com/cover.jpg", s.extractCoverURLNewSite(doc, "abc"))
	require.Equal(t, []string{"https://pics.dmm.co.jp/digital/video/abc/abc-1.jpg"}, s.extractScreenshotsNewSite(doc))
	require.Equal(t, "https://example.com/trailer.mp4", s.extractTrailerURLNewSite(doc))
}
