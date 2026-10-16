package batch

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
)

// shouldPreserveReviewCast is the shared review intent decision for Save and
// preview. Keep version/echo classification here so both paths interpret the
// same request alike; authoritative identity fields are never writable input.
func shouldPreserveReviewCast(view *contracts.MovieView, baseline *models.Movie) bool {
	return shouldPreserveCachedActresses(contracts.MovieViewToModel(view), baseline)
}
