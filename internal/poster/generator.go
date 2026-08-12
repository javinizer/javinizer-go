package poster

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// PosterGenerator generates a poster for a movie during a scrape job.
type PosterGenerator interface {
	GeneratePoster(ctx context.Context, jobID string, movie *models.Movie) error
}

// ScrapePosterGenerator is a domain adapter that sits between the scrape pipeline
// and the low-level PosterManager. It earns its keep through three responsibilities
// that would not belong in PosterManager (which operates on primitive IDs/URLs,
// not models.Movie):
//
//  1. Poster→Cover URL fallback: resolves the poster URL from the movie's
//     PosterURL field, falling back to CoverURL when no explicit poster exists.
//  2. Movie state mutation: after a successful download, sets
//     CroppedPosterURL on the movie so downstream consumers (API handlers,
//     persistence) see the updated temp preview poster. It intentionally
//     does NOT touch ShouldCropPoster: that flag is the aggregator's
//     source-derived statement about whether the FINAL poster needs
//     cropping, and the apply-phase downloadPoster relies on it surviving
//     scrape -> commit -> apply to crop the on-disk poster. Resetting it
//     here (as an earlier version did) defeated that gate and left the
//     final folder poster uncropped.
//  3. Error sanitization: wraps download errors through sanitizedErrorFrom/
//     stripSensitivePaths so internal filesystem paths never leak to callers.
//
// The referer auto-resolution (deriving Referer from the download URL's origin)
// is intentionally NOT duplicated here — PosterManager.DownloadFromURL already
// performs that fallback internally when referer is empty.
type ScrapePosterGenerator struct {
	manager   PosterManagerInterface
	userAgent string
	referer   string
	ssrfCheck ssrfCheckFunc
}

// NewScrapePosterGenerator creates a ScrapePosterGenerator backed by the given
// PosterManager. userAgent and referer are forwarded to DownloadFromURL for
// HTTP request headers. When referer is empty, DownloadFromURL auto-derives
// it from the download URL's origin.
func NewScrapePosterGenerator(manager PosterManagerInterface, userAgent string, referer string) *ScrapePosterGenerator {
	return &ScrapePosterGenerator{
		manager:   manager,
		userAgent: userAgent,
		referer:   referer,
	}
}

// WithSSRFCheck returns a copy of the generator that validates download URLs against the given SSRF check.
func (g *ScrapePosterGenerator) WithSSRFCheck(fn ssrfCheckFunc) *ScrapePosterGenerator {
	cp := *g
	cp.ssrfCheck = fn
	return &cp
}

// GeneratePoster downloads and stores a poster for the movie, falling back to the cover URL when no poster URL is set.
func (g *ScrapePosterGenerator) GeneratePoster(ctx context.Context, jobID string, movie *models.Movie) error {
	if g.manager == nil || movie == nil {
		return nil
	}

	posterURL := movie.Poster.PosterURL
	if posterURL == "" {
		posterURL = movie.Poster.CoverURL
	}
	if posterURL == "" {
		return fmt.Errorf("no poster or cover URL available")
	}

	// Pass the explicit referer if set; otherwise let DownloadFromURL auto-derive
	// it from the download URL's origin (it already implements that fallback).
	// jobID is the batch job ID so posters are stored under the correct directory
	// and accessible via the temp poster API endpoint.
	result, err := g.manager.DownloadFromURL(ctx, jobID, movie.ID, posterURL, g.userAgent, g.referer)
	if err != nil {
		sanitizedErr := sanitizedErrorFrom(err)
		logging.Warnf("[scrape] Failed to create temp poster: %s (continuing anyway)", stripSensitivePaths(err))
		return sanitizedErr
	}

	// CroppedPosterURL points at the temp preview poster (always cropped by
	// DownloadFromURL). ShouldCropPoster is deliberately left untouched: it is
	// the aggregator's source-derived flag that the apply-phase downloadPoster
	// uses to decide whether to crop the FINAL on-disk poster.
	movie.Poster.CroppedPosterURL = result.CroppedURL
	return nil
}

// resolveReferer was removed — PosterManager.DownloadFromURL already performs
// the same auto-derivation from the download URL when referer is empty.
// Duplicating it here was redundant and meant both sites had to stay in sync.

// StagingPosterGenerator splits poster generation into an unlocked network
// stage and an in-lock promote/commit (POSTER-WRITE-HARDENING P2): the edit
// lock window never covers a network fetch. Generators that lack staging
// support keep serving GeneratePoster (the legacy in-lock path).
type StagingPosterGenerator interface {
	// StagePoster resolves the poster source (poster→cover fallback) and
	// downloads it to a unique staged identity. Network/unlocked only.
	StagePoster(ctx context.Context, jobID string, movie *models.Movie) (*StagedPoster, error)
	// CommitStagedPoster promotes staged bytes to canonical names (fs-only
	// — the caller's lock must be held) and updates the movie preview URL.
	CommitStagedPoster(movie *models.Movie, staged *StagedPoster) error
	// DiscardStaged removes staged residue after a declined/aborted op.
	DiscardStaged(staged *StagedPoster)
}

// StagePoster resolves the poster URL (poster→cover fallback) and stages the
// download under a unique identity. Errors are path-sanitized like
// GeneratePoster.
func (g *ScrapePosterGenerator) StagePoster(ctx context.Context, jobID string, movie *models.Movie) (*StagedPoster, error) {
	if g.manager == nil || movie == nil {
		return nil, nil
	}
	posterURL := movie.Poster.PosterURL
	if posterURL == "" {
		posterURL = movie.Poster.CoverURL
	}
	if posterURL == "" {
		return nil, fmt.Errorf("no poster or cover URL available")
	}
	staged, err := g.manager.StagePosterDownload(ctx, StagePosterRequest{
		JobID: jobID, PosterID: movie.ID, URL: posterURL,
		UserAgent: g.userAgent, Referer: g.referer,
	})
	if err != nil {
		logging.Warnf("[scrape] Failed to stage temp poster: %s (continuing anyway)", stripSensitivePaths(err))
		return nil, sanitizedErrorFrom(err)
	}
	return staged, nil
}

// CommitStagedPoster promotes the staged pair under canonical names and sets
// the movie's preview pointer. Call it with the caller's edit lock held,
// immediately before the state commit.
func (g *ScrapePosterGenerator) CommitStagedPoster(movie *models.Movie, staged *StagedPoster) error {
	if g.manager == nil || movie == nil || staged == nil {
		return nil
	}
	res, err := g.manager.PromoteStagedPoster(staged)
	if err != nil {
		sanitizedErr := sanitizedErrorFrom(err)
		logging.Warnf("[scrape] Failed to promote temp poster: %s (continuing anyway)", stripSensitivePaths(err))
		return sanitizedErr
	}
	movie.Poster.CroppedPosterURL = res.CroppedURL
	return nil
}

// DiscardStaged removes staged residue (declines/cancel paths).
func (g *ScrapePosterGenerator) DiscardStaged(staged *StagedPoster) {
	if g.manager == nil {
		return
	}
	g.manager.DiscardStagedPoster(staged)
}

var _ StagingPosterGenerator = (*ScrapePosterGenerator)(nil)

var _ PosterGenerator = (*ScrapePosterGenerator)(nil)
