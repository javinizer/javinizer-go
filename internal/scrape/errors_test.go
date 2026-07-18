package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestClassifyFailures(t *testing.T) {
	t.Run("empty returns unknown", func(t *testing.T) {
		assert.Equal(t, models.ScraperErrorKindUnknown, classifyFailures(nil))
	})

	t.Run("single not_found", func(t *testing.T) {
		f := []models.ScraperError{{Kind: models.ScraperErrorKindNotFound}}
		assert.Equal(t, models.ScraperErrorKindNotFound, classifyFailures(f))
	})

	t.Run("not_found takes precedence over rate_limited and unavailable", func(t *testing.T) {
		f := []models.ScraperError{
			{Kind: models.ScraperErrorKindUnavailable},
			{Kind: models.ScraperErrorKindRateLimited},
			{Kind: models.ScraperErrorKindNotFound},
		}
		assert.Equal(t, models.ScraperErrorKindNotFound, classifyFailures(f))
	})

	t.Run("blocked takes precedence over rate_limited", func(t *testing.T) {
		f := []models.ScraperError{
			{Kind: models.ScraperErrorKindRateLimited},
			{Kind: models.ScraperErrorKindBlocked},
		}
		assert.Equal(t, models.ScraperErrorKindBlocked, classifyFailures(f))
	})

	t.Run("rate_limited takes precedence over unavailable", func(t *testing.T) {
		f := []models.ScraperError{
			{Kind: models.ScraperErrorKindUnavailable},
			{Kind: models.ScraperErrorKindRateLimited},
		}
		assert.Equal(t, models.ScraperErrorKindRateLimited, classifyFailures(f))
	})

	t.Run("empty kind treated as unknown", func(t *testing.T) {
		f := []models.ScraperError{{Kind: ""}}
		assert.Equal(t, models.ScraperErrorKindUnknown, classifyFailures(f))
	})

	t.Run("unknown present with not_found returns not_found", func(t *testing.T) {
		f := []models.ScraperError{
			{Kind: models.ScraperErrorKindUnknown},
			{Kind: models.ScraperErrorKindNotFound},
		}
		assert.Equal(t, models.ScraperErrorKindNotFound, classifyFailures(f))
	})

	t.Run("kind not in precedence returns unknown via final fallback", func(t *testing.T) {
		f := []models.ScraperError{{Kind: models.ScraperErrorKind("custom_kind")}}
		assert.Equal(t, models.ScraperErrorKindUnknown, classifyFailures(f))
	})
}
