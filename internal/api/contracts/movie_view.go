package contracts

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// MovieView is the API-layer projection of a Movie. It carries explicit
// JSON tags that are the API contract — independent of *models.Movie's
// persistence-layer json tags. Per ADR-0007: "one representation per layer,
// not one representation total."
//
// The only rename from the persistence format is content_id → code.
// All other field names are preserved to minimize frontend churn.
// PosterState fields are flattened as explicit top-level fields — no custom
// MarshalJSON is needed on MovieView (standard json.Marshal produces the
// flat shape that the frontend expects).
type MovieView struct {
	// Canonical JAV code (e.g., "IPX-535"). Renamed from content_id.
	Code string `json:"code" example:"IPX-535"`

	// Secondary identifier — often equals Code but can differ.
	ID string `json:"id" example:"IPX-535"`

	// Titles
	DisplayTitle  string `json:"display_title" example:"IPX-535 Beautiful Woman"`
	Title         string `json:"title" example:"Beautiful Woman"`
	OriginalTitle string `json:"original_title" example:"美人"`

	// Metadata
	Description   string     `json:"description"`
	ReleaseDate   *time.Time `json:"release_date"`
	ReleaseYear   int        `json:"release_year"`
	Runtime       int        `json:"runtime"` // in minutes
	Director      string     `json:"director"`
	Maker         string     `json:"maker"`  // Studio/maker
	Label         string     `json:"label"`  // Sub-label
	Series        string     `json:"series"` // Series name
	RatingScore   float64    `json:"rating_score"`
	RatingVotes   int        `json:"rating_votes"`
	RatingWarning string     `json:"rating_warning,omitempty"`

	// Poster / cover (flattened from PosterState — no custom marshaler needed)
	PosterURL                string `json:"poster_url"`
	CoverURL                 string `json:"cover_url"`
	CroppedPosterURL         string `json:"cropped_poster_url"`
	ShouldCropPoster         bool   `json:"should_crop_poster"`
	OriginalPosterURL        string `json:"original_poster_url"`
	OriginalCroppedPosterURL string `json:"original_cropped_poster_url"`
	OriginalShouldCropPoster *bool  `json:"original_should_crop_poster"`
	OriginalCoverURL         string `json:"original_cover_url"`

	// Media
	TrailerURL       string   `json:"trailer_url"`
	OriginalFileName string   `json:"original_filename"`
	Screenshots      []string `json:"screenshot_urls"`

	// Relationships (contract DTOs — see collection_views.go; persistence
	// models.* types never cross the API boundary)
	Actresses    []ActressView          `json:"actresses"`
	Genres       []GenreView            `json:"genres"`
	Translations []MovieTranslationView `json:"translations"`

	// Source provenance
	SourceName string `json:"source_name"` // Primary source
	SourceURL  string `json:"source_url"`

	// Audit timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MovieViewFromModel maps a persistence-layer Movie to an API-layer MovieView.
// The single rename (content_id → code) is the only wire-format change.
// PosterState fields are lifted to top-level MovieView fields. Relationship
// collections (Actresses/Genres/Translations) are projected through contract
// DTOs and copied into fresh slices, so the view does not alias the model.
// Screenshots remains a shared string slice — treat it as read-only.
func MovieViewFromModel(m *models.Movie) *MovieView {
	if m == nil {
		return nil
	}
	return &MovieView{
		Code:                     m.ContentID,
		ID:                       m.ID,
		DisplayTitle:             m.DisplayTitle,
		Title:                    m.Title,
		OriginalTitle:            m.OriginalTitle,
		Description:              m.Description,
		ReleaseDate:              m.ReleaseDate,
		ReleaseYear:              m.ReleaseYear,
		Runtime:                  m.Runtime,
		Director:                 m.Director,
		Maker:                    m.Maker,
		Label:                    m.Label,
		Series:                   m.Series,
		RatingScore:              m.RatingScore,
		RatingVotes:              m.RatingVotes,
		RatingWarning:            m.RatingWarning,
		PosterURL:                m.Poster.PosterURL,
		CoverURL:                 m.Poster.CoverURL,
		CroppedPosterURL:         m.Poster.CroppedPosterURL,
		ShouldCropPoster:         m.Poster.ShouldCropPoster,
		OriginalPosterURL:        m.Poster.OriginalPosterURL,
		OriginalCroppedPosterURL: m.Poster.OriginalCroppedPosterURL,
		OriginalShouldCropPoster: m.Poster.OriginalShouldCropPoster,
		OriginalCoverURL:         m.Poster.OriginalCoverURL,
		TrailerURL:               m.TrailerURL,
		OriginalFileName:         m.OriginalFileName,
		Screenshots:              m.Screenshots,
		Actresses:                ActressViewSliceFromModels(m.Actresses),
		Genres:                   GenreViewSliceFromModels(m.Genres),
		Translations:             MovieTranslationViewSliceFromModels(m.Translations),
		SourceName:               m.SourceName,
		SourceURL:                m.SourceURL,
		CreatedAt:                m.CreatedAt,
		UpdatedAt:                m.UpdatedAt,
	}
}

// MovieViewToModel maps an API-layer MovieView back to a persistence-layer Movie.
// This is the inverse of MovieViewFromModel — used for request types where
// the API client sends a MovieView and the handler needs *models.Movie for
// processing (e.g., UpdateMovieRequest, OrganizePreviewRequest).
func MovieViewToModel(v *MovieView) *models.Movie {
	if v == nil {
		return nil
	}
	m := &models.Movie{
		ContentID:        v.Code,
		ID:               v.ID,
		DisplayTitle:     v.DisplayTitle,
		Title:            v.Title,
		OriginalTitle:    v.OriginalTitle,
		Description:      v.Description,
		ReleaseDate:      v.ReleaseDate,
		ReleaseYear:      v.ReleaseYear,
		Runtime:          v.Runtime,
		Director:         v.Director,
		Maker:            v.Maker,
		Label:            v.Label,
		Series:           v.Series,
		RatingScore:      v.RatingScore,
		RatingVotes:      v.RatingVotes,
		RatingWarning:    v.RatingWarning,
		TrailerURL:       v.TrailerURL,
		OriginalFileName: v.OriginalFileName,
		Screenshots:      v.Screenshots,
		Actresses:        ActressViewSliceToModels(v.Actresses),
		Genres:           GenreViewSliceToModels(v.Genres),
		Translations:     MovieTranslationViewSliceToModels(v.Translations),
		SourceName:       v.SourceName,
		SourceURL:        v.SourceURL,
		CreatedAt:        v.CreatedAt,
		UpdatedAt:        v.UpdatedAt,
	}
	// Rebuild PosterState from the flattened MovieView fields.
	m.Poster = models.PosterState{
		PosterURL:                v.PosterURL,
		CoverURL:                 v.CoverURL,
		CroppedPosterURL:         v.CroppedPosterURL,
		ShouldCropPoster:         v.ShouldCropPoster,
		OriginalPosterURL:        v.OriginalPosterURL,
		OriginalCroppedPosterURL: v.OriginalCroppedPosterURL,
		OriginalShouldCropPoster: v.OriginalShouldCropPoster,
		OriginalCoverURL:         v.OriginalCoverURL,
	}
	return m
}

// MovieViewSliceFromModels maps a slice of Movies to MovieViews.
func MovieViewSliceFromModels(movies []models.Movie) []MovieView {
	views := make([]MovieView, len(movies))
	for i := range movies {
		views[i] = *MovieViewFromModel(&movies[i])
	}
	return views
}
