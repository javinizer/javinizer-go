package actresscache

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pinnedTestAnswers = map[string][]net.IP{}
var pinnedTestErr error

func withPinnedLookup(t *testing.T) {
	t.Helper()
	prev := lookupIP
	lookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
		if pinnedTestErr != nil {
			return nil, pinnedTestErr
		}
		return pinnedTestAnswers[host], nil
	}
	t.Cleanup(func() { lookupIP = prev; pinnedTestAnswers = map[string][]net.IP{}; pinnedTestErr = nil })
}

type recordingRoundTripper struct{ last *http.Request }

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.last = req
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
}

func pinningTestTransport(servedIP string, proxied bool) *proxyPinningTransport {
	return &proxyPinningTransport{
		base:     &recordingRoundTripper{},
		viaProxy: func(string, string) bool { return proxied },
	}
}

func TestProxyPinningPassthroughs(t *testing.T) {
	t.Run("https bypasses pinning (CONNECT trust boundary)", func(t *testing.T) {
		tr := &proxyPinningTransport{base: &recordingRoundTripper{}, viaProxy: func(string, string) bool { return true }}
		req, _ := http.NewRequest(http.MethodGet, "https://tunnel.example/x", nil)
		_, err := tr.RoundTrip(req)
		require.NoError(t, err)
		assert.Equal(t, "tunnel.example", tr.base.(*recordingRoundTripper).last.URL.Hostname(), "https must pass the hostname through unchanged")
	})
	t.Run("no proxy configured: no rewrite", func(t *testing.T) {
		tr := pinningTestTransport("", false)
		req, _ := http.NewRequest(http.MethodGet, "http://direct.example/x", nil)
		_, err := tr.RoundTrip(req)
		require.NoError(t, err)
		assert.Equal(t, "direct.example", tr.base.(*recordingRoundTripper).last.URL.Hostname())
	})
	t.Run("literal IP targets skip lookup", func(t *testing.T) {
		withPinnedLookup(t)
		pinnedTestErr = errors.New("resolver must not be consulted for literals")
		tr := pinningTestTransport("", true)
		req, _ := http.NewRequest(http.MethodGet, "http://93.184.216.34/x", nil)
		_, err := tr.RoundTrip(req)
		require.NoError(t, err)
		assert.Equal(t, "93.184.216.34", tr.base.(*recordingRoundTripper).last.URL.Hostname())
	})
}

func TestProxyPinningRewritesResolvedTarget(t *testing.T) {
	withPinnedLookup(t)
	pinnedTestAnswers["media.example"] = []net.IP{net.ParseIP("2606:4700:4700::1111"), net.ParseIP("93.184.216.34")}
	tr := pinningTestTransport("", true)
	req, _ := http.NewRequest(http.MethodGet, "http://media.example/thumb.jpg", nil)
	_, err := tr.RoundTrip(req)
	require.NoError(t, err)
	got := tr.base.(*recordingRoundTripper).last
	assert.Equal(t, "93.184.216.34", got.URL.Hostname(), "IPv4 answer chosen for maximal proxy compatibility")
	assert.Equal(t, "media.example", got.Host, "virtual host identity preserved")
}

func TestProxyPinningPreservesNonstandardPort(t *testing.T) {
	withPinnedLookup(t)
	pinnedTestAnswers["media.example"] = []net.IP{net.ParseIP("93.184.216.34")}
	tr := pinningTestTransport("", true)
	req, _ := http.NewRequest(http.MethodGet, "http://media.example:8081/x", nil)
	_, err := tr.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, "93.184.216.34:8081", tr.base.(*recordingRoundTripper).last.URL.Host)
}

func TestProxyPinningBlocksPrivateReResolution(t *testing.T) {
	withPinnedLookup(t)
	pinnedTestAnswers["envoy.internal"] = []net.IP{net.ParseIP("10.2.3.4")}
	tr := pinningTestTransport("", true)
	req, _ := http.NewRequest(http.MethodGet, "http://envoy.internal/x", nil)
	_, err := tr.RoundTrip(req)
	var blocked *BlockedFetchError
	require.ErrorAs(t, err, &blocked)
}

func TestProxyPinningUnresolvableFailsUnverifiable(t *testing.T) {
	withPinnedLookup(t)
	// no answers registered: zero-answer branch
	tr := pinningTestTransport("", true)
	req, _ := http.NewRequest(http.MethodGet, "http://gone.example/x", nil)
	_, err := tr.RoundTrip(req)
	var unverifiable *ssrf.UnverifiableHostError
	require.ErrorAs(t, err, &unverifiable)

	pinnedTestErr = errors.New("resolver offline")
	_, err = tr.RoundTrip(req)
	require.ErrorAs(t, err, &unverifiable)
	assert.ErrorContains(t, err, "resolver offline")
}
