package dmm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDisplayIdentityCatalogSuffix(t *testing.T) {
	series, num, suffix, marker, ok := parseDisplayIdentity("ipx535zh")
	require.True(t, ok)
	assert.Equal(t, "ipx", series)
	assert.Equal(t, "535", num)
	assert.Equal(t, "z", suffix)
	assert.Equal(t, "h", marker)

	_, _, suffix, _, ok = parseDisplayIdentity("ipx535h")
	require.True(t, ok)
	assert.Empty(t, suffix)

	_, _, _, _, ok = parseDisplayIdentity("ipx535dh")
	assert.False(t, ok)
}

func TestRemasterVerifyCatalogSuffixIdentity(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<table><tr><td>品番：</td><td>IPX-535Z-HD</td></tr></table>`
	}})
	st, err := s.verifyCandidateDisplayID(context.Background(), "ipx535zh", []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1ipx00535zh/"})
	require.NoError(t, err)
	assert.Equal(t, displayVerified, st)

	s2, _ := newRemasterTestScraper(t)
	s2.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<table><tr><td>品番：</td><td>IPX-535-HD</td></tr></table>`
	}})
	st, err = s2.verifyCandidateDisplayID(context.Background(), "ipx535zh", []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1ipx00535h/"})
	require.NoError(t, err)
	assert.Equal(t, displayRejected, st)
}

func TestRemasterCacheCatalogSuffixMismatch(t *testing.T) {
	assert.False(t, cachedRemasterIdentityMatches("IPX-535Z-HD", "1ipx00535h", "h", "ipx", "z", false))
	assert.True(t, cachedRemasterIdentityMatches("IPX-535Z-HD", "1ipx00535zh", "h", "ipx", "z", false))
	assert.False(t, cachedRemasterIdentityMatches("IPX-535-HD", "1ipx00535zh", "h", "ipx", "", false))
}

type cancelTransport struct {
	cancel context.CancelFunc
}

func (t *cancelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.cancel()
	return nil, context.Canceled
}

func TestRemasterVerifyCancellation(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st, err := s.verifyCandidateDisplayID(ctx, "rct156h", []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/"})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, displayUnverifiable, st)

	ctx2, cancel2 := context.WithCancel(context.Background())
	s2, _ := newRemasterTestScraper(t)
	s2.useBrowser = true
	s2.browserFetch = func(ctx context.Context, u string) (string, error) {
		cancel2()
		return "", context.Canceled
	}
	st, err = s2.verifyCandidateDisplayID(ctx2, "rct156h", []string{"https://video.dmm.co.jp/av/content/?id=1rct00156h"})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, displayUnverifiable, st)

	ctx3, cancel3 := context.WithCancel(context.Background())
	s3, _ := newRemasterTestScraper(t)
	s3.client.SetTransport(&cancelTransport{cancel: cancel3})
	st, err = s3.verifyCandidateDisplayID(ctx3, "rct156h", []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/"})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, displayUnverifiable, st)
}

func TestRemasterResolveCancellationPropagates(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.client.SetTransport(&cancelAfterSearchTransport{cancel: cancel})
	_, err := s.ResolveContentIDCtx(ctx, "RCT-156H")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

type cancelAfterSearchTransport struct {
	cancel context.CancelFunc
}

func (t *cancelAfterSearchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.String(), "/search/=") {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		body := `<a href="/mono/dvd/-/detail/=/cid=1rct00156h/">R</a>` +
			`<a href="/mono/dvd/-/detail/=/cid=1rct00156v/">V</a>` +
			`<a href="/mono/dvd/-/detail/=/cid=1rct00156uh/">U</a>` +
			`<a href="/mono/dvd/-/detail/=/cid=1rct00156eh/">E</a>`
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
	t.cancel()
	return nil, context.Canceled
}

func TestRemasterSearchSpellingsCatalogSuffix(t *testing.T) {
	for _, q := range remasterSearchSpellings("IPX-535Z-HD") {
		assert.Contains(t, q, "z", "spelling %q keeps the catalog suffix", q)
	}
	assert.NotEmpty(t, remasterSearchSpellings("IPX-535Z-HD"))
	assert.True(t, containsString(remasterSearchSpellings("IPX-535Z-HD"), "ipx-535z-hd"))
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
