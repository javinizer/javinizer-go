package worker

import (
	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// resolvesPosterRecrop reports whether an update discharges a pending recrop
// block: a measured crop bound to the full source (verifiable fingerprint), or
// an explicit removal (no bounds AND no new cropped URL). An unmeasured
// preview-only crop commits no source-bound geometry, so it must NOT clear the
// block — the next apply would otherwise silently install the uncropped source.
func resolvesPosterRecrop(croppedURL string, bounds *models.CropBounds, sourceFull bool) bool {
	if bounds == nil {
		return croppedURL == ""
	}
	return sourceFull && bounds.Valid() && bounds.SourceFingerprint != "" && assetidentity.ValidFingerprint(bounds.SourceFingerprint)
}

func samePosterCropIntent(attempted, live *models.Movie) bool {
	if attempted == nil || live == nil {
		return false
	}
	a, b := attempted.Poster.PosterCropBounds, live.Poster.PosterCropBounds
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return attempted.Poster.PosterCropSourceFull == live.Poster.PosterCropSourceFull && *a == *b
}

func clearPosterRecrop(result *resultstore.MovieResult) {
	if result.ErrorCode == downloader.PosterRecropRequiredCode {
		result.ErrorCode = ""
		result.Error = ""
	}
}
