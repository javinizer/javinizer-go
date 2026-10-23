package r18dev

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type mediaBoundaryTransport func(*http.Request) (*http.Response, error)

func (f mediaBoundaryTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestR18OfflineMalformedResponsePreviewAndCancellation(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(fmt.Sprint(direct), func(t *testing.T) {
			body := strings.Repeat("x", 200) + "private-tail-not-in-preview"
			s := &scraper{enabled: true, rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(mediaBoundaryTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
			}))}
			var result *models.ScraperResult
			var err error
			if direct {
				result, err = s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined=abc00123/json")
			} else {
				result, err = s.fetchAndParseCombined(context.Background(), "https://r18.dev/fixture", "ABC-123")
			}
			require.Nil(t, result)
			require.Error(t, err)
			require.Contains(t, err.Error(), strings.Repeat("x", 200))
			require.NotContains(t, err.Error(), "private-tail-not-in-preview")
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour)}
	resp, err := s.doRequestWithRetryCtx(ctx, "https://r18.dev/")
	require.Nil(t, resp)
	require.ErrorIs(t, err, context.Canceled)
	digits := strings.Repeat("9", 30)
	require.True(t, contentIDCoreMatch("abc"+digits, "abc"+digits))
	require.False(t, contentIDCoreMatch("abc"+digits, "abc"+strings.Repeat("8", 30)))
}

func TestR18OfflinePosterFallbackAndScreenshotDiscovery(t *testing.T) {
	var img bytes.Buffer
	require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 600, 800))))
	placeholderBody := []byte("custom placeholder")
	hash := fmt.Sprintf("%x", sha256.Sum256(placeholderBody))
	calls := map[string]int{}
	s := &scraper{settings: models.ScraperSettings{ExtraPlaceholderHashes: []string{hash}}, client: resty.New().SetTransport(mediaBoundaryTransport(func(req *http.Request) (*http.Response, error) {
		key := req.Method + " " + req.URL.String()
		calls[key]++
		status, body := http.StatusNotFound, []byte(nil)
		switch key {
		case "GET https://awsimgsrc.dmm.com/dig/digital/video/zzqq001/zzqq001ps.jpg": // Cover-derived candidate unavailable; series fallback remains usable.
		case "GET https://awsimgsrc.dmm.com/dig/mono/movie/zzqq001/zzqq001ps.jpg":
			status, body = http.StatusOK, img.Bytes()
		case "GET https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-99.jpg":
			status, body = http.StatusOK, placeholderBody
		case "HEAD https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-99.jpg", "HEAD https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-1.jpg":
			status = http.StatusOK
		case "HEAD https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-2.jpg":
		default:
			t.Errorf("unexpected offline media request: %s", key)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(bytes.NewReader(body))}, nil
	}))}
	data := &r18Response{ContentID: "zzqq001", JacketFullURL: "https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001pl.jpg"}
	data.Images.SampleImages = []string{"https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-99.jpg"}
	result := &models.ScraperResult{ID: "ZZQQ-001"}
	resolveMediaURLs(context.Background(), s, data, result)
	require.Equal(t, "https://awsimgsrc.dmm.com/dig/mono/movie/zzqq001/zzqq001ps.jpg", result.PosterURL)
	require.False(t, result.ShouldCropPoster)
	require.Equal(t, []string{"https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-1.jpg"}, result.ScreenshotURL)
	require.Equal(t, map[string]int{
		"GET https://awsimgsrc.dmm.com/dig/digital/video/zzqq001/zzqq001ps.jpg": 1,
		"GET https://awsimgsrc.dmm.com/dig/mono/movie/zzqq001/zzqq001ps.jpg":    1,
		"GET https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-99.jpg":     1,
		"HEAD https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-99.jpg":    1,
		"HEAD https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-1.jpg":     1,
		"HEAD https://pics.dmm.co.jp/digital/video/zzqq001/zzqq001jp-2.jpg":     1,
	}, calls)
}

func TestR18OfflineConfigurationCloneAndRetryRecovery(t *testing.T) {
	settings := models.ScraperSettings{Enabled: true, ExtraPlaceholderHashes: []string{"original"}, Proxy: &models.ProxyConfig{Enabled: true, Profile: "fixture", Profiles: map[string]models.ProxyProfile{"fixture": {URL: "http://127.0.0.1:1"}}}}
	s := newScraper(&settings, nil, models.FlareSolverrConfig{}, nil)
	clone := s.Config()
	clone.ExtraPlaceholderHashes[0] = "changed"
	clone.Proxy.Profiles["fixture"] = models.ProxyProfile{URL: "http://127.0.0.1:2"}
	require.Equal(t, "original", s.Config().ExtraPlaceholderHashes[0])
	require.Equal(t, "http://127.0.0.1:1", s.Config().Proxy.Profiles["fixture"].URL)
	attempts := 0
	s.client = resty.New().SetTransport(mediaBoundaryTransport(func(req *http.Request) (*http.Response, error) {
		attempts++
		status := http.StatusOK
		if attempts == 1 {
			status = http.StatusTooManyRequests
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html"}}, Request: req, Body: io.NopCloser(strings.NewReader("<html>Not found</html>"))}, nil
	}))
	s.maxRetries = 1
	result, err := s.fetchAndParseCombined(context.Background(), "https://r18.dev/fixture", "ABC-123")
	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "returned HTML")
	require.Equal(t, 2, attempts)
}
