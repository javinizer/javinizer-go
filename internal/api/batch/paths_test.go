package batch

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPathWithin(t *testing.T) {
	tests := []struct {
		name string
		path string
		base string
		want bool
	}{
		{"same path", "/media/videos", "/media/videos", true},
		{"direct child", "/media/videos/anime", "/media/videos", true},
		{"deep child", "/media/videos/anime/2024", "/media/videos", true},
		{"sibling is not within", "/media/photos", "/media/videos", false},
		{"parent is not within", "/media", "/media/videos", false},
		{"traversal attempt", "/media/../etc/passwd", "/media", false},
		{"dot-prefixed child is within", "/media/videos/.Others", "/media/videos", true},
		{"dot-dot child is within", "/media/videos/..hidden", "/media/videos", true},
		{"dot-dot-slash traversal is not within", "/media/../etc", "/media", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPathWithin(tt.path, tt.base))
		})
	}

	t.Run("Windows paths", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Windows-specific path test")
		}
		assert.True(t, isPathWithin(`X:\videos\sorted`, `X:\`))
		assert.True(t, isPathWithin(`X:\`, `X:\`))
		assert.True(t, isPathWithin(`X:\videos\others`, `X:\videos`))
		assert.False(t, isPathWithin(`Y:\videos`, `X:\`))
		assert.False(t, isPathWithin(`X:\photos`, `X:\videos`))
	})
}
