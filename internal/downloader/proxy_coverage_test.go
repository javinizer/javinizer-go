package downloader

import (
	"fmt"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGetRequest creates a proper *http.Request for use with http.Client.Do.
func newGetRequest(url string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	return req
}

// --- resolveScraperDownloadProxy coverage ---

func TestResolveScraperDownloadProxy_GlobalDisabled(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{Enabled: false},
		},
	}
	result := c.resolveScraperDownloadProxy(nil, nil)
	assert.Nil(t, result, "global disabled → nil")
}

func TestResolveScraperDownloadProxy_DownloadOverrideDisabled(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{Enabled: true},
			GlobalProxy:       &models.ProxyProfile{URL: "http://global:8080"},
		},
	}
	result := c.resolveScraperDownloadProxy(&models.ProxyConfig{Enabled: false}, nil)
	assert.Nil(t, result, "download override explicitly disabled → nil")
}

func TestResolveScraperDownloadProxy_DownloadOverrideWithProfile(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{
				Enabled:  true,
				Profiles: map[string]models.ProxyProfile{"custom": {URL: "http://custom:8080"}},
			},
			GlobalProxy: &models.ProxyProfile{URL: "http://global:8080"},
		},
	}
	result := c.resolveScraperDownloadProxy(&models.ProxyConfig{Enabled: true, Profile: "custom"}, nil)
	require.NotNil(t, result)
	assert.Equal(t, "http://custom:8080", result.URL)
}

func TestResolveScraperDownloadProxy_DownloadOverrideInherit(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{Enabled: true},
			GlobalProxy:       &models.ProxyProfile{URL: "http://global:8080"},
		},
	}
	result := c.resolveScraperDownloadProxy(&models.ProxyConfig{Enabled: true}, nil)
	require.NotNil(t, result)
	assert.Equal(t, "http://global:8080", result.URL, "inherit → global proxy")
}

func TestResolveScraperDownloadProxy_ScraperProxyDisabled(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{Enabled: true},
			GlobalProxy:       &models.ProxyProfile{URL: "http://global:8080"},
		},
	}
	result := c.resolveScraperDownloadProxy(nil, &models.ProxyConfig{Enabled: false})
	assert.Nil(t, result, "scraper proxy disabled → nil")
}

func TestResolveScraperDownloadProxy_ScraperProxyWithProfile(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{
				Enabled:  true,
				Profiles: map[string]models.ProxyProfile{"sp": {URL: "http://sp:8080"}},
			},
			GlobalProxy: &models.ProxyProfile{URL: "http://global:8080"},
		},
	}
	result := c.resolveScraperDownloadProxy(nil, &models.ProxyConfig{Enabled: true, Profile: "sp"})
	require.NotNil(t, result)
	assert.Equal(t, "http://sp:8080", result.URL)
}

func TestResolveScraperDownloadProxy_ScraperProxyInherit(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{Enabled: true},
			GlobalProxy:       &models.ProxyProfile{URL: "http://global:8080"},
		},
	}
	result := c.resolveScraperDownloadProxy(nil, &models.ProxyConfig{Enabled: true})
	require.NotNil(t, result)
	assert.Equal(t, "http://global:8080", result.URL, "inherit → global proxy")
}

func TestResolveScraperDownloadProxy_NoOverridesReturnsGlobal(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{Enabled: true},
			GlobalProxy:       &models.ProxyProfile{URL: "http://global:8080"},
		},
	}
	result := c.resolveScraperDownloadProxy(nil, nil)
	require.NotNil(t, result)
	assert.Equal(t, "http://global:8080", result.URL)
}

// --- resolveProfile coverage ---

func TestResolveProfile_UnknownProfileFallsBackToGlobal(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxy: &models.ProxyProfile{URL: "http://global:8080"},
		},
	}
	result := c.resolveProfile("nonexistent", &models.ProxyConfig{
		Enabled:  true,
		Profiles: map[string]models.ProxyProfile{},
	})
	require.NotNil(t, result)
	assert.Equal(t, "http://global:8080", result.URL, "unknown profile → global fallback")
}

func TestResolveProfile_InheritsCredentialsFromGlobal(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxy: &models.ProxyProfile{
				URL:      "http://global:8080",
				Username: "globaluser",
				Password: "globalpass",
			},
		},
	}
	result := c.resolveProfile("partial", &models.ProxyConfig{
		Enabled: true,
		Profiles: map[string]models.ProxyProfile{
			"partial": {Username: "localuser"}, // URL and Password missing → inherit from global
		},
	})
	require.NotNil(t, result)
	assert.Equal(t, "http://global:8080", result.URL, "inherited URL from global")
	assert.Equal(t, "localuser", result.Username, "kept local username")
	assert.Equal(t, "globalpass", result.Password, "inherited password from global")
}

// --- selectProxyForRequest coverage ---

func TestSelectProxyForRequest_NilRequest(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{}
	assert.Nil(t, c.selectProxyForRequest(nil))
}

func TestSelectProxyForRequest_NilURL(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL = nil
	assert.Nil(t, c.selectProxyForRequest(req))
}

func TestSelectProxyForRequest_EmptyHost(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{}
	req := httptest.NewRequest(http.MethodGet, "http:///path", nil)
	assert.Nil(t, c.selectProxyForRequest(req))
}

func TestSelectProxyForRequest_NoResolverMatch_NoGlobalProxy(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		proxyResolvers: []models.DownloadProxyResolver{
			testDownloadProxyResolver{match: func(host string) bool { return false }},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/x.jpg", nil)
	assert.Nil(t, c.selectProxyForRequest(req))
}

func TestSelectProxyForRequest_NoResolverMatch_GlobalProxyFallback(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxy: &models.ProxyProfile{URL: "http://global:8080"},
		},
		proxyResolvers: []models.DownloadProxyResolver{
			testDownloadProxyResolver{match: func(host string) bool { return false }},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/x.jpg", nil)
	result := c.selectProxyForRequest(req)
	require.NotNil(t, result)
	assert.Equal(t, "http://global:8080", result.URL)
}

// --- Do coverage ---

func TestDo_ForceClientUsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	forceClient, _ := httpclient.NewHTTPClient(nil, 10*time.Second)
	c := &adaptiveDownloaderHTTPClient{forceClient: forceClient}
	req := newGetRequest(server.URL)
	resp, err := c.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDo_NilProxyProfileUsesDirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	directClient, _ := httpclient.NewHTTPClient(nil, 10*time.Second)
	c := &adaptiveDownloaderHTTPClient{directClient: directClient}
	req := newGetRequest(server.URL)
	resp, err := c.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDo_ProxyClientCreationFails_FallsBackToDirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	directClient, _ := httpclient.NewHTTPClient(nil, 10*time.Second)
	c := &adaptiveDownloaderHTTPClient{
		directClient: directClient,
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{Enabled: true},
			GlobalProxy:       &models.ProxyProfile{URL: "http://[::1]:%invalid"}, // invalid URL
		},
		proxyResolvers: []models.DownloadProxyResolver{
			testDownloadProxyResolver{
				match:            func(host string) bool { return true },
				downloadOverride: &models.ProxyConfig{Enabled: true},
			},
		},
		clients: make(map[string]httpclient.HTTPClient),
	}
	req := newGetRequest(server.URL + "/x.jpg")
	resp, err := c.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// --- NewHTTPClient coverage ---

func TestNewHTTPClient_DownloadProxyEmptyURL(t *testing.T) {
	client, err := NewHTTPClient(HTTPClientConfig{
		Timeout:       10 * time.Second,
		DownloadProxy: &models.ProxyProfile{URL: ""},
	})
	require.NoError(t, err)
	adaptive := client.(*adaptiveDownloaderHTTPClient)
	assert.Nil(t, adaptive.forceClient, "empty URL should not set forceClient")
	assert.NotNil(t, adaptive.directClient)
}

func TestNewHTTPClient_DownloadProxyInvalidURL(t *testing.T) {
	client, err := NewHTTPClient(HTTPClientConfig{
		Timeout:       10 * time.Second,
		DownloadProxy: &models.ProxyProfile{URL: "http://[::1]:%invalid"},
	})
	require.NoError(t, err)
	adaptive := client.(*adaptiveDownloaderHTTPClient)
	assert.Nil(t, adaptive.forceClient, "invalid proxy URL should not set forceClient")
	assert.NotNil(t, adaptive.directClient, "should fall back to direct client")
}

// --- generateActressFilename coverage ---

func TestGenerateActressFilename_EmptyTemplate(t *testing.T) {
	cfg := &Config{}
	d := NewDownloader(nil, nil, cfg, nil)
	assert.Equal(t, "", d.generateActressFilename(&models.Movie{ID: "ABC-123"}, "Name", ""))
}

func TestGenerateActressFilename_ErrorTemplate(t *testing.T) {
	cfg := &Config{MediaFormatConfig: organizer.MediaFormatConfig{ActressFormat: "<ACTORNAME>"}}
	d := NewDownloader(nil, nil, cfg, &errorEngine{})
	result := d.generateActressFilename(&models.Movie{ID: "ABC-123"}, "Actress Name", "<ACTORNAME>")
	assert.Equal(t, "Actress Name.jpg", result, "should fallback to sanitized name on template error")
}

type errorEngine struct{ template.EngineInterface }

func (e *errorEngine) Execute(_ string, _ *template.Context) (string, error) {
	return "", fmt.Errorf("template error")
}

// --- getOrCreateProxyClient coverage ---

func TestGetOrCreateProxyClient_CachesClient(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		timeout: 10 * time.Second,
		clients: make(map[string]httpclient.HTTPClient),
	}
	profile := &models.ProxyProfile{URL: "http://proxy:8080", Username: "u", Password: "p"}
	client1, err := c.getOrCreateProxyClient(profile)
	require.NoError(t, err)
	client2, err := c.getOrCreateProxyClient(profile)
	require.NoError(t, err)
	assert.Same(t, client1, client2, "should return cached client")
}
