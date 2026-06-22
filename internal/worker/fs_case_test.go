package worker

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestIsCaseInsensitiveFS(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	tmpDir := t.TempDir()

	result := cache.IsCaseInsensitive(tmpDir)

	t.Logf("Filesystem case-insensitive: %v", result)
}

func TestFSCaseCache_IsCaseInsensitive_CachesResult(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())

	tmpDir := t.TempDir()

	result1 := cache.IsCaseInsensitive(tmpDir)
	result2 := cache.IsCaseInsensitive(tmpDir)

	assert.Equal(t, result1, result2, "cached results should match for same directory")

	cache.mu.RLock()
	cached, exists := cache.cache[tmpDir]
	cache.mu.RUnlock()
	assert.True(t, exists, "result should be cached under exact directory path")
	assert.Equal(t, result1, cached, "cached value should match returned value")
}

func TestIsCaseInsensitiveFS_HandlesNonexistentDir(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	result := cache.IsCaseInsensitive("/nonexistent/path/that/does/not/exist")

	assert.False(t, result, "nonexistent directory should default to case-sensitive (safe)")
}

func TestFSCaseCache_Reset(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	tmpDir := t.TempDir()

	_ = cache.IsCaseInsensitive(tmpDir)

	cache.mu.RLock()
	_, exists := cache.cache[tmpDir]
	cache.mu.RUnlock()
	assert.True(t, exists, "result should be cached before reset")

	cache.Reset()

	cache.mu.RLock()
	_, existsAfter := cache.cache[tmpDir]
	cache.mu.RUnlock()
	assert.False(t, existsAfter, "cache should be empty after reset")

	result := cache.IsCaseInsensitive(tmpDir)
	assert.NotNil(t, result)
}

func TestSuffixOrder(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 100},        // Empty string → 100 (sorted last)
		{"-A", 0},        // Single uppercase letter A
		{"-B", 1},        // Single uppercase letter B
		{"-Z", 25},       // Single uppercase letter Z
		{"-1", 11},       // Numeric suffix → 10 + 1
		{"-2", 12},       // Numeric suffix → 10 + 2
		{"-5", 15},       // Numeric suffix → 10 + 5
		{"-10", 20},      // Numeric suffix → 10 + 10
		{"-pt1", 11},     // pt-prefixed → 10 + 1
		{"-pt2", 12},     // pt-prefixed → 10 + 2
		{"-pt10", 20},    // pt-prefixed → 10 + 10
		{"pt1", 11},      // pt-prefixed without dash → 10 + 1
		{"pt3", 13},      // pt-prefixed without dash → 10 + 3
		{"-unknown", 50}, // Unrecognized → 50
		{"abc", 50},      // Unrecognized → 50
		{"A", 0},         // Without dash prefix
		{"1", 11},        // Without dash prefix numeric
		{"2", 12},        // Without dash prefix numeric
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := suffixOrder(tt.input)
			assert.Equal(t, tt.expected, result, "suffixOrder(%q) = %d, want %d", tt.input, result, tt.expected)
		})
	}
}

func TestSuffixOrder_Ordering(t *testing.T) {
	// Verify that suffixOrder produces correct relative ordering
	assert.Less(t, suffixOrder("A"), suffixOrder("B"), "A should sort before B")
	assert.Less(t, suffixOrder("B"), suffixOrder("1"), "B should sort before 1")
	assert.Less(t, suffixOrder("1"), suffixOrder("abc"), "1 should sort before unrecognized")
	assert.Less(t, suffixOrder("abc"), suffixOrder(""), "unrecognized should sort before empty")
}
