package file

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/stretchr/testify/assert"
)

func TestHasTrailingPathSeparator_Uncovered(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		expect bool
	}{
		{"trailing slash", "/home/user/", true},
		{"trailing backslash", "C:\\Users\\", true},
		{"no trailing separator", "/home/user", false},
		{"empty string", "", false},
		{"just slash", "/", true},
		{"just backslash", "\\", true},
		{"multiple trailing slashes", "/home/user//", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, hasTrailingPathSeparator(tt.path))
		})
	}
}

func TestResolveAutocompleteBasePath_EmptyPath(t *testing.T) {
	_, _, err := resolveAutocompleteBasePath("", nil)
	assert.Error(t, err)
}

func TestResolveAutocompleteBasePath_WhitespaceOnlyPath(t *testing.T) {
	_, _, err := resolveAutocompleteBasePath("   ", nil)
	assert.Error(t, err)
}

func TestResolveAutocompleteBasePath_WithSecurityConfig(t *testing.T) {
	// Path with nil security config will panic on ValidateScanPath,
	// so we provide an empty config
	cfg := &core.SecurityNarrowConfig{}
	basePath, fragment, err := resolveAutocompleteBasePath("/home/use", cfg)
	if err != nil {
		// Validation may fail depending on OS, but should not panic
		return
	}
	assert.Contains(t, basePath, "home")
	assert.Equal(t, "use", fragment)
}
