package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClampBytes_AlreadyFits(t *testing.T) {
	assert.Equal(t, "hello", clampBytes("hello", 10))
}

func TestClampBytes_ExactFit(t *testing.T) {
	assert.Equal(t, "hello", clampBytes("hello", 5))
}

func TestClampBytes_Truncation(t *testing.T) {
	assert.Equal(t, "hel", clampBytes("hello", 3))
}

func TestClampBytes_ZeroMaxBytes(t *testing.T) {
	assert.Equal(t, "hello", clampBytes("hello", 0))
}

func TestClampBytes_NegativeMaxBytes(t *testing.T) {
	assert.Equal(t, "hello", clampBytes("hello", -1))
}

func TestClampBytes_EmptyString(t *testing.T) {
	assert.Equal(t, "", clampBytes("", 5))
}

func TestClampBytes_CJKTruncation(t *testing.T) {
	// Each CJK rune is 3 bytes. With maxBytes=6, we can fit 2 runes.
	assert.Equal(t, "これ", clampBytes("これは日本語", 6))
}

func TestClampBytes_CJKExactFit(t *testing.T) {
	// 3 CJK runes = 9 bytes
	assert.Equal(t, "これは", clampBytes("これは", 9))
}

func TestClampBytes_CJKOneByteShort(t *testing.T) {
	// 3 CJK runes = 9 bytes. maxBytes=8 can only fit 2 runes (6 bytes).
	assert.Equal(t, "これ", clampBytes("これは日", 8))
}
