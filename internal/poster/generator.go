package poster

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

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
//     CroppedPosterURL and ShouldCropPoster on the movie so downstream
//     consumers (API handlers, persistence) see the updated poster state.
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

func (g *ScrapePosterGenerator) WithSSRFCheck(fn ssrfCheckFunc) *ScrapePosterGenerator {
	cp := *g
	cp.ssrfCheck = fn
	return &cp
}

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

	movie.Poster.CroppedPosterURL = result.CroppedURL
	movie.Poster.ShouldCropPoster = false
	return nil
}

// resolveReferer was removed — PosterManager.DownloadFromURL already performs
// the same auto-derivation from the download URL when referer is empty.
// Duplicating it here was redundant and meant both sites had to stay in sync.

var _ PosterGenerator = (*ScrapePosterGenerator)(nil)
