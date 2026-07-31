package actresscache

import (
	"context"
	"errors"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"
)

type thumbnailValidationResult struct {
	validation ThumbnailValidation
	err        error
}

type thumbnailValidatorCache struct {
	validator ThumbnailValidator
	group     singleflight.Group
	mu        sync.Mutex
	entries   map[string]thumbnailValidationResult
}

func newThumbnailValidatorCache(validator ThumbnailValidator) *thumbnailValidatorCache {
	return &thumbnailValidatorCache{
		validator: validator,
		entries:   make(map[string]thumbnailValidationResult),
	}
}

func (c *thumbnailValidatorCache) Validate(ctx context.Context, candidate Candidate) (ThumbnailValidation, error) {
	if c == nil || c.validator == nil {
		return ThumbnailValidation{}, errors.New("thumbnail validator cache is not initialized")
	}
	key := strings.TrimSpace(candidate.ThumbURL)
	if key == "" {
		return c.validator(ctx, candidate)
	}
	if result, ok := c.cached(key); ok {
		return result.validation, result.err
	}
	value, err, _ := c.group.Do(key, func() (any, error) {
		if result, ok := c.cached(key); ok {
			return result, result.err
		}
		validation, validationErr := c.validator(ctx, candidate)
		result := thumbnailValidationResult{validation: validation, err: validationErr}
		if validationErr == nil || isPermanentThumbnailError(validationErr) {
			c.mu.Lock()
			c.entries[key] = result
			c.mu.Unlock()
		}
		return result, validationErr
	})
	if result, ok := value.(thumbnailValidationResult); ok {
		return result.validation, result.err
	}
	return ThumbnailValidation{}, err
}

func (c *thumbnailValidatorCache) cached(key string) (thumbnailValidationResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result, ok := c.entries[key]
	return result, ok
}

func isPermanentThumbnailError(err error) bool {
	var rejected *ThumbnailRejectedError
	return errors.As(err, &rejected)
}
