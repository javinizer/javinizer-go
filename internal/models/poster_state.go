package models

// PosterState groups the seven poster/cropping fields extracted from Movie.
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
}

// Clone returns a deep copy of the PosterState.
func (p PosterState) Clone() PosterState {
	cp := p
	if p.OriginalShouldCropPoster != nil {
		b := *p.OriginalShouldCropPoster
		cp.OriginalShouldCropPoster = &b
	}
	return cp
}
