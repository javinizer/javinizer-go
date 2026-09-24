package contracts

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Collection view DTOs decouple MovieView's nested relationships from the
// persistence-layer models.* types. Per ADR-0007 ("one representation per
// layer, not one representation total"), schema/tag changes in
// internal/models must not leak into the public API contract.
//
// The JSON tags below are the API contract and intentionally mirror the
// current models json tags so the wire format is preserved — the
// decoupling is structural, not a rename. DTOs carry no gorm/persistence
// tags.

// ActressView is the API-layer projection of models.Actress.
type ActressView struct {
	ID           uint   `json:"id"`
	DMMID        int    `json:"dmm_id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	JapaneseName string `json:"japanese_name"`
	ThumbURL     string `json:"thumb_url"`
	// ThumbEdited marks an explicit client edit of thumb_url so the server never
	// infers thumbnail intent from snapshot or database comparisons.
	ThumbEdited  bool                     `json:"thumb_edited,omitempty"`
	Aliases      string                   `json:"aliases"`
	Verified     bool                     `json:"verified"`
	Origin       string                   `json:"origin"`
	NameKey      string                   `json:"name_key"`
	Translations []ActressTranslationView `json:"translations,omitempty"`
	CreatedAt    time.Time                `json:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

// MovieCreditView is the read-only API projection of a per-movie actress credit.
// Credit mutations use dedicated endpoints; review PATCH payloads must not map
// these rows back into models.Movie.
type MovieCreditView struct {
	ID                   uint   `json:"id"`
	ActressID            uint   `json:"actress_id"`
	CreditedName         string `json:"credited_name"`
	CreditedJapaneseName string `json:"credited_japanese_name"`
	ReportedThumbURL     string `json:"reported_thumb_url"`
	OverrideName         string `json:"override_name"`
	UserOverride         bool   `json:"user_override"`
	Suppressed           bool   `json:"suppressed"`
	OrderIndex           int    `json:"order_index"`
}

// GenreView is the API-layer projection of models.Genre.
type GenreView struct {
	ID           uint                   `json:"id"`
	Name         string                 `json:"name"`
	Translations []GenreTranslationView `json:"translations,omitempty"`
}

// MovieTranslationView is the API-layer projection of models.MovieTranslation.
type MovieTranslationView struct {
	ID            uint      `json:"id"`
	MovieID       string    `json:"movie_id"`
	Language      string    `json:"language"`
	Title         string    `json:"title"`
	OriginalTitle string    `json:"original_title"`
	Description   string    `json:"description"`
	Director      string    `json:"director"`
	Maker         string    `json:"maker"`
	Label         string    `json:"label"`
	Series        string    `json:"series"`
	SourceName    string    `json:"source_name"`
	SettingsHash  string    `json:"settings_hash"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ActressTranslationView is the API-layer projection of models.ActressTranslation.
type ActressTranslationView struct {
	ID           uint      `json:"id"`
	ActressID    uint      `json:"actress_id"`
	Language     string    `json:"language"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	JapaneseName string    `json:"japanese_name"`
	DisplayName  string    `json:"display_name"`
	SourceName   string    `json:"source_name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// GenreTranslationView is the API-layer projection of models.GenreTranslation.
type GenreTranslationView struct {
	ID         uint      `json:"id"`
	GenreID    uint      `json:"genre_id"`
	Language   string    `json:"language"`
	Name       string    `json:"name"`
	SourceName string    `json:"source_name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ActressViewFromModel maps a persistence-layer Actress to an API-layer
// ActressView. Returns nil for nil input. Nested translations are copied
// into fresh slices so the view does not alias the model.
func ActressViewFromModel(a *models.Actress) *ActressView {
	if a == nil {
		return nil
	}
	return &ActressView{
		ID:           a.ID,
		DMMID:        a.DMMID,
		FirstName:    a.FirstName,
		LastName:     a.LastName,
		JapaneseName: a.JapaneseName,
		ThumbURL:     a.ThumbURL,
		Aliases:      a.Aliases,
		Verified:     a.Verified,
		Origin:       a.Origin,
		NameKey:      a.NameKey,
		Translations: ActressTranslationViewSliceFromModels(a.Translations),
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
}

// ActressViewToModel maps an API-layer ActressView back to a persistence-layer
// Actress. Returns nil for nil input. Inverse of ActressViewFromModel.
func ActressViewToModel(v *ActressView) *models.Actress {
	if v == nil {
		return nil
	}
	return &models.Actress{
		ID:           v.ID,
		DMMID:        v.DMMID,
		FirstName:    v.FirstName,
		LastName:     v.LastName,
		JapaneseName: v.JapaneseName,
		ThumbURL:     v.ThumbURL,
		ThumbEdited:  v.ThumbEdited,
		Aliases:      v.Aliases,
		Translations: ActressTranslationViewSliceToModels(v.Translations),
		CreatedAt:    v.CreatedAt,
		UpdatedAt:    v.UpdatedAt,
	}
}

// MovieCreditViewFromModel maps the read-only fields of a persistence-layer
// MovieCredit to its API projection.
func MovieCreditViewFromModel(c *models.MovieCredit) *MovieCreditView {
	if c == nil {
		return nil
	}
	return &MovieCreditView{
		ID:                   c.ID,
		ActressID:            c.ActressID,
		CreditedName:         c.CreditedName,
		CreditedJapaneseName: c.CreditedJapaneseName,
		ReportedThumbURL:     c.ReportedThumbURL,
		OverrideName:         c.OverrideName,
		UserOverride:         c.UserOverride,
		Suppressed:           c.Suppressed,
		OrderIndex:           c.OrderIndex,
	}
}

// GenreViewFromModel maps a persistence-layer Genre to an API-layer GenreView.
// Returns nil for nil input.
func GenreViewFromModel(g *models.Genre) *GenreView {
	if g == nil {
		return nil
	}
	return &GenreView{
		ID:           g.ID,
		Name:         g.Name,
		Translations: GenreTranslationViewSliceFromModels(g.Translations),
	}
}

// GenreViewToModel maps an API-layer GenreView back to a persistence-layer
// Genre. Returns nil for nil input. Inverse of GenreViewFromModel.
func GenreViewToModel(v *GenreView) *models.Genre {
	if v == nil {
		return nil
	}
	return &models.Genre{
		ID:           v.ID,
		Name:         v.Name,
		Translations: GenreTranslationViewSliceToModels(v.Translations),
	}
}

// MovieTranslationViewFromModel maps a persistence-layer MovieTranslation to
// an API-layer MovieTranslationView. Returns nil for nil input.
func MovieTranslationViewFromModel(t *models.MovieTranslation) *MovieTranslationView {
	if t == nil {
		return nil
	}
	return &MovieTranslationView{
		ID:            t.ID,
		MovieID:       t.MovieID,
		Language:      t.Language,
		Title:         t.Title,
		OriginalTitle: t.OriginalTitle,
		Description:   t.Description,
		Director:      t.Director,
		Maker:         t.Maker,
		Label:         t.Label,
		Series:        t.Series,
		SourceName:    t.SourceName,
		SettingsHash:  t.SettingsHash,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
}

// MovieTranslationViewToModel maps an API-layer MovieTranslationView back to a
// persistence-layer MovieTranslation. Returns nil for nil input. Inverse of
// MovieTranslationViewFromModel.
func MovieTranslationViewToModel(v *MovieTranslationView) *models.MovieTranslation {
	if v == nil {
		return nil
	}
	return &models.MovieTranslation{
		ID:            v.ID,
		MovieID:       v.MovieID,
		Language:      v.Language,
		Title:         v.Title,
		OriginalTitle: v.OriginalTitle,
		Description:   v.Description,
		Director:      v.Director,
		Maker:         v.Maker,
		Label:         v.Label,
		Series:        v.Series,
		SourceName:    v.SourceName,
		SettingsHash:  v.SettingsHash,
		CreatedAt:     v.CreatedAt,
		UpdatedAt:     v.UpdatedAt,
	}
}

// ActressTranslationViewFromModel maps a persistence-layer ActressTranslation
// to an API-layer ActressTranslationView. Returns nil for nil input.
func ActressTranslationViewFromModel(t *models.ActressTranslation) *ActressTranslationView {
	if t == nil {
		return nil
	}
	return &ActressTranslationView{
		ID:           t.ID,
		ActressID:    t.ActressID,
		Language:     t.Language,
		FirstName:    t.FirstName,
		LastName:     t.LastName,
		JapaneseName: t.JapaneseName,
		DisplayName:  t.DisplayName,
		SourceName:   t.SourceName,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}

// ActressTranslationViewToModel maps an API-layer ActressTranslationView back
// to a persistence-layer ActressTranslation. Returns nil for nil input.
func ActressTranslationViewToModel(v *ActressTranslationView) *models.ActressTranslation {
	if v == nil {
		return nil
	}
	return &models.ActressTranslation{
		ID:           v.ID,
		ActressID:    v.ActressID,
		Language:     v.Language,
		FirstName:    v.FirstName,
		LastName:     v.LastName,
		JapaneseName: v.JapaneseName,
		DisplayName:  v.DisplayName,
		SourceName:   v.SourceName,
		CreatedAt:    v.CreatedAt,
		UpdatedAt:    v.UpdatedAt,
	}
}

// GenreTranslationViewFromModel maps a persistence-layer GenreTranslation to
// an API-layer GenreTranslationView. Returns nil for nil input.
func GenreTranslationViewFromModel(t *models.GenreTranslation) *GenreTranslationView {
	if t == nil {
		return nil
	}
	return &GenreTranslationView{
		ID:         t.ID,
		GenreID:    t.GenreID,
		Language:   t.Language,
		Name:       t.Name,
		SourceName: t.SourceName,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
	}
}

// GenreTranslationViewToModel maps an API-layer GenreTranslationView back to a
// persistence-layer GenreTranslation. Returns nil for nil input.
func GenreTranslationViewToModel(v *GenreTranslationView) *models.GenreTranslation {
	if v == nil {
		return nil
	}
	return &models.GenreTranslation{
		ID:         v.ID,
		GenreID:    v.GenreID,
		Language:   v.Language,
		Name:       v.Name,
		SourceName: v.SourceName,
		CreatedAt:  v.CreatedAt,
		UpdatedAt:  v.UpdatedAt,
	}
}

// sliceFromModels maps a slice of persistence models to contract views via
// conv. A nil input yields a nil result so the JSON shape (null vs []) is
// preserved. It is the shared implementation behind every *SliceFromModels
// helper, keeping nil-handling and value-semantics identical across DTOs.
func sliceFromModels[M any, V any](ms []M, conv func(*M) *V) []V {
	if ms == nil {
		return nil
	}
	vs := make([]V, len(ms))
	for i := range ms {
		vs[i] = *conv(&ms[i])
	}
	return vs
}

// sliceToModels is the inverse of sliceFromModels, mapping contract views back
// to persistence models. A nil input yields a nil result so the JSON shape
// (null vs []) is preserved.
func sliceToModels[V any, M any](vs []V, conv func(*V) *M) []M {
	if vs == nil {
		return nil
	}
	ms := make([]M, len(vs))
	for i := range vs {
		ms[i] = *conv(&vs[i])
	}
	return ms
}

// ActressViewSliceFromModels maps a slice of Actresses to ActressViews.
// A nil input yields a nil result so the JSON shape (null vs []) is preserved.
func ActressViewSliceFromModels(ms []models.Actress) []ActressView {
	return sliceFromModels(ms, ActressViewFromModel)
}

// MovieCreditViewSliceFromModels maps movie credits to read-only API views.
func MovieCreditViewSliceFromModels(ms []models.MovieCredit) []MovieCreditView {
	return sliceFromModels(ms, MovieCreditViewFromModel)
}

// ActressViewSliceToModels maps a slice of ActressViews to Actresses.
// A nil input yields a nil result so the JSON shape (null vs []) is preserved.
func ActressViewSliceToModels(vs []ActressView) []models.Actress {
	return sliceToModels(vs, ActressViewToModel)
}

// GenreViewSliceFromModels maps a slice of Genres to GenreViews.
// A nil input yields a nil result so the JSON shape (null vs []) is preserved.
func GenreViewSliceFromModels(ms []models.Genre) []GenreView {
	return sliceFromModels(ms, GenreViewFromModel)
}

// GenreViewSliceToModels maps a slice of GenreViews to Genres.
// A nil input yields a nil result so the JSON shape (null vs []) is preserved.
func GenreViewSliceToModels(vs []GenreView) []models.Genre {
	return sliceToModels(vs, GenreViewToModel)
}

// MovieTranslationViewSliceFromModels maps a slice of MovieTranslations to
// MovieTranslationViews. A nil input yields a nil result so the JSON shape
// (null vs []) is preserved.
func MovieTranslationViewSliceFromModels(ms []models.MovieTranslation) []MovieTranslationView {
	return sliceFromModels(ms, MovieTranslationViewFromModel)
}

// MovieTranslationViewSliceToModels maps a slice of MovieTranslationViews to
// MovieTranslations. A nil input yields a nil result so the JSON shape
// (null vs []) is preserved.
func MovieTranslationViewSliceToModels(vs []MovieTranslationView) []models.MovieTranslation {
	return sliceToModels(vs, MovieTranslationViewToModel)
}

// ActressTranslationViewSliceFromModels maps a slice of ActressTranslations to
// ActressTranslationViews. A nil input yields a nil result so the JSON shape
// (null vs []) is preserved.
func ActressTranslationViewSliceFromModels(ms []models.ActressTranslation) []ActressTranslationView {
	return sliceFromModels(ms, ActressTranslationViewFromModel)
}

// ActressTranslationViewSliceToModels maps a slice of ActressTranslationViews
// to ActressTranslations. A nil input yields a nil result so the JSON shape
// (null vs []) is preserved.
func ActressTranslationViewSliceToModels(vs []ActressTranslationView) []models.ActressTranslation {
	return sliceToModels(vs, ActressTranslationViewToModel)
}

// GenreTranslationViewSliceFromModels maps a slice of GenreTranslations to
// GenreTranslationViews. A nil input yields a nil result so the JSON shape
// (null vs []) is preserved.
func GenreTranslationViewSliceFromModels(ms []models.GenreTranslation) []GenreTranslationView {
	return sliceFromModels(ms, GenreTranslationViewFromModel)
}

// GenreTranslationViewSliceToModels maps a slice of GenreTranslationViews to
// GenreTranslations. A nil input yields a nil result so the JSON shape
// (null vs []) is preserved.
func GenreTranslationViewSliceToModels(vs []GenreTranslationView) []models.GenreTranslation {
	return sliceToModels(vs, GenreTranslationViewToModel)
}
