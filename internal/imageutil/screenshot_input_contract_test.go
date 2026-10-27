package imageutil

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeDMMScreenshotURLPreservesMalformedInput(t *testing.T) {
	const malformed = "https://pics.dmm.co.jp/digital/video/abc/abc-1.jpg%zz"
	require.Equal(t, malformed, NormalizeDMMScreenshotURL("  "+malformed+"  "))
}

func TestDiscoverScreenshotsRejectsInvalidInputBeforeTransport(t *testing.T) {
	for _, tc := range []struct{ name, coverURL string }{
		{"malformed escape", "https://pics.dmm.co.jp/digital/video/abc/abcpl.jpg%zz"},
		{"empty content identifier", "https://pics.dmm.co.jp/digital/video/abc/pl.jpg"},
		// URL parsing decodes this escape into a newline in the path. The
		// generated screenshot request must reject it rather than dispatching.
		{"escaped newline in directory", "https://pics.dmm.co.jp/digital/video/ab%0Ac/abcpl.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				t.Errorf("invalid cover dispatched %s %s", req.Method, req.URL)
				return nil, fmt.Errorf("unexpected request for invalid cover")
			})}
			require.Nil(t, DiscoverScreenshots(tc.coverURL, client))
			require.Zero(t, calls)
		})
	}
}

func TestDiscoverScreenshotsPreservesOverflowingNumericIdentifier(t *testing.T) {
	// This valid-looking content identifier exceeds int on both 32-bit and
	// 64-bit platforms. It must remain opaque rather than wrap or become zero.
	const contentID = "118gets9999999999999999999999999999999999999999"
	const baseURL = "https://pics.dmm.co.jp/digital/video/" + contentID + "/" + contentID
	calls := 0
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodHead, req.Method)
		require.Equal(t, baseURL+"jp-1.jpg", req.URL.String())
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}

	require.Nil(t, DiscoverScreenshots(baseURL+"pl.jpg", client))
	require.Equal(t, 1, calls, "overflow must not create a second, corrupted identifier candidate")
}
