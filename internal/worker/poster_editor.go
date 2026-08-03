package worker

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// PosterEditor handles poster-related mutations on job results.
// Extracted from BatchJob to isolate the poster update concern —
// BatchJob no longer directly implements poster editing logic.
//
// PosterEditor is held by BatchJob and used to satisfy JobEditor's
// UpdatePosterCrop and UpdatePosterFromURL methods.
//
// When movieRepo is provided, PosterEditor also persists poster updates to
// the database (best-effort). This concentrates the full poster update
// lifecycle — in-memory state and DB persistence — in one place, so that
// any caller using PosterEditor automatically gets DB persistence without
// risking a split between in-memory and persistent state.
type PosterEditor struct {
	lookup    resultstore.ResultReadFacade
	updater   resultstore.ResultUpdater
	movieRepo database.MovieRepositoryInterface // optional: when set, poster updates are persisted to DB
}

// NewPosterEditor creates a PosterEditor backed by a ResultReadFacade (for
// lookups) and a ResultUpdater (for atomic mutations). If movieRepo is
// non-nil, UpdatePosterFromURL will also persist the poster change to the
// database (best-effort: DB failures are logged, not returned).
func NewPosterEditor(lookup resultstore.ResultReadFacade, updater resultstore.ResultUpdater, movieRepo database.MovieRepositoryInterface) *PosterEditor {
	return &PosterEditor{lookup: lookup, updater: updater, movieRepo: movieRepo}
}

// UpdatePosterCrop updates the cropped poster URL and the manual crop
// geometry for all files matching movieID. bounds is nil (and sourceFull
// false) when the crop was measured against a legacy already-cropped
// preview — no applyable geometry exists, so any stored geometry is cleared
// and the job keeps pre-change behavior.
func (pe *PosterEditor) UpdatePosterCrop(movieID string, croppedURL string, bounds *models.CropBounds, sourceFull bool) error {
	filePaths := pe.lookup.FindFilePathsForMovieID(movieID)
	for _, filePath := range filePaths {
		err := pe.updater.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
			if current.Movie == nil {
				return current, nil // skip files with nil Movie
			}
			movie := current.Movie.Clone()
			backupPosterOriginals(movie)
			movie.Poster.CroppedPosterURL = croppedURL
			movie.Poster.ShouldCropPoster = false
			movie.Poster.PosterCropBounds = bounds
			movie.Poster.PosterCropSourceFull = sourceFull
			current.Movie = movie
			current.FileMatchInfo.MovieID = movie.ID
			return current, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdatePosterFromURL updates the poster URL and cropped poster URL for all files matching movieID.
// When a movieRepo is configured, the poster change is also persisted to the database.
// DB persistence is best-effort: failures are logged but do not propagate to the caller.
func (pe *PosterEditor) UpdatePosterFromURL(ctx context.Context, movieID string, posterURL string, croppedURL string) error {
	filePaths := pe.lookup.FindFilePathsForMovieID(movieID)
	for _, filePath := range filePaths {
		err := pe.updater.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
			if current.Movie == nil {
				return current, nil // skip files with nil Movie
			}
			movie := current.Movie.Clone()
			backupPosterOriginals(movie)
			movie.Poster.PosterURL = posterURL
			movie.Poster.CroppedPosterURL = croppedURL
			movie.Poster.ShouldCropPoster = false
			clearPosterCropGeometry(movie) // new source: stored geometry is stale
			current.Movie = movie
			current.FileMatchInfo.MovieID = movie.ID
			return current, nil
		})
		if err != nil {
			return err
		}
	}

	// Persist poster update to database. Best-effort: failures are logged but
	// do not fail the request, matching the previous adapter-level behavior.
	if pe.movieRepo != nil {
		posterID := movieID
		if mr, _ := pe.lookup.FindMovieResultForMovieID(movieID); mr != nil && mr.Movie != nil && mr.Movie.ID != "" {
			posterID = mr.Movie.ID
		}
		existing, dbErr := pe.movieRepo.FindByID(ctx, posterID)
		if dbErr == nil && existing != nil {
			existing.Poster.PosterURL = posterURL
			existing.Poster.CroppedPosterURL = croppedURL
			if _, upErr := pe.movieRepo.Upsert(ctx, existing); upErr != nil {
				logging.Warnf("Failed to update movie poster in database: %v", upErr)
			}
		} else if dbErr != nil {
			logging.Warnf("Failed to find movie %s for poster update: %v", posterID, dbErr)
		}
	}

	return nil
}

// clearPosterCropGeometry drops persisted manual crop geometry from m.
// Called at every flow that replaces the poster source or crop intent so a
// stale crop can never be applied to a different image.
func clearPosterCropGeometry(m *models.Movie) {
	if m == nil {
		return
	}
	m.Poster.PosterCropBounds = nil
	m.Poster.PosterCropSourceFull = false
}

// sanitizePosterCropGeometry enforces the manual-crop invalidation contract
// when a whole movie is stored (UpdateMovie): carried geometry survives only
// if it is valid AND the movie's EFFECTIVE poster source (poster_url, falling
// back to cover_url — the same selection the downloader and the apply boundary
// use) and crop intent are unchanged. A fanart-only edit (cover_url changes
// while poster_url selects the source) must not discard a still-valid crop.
// A nil next-bounds means "no geometry" (the batch PATCH handler resolves
// omitted-vs-explicit-null upstream) — only normalize the flag in that case.
func sanitizePosterCropGeometry(next *models.Movie, haveCurrent bool, curPosterURL, curCoverURL string, curShouldCrop bool) {
	if next == nil {
		return
	}
	if next.Poster.PosterCropBounds == nil {
		next.Poster.PosterCropSourceFull = false
		return
	}
	if !next.Poster.PosterCropBounds.Valid() {
		clearPosterCropGeometry(next)
		return
	}
	if !haveCurrent {
		return
	}
	if effectivePosterSourceOf(next.Poster.PosterURL, next.Poster.CoverURL) != effectivePosterSourceOf(curPosterURL, curCoverURL) ||
		next.Poster.ShouldCropPoster != curShouldCrop {
		clearPosterCropGeometry(next)
	}
}

// effectivePosterSourceOf mirrors the downloader's poster source selection:
// poster_url when present, otherwise cover_url.
func effectivePosterSourceOf(posterURL, coverURL string) string {
	if posterURL != "" {
		return posterURL
	}
	return coverURL
}

// backupPosterOriginals preserves the original poster URLs before they are overwritten.
//
// The sentinel for "baseline already captured" is EITHER field present:
// — OriginalPosterURL non-empty covers legacy envelopes (URL-only backups
// predate the eager baseline) — while OriginalShouldCropPoster non-nil covers
// cover-fallback movies whose baseline legitimately has an empty poster URL;
// URL-only sentinel would re-snapshot on the SECOND crop of such a movie and
// store the first manual crop as the "original".
func backupPosterOriginals(movie *models.Movie) {
	if movie.Poster.OriginalPosterURL == "" && movie.Poster.OriginalShouldCropPoster == nil {
		shouldCrop := movie.Poster.ShouldCropPoster
		movie.Poster.OriginalPosterURL = movie.Poster.PosterURL
		movie.Poster.OriginalCroppedPosterURL = movie.Poster.CroppedPosterURL
		movie.Poster.OriginalShouldCropPoster = &shouldCrop
	}
}

// backupCoverOriginal preserves the original cover URL so the cover/fanart
// reset survives server restarts. The existing movie (current) holds the
// authoritative original snapshot; the incoming movie (next) is what the
// client wants to persist. If an original was already captured on the
// existing movie, carry it forward. Otherwise, if the cover is changing,
// snapshot the existing cover as the original.
func backupCoverOriginal(current, next *models.Movie) {
	if current == nil || next == nil {
		return
	}
	if orig := current.Poster.OriginalCoverURL; orig != "" {
		next.Poster.OriginalCoverURL = orig
		return
	}
	if current.Poster.CoverURL != "" && current.Poster.CoverURL != next.Poster.CoverURL {
		next.Poster.OriginalCoverURL = current.Poster.CoverURL
	}
}

// establishScrapedBaseline sets the poster-original revert group on target
// from source's current poster fields, establishing the scraper's value as
// the Reset baseline. Called by both the initial scrape phase and the
// rescrape phase (merge + non-merge paths) so the review UI's Reset always
// returns to what the scraper produced — never a stale prior-content value
// carried across a content-id change. The baseline may legitimately be empty
// when the scraper found no image; the frontend falls back to the current
// field, so an empty baseline makes Reset a no-op rather than wiping a valid
// image.
//
// URL fields are trimmed so the baseline matches the display field's
// trimming in mergeRescrapeMovie (a whitespace-only scraper value should
// not become a non-empty baseline that falsely enables the Reset button).
//
// This is the eager counterpart to backupPosterOriginals: backupPosterOriginals
// snapshots the pre-edit state lazily on the first manual edit, while
// establishScrapedBaseline snapshots the scraped state eagerly at scrape time.
// Mirrors backupPosterOriginals' field grouping (PosterURL/CroppedPosterURL/
// ShouldCropPoster) and extends it to CoverURL, which the lazy backup handles
// separately via backupCoverOriginal.
func establishScrapedBaseline(target, source *models.Movie) {
	if target == nil || source == nil {
		return
	}
	posterURL := strings.TrimSpace(source.Poster.PosterURL)
	croppedURL := strings.TrimSpace(source.Poster.CroppedPosterURL)
	target.Poster.OriginalPosterURL = posterURL
	target.Poster.OriginalCroppedPosterURL = croppedURL
	// Only anchor the crop baseline when there's a real poster baseline. When
	// the scraper found no image, leave OriginalShouldCropPoster nil so the
	// frontend falls back to the current field (matching the empty-URL
	// fallback) instead of a non-nil false that could spuriously enable Reset.
	if posterURL != "" || croppedURL != "" {
		shouldCrop := source.Poster.ShouldCropPoster
		target.Poster.OriginalShouldCropPoster = &shouldCrop
	} else {
		target.Poster.OriginalShouldCropPoster = nil
	}
	target.Poster.OriginalCoverURL = strings.TrimSpace(source.Poster.CoverURL)
}
