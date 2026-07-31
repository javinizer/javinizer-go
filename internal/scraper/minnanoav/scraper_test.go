package minnanoav

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResolveActressMetadataMatchesAliasAndKeepsRequestedDMMID(t *testing.T) {
	client := resty.New()
	client.SetRedirectPolicy(resty.FlexibleRedirectPolicy(5))
	client.SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/search_result.php" {
			require.Equal(t, "actress", req.URL.Query().Get("search_scope"))
			require.Equal(t, "安倍亜沙美", req.URL.Query().Get("search_word"))
			require.Equal(t, " Go ", req.URL.Query().Get("search"))
			return response(req, http.StatusFound, "", map[string]string{"Location": "/actress811239.html"}), nil
		}
		require.Equal(t, "/actress811239.html", req.URL.Path)
		return response(req, http.StatusOK, actressHTML, nil), nil
	}))

	s := newScraperWithClient(&models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.test"}, client)
	got := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 19244, JapaneseName: "安倍亜沙美"})

	require.Equal(t, models.ActressInfo{
		DMMID:        19244,
		FirstName:    "Asami",
		LastName:     "Abe",
		JapaneseName: "安倍亜沙美",
		ThumbURL:     "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg",
	}, got)
}

func TestResolveActressMetadataRejectsUnverifiedSearchResult(t *testing.T) {
	client := resty.New()
	client.SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, actressHTML, nil), nil
	}))
	s := newScraperWithClient(&models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.test"}, client)

	got := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 7, JapaneseName: "別人"})
	require.Equal(t, models.ActressInfo{DMMID: 7}, got)
}

func TestSearchIsActressMetadataOnly(t *testing.T) {
	s := newScraperWithClient(&models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.test"}, resty.New())
	_, err := s.Search(context.Background(), "IPX-123")
	require.Error(t, err)
}

func TestResolveActressMetadataMatchesPrimaryNameViaH1(t *testing.T) {
	client := resty.New()
	client.SetRedirectPolicy(resty.FlexibleRedirectPolicy(5))
	client.SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/search_result.php" {
			return response(req, http.StatusFound, "", map[string]string{"Location": "/actress811239.html"}), nil
		}
		return response(req, http.StatusOK, actressHTML, nil), nil
	}))

	s := newScraperWithClient(&models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.test"}, client)
	got := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 28262, JapaneseName: "安部麻沙美"})

	require.Equal(t, "安部麻沙美", got.JapaneseName)
	require.Equal(t, "Masami", got.FirstName)
	require.Equal(t, "Abe", got.LastName)
	require.Equal(t, 28262, got.DMMID)
	require.Equal(t, "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg", got.ThumbURL)
}

func TestSupportsMovieSearchIsFalse(t *testing.T) {
	s := newScraperWithClient(&models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.test"}, resty.New())
	require.False(t, s.SupportsMovieSearch())
}

func response(req *http.Request, status int, body string, headers map[string]string) *http.Response {
	h := make(http.Header)
	for key, value := range headers {
		h.Set(key, value)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

const actressHTML = `<html><head><meta property="og:image" content="https://www.minnano-av.com/p_actress_125_125/001/811239.jpg?new"></head><body>
<h1>安部麻沙美<span>あべまさみ / Abe Masami</span></h1>
<div class="act-profile"><table>
<tr><td><h2>安部麻沙美 （あべまさみ / Abe Masami）</h2></td></tr>
<tr><td><span>別名</span><p>安倍麻沙美 （あべまさみ / Abe Masami）</p></td></tr>
<tr><td><span>別名</span><p>安倍亜沙美 （あべあさみ / Abe Asami）</p></td></tr>
<tr><td><span>生年月日</span><p>1986年09月09日</p></td></tr>
</table></div>
<a href="https://al.dmm.co.jp/?lurl=https%3A%2F%2Fvideo.dmm.co.jp%2Fav%2Flist%2F%3Factress%3D28262">FANZA</a>
</body></html>`

func TestParseActressProfileExtractsCacheFields(t *testing.T) {
	profile, err := ParseActressProfile(actressHTML, "https://www.minnano-av.test/actress811239.html")
	require.NoError(t, err)
	require.Equal(t, 28262, profile.DMMID)
	require.Equal(t, "Masami", profile.FirstName)
	require.Equal(t, "Abe", profile.LastName)
	require.Equal(t, "安部麻沙美", profile.JapaneseName)
	require.Equal(t, "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg", profile.ThumbURL)
	require.ElementsMatch(t, []string{"安倍麻沙美", "安倍亜沙美"}, profile.Aliases)
}
