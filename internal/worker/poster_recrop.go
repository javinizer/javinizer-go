package worker

import (
	"strings"

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
	if a != nil && b != nil {
		return attempted.Poster.PosterCropSourceFull == live.Poster.PosterCropSourceFull && *a == *b
	}
	if a != nil || b != nil {
		return false
	}
	// codex r6 P2: both bounds nil is ambiguous between "unmeasured preview
	// crop awaiting resolution" and "explicit removal committed mid-apply".
	// Distinguish by the cropped-pointer the removal clears while the preview
	// state keeps — otherwise a racing older failure resurrects the marker
	// over a deliberate removal.
	return attempted.Poster.CroppedPosterURL == live.Poster.CroppedPosterURL
}

// RecropRefusalPrefix is the refusal (direct) error prefix stamped onto the
// ErrorCode path by InterpretPosterRecropError. Centralized so callers and the
// clearing helper check against one constant.
//
// codex r11 P2: production recrop messages reach Error via mid-layer wrappers
// ("download failed: apply poster: poster requires a fresh crop: ..."), so a
// HasPrefix gate would miss them. Containment recognizes the wrapped form;
// the earlier "preserve unrelated failures" worry lands naturally under the
// ErrorCode==PosterRecropRequiredCode guard (matrix only enters here when the
// marker path is being resolved).
const RecropRefusalPrefix = "poster requires a fresh crop:"

func clearPosterRecrop(result *resultstore.MovieResult) {
	if result.ErrorCode == downloader.PosterRecropRequiredCode {
		result.ErrorCode = ""
		// codex r11 P2: recrop refusal text preserved verbatim on Error is the
		// sentinel; containment tolerates wrappers applied between the producer
		// (interpretApplyResult) and this helper's callers.
		if strings.Contains(result.Error, RecropRefusalPrefix) {
			result.Error = ""
		}
	}
}
