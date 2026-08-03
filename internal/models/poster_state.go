package models

import "math"

// PosterState groups the seven poster/cropping fields extracted from Movie.
// Embedded in Movie with gorm:"embedded" so column names are preserved (zero-migration).
// JSON serialization is handled by custom MarshalJSON/UnmarshalJSON on Movie to keep
// the flat wire format — do NOT change the json tag on the Movie.Poster field from
// json:"-" or poster fields will disappear from API responses.
type PosterState struct {
	// PosterCropBounds is the manual review-page crop geometry, normalized to
	// 0–1 fractions of the full-size source image measured at crop time.
	// Runtime-only (gorm:"-"): review intent is not library truth — no DB column.
	// Nil when no manual crop is pending apply.
	PosterCropBounds *CropBounds `json:"poster_crop_bounds,omitempty" gorm:"-"`
	// PosterCropSourceFull records whether the bounds were measured against the
	// full-size source (<id>-full.jpg). False (legacy already-cropped fallback)
	// means the bounds are not applyable at organize time.
	PosterCropSourceFull     bool   `json:"poster_crop_source_full,omitempty" gorm:"-"`
	PosterURL                string `json:"poster_url"`
	CoverURL                 string `json:"cover_url"`
	CroppedPosterURL         string `json:"cropped_poster_url"`
	ShouldCropPoster         bool   `json:"should_crop_poster"`
	OriginalPosterURL        string `json:"original_poster_url"`
	OriginalCroppedPosterURL string `json:"original_cropped_poster_url"`
	OriginalShouldCropPoster *bool  `json:"original_should_crop_poster"`
	OriginalCoverURL         string `json:"original_cover_url"`
}

// Clone returns a deep copy of the PosterState.
func (p PosterState) Clone() PosterState {
	cp := p
	if p.PosterCropBounds != nil {
		b := *p.PosterCropBounds
		cp.PosterCropBounds = &b
	}
	if p.OriginalShouldCropPoster != nil {
		b := *p.OriginalShouldCropPoster
		cp.OriginalShouldCropPoster = &b
	}
	return cp
}

// CropBounds is a normalized poster crop rectangle. All components are 0–1
// fractions of the full-size source image the crop was measured against, so
// the geometry is resolution-stable across the maxPosterHeight downscale.
type CropBounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	// SourceAspect is the width/height ratio of the source image the crop was
	// measured against. The apply phase refuses geometry whose aspect no
	// longer matches the downloaded image, so same-URL source swaps fall back
	// to pre-change behavior instead of cropping the wrong image.
	SourceAspect float64 `json:"source_aspect,omitempty"`
}

// Valid reports whether the normalized crop geometry is applyable: every
// component finite and within [0,1], strictly positive size, and the
// rectangle contained in the unit square. The 1e-9 tolerance absorbs float
// division error when pixel bounds land exactly on the source edge.
func (b CropBounds) Valid() bool {
	for _, v := range []float64{b.X, b.Y, b.Width, b.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return false
		}
	}
	// SourceAspect 0 = unset (no aspect guard); a negative or non-finite
	// aspect is invalid — never "skip the guard" on corrupted geometry.
	if b.SourceAspect < 0 || math.IsNaN(b.SourceAspect) || math.IsInf(b.SourceAspect, 0) {
		return false
	}
	const tol = 1e-9
	return b.Width > 0 && b.Height > 0 && b.X+b.Width <= 1+tol && b.Y+b.Height <= 1+tol
}
