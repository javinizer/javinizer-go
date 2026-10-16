package batch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

const castPreviewTimeout = 2 * time.Second

// previewCastUnchanged compares writable cast fields only. Response-only identity
// provenance is stripped on both sides, just as it is for the proposed DTO.
func previewCastUnchanged(proposed, baseline *models.Movie) bool {
	return shouldPreserveCachedActresses(proposed, contracts.MovieViewToModel(contracts.MovieViewFromModel(baseline)))
}

// previewProposedCast uses the same DB-only rename and reconciliation legs as
// Save, on detached inputs in a rollback-only transaction. Plain title edits and
// unchanged stale echoes need no write transaction. No job/envelope/filesystem
// publication is involved; a result is usable only after checked rollback.
func previewProposedCast(ctx context.Context, db *database.DB, authority, baseline, proposed *models.Movie, view *contracts.MovieView, knownPersisted bool) (*models.Movie, *previewResolveError) {
	if previewCastUnchanged(proposed, authority) {
		return authority, nil
	}
	version := strings.TrimSpace(view.CastVersion)
	if shouldPreserveReviewCast(view, baseline) {
		return authority, nil
	}
	if version == "" || version != models.ActressCastVersion(authority.Actresses) {
		return nil, &previewResolveError{Status: http.StatusConflict, Err: "movie cast changed in database; refresh and retry"}
	}
	if db == nil {
		return nil, &previewResolveError{Status: http.StatusServiceUnavailable, Err: "cast preview requires an authoritative database"}
	}
	ctx, cancel := context.WithTimeout(ctx, castPreviewTimeout)
	defer cancel()
	candidate := proposed.Clone()
	candidate.Credits = nil // echoed credits never participate in reconciliation
	var projected *models.Movie
	err := db.WithPreviewTx(ctx, func(u database.EditUnit) error {
		current, err := u.Movies.FindByContentID(ctx, authority.ContentID)
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		if current == nil {
			if knownPersisted {
				return &worker.EditAdmissionConflictError{Message: "persisted movie missing in database; refresh and retry"}
			}
			current = authority
		}
		if version != models.ActressCastVersion(current.Actresses) {
			return &worker.EditAdmissionConflictError{Message: "movie cast changed in database; refresh and retry"}
		}
		renames := make([]worker.ActressRenamePlan, 0, len(candidate.Actresses))
		for _, actress := range candidate.Actresses {
			if actress.ID != 0 {
				renames = append(renames, worker.ActressRenamePlan{ID: actress.ID, FirstName: actress.FirstName, LastName: actress.LastName, JapaneseName: actress.JapaneseName, ThumbURL: actress.ThumbURL, ThumbEdited: actress.ThumbEdited})
			}
		}
		if err := worker.ApplyActressRenames(ctx, u.Actresses, renames); err != nil {
			return err
		}
		projected, err = u.Movies.Upsert(ctx, candidate)
		return err
	})
	if err != nil {
		status := http.StatusInternalServerError
		var conflict *worker.EditAdmissionConflictError
		if errors.As(err, &conflict) {
			status = http.StatusConflict
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusServiceUnavailable
		}
		return nil, &previewResolveError{Status: status, Err: "failed to preview movie cast: " + err.Error()}
	}
	return projected, nil
}
