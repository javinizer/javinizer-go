package dmm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
)

func TestNormalizeActressThumbURLPreservesAWSCDNPath(t *testing.T) {
	require.Equal(t,
		"https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami.jpg",
		normalizeActressThumbURL("https://AWSIMGSRC.DMM.CO.JP/pics_dig/mono/actjpgs/iseya_takami.jpg?width=125#profile"),
	)
}

func TestExtractActressProfileImageCandidatesPrefersAWSAndDeduplicates(t *testing.T) {
	doc := docFromHTMLDMM(t, `<html><body>
		<img src="https://pics.dmm.co.jp/mono/actjpgs/iseya_takami.jpg?size=small">
		<img data-src="https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami.jpg?width=125">
		<source srcset="https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami.jpg?width=250 2x, https://pics.dmm.co.jp/mono/actjpgs/iseya_takami.jpg 1x">
		<img src="https://awsimgsrc.dmm.co.jp/pics_dig/digital/video/work/workps.jpg">
	</body></html>`)

	require.Equal(t, []string{
		"https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami.jpg",
		"https://pics.dmm.co.jp/mono/actjpgs/iseya_takami.jpg",
	}, extractActressProfileImageCandidates(doc))
}

func TestFirstActressStreamingDetailURLSelectsFirstSafeURL(t *testing.T) {
	doc := docFromHTMLDMM(t, `<html><body>
		<a href="https://evil.example/av/content/?id=evil">evil</a>
		<a href="http://video.dmm.co.jp/av/content/?id=insecure">insecure</a>
		<a href="https://video.dmm.co.jp:8443/av/content/?id=port">port</a>
		<a href="/av/content/?id=good&amp;redirect=https%3A%2F%2Fevil.example#fragment">good</a>
		<a href="/av/content/?id=later">later</a>
	</body></html>`)

	require.Equal(t, "https://video.dmm.co.jp/av/content/?id=good", firstActressStreamingDetailURL(doc))
}

func TestExtractExactActressThumbFromStreamingDoc(t *testing.T) {
	doc := docFromHTMLDMM(t, `<html><body>
		<div><a href="/av/list/?actress=99"><img src="https://pics.dmm.co.jp/mono/actjpgs/wrong.jpg">Wrong</a></div>
		<div><a href="/av/list/?actress=42"><picture><source srcset="https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/exact.jpg?w=125"></picture>Exact</a></div>
	</body></html>`)
	require.Equal(t, "https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/exact.jpg", extractExactActressThumbFromStreamingDoc(doc, 42))

	ambiguous := docFromHTMLDMM(t, `<html><body><div>
		<img src="https://pics.dmm.co.jp/mono/actjpgs/unrelated.jpg">
		<a href="/av/list/?actress=42">Exact without image</a>
		<a href="/av/list/?actress=99">Other</a>
	</div></body></html>`)
	require.Empty(t, extractExactActressThumbFromStreamingDoc(ambiguous, 42))
}

func TestActressImageProbeDoesNotFollowRedirects(t *testing.T) {
	var imageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/image.jpg" {
			imageRequests.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/image.jpg", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	client := resty.New()
	client.SetRedirectPolicy(resty.NoRedirectPolicy())

	require.False(t, actressImageExists(context.Background(), client, server.URL+"/redirect"))
	require.Zero(t, imageRequests.Load())
}
