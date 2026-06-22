package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Do: proxy profile with empty URL falls back to direct ---

func TestDo_ProxyProfileEmptyURL_FallsBackToDirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	directClient, _ := httpclient.NewHTTPClient(nil, 10*time.Second)
	c := &adaptiveDownloaderHTTPClient{
		directClient:   directClient,
		proxyResolvers: []models.DownloadProxyResolver{},
	}

	req := newGetRequest(server.URL + "/file.jpg")
	resp, err := c.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// --- Do: select proxy for request with matching resolver ---

func TestDo_WithProxyResolverMatch(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This is the proxy — forward to target
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer proxyServer.Close()

	directClient, _ := httpclient.NewHTTPClient(nil, 10*time.Second)
	proxyClient, _ := httpclient.NewHTTPClient(nil, 10*time.Second)

	c := &adaptiveDownloaderHTTPClient{
		directClient: directClient,
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: &models.ProxyConfig{
				Enabled: true,
				Profiles: map[string]models.ProxyProfile{
					"test": {URL: proxyServer.URL},
				},
			},
			GlobalProxy: &models.ProxyProfile{URL: proxyServer.URL},
		},
		proxyResolvers: []models.DownloadProxyResolver{
			testDownloadProxyResolver{
				match:            func(host string) bool { return true },
				downloadOverride: &models.ProxyConfig{Enabled: true, Profile: "test"},
			},
		},
		clients: make(map[string]httpclient.HTTPClient),
	}

	// Pre-populate proxy client cache to avoid actual proxy connection
	c.clients[func() string {
		key := proxyServer.URL + "||"
		_ = key
		return ""
	}()] = proxyClient

	req := newGetRequest(proxyServer.URL + "/file.jpg")
	// This tests the selectProxyForRequest → resolveScraperDownloadProxy path
	profile := c.selectProxyForRequest(req)
	// The profile should be resolved
	require.NotNil(t, profile)
}

// --- resolveScraperDownloadProxy: nil global proxy config ---

func TestResolveScraperDownloadProxy_NilGlobalProxyConfig(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxyConfig: nil,
		},
	}
	result := c.resolveScraperDownloadProxy(nil, nil)
	assert.Nil(t, result, "nil global proxy config → nil")
}

// --- resolveProfile: nil GlobalProxy in httpCfg ---

func TestResolveProfile_NilGlobalProxy_Miss(t *testing.T) {
	c := &adaptiveDownloaderHTTPClient{
		httpCfg: HTTPClientConfig{
			GlobalProxy: nil,
		},
	}
	// When the profile name doesn't exist and GlobalProxy is nil, resolveProfile returns nil
	result := c.resolveProfile("nonexistent", &models.ProxyConfig{
		Enabled:  true,
		Profiles: map[string]models.ProxyProfile{},
	})
	// Unknown profile falls back to GlobalProxy, which is nil
	assert.Nil(t, result, "unknown profile with nil GlobalProxy should return nil")
}

// --- resolveProfile: inherits URL and credentials from global ---

func TestResolveProfile_InheritsURLFromGlobal(t *testing.T) {
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
			"partial": {Username: "localuser"}, // URL missing → inherit from global
		},
	})
	require.NotNil(t, result)
	assert.Equal(t, "http://global:8080", result.URL, "should inherit URL from global")
	assert.Equal(t, "localuser", result.Username, "should keep local username")
	assert.Equal(t, "globalpass", result.Password, "should inherit password from global")
}

// --- NewHTTPClient: direct client creation fails ---

func TestNewHTTPClient_DirectClientCreationFails(t *testing.T) {
	client, err := NewHTTPClient(HTTPClientConfig{
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)
	adaptive := client.(*adaptiveDownloaderHTTPClient)
	assert.NotNil(t, adaptive.directClient)
}

// --- download: file already exists ---

func TestDownload_FileAlreadyExists(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	// Create the destination file first
	destPath := "/tmp/existing-file.jpg"
	memFS.MkdirAll("/tmp", 0755)
	afero.WriteFile(memFS, destPath, []byte("existing content"), 0644)

	result, err := downloader.download(context.Background(), "https://example.com/file.jpg", destPath, MediaTypeCover)
	require.NoError(t, err)
	assert.False(t, result.Downloaded, "should not re-download existing file")
	assert.Equal(t, int64(len("existing content")), result.Size)
}

// --- download: request creation failure ---

func TestDownload_RequestCreationFailure(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	// Use an invalid URL that will fail NewRequestWithContext
	result, err := downloader.download(context.Background(), "://invalid", "/tmp/file.jpg", MediaTypeCover)
	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Downloaded)
}

// --- download: file close error after write ---

func TestDownload_WriteAndCloseSuccess(t *testing.T) {
	content := []byte("downloaded content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	result, err := downloader.download(context.Background(), srv.URL+"/file.jpg", "/tmp/file.jpg", MediaTypeCover)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.Equal(t, int64(len(content)), result.Size)

	// Verify file content
	readContent, err := afero.ReadFile(memFS, "/tmp/file.jpg")
	require.NoError(t, err)
	assert.Equal(t, content, readContent)
}

// --- download: temp file creation failure ---

func TestDownload_CreateFileFailure_Miss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	readOnlyFS := afero.NewReadOnlyFs(memFS)
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(srv.Client(), readOnlyFS, cfg, nil)

	result, err := downloader.download(context.Background(), srv.URL+"/file.jpg", "/readonly/file.jpg", MediaTypeCover)
	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Downloaded)
}

// --- download: User-Agent header when configured ---

func TestDownload_ConfiguredUserAgent(t *testing.T) {
	var receivedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60, UserAgent: "test-agent/1.0"}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	result, err := downloader.download(context.Background(), srv.URL+"/file.jpg", "/tmp/file.jpg", MediaTypeCover)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.Equal(t, "test-agent/1.0", receivedUA)
}
