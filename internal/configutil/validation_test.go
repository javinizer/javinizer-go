package configutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRequestDelay(t *testing.T) {
	tests := []struct {
		name    string
		delay   int
		wantErr bool
		errMsg  string
	}{
		{
			name:    "negative returns error",
			delay:   -1,
			wantErr: true,
			errMsg:  "request_delay.request_delay must be non-negative",
		},
		{
			name:    "zero is valid",
			delay:   0,
			wantErr: false,
		},
		{
			name:    "500 is valid",
			delay:   500,
			wantErr: false,
		},
		{
			name:    "5000 is valid",
			delay:   5000,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequestDelay("request_delay", tt.delay)
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

func TestValidateHTTPBaseURL(t *testing.T) {
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
