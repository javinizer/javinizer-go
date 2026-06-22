package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStoreUmask(t *testing.T) {
	original := cachedUmask.Load()
	defer cachedUmask.Store(original)

	StoreUmask(0o002)
	if got := cachedUmask.Load(); got != 0o002 {
		t.Errorf("StoreUmask(002): got %o, want %o", got, 0o002)
	}

	StoreUmask(0o022)
	if got := cachedUmask.Load(); got != 0o022 {
		t.Errorf("StoreUmask(022): got %o, want %o", got, 0o022)
	}

	StoreUmask(0)
	if got := cachedUmask.Load(); got != 0 {
		t.Errorf("StoreUmask(0): got %o, want 0", got)
	}
}

// TestValidateHTTPBaseURL_Exported tests the exported ValidateHTTPBaseURL
// which allows empty strings (optional fields) unlike the unexported version.
func TestValidateHTTPBaseURL_Exported(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty string is valid (optional)",
			raw:     "",
			wantErr: false,
		},
		{
			name:    "http URL is valid",
			raw:     "http://example.com",
			wantErr: false,
		},
		{
			name:    "https URL is valid",
			raw:     "https://example.com",
			wantErr: false,
		},
		{
			name:    "ftp URL returns error",
			raw:     "ftp://example.com",
			wantErr: true,
			errMsg:  "base_url must be a valid HTTP or HTTPS URL",
		},
		{
			name:    "invalid returns error",
			raw:     "invalid",
			wantErr: true,
			errMsg:  "base_url must be a valid HTTP or HTTPS URL",
		},
		{
			name:    "whitespace trimmed",
			raw:     "  https://example.com  ",
			wantErr: false,
		},
		{
			name:    "parse error",
			raw:     "http://\x00invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHTTPBaseURL("base_url", tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Equal(t, tt.errMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPermissionConstants(t *testing.T) {
	assert.Equal(t, 0777, DirPerm)
	assert.Equal(t, 0700, DirPermTemp)
	assert.Equal(t, 0666, FilePerm)
}

func TestUserAgentConstants(t *testing.T) {
	assert.NotEmpty(t, DefaultUserAgent)
	assert.NotEmpty(t, DefaultScraperUserAgent)
	assert.Contains(t, DefaultUserAgent, "Javinizer")
	assert.Contains(t, DefaultScraperUserAgent, "Mozilla")
}
