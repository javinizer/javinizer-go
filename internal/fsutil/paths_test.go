package fsutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizePath_Basic tests path normalization on non-Windows platforms.
func TestNormalizePath_Basic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple path", "/foo/bar", "/foo/bar"},
		{"empty path", "", ""},
		{"dot path", ".", "."},
		{"dotdot path", "..", ".."},
		{"root path", "/", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizePath_WindowsDriveLetter tests drive letter stripping logic.
func TestNormalizePath_WindowsDriveLetter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"C: with forward slash", "C:/Users/test", "/Users/test"},
		{"D: with forward slash", "D:/data/files", "/data/files"},
		{"short drive path", "E:/a", "/a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
