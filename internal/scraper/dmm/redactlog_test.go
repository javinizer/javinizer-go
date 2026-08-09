package dmm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactLogURL(t *testing.T) {
	result := redactLogURL("https://user:pass@example.com/page?token=secret&id=123")
	assert.NotContains(t, result, "user:pass")
	assert.NotContains(t, result, "token=secret")
	assert.Contains(t, result, "id=123")

	result2 := redactLogURL("https://www.javlibrary.com/en/?keyword=IPX-123")
	assert.Contains(t, result2, "keyword=IPX-123")

	result3 := redactLogURL("://invalid")
	assert.Equal(t, "://invalid", result3)
}
