package dmm

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type actressRoundTripFunc func(*http.Request) (*http.Response, error)

func (f actressRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func actressResponse(req *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func newActressTestScraper(transport http.RoundTripper) *scraper {
	client := resty.New()
	client.SetTransport(transport)
	return &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}
}

func TestTryActressThumbURLsUsesProfileAndStreamingCandidates(t *testing.T) {
	pngBody, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFElEQVR4nGP4z8DAwMDAxMDAwMDAAAANHQEDasKb6QAAAABJRU5ErkJggg==")
	require.NoError(t, err)

	oldFactory := newActressProbeClient
	newActressProbeClient = func(*models.ProxyProfile, time.Duration, int) (*resty.Client, error) {
		client := resty.New()
		client.SetTransport(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "exact.jpg") {
				resp := actressResponse(req, http.StatusOK, pngBody)
				resp.Header.Set("Content-Type", "image/png")
				return resp, nil
			}
			return actressResponse(req, http.StatusNotFound, nil), nil
		}))
		return client, nil
	}
	t.Cleanup(func() { newActressProbeClient = oldFactory })

	transport := actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://video.dmm.co.jp/av/list/?actress=42":
			return actressResponse(req, http.StatusOK, []byte(`<a href="/av/content/?id=movie">movie</a>`)), nil
		case "https://video.dmm.co.jp/av/content/?id=movie":
			return actressResponse(req, http.StatusOK, []byte(`<a href="/av/list/?actress=42"><img src="https://pics.dmm.co.jp/mono/actjpgs/exact.jpg"></a>`)), nil
		default:
			return actressResponse(req, http.StatusNotFound, nil), nil
		}
	})
	s := newActressTestScraper(transport)
	profile := docFromHTMLDMM(t, `<title>女優 (あい)</title>`)

	require.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/exact.jpg", s.tryActressThumbURLsWithProfileDoc(context.Background(), "", "", 42, profile))
}

func TestFirstExistingActressImageCoversFactoryFailureAndPlaceholderRejection(t *testing.T) {
	s := newActressTestScraper(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return actressResponse(req, http.StatusNotFound, nil), nil
	}))
	s.proxyProfile = &models.ProxyProfile{URL: "%"}
	require.Empty(t, s.firstExistingActressImage(context.Background(), nil))

	oldFactory := newActressProbeClient
	newActressProbeClient = func(*models.ProxyProfile, time.Duration, int) (*resty.Client, error) {
		client := resty.New()
		client.SetTransport(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return actressResponse(req, http.StatusOK, nil), nil
		}))
		return client, nil
	}
	t.Cleanup(func() { newActressProbeClient = oldFactory })
	s.settings.ExtraPlaceholderHashes = []string{"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}
	require.Empty(t, s.firstExistingActressImage(context.Background(), []string{"https://pics.dmm.co.jp/mono/actjpgs/bad.jpg"}))
}

func TestResolveActressThumbnailAndMetadataBranches(t *testing.T) {
	s := newActressTestScraper(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return actressResponse(req, http.StatusNotFound, nil), nil
	}))

	require.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/a.jpg", s.ResolveActressThumbnail(context.Background(), models.ActressInfo{ThumbURL: "//pics.dmm.co.jp/mono/actjpgs/a.jpg"}))
	require.Empty(t, s.ResolveActressThumbnail(context.Background(), models.ActressInfo{}))
	require.Empty(t, s.ResolveActressMetadata(context.Background(), models.ActressInfo{}))
	require.Equal(t, models.ActressInfo{DMMID: 9}, s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 9}))

	s.client.SetTransport(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return actressResponse(req, http.StatusOK, []byte(`<h1 class="list-title"><span class="bold">Yui Hatanoの商品一覧</span></h1>`)), nil
	}))
	got := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 9, JapaneseName: "fallback", ThumbURL: "keep.jpg"})
	require.Equal(t, "Yui", got.FirstName)
	require.Equal(t, "Hatano", got.LastName)
	require.Empty(t, got.ThumbURL)

	s.client.SetTransport(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return actressResponse(req, http.StatusOK, []byte(`<h1 class="list-title"></h1>`)), nil
	}))
	got = s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 10, JapaneseName: "fallback", ThumbURL: "keep.jpg"})
	require.Equal(t, "fallback", got.JapaneseName)
	got = s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 10, JapaneseName: "fallback"})
	require.Equal(t, "fallback", got.JapaneseName)
}

func TestFetchActressDocumentsBrowserAndHTTPFailures(t *testing.T) {
	oldFetch, oldParse := fetchActressPageWithBrowser, parseActressPageHTML
	t.Cleanup(func() {
		fetchActressPageWithBrowser = oldFetch
		parseActressPageHTML = oldParse
	})

	s := newActressTestScraper(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "transport-error") {
			return nil, errors.New("transport")
		}
		return actressResponse(req, http.StatusTeapot, nil), nil
	}))
	s.useBrowser = true
	fetchActressPageWithBrowser = func(context.Context, string, int, *models.ProxyProfile, func(string) string, afero.Fs) (string, error) {
		return "<title>ok</title>", nil
	}
	doc, err := s.fetchActressStreamingDoc(context.Background(), "https://video.dmm.co.jp/ok")
	require.NoError(t, err)
	require.Equal(t, "ok", doc.Find("title").Text())
	require.NotNil(t, s.fetchActressMetadataDoc(context.Background(), 7))

	fetchActressPageWithBrowser = func(context.Context, string, int, *models.ProxyProfile, func(string) string, afero.Fs) (string, error) {
		return "", errors.New("browser")
	}
	_, err = s.fetchActressStreamingDoc(context.Background(), "https://video.dmm.co.jp/error")
	require.ErrorContains(t, err, "browser")
	require.Nil(t, s.fetchActressMetadataDoc(context.Background(), 7))

	fetchActressPageWithBrowser = func(context.Context, string, int, *models.ProxyProfile, func(string) string, afero.Fs) (string, error) {
		return "bad", nil
	}
	parseActressPageHTML = func(string) (*goquery.Document, error) { return nil, errors.New("parse") }
	_, err = s.fetchActressStreamingDoc(context.Background(), "https://video.dmm.co.jp/parse")
	require.ErrorContains(t, err, "parse")
	require.Nil(t, s.fetchActressMetadataDoc(context.Background(), 7))

	s.useBrowser = false
	parseActressPageHTML = oldParse
	limited := ratelimit.NewLimiter(time.Hour)
	require.NoError(t, limited.Wait(context.Background()))
	s.rateLimiter = limited
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.fetchActressStreamingDoc(cancelled, "https://video.dmm.co.jp/cancelled")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, s.fetchActressPageDoc(cancelled, 7))
	s.rateLimiter = ratelimit.NewLimiter(0)
	_, err = s.fetchActressStreamingDoc(context.Background(), "https://video.dmm.co.jp/transport-error")
	require.ErrorContains(t, err, "transport")
	_, err = s.fetchActressStreamingDoc(context.Background(), "https://video.dmm.co.jp/status")
	require.Error(t, err)

	s.client.SetTransport(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return actressResponse(req, http.StatusOK, []byte("bad")), nil
	}))
	parseActressPageHTML = func(string) (*goquery.Document, error) { return nil, errors.New("parse") }
	require.Nil(t, s.fetchActressPageDoc(context.Background(), 7))
}

func TestStreamingResolutionAndHelpersFailureBranches(t *testing.T) {
	s := newActressTestScraper(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("lookup")
	}))
	require.Empty(t, s.resolveActressThumbnailFromStreamingList(context.Background(), 3))

	s.client.SetTransport(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return actressResponse(req, http.StatusOK, []byte(`<p>no detail</p>`)), nil
	}))
	require.Empty(t, s.resolveActressThumbnailFromStreamingList(context.Background(), 3))

	s.client.SetTransport(actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/av/content/") {
			return nil, errors.New("detail")
		}
		return actressResponse(req, http.StatusOK, []byte(`<a href="/av/content/?id=movie">movie</a>`)), nil
	}))
	require.Empty(t, s.resolveActressThumbnailFromStreamingList(context.Background(), 3))

	require.Empty(t, firstActressStreamingDetailURL(nil))
	doc := docFromHTMLDMM(t, `<a>missing</a><a href="/%zz/av/content/?id=x">invalid</a><a href="https://evil.example/av/content/?id=x">unsafe</a>`)
	require.Empty(t, firstActressStreamingDetailURL(doc))
	require.Empty(t, extractExactActressThumbFromStreamingDoc(nil, 1))
	require.Empty(t, extractExactActressThumbFromStreamingDoc(doc, 0))
	require.Nil(t, extractActressProfileImageCandidates(nil))
	require.Equal(t, models.ActressInfo{DMMID: 4}, extractActressProfileMetadata(nil, 4))
	require.Nil(t, extractRomajiVariantsFromActressDoc(nil))
	h1Doc := docFromHTMLDMM(t, `<h1 class="list-title"><span class="bold">女優 (あい)</span></h1>`)
	require.NotEmpty(t, extractRomajiVariantsFromActressDoc(h1Doc))
	longDoc := docFromHTMLDMM(t, `<title>女優 (しらかみえみか)</title>`)
	splitFound := false
	for _, variant := range extractRomajiVariantsFromActressDoc(longDoc) {
		if strings.Contains(variant, "_") {
			splitFound = true
		}
	}
	require.True(t, splitFound)
	require.Equal(t, []string{"one"}, dedupeActressImageCandidates([]string{"", "one", "one"}))
	require.Equal(t, 0, actressProbeStatus(nil))
}

func TestValidateActressThumbnailUsesConfiguredAndDefaultUserAgent(t *testing.T) {
	var userAgents []string
	transport := actressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		userAgents = append(userAgents, req.Header.Get("User-Agent"))
		return actressResponse(req, http.StatusBadRequest, nil), nil
	})
	s := newActressTestScraper(transport)
	_ = s.ValidateActressThumbnail(context.Background(), "https://example.com/image.jpg")
	s.settings.UserAgent = "coverage-agent"
	_ = s.ValidateActressThumbnail(context.Background(), "https://example.com/image.jpg")
	require.Contains(t, userAgents, config.DefaultUserAgent)
	require.Contains(t, userAgents, "coverage-agent")
}
