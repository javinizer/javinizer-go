package dmm

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

func TestDefaultPlaceholderHashes(t *testing.T) {
	assert.NotNil(t, DefaultPlaceholderHashes)
}

func TestGetPlaceholderThreshold(t *testing.T) {
	tests := []struct {
		name     string
		settings *config.ScraperSettings
		want     int
	}{
		{
			name:     "nil settings returns default",
			settings: nil,
			want:     DefaultPlaceholderThresholdKB,
		},
		{
			name:     "empty extra returns default",
			settings: &config.ScraperSettings{},
			want:     DefaultPlaceholderThresholdKB,
		},
		{
			name: "user value in extra",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyPlaceholderThreshold: 20,
				},
			},
			want: 20,
		},
		{
			name: "float64 from json unmarshal",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyPlaceholderThreshold: float64(15),
				},
			},
			want: 15,
		},
		{
			name: "negative value returns default",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyPlaceholderThreshold: -5,
				},
			},
			want: DefaultPlaceholderThresholdKB,
		},
		{
			name: "zero returns default",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyPlaceholderThreshold: 0,
				},
			},
			want: DefaultPlaceholderThresholdKB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPlaceholderThreshold(tt.settings)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetExtraPlaceholderHashes(t *testing.T) {
	testHash := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	uppercaseHash := "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890"
	whitespaceHash := "  1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef  "

	tests := []struct {
		name     string
		settings *config.ScraperSettings
		want     []string
	}{
		{
			name:     "nil settings returns nil",
			settings: nil,
			want:     nil,
		},
		{
			name:     "empty extra returns nil",
			settings: &config.ScraperSettings{},
			want:     nil,
		},
		{
			name: "user hashes in extra",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: []string{testHash},
				},
			},
			want: []string{testHash},
		},
		{
			name: "interface slice from json",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: []interface{}{testHash},
				},
			},
			want: []string{testHash},
		},
		{
			name: "invalid hash length filtered out",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: []interface{}{"invalid", testHash},
				},
			},
			want: []string{testHash},
		},
		{
			name: "single string hash",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: testHash,
				},
			},
			want: []string{testHash},
		},
		{
			name: "uppercase hash normalized to lowercase",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: uppercaseHash,
				},
			},
			want: []string{"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		},
		{
			name: "whitespace trimmed from hash",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: whitespaceHash,
				},
			},
			want: []string{testHash},
		},
		{
			name: "invalid single string ignored",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: "invalid",
				},
			},
			want: nil,
		},
		{
			name: "array with normalization",
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: []string{uppercaseHash, whitespaceHash},
				},
			},
			want: []string{"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", testHash},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetExtraPlaceholderHashes(tt.settings)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMergePlaceholderHashes(t *testing.T) {
	testHash1 := "1111111111111111111111111111111111111111111111111111111111111111"
	testHash2 := "2222222222222222222222222222222222222222222222222222222222222222"

	tests := []struct {
		name          string
		defaultHashes []string
		settings      *config.ScraperSettings
		wantLen       int
		wantContains  []string
	}{
		{
			name:          "empty defaults no user hashes",
			defaultHashes: nil,
			settings:      nil,
			wantLen:       0,
			wantContains:  nil,
		},
		{
			name:          "defaults only",
			defaultHashes: []string{testHash1},
			settings:      nil,
			wantLen:       1,
			wantContains:  []string{testHash1},
		},
		{
			name:          "merge defaults and user",
			defaultHashes: []string{testHash1},
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: []string{testHash2},
				},
			},
			wantLen:      2,
			wantContains: []string{testHash1, testHash2},
		},
		{
			name:          "dedup same hash",
			defaultHashes: []string{testHash1},
			settings: &config.ScraperSettings{
				Extra: map[string]any{
					ConfigKeyExtraPlaceholderHashes: []string{testHash1},
				},
			},
			wantLen:      1,
			wantContains: []string{testHash1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := DefaultPlaceholderHashes
			DefaultPlaceholderHashes = tt.defaultHashes
			defer func() { DefaultPlaceholderHashes = original }()

			got := MergePlaceholderHashes(tt.settings)
			assert.Equal(t, tt.wantLen, len(got))

			gotSet := make(map[string]bool)
			for _, h := range got {
				gotSet[h] = true
			}
			for _, want := range tt.wantContains {
				assert.True(t, gotSet[want], "expected hash %s in result", want)
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
		name           string
		setupServer    func() *httptest.Server
		thresholdBytes int64
		hashes         []string
		wantResult     bool
		wantErr        bool
		errContains    string
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
			thresholdBytes: 10 * 1024,
			hashes:         []string{placeholderHash},
			wantResult:     true,
			wantErr:        false,
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
			thresholdBytes: 10 * 1024,
			hashes:         []string{placeholderHash},
			wantResult:     false,
			wantErr:        false,
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
			thresholdBytes: 10 * 1024,
			hashes:         []string{},
			wantResult:     false,
			wantErr:        false,
		},
		{
			name: "404 response returns false not error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			thresholdBytes: 10 * 1024,
			hashes:         []string{},
			wantResult:     false,
			wantErr:        false,
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
			thresholdBytes: 10 * 1024,
			hashes:         []string{placeholderHash},
			wantResult:     true,
			wantErr:        false,
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
			thresholdBytes: 10 * 1024,
			hashes:         []string{},
			wantResult:     false,
			wantErr:        false,
		},
		{
			name: "timeout returns error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(200 * time.Millisecond)
					w.Write(placeholderImage)
				}))
			},
			thresholdBytes: 10 * 1024,
			hashes:         []string{},
			wantResult:     false,
			wantErr:        true,
			errContains:    "context deadline exceeded",
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

			result, err := IsPlaceholder(ctx, client, server.URL, tt.thresholdBytes, tt.hashes)

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

	result, err := IsPlaceholder(ctx, client, "", 10*1024, []string{})
	assert.False(t, result)
	assert.Error(t, err)
}
