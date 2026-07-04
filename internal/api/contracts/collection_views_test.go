package contracts

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFixedTime() time.Time {
	return time.Date(2024, 3, 15, 9, 30, 0, 0, time.UTC)
}

func sampleActress() models.Actress {
	return models.Actress{
		ID:           7,
		DMMID:        9012,
		FirstName:    "Yui",
		LastName:     "Hatano",
		JapaneseName: "波多野結衣",
		ThumbURL:     "https://example.com/yui.jpg",
		Aliases:      "Yui Hatano|Hatano Yui",
		Translations: []models.ActressTranslation{
			{
				ID:           100,
				ActressID:    7,
				Language:     "ja",
				FirstName:    "結衣",
				LastName:     "波多野",
				JapaneseName: "波多野結衣",
				DisplayName:  "波多野結衣",
				SourceName:   "dmm",
				CreatedAt:    newFixedTime(),
				UpdatedAt:    newFixedTime(),
			},
		},
		CreatedAt: newFixedTime(),
		UpdatedAt: newFixedTime(),
	}
}

func sampleGenre() models.Genre {
	return models.Genre{
		ID:   42,
		Name: "Featured Actress",
		Translations: []models.GenreTranslation{
			{
				ID:         5,
				GenreID:    42,
				Language:   "ja",
				Name:       "魅力女優",
				SourceName: "r18dev",
				CreatedAt:  newFixedTime(),
				UpdatedAt:  newFixedTime(),
			},
		},
	}
}

func sampleMovieTranslation() models.MovieTranslation {
	return models.MovieTranslation{
		ID:            55,
		MovieID:       "IPX-535",
		Language:      "ja",
		Title:         "美人女教師",
		OriginalTitle: "美人女教師",
		Description:   "あらすじ",
		Director:      "山田",
		Maker:         "IPX",
		Label:         "IPX Zeta",
		Series:        "Lady Teacher",
		SourceName:    "dmm",
		SettingsHash:  "abc12345",
		CreatedAt:     newFixedTime(),
		UpdatedAt:     newFixedTime(),
	}
}

func TestActressView_RoundTrip(t *testing.T) {
	orig := sampleActress()
	back := ActressViewToModel(ActressViewFromModel(&orig))
	require.NotNil(t, back)
	assert.Equal(t, orig, *back)
}

func TestGenreView_RoundTrip(t *testing.T) {
	orig := sampleGenre()
	back := GenreViewToModel(GenreViewFromModel(&orig))
	require.NotNil(t, back)
	assert.Equal(t, orig, *back)
}

func TestMovieTranslationView_RoundTrip(t *testing.T) {
	orig := sampleMovieTranslation()
	back := MovieTranslationViewToModel(MovieTranslationViewFromModel(&orig))
	require.NotNil(t, back)
	assert.Equal(t, orig, *back)
}

// JSON parity guards the API contract: the DTO must serialize identically to
// the persistence model it replaces, so the refactor is wire-format-neutral.
func TestActressView_JSONParity(t *testing.T) {
	orig := sampleActress()
	view := ActressViewFromModel(&orig)

	modelJSON, err := json.Marshal(orig)
	require.NoError(t, err)
	viewJSON, err := json.Marshal(view)
	require.NoError(t, err)
	assert.JSONEq(t, string(modelJSON), string(viewJSON))
}

func TestGenreView_JSONParity(t *testing.T) {
	orig := sampleGenre()
	view := GenreViewFromModel(&orig)

	modelJSON, err := json.Marshal(orig)
	require.NoError(t, err)
	viewJSON, err := json.Marshal(view)
	require.NoError(t, err)
	assert.JSONEq(t, string(modelJSON), string(viewJSON))
}

func TestMovieTranslationView_JSONParity(t *testing.T) {
	orig := sampleMovieTranslation()
	view := MovieTranslationViewFromModel(&orig)

	modelJSON, err := json.Marshal(orig)
	require.NoError(t, err)
	viewJSON, err := json.Marshal(view)
	require.NoError(t, err)
	assert.JSONEq(t, string(modelJSON), string(viewJSON))
}

func TestCollectionViews_NilElementMappers(t *testing.T) {
	assert.Nil(t, ActressViewFromModel(nil))
	assert.Nil(t, ActressViewToModel(nil))
	assert.Nil(t, GenreViewFromModel(nil))
	assert.Nil(t, GenreViewToModel(nil))
	assert.Nil(t, MovieTranslationViewFromModel(nil))
	assert.Nil(t, MovieTranslationViewToModel(nil))
	assert.Nil(t, ActressTranslationViewFromModel(nil))
	assert.Nil(t, ActressTranslationViewToModel(nil))
	assert.Nil(t, GenreTranslationViewFromModel(nil))
	assert.Nil(t, GenreTranslationViewToModel(nil))
}

// nil slices must stay nil (→ JSON null), not become [] (→ JSON []).
func TestCollectionViews_NilSlicesPreserveNull(t *testing.T) {
	assert.Nil(t, ActressViewSliceFromModels(nil))
	assert.Nil(t, ActressViewSliceToModels(nil))
	assert.Nil(t, GenreViewSliceFromModels(nil))
	assert.Nil(t, GenreViewSliceToModels(nil))
	assert.Nil(t, MovieTranslationViewSliceFromModels(nil))
	assert.Nil(t, MovieTranslationViewSliceToModels(nil))
	assert.Nil(t, ActressTranslationViewSliceFromModels(nil))
	assert.Nil(t, ActressTranslationViewSliceToModels(nil))
	assert.Nil(t, GenreTranslationViewSliceFromModels(nil))
	assert.Nil(t, GenreTranslationViewSliceToModels(nil))

	// End-to-end: a nil collection on the model surfaces as JSON null on the view.
	m := &models.Movie{ContentID: "IPX-1"}
	view := MovieViewFromModel(m)
	raw, err := json.Marshal(view)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	assert.JSONEq(t, `null`, string(fields["actresses"]))
	assert.JSONEq(t, `null`, string(fields["genres"]))
	assert.JSONEq(t, `null`, string(fields["translations"]))
}

func TestCollectionViews_EmptySlicesPreserveArray(t *testing.T) {
	// Non-nil empty slices must stay non-nil (→ JSON []), not become null.
	a := ActressViewSliceFromModels([]models.Actress{})
	require.NotNil(t, a)
	assert.Empty(t, a)
	raw, err := json.Marshal(struct {
		A []ActressView `json:"actresses"`
	}{A: a})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"actresses":[]`)
}

// MovieView round-trip ensures the wired-up MovieView mappers preserve the
// relationship collections end to end.
func TestMovieView_CollectionRoundTrip(t *testing.T) {
	m := &models.Movie{
		ContentID: "IPX-535",
		Actresses: []models.Actress{sampleActress()},
		Genres:    []models.Genre{sampleGenre(), {ID: 8, Name: "Solowork"}},
		Translations: []models.MovieTranslation{
			sampleMovieTranslation(),
			{ID: 56, MovieID: "IPX-535", Language: "en", Title: "Beautiful Woman"},
		},
	}

	view := MovieViewFromModel(m)
	require.NotNil(t, view)
	require.Len(t, view.Actresses, 1)
	require.Len(t, view.Genres, 2)
	require.Len(t, view.Translations, 2)

	back := MovieViewToModel(view)
	require.NotNil(t, back)
	assert.Equal(t, m.Actresses, back.Actresses)
	assert.Equal(t, m.Genres, back.Genres)
	assert.Equal(t, m.Translations, back.Translations)
}

// Mutating the view's collections must not bleed back into the model — the
// projection owns its own slices.
func TestMovieView_CollectionIsolation(t *testing.T) {
	m := &models.Movie{
		ContentID: "IPX-535",
		Actresses: []models.Actress{sampleActress()},
	}
	view := MovieViewFromModel(m)
	view.Actresses[0].FirstName = "MUTATED"
	view.Actresses[0].Translations[0].DisplayName = "MUTATED"

	assert.NotEqual(t, "MUTATED", m.Actresses[0].FirstName)
	assert.NotEqual(t, "MUTATED", m.Actresses[0].Translations[0].DisplayName)
}

// Non-nil empty collections on the model must serialize as JSON [] through
// MovieView, not null or omitted — mirroring the nil→null guard above for
// the empty-slice case, across all three collections end-to-end.
func TestMovieView_EmptySlicesPreserveArray(t *testing.T) {
	m := &models.Movie{
		ContentID:    "IPX-1",
		Actresses:    []models.Actress{},
		Genres:       []models.Genre{},
		Translations: []models.MovieTranslation{},
	}
	view := MovieViewFromModel(m)
	require.NotNil(t, view)
	raw, err := json.Marshal(view)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	assert.JSONEq(t, `[]`, string(fields["actresses"]))
	assert.JSONEq(t, `[]`, string(fields["genres"]))
	assert.JSONEq(t, `[]`, string(fields["translations"]))
}

// Extends collection-isolation coverage to Genres (including nested
// GenreTranslation) and Translations, proving the view's slices don't alias
// the model's for all three relationship collections.
func TestMovieView_CollectionIsolation_AllCollections(t *testing.T) {
	m := &models.Movie{
		ContentID:    "IPX-535",
		Actresses:    []models.Actress{sampleActress()},
		Genres:       []models.Genre{sampleGenre()},
		Translations: []models.MovieTranslation{sampleMovieTranslation()},
	}
	view := MovieViewFromModel(m)
	require.NotNil(t, view)

	view.Genres[0].Name = "MUTATED"
	view.Genres[0].Translations[0].Name = "MUTATED"
	view.Translations[0].Title = "MUTATED"

	assert.NotEqual(t, "MUTATED", m.Genres[0].Name)
	assert.NotEqual(t, "MUTATED", m.Genres[0].Translations[0].Name)
	assert.NotEqual(t, "MUTATED", m.Translations[0].Title)
}
