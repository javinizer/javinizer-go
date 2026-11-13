package httpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestDirectClientsCloneTransportWithoutInheritedProxy(t *testing.T) {
	original := http.DefaultTransport
	baseline := original.(*http.Transport).Clone()
	baseline.TLSHandshakeTimeout = 7 * time.Second
	baseline.ResponseHeaderTimeout = 11 * time.Second
	baseline.ExpectContinueTimeout = 3 * time.Second
	baseline.IdleConnTimeout = 19 * time.Second
	baseline.MaxIdleConns = 17
	baseline.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "fixture.invalid"}
	baseline.Proxy = http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.invalid:8080"})
	http.DefaultTransport = baseline
	t.Cleanup(func() { http.DefaultTransport = original; baseline.CloseIdleConnections() })

	constructors := []struct {
		name  string
		build func(t *testing.T) *http.Transport
	}{
		{"nil profile", func(t *testing.T) *http.Transport { tr, err := NewTransport(nil); require.NoError(t, err); return tr }},
		{"empty profile", func(t *testing.T) *http.Transport {
			tr, err := NewTransport(&models.ProxyProfile{})
			require.NoError(t, err)
			return tr
		}},
		{"HTTP client", func(t *testing.T) *http.Transport {
			c, err := NewHTTPClient(nil, 5*time.Second)
			require.NoError(t, err)
			require.Equal(t, 5*time.Second, c.Timeout)
			return c.Transport.(*http.Transport)
		}},
		{"Resty no proxy", func(t *testing.T) *http.Transport {
			c := NewRestyClientNoProxy(5*time.Second, 2)
			require.Equal(t, 5*time.Second, c.GetClient().Timeout)
			require.Equal(t, 2, c.RetryCount)
			return c.GetClient().Transport.(*http.Transport)
		}},
	}
	for _, tc := range constructors {
		t.Run(tc.name, func(t *testing.T) {
			first, second := tc.build(t), tc.build(t)
			defer first.CloseIdleConnections()
			defer second.CloseIdleConnections()
			require.NotSame(t, baseline, first)
			require.NotSame(t, first, second)
			require.Nil(t, first.Proxy)
			require.Nil(t, second.Proxy)
			require.NotNil(t, baseline.Proxy)
			require.NotNil(t, first.DialContext)
			require.Equal(t, baseline.TLSHandshakeTimeout, first.TLSHandshakeTimeout)
			require.Equal(t, baseline.ResponseHeaderTimeout, first.ResponseHeaderTimeout)
			require.Equal(t, baseline.ExpectContinueTimeout, first.ExpectContinueTimeout)
			require.Equal(t, baseline.IdleConnTimeout, first.IdleConnTimeout)
			require.Equal(t, baseline.MaxIdleConns, first.MaxIdleConns)
			require.Equal(t, baseline.TLSClientConfig, first.TLSClientConfig)
			require.NotSame(t, baseline.TLSClientConfig, first.TLSClientConfig)
			first.TLSClientConfig.ServerName = "changed.invalid"
			first.MaxIdleConns = 1
			require.Equal(t, "fixture.invalid", baseline.TLSClientConfig.ServerName)
			require.Equal(t, "fixture.invalid", second.TLSClientConfig.ServerName)
			require.Equal(t, 17, second.MaxIdleConns)
		})
	}
}

func TestRestyNoProxyRequestsStayDirectAndHonorCancellation(t *testing.T) {
	var directRequests, proxyRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directRequests.Add(1)
		_, _ = w.Write([]byte("direct response"))
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		http.Error(w, "unexpected proxy request", http.StatusBadGateway)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	original := http.DefaultTransport
	baseline := original.(*http.Transport).Clone()
	baseline.Proxy = http.ProxyURL(proxyURL)
	http.DefaultTransport = baseline
	t.Cleanup(func() { http.DefaultTransport = original; baseline.CloseIdleConnections() })
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	client := NewRestyClientNoProxy(5*time.Second, 0)
	defer client.GetClient().CloseIdleConnections()
	response, err := client.R().Get(target.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode())
	require.Equal(t, "direct response", response.String())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.R().SetContext(ctx).Get(target.URL)
	require.True(t, errors.Is(err, context.Canceled), "got %v", err)
	require.EqualValues(t, 1, directRequests.Load())
	require.Zero(t, proxyRequests.Load())
	require.NotNil(t, baseline.Proxy)
}
