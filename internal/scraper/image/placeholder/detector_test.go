package placeholder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/config"
)

func TestDefaultDMMPlaceholderHashes(t *testing.T) {
	assert.NotNil(t, DefaultDMMPlaceholderHashes)
	assert.Len(t, DefaultDMMPlaceholderHashes, 1)
	assert.Len(t, DefaultDMMPlaceholderHashes[0], 64)
}

func TestConfigFromSettings(t *testing.T) {
	testHash := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	defaultHashes := []string{"aaa1111111111111111111111111111111111111111111111111111111111111"}

	tests := []struct {
		name          string
		settings      *config.ScraperSettings
		defaultHashes []string
		wantEnabled   bool
		wantThreshold int64
		wantHashCount int
		wantContains  []string
	}{
		{
			name:          "nil settings returns default config",
			settings:      nil,
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: DefaultThresholdKB * 1024,
			wantHashCount: 1,
			wantContains:  defaultHashes,
		},
		{
			name:          "empty extra returns default config",
			settings:      &config.ScraperSettings{},
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: DefaultThresholdKB * 1024,
			wantHashCount: 1,
			wantContains:  defaultHashes,
		},
		{
			name: "user threshold in extra",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyThreshold: 20,
				},
			},
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: 20 * 1024,
			wantHashCount: 1,
			wantContains:  defaultHashes,
		},
		{
			name: "float64 threshold from json unmarshal",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyThreshold: float64(15),
				},
			},
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: 15 * 1024,
			wantHashCount: 1,
			wantContains:  defaultHashes,
		},
		{
			name: "user hashes in extra merged with defaults",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyHashes: []string{testHash},
				},
			},
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: DefaultThresholdKB * 1024,
			wantHashCount: 2,
			wantContains:  []string{defaultHashes[0], testHash},
		},
		{
			name: "interface slice from json",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyHashes: []interface{}{testHash},
				},
			},
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: DefaultThresholdKB * 1024,
			wantHashCount: 2,
			wantContains:  []string{defaultHashes[0], testHash},
		},
		{
			name: "invalid hash length filtered out",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyHashes: []interface{}{"invalid", testHash},
				},
			},
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: DefaultThresholdKB * 1024,
			wantHashCount: 2,
			wantContains:  []string{defaultHashes[0], testHash},
		},
		{
			name: "negative threshold uses default",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyThreshold: -5,
				},
			},
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: DefaultThresholdKB * 1024,
			wantHashCount: 1,
			wantContains:  defaultHashes,
		},
		{
			name: "zero threshold uses default",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyThreshold: 0,
				},
			},
			defaultHashes: defaultHashes,
			wantEnabled:   true,
			wantThreshold: DefaultThresholdKB * 1024,
			wantHashCount: 1,
			wantContains:  defaultHashes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConfigFromSettings(tt.settings, tt.defaultHashes)
			assert.Equal(t, tt.wantEnabled, cfg.Enabled)
			assert.Equal(t, tt.wantThreshold, cfg.Threshold)
			assert.Equal(t, tt.wantHashCount, len(cfg.Hashes))

			hashSet := make(map[string]bool)
			for _, h := range cfg.Hashes {
				hashSet[h] = true
			}
			for _, want := range tt.wantContains {
				assert.True(t, hashSet[want], "expected hash %s in result", want)
			}
		})
	}
}

func TestIsPlaceholder(t *testing.T) {
	placeholderImage := make([]byte, 100)
	for i := range placeholderImage {
		placeholderImage[i] = byte(i)
	}
	hash := sha256.Sum256(placeholderImage)
	placeholderHash := hex.EncodeToString(hash[:])

	largeImage := make([]byte, 20*1024)
	for i := range largeImage {
		largeImage[i] = byte(i % 256)
	}

	nonPlaceholderImage := make([]byte, 500)
	for i := range nonPlaceholderImage {
		nonPlaceholderImage[i] = byte(255 - i)
	}

	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		cfg         Config
		wantResult  bool
		wantErr     bool
		errContains string
	}{
		{
			name: "hash match detection returns true",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.Header().Set("Content-Length", "100")
						return
					}
					w.Write(placeholderImage)
				}))
			},
			cfg:        Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{placeholderHash}},
			wantResult: true,
			wantErr:    false,
		},
		{
			name: "size threshold detection triggers download and hash check",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.Header().Set("Content-Length", "500")
						return
					}
					w.Write(nonPlaceholderImage)
				}))
			},
			cfg:        Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{placeholderHash}},
			wantResult: false,
			wantErr:    false,
		},
		{
			name: "large file bypass returns false without download",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.Header().Set("Content-Length", "15360")
						return
					}
					t.Error("should not download large file")
				}))
			},
			cfg:        Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{}},
			wantResult: false,
			wantErr:    false,
		},
		{
			name: "404 response returns false not error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			cfg:        Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{}},
			wantResult: false,
			wantErr:    false,
		},
		{
			name: "missing content-length falls back to download",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						return
					}
					w.Write(placeholderImage)
				}))
			},
			cfg:        Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{placeholderHash}},
			wantResult: true,
			wantErr:    false,
		},
		{
			name: "empty hash list detection works via size only",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.Header().Set("Content-Length", "100")
						return
					}
					w.Write(placeholderImage)
				}))
			},
			cfg:        Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{}},
			wantResult: false,
			wantErr:    false,
		},
		{
			name: "timeout returns error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(200 * time.Millisecond)
					w.Write(placeholderImage)
				}))
			},
			cfg:         Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{}},
			wantResult:  false,
			wantErr:     true,
			errContains: "context deadline exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			defer server.Close()

			ctx := context.Background()
			if tt.errContains != "" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}

			client := resty.New()

			result, err := IsPlaceholder(ctx, client, server.URL, tt.cfg)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantResult, result)
		})
	}
}

func TestIsPlaceholder_EmptyURL(t *testing.T) {
	client := resty.New()
	ctx := context.Background()

	result, err := IsPlaceholder(ctx, client, "", Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{}})
	assert.False(t, result)
	assert.Error(t, err)
}

func TestFilterURLs(t *testing.T) {
	placeholderImage := make([]byte, 100)
	for i := range placeholderImage {
		placeholderImage[i] = byte(i)
	}
	hash := sha256.Sum256(placeholderImage)
	placeholderHash := hex.EncodeToString(hash[:])

	largeImage := make([]byte, 20*1024)
	for i := range largeImage {
		largeImage[i] = byte(i % 256)
	}

	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		cfg         Config
		wantLen     int
		wantRemoved int
		wantErr     bool
	}{
		{
			name:        "empty urls returns empty",
			cfg:         Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{}},
			wantLen:     0,
			wantRemoved: 0,
		},
		{
			name: "filters one placeholder",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					id := r.URL.Query().Get("id")
					if r.Method == http.MethodHead {
						if id == "placeholder" {
							w.Header().Set("Content-Length", "100")
						} else {
							w.Header().Set("Content-Length", "15360")
						}
						return
					}
					if id == "placeholder" {
						w.Write(placeholderImage)
					} else {
						w.Write(largeImage)
					}
				}))
			},
			cfg:         Config{Enabled: true, Threshold: 10 * 1024, Hashes: []string{placeholderHash}},
			wantLen:     1,
			wantRemoved: 1,
		},
		{
			name:        "no filtering when disabled",
			cfg:         Config{Enabled: false, Threshold: 10 * 1024, Hashes: []string{}},
			wantLen:     2,
			wantRemoved: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := resty.New()

			var urlsToTest []string
			if tt.setupServer != nil {
				server := tt.setupServer()
				defer server.Close()
				urlsToTest = []string{
					server.URL + "?id=placeholder",
					server.URL + "?id=valid",
				}
			} else if tt.name == "empty urls returns empty" {
				urlsToTest = []string{}
			} else {
				urlsToTest = []string{"url1", "url2"}
			}

			result, removed, err := FilterURLs(ctx, client, urlsToTest, tt.cfg)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantLen, len(result))
			assert.Equal(t, tt.wantRemoved, removed)
		})
	}
}
