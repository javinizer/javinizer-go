package worker

import (
	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

func resolvesPosterRecrop(bounds *models.CropBounds, sourceFull bool) bool {
	return bounds == nil || (sourceFull && bounds.Valid() && bounds.SourceFingerprint != "" && assetidentity.ValidFingerprint(bounds.SourceFingerprint))
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
