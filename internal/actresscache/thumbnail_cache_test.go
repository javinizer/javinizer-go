package actresscache

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThumbnailValidatorCacheReusesSuccessfulResult(t *testing.T) {
	calls := 0
	cache := newThumbnailValidatorCache(func(context.Context, Candidate) (ThumbnailValidation, error) {
		calls++
		return ThumbnailValidation{SHA256: "digest"}, nil
	})
	candidate := Candidate{ThumbURL: "https://example.test/thumb.jpg"}
	first, err := cache.Validate(context.Background(), candidate)
	require.NoError(t, err)
	second, err := cache.Validate(context.Background(), candidate)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, calls)
}

func TestThumbnailValidatorCacheRetriesTransientErrors(t *testing.T) {
	calls := 0
	cache := newThumbnailValidatorCache(func(context.Context, Candidate) (ThumbnailValidation, error) {
		calls++
		if calls == 1 {
			return ThumbnailValidation{}, errors.New("temporary failure")
		}
		return ThumbnailValidation{SHA256: "digest"}, nil
	})
	candidate := Candidate{ThumbURL: "https://example.test/thumb.jpg"}
	_, err := cache.Validate(context.Background(), candidate)
	require.Error(t, err)
	_, err = cache.Validate(context.Background(), candidate)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestThumbnailValidatorCacheReusesPermanentRejection(t *testing.T) {
	calls := 0
	cache := newThumbnailValidatorCache(func(context.Context, Candidate) (ThumbnailValidation, error) {
		calls++
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: "placeholder"}
	})
	candidate := Candidate{ThumbURL: "https://example.test/placeholder.jpg"}
	_, firstErr := cache.Validate(context.Background(), candidate)
	_, secondErr := cache.Validate(context.Background(), candidate)
	var firstRejected *ThumbnailRejectedError
	var secondRejected *ThumbnailRejectedError
	require.ErrorAs(t, firstErr, &firstRejected)
	require.ErrorAs(t, secondErr, &secondRejected)
	assert.Equal(t, 1, calls)
}
