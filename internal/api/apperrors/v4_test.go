package apperrors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPathErrorV4(t *testing.T) {
	t.Run("nil error returns false", func(t *testing.T) {
		assert.False(t, IsPathError(nil, &PathError{Code: "test"}))
	})

	t.Run("nil target returns false", func(t *testing.T) {
		err := &PathError{Code: "test"}
		assert.False(t, IsPathError(err, nil))
	})

	t.Run("matching code returns true", func(t *testing.T) {
		err := &PathError{Code: "not_found"}
		assert.True(t, IsPathError(err, &PathError{Code: "not_found"}))
	})

	t.Run("non-matching code returns false", func(t *testing.T) {
		err := &PathError{Code: "not_found"}
		assert.False(t, IsPathError(err, &PathError{Code: "permission"}))
	})

	t.Run("non-PathError returns false", func(t *testing.T) {
		assert.False(t, IsPathError(assert.AnError, &PathError{Code: "test"}))
	})
}

func TestNewPathErrorV4(t *testing.T) {
	base := &PathError{Code: "test_code"}
	pe := NewPathError(base, "/test/path")
	assert.NotNil(t, pe)
	assert.Equal(t, errorCode("test_code"), pe.Code)
	assert.Equal(t, "/test/path", pe.Path)
}
