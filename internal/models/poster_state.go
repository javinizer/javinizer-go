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
