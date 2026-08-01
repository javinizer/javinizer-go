package models

// PosterState groups the poster/cropping fields extracted from Movie.
// Embedded in Movie with gorm:"embedded" so column names are preserved (zero-migration).
// JSON serialization is handled by custom MarshalJSON/UnmarshalJSON on Movie to keep
// the flat wire format — do NOT change the json tag on the Movie.Poster field from
// json:"-" or poster fields will disappear from API responses.
type PosterState struct {
	PosterURL                string `json:"poster_url"`
	CoverURL                 string `json:"cover_url"`
	CroppedPosterURL         string `json:"cropped_poster_url"`
	ShouldCropPoster         bool   `json:"should_crop_poster"`
	OriginalPosterURL        string `json:"original_poster_url"`
	OriginalCroppedPosterURL string `json:"original_cropped_poster_url"`
	OriginalShouldCropPoster *bool  `json:"original_should_crop_poster"`
	OriginalCoverURL         string `json:"original_cover_url"`
	// CropBounds records a user-applied manual poster crop (source-image pixels).
	// Runtime-only: carried through job state and the API wire so the apply
	// phase can reproduce the user's crop; not stored in the movies table or NFO.
	CropBounds *CropBounds `json:"poster_crop_bounds,omitempty" gorm:"-"`
}

// CropBounds is a rectangular crop region in source-image pixels.
type CropBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
	// MaxPosterHeight is the output height cap effective when the crop was
	// made (0 = no cap). Stored with the bounds so Organize reproduces the
	// exact previewed dimensions even if the configured default differs.
	MaxPosterHeight int `json:"max_poster_height,omitempty"`
	// ImageWidth/ImageHeight describe the source image the rectangle was
	// measured against. Apply scales the rectangle when it downloads the same
	// image at a different resolution (0 = unknown, apply unscaled).
	ImageWidth  int `json:"image_width,omitempty"`
	ImageHeight int `json:"image_height,omitempty"`
	// SourceWasCover records whether the cropped source was a cover needing
	// auto-crop at measurement time, so the geometry fallback degrades cover
	// sources to the default crop and keeps poster-grade sources whole — the
	// scrape-time baseline (OriginalShouldCropPoster) is wrong once the user
	// replaced the poster image.
	SourceWasCover bool `json:"source_was_cover,omitempty"`
}

// SyncCropIntentWithSource re-derives ShouldCropPoster from the class of the
// effective poster source after a source-changing edit (a poster_url override
// or a whole-movie PATCH that swaps the source — the downloader reads
// PosterURL ?? CoverURL):
//
//   - PosterURL set → a poster-grade image feeds the pipeline: false. Without
//     this, a cover-backed movie's stale intent would make Organize
//     default-crop the new poster wholesale, and a later manual crop would
//     wrongly record CropBounds.SourceWasCover=true — degrading the
//     apply-time geometry fallback to the default cover crop
//     (internal/downloader/media.go) instead of keeping the poster whole.
//   - PosterURL empty while CoverURL is set → the cover feeds the pipeline
//     again: true, matching the scraper's cover-backed semantics (a cleared
//     poster override falls back to a source that needs the default crop).
//   - Both empty → the edit cleared the LAST source (cleanup path): the flag
//     is left untouched; nothing will be downloaded, and re-adding a source
//     later re-derives it.
//
// Callers must invoke this ONLY when the effective source actually changed —
// an unchanged source may carry a deliberate user crop decision (an explicit
// should_crop_poster edit) that must not be clobbered.
func (p *PosterState) SyncCropIntentWithSource() {
	if p.PosterURL != "" {
		p.ShouldCropPoster = false
	} else if p.CoverURL != "" {
		p.ShouldCropPoster = true
	}
}

// Clone returns a deep copy of the PosterState.
func (p PosterState) Clone() PosterState {
	cp := p
	if p.OriginalShouldCropPoster != nil {
		b := *p.OriginalShouldCropPoster
		cp.OriginalShouldCropPoster = &b
	}
	if p.CropBounds != nil {
		b := *p.CropBounds
		cp.CropBounds = &b
	}
	return cp
}
