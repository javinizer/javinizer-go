package batch

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
)

// shouldPreserveReviewCast is the shared review intent decision for Save and
// preview. Keep version/echo classification here so both paths interpret the
// same request alike; authoritative identity fields are never writable input.
func shouldPreserveReviewCast(view *contracts.MovieView, baseline *models.Movie) bool {
	payload := contracts.MovieViewToModel(view)
	preserve := shouldPreserveCachedActresses(payload, baseline)
	// A fresh cast version can identify an intentional edit back to an old
	// cached cast (for example re-adding a just-suppressed actor). An echo
	// matching its advertised cast version still preserves current authority.
	if preserve && payload.Actresses != nil && baseline != nil &&
		strings.TrimSpace(view.CastVersion) != "" && view.CastVersion != models.ActressCastVersion(baseline.Actresses) && !castVersionMatchesEcho(view) {
		return false
	}
	return preserve
}

// castVersionMatchesEcho compares the response-only identity fields without
// persisting them. An unchanged fresh echo can differ from the job snapshot
// after a canonical identity update and still needs no new revision field.
func castVersionMatchesEcho(view *contracts.MovieView) bool {
	cast := contracts.ActressViewSliceToModels(view.Actresses)
	for i, actress := range view.Actresses {
		cast[i].NameKey = actress.NameKey
		cast[i].Verified = actress.Verified
		cast[i].Origin = actress.Origin
	}
	return view.CastVersion == models.ActressCastVersion(cast)
}
