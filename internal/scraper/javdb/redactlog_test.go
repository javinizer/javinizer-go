package javdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactLogURL(t *testing.T) {
	// Strips userinfo and secret query params
	result := redactLogURL("https://user:pass@example.com/page?token=secret&id=123")
	assert.NotContains(t, result, "user:pass")
	assert.NotContains(t, result, "token=secret")
	assert.Contains(t, result, "id=123")

	// Preserves keyword
	result2 := redactLogURL("https://www.javlibrary.com/en/?keyword=IPX-123")
	assert.Contains(t, result2, "keyword=IPX-123")

	// Invalid URL returns as-is
	result3 := redactLogURL("://invalid")
	assert.Equal(t, "://invalid", result3)
}
