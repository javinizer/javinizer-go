package poster

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripSensitivePaths_Unexported(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "nil error",
			err:     nil,
			wantMsg: "",
		},
		{
			name:    "strips absolute path",
			err:     fmt.Errorf("failed to create temp file in /tmp/posters/scrape/ABC-001-full.jpg"),
			wantMsg: "failed to create temp file in [path]",
		},
		{
			name:    "strips relative path",
			err:     fmt.Errorf("failed to write to /data/temp/posters/scrape/ABC-001.jpg"),
			wantMsg: "failed to write to [path]",
		},
		{
			name:    "strips URL",
			err:     fmt.Errorf("failed to download poster from https://example.com/images/poster.jpg"),
			wantMsg: "failed to download poster from [url]",
		},
		{
			name:    "no paths or URLs",
			err:     fmt.Errorf("no poster or cover URL available"),
			wantMsg: "no poster or cover URL available",
		},
		{
			name:    "multiple paths and URLs",
			err:     fmt.Errorf("download from https://cdn.example.com/img.jpg to /var/tmp/poster.jpg failed"),
			wantMsg: "download from [url] to [path] failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripSensitivePaths(tt.err)
			assert.Equal(t, tt.wantMsg, got)
		})
	}
}

func TestSanitizedError(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		assert.Nil(t, sanitizedErrorFrom(nil))
	})
	t.Run("non-nil returns sanitized error", func(t *testing.T) {
		err := fmt.Errorf("failed to write /tmp/posters/scrape/ABC-001.jpg")
		sanitized := sanitizedErrorFrom(err)
		require.NotNil(t, sanitized)
		assert.Contains(t, sanitized.Error(), "[path]")
		assert.NotContains(t, sanitized.Error(), "/tmp/posters")
	})
	t.Run("Unwrap preserves original error for errors.Is", func(t *testing.T) {
		original := fmt.Errorf("failed to write /tmp/posters/scrape/ABC-001.jpg")
		sanitized := sanitizedErrorFrom(original)
		require.NotNil(t, sanitized)
		unwrapped := errors.Unwrap(sanitized)
		require.NotNil(t, unwrapped)
		assert.Equal(t, original, unwrapped)
		assert.True(t, errors.Is(sanitized, original))
	})
	t.Run("sanitizedError type supports errors.As", func(t *testing.T) {
		original := fmt.Errorf("failed to create /var/tmp/poster.jpg")
		sanitized := sanitizedErrorFrom(original)
		require.NotNil(t, sanitized)
		var se *sanitizedError
		assert.True(t, errors.As(sanitized, &se))
		assert.Contains(t, se.Error(), "[path]")
		assert.NotContains(t, se.Error(), "/var/tmp")
	})
}
