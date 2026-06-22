package models

import (
	"testing"
	"time"
)

func TestMovieClone_NilReceiver(t *testing.T) {
	t.Parallel()
	var m *Movie
	if got := m.Clone(); got != nil {
		t.Errorf("(*Movie)(nil).Clone() = %v, want nil", got)
	}
}

func TestMovieClone_EqualValues(t *testing.T) {
	t.Parallel()
	now := time.Now()
	shouldCrop := true
	orig := &Movie{
		ContentID:     "ABCD-123",
		ID:            "ABCD-123",
		DisplayTitle:  "Test Title",
		Title:         "Original Title",
		OriginalTitle: "Japanese Title",
		Description:   "A description",
		ReleaseDate:   &now,
		ReleaseYear:   2025,
		Runtime:       120,
		Director:      "Director",
		Maker:         "Maker",
		Label:         "Label",
		Series:        "Series",
		RatingScore:   8.5,
		RatingVotes:   100,
		Poster: PosterState{
			PosterURL:                "https://example.com/poster.jpg",
			CoverURL:                 "https://example.com/cover.jpg",
			CroppedPosterURL:         "https://example.com/cropped.jpg",
			ShouldCropPoster:         true,
			OriginalPosterURL:        "https://example.com/orig.jpg",
			OriginalCroppedPosterURL: "https://example.com/orig-crop.jpg",
			OriginalShouldCropPoster: &shouldCrop,
		},
		TrailerURL:       "https://example.com/trailer.mp4",
		OriginalFileName: "ABCD-123.mp4",
		Actresses: []Actress{
			{ID: 1, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Translations: []ActressTranslation{
				{ID: 10, Language: "en", FirstName: "Yui", LastName: "Hatano"},
			}},
		},
		Genres: []Genre{
			{ID: 1, Name: "Action", Translations: []GenreTranslation{
				{ID: 10, Language: "en", Name: "Action"},
			}},
		},
		Screenshots:  []string{"ss1.jpg", "ss2.jpg"},
		Translations: []MovieTranslation{{Language: "en", Title: "Test EN"}},
		SourceName:   "r18dev",
		SourceURL:    "https://r18.dev",
	}

	clone := orig.Clone()
	if clone == nil {
		t.Fatal("Clone() returned nil for non-nil receiver")
	}

	// Verify all primitive fields equal
	if clone.ContentID != orig.ContentID {
		t.Errorf("ContentID: got %q, want %q", clone.ContentID, orig.ContentID)
	}
	if clone.ID != orig.ID {
		t.Errorf("ID: got %q, want %q", clone.ID, orig.ID)
	}
	if clone.Title != orig.Title {
		t.Errorf("Title: got %q, want %q", clone.Title, orig.Title)
	}
	if clone.Description != orig.Description {
		t.Errorf("Description: got %q, want %q", clone.Description, orig.Description)
	}
	if clone.ReleaseYear != orig.ReleaseYear {
		t.Errorf("ReleaseYear: got %d, want %d", clone.ReleaseYear, orig.ReleaseYear)
	}
	if clone.Runtime != orig.Runtime {
		t.Errorf("Runtime: got %d, want %d", clone.Runtime, orig.Runtime)
	}
	if clone.RatingScore != orig.RatingScore {
		t.Errorf("RatingScore: got %f, want %f", clone.RatingScore, orig.RatingScore)
	}
	if clone.Poster.ShouldCropPoster != orig.Poster.ShouldCropPoster {
		t.Errorf("ShouldCropPoster: got %v, want %v", clone.Poster.ShouldCropPoster, orig.Poster.ShouldCropPoster)
	}

	// Verify pointer fields equal in value but different in address
	if clone.ReleaseDate == nil {
		t.Error("ReleaseDate: got nil, want non-nil")
	} else if !clone.ReleaseDate.Equal(*orig.ReleaseDate) {
		t.Errorf("ReleaseDate value: got %v, want %v", *clone.ReleaseDate, *orig.ReleaseDate)
	}
	if clone.Poster.OriginalShouldCropPoster == nil {
		t.Error("OriginalShouldCropPoster: got nil, want non-nil")
	} else if *clone.Poster.OriginalShouldCropPoster != *orig.Poster.OriginalShouldCropPoster {
		t.Errorf("OriginalShouldCropPoster value: got %v, want %v", *clone.Poster.OriginalShouldCropPoster, *orig.Poster.OriginalShouldCropPoster)
	}

	// Verify slice lengths equal
	if len(clone.Actresses) != len(orig.Actresses) {
		t.Errorf("Actresses length: got %d, want %d", len(clone.Actresses), len(orig.Actresses))
	}
	if len(clone.Genres) != len(orig.Genres) {
		t.Errorf("Genres length: got %d, want %d", len(clone.Genres), len(orig.Genres))
	}
	if len(clone.Screenshots) != len(orig.Screenshots) {
		t.Errorf("Screenshots length: got %d, want %d", len(clone.Screenshots), len(orig.Screenshots))
	}
	if len(clone.Translations) != len(orig.Translations) {
		t.Errorf("Translations length: got %d, want %d", len(clone.Translations), len(orig.Translations))
	}
}

func TestMovieClone_PointerFields_Independent(t *testing.T) {
	t.Parallel()
	now := time.Now()
	shouldCrop := true
	orig := &Movie{
		ReleaseDate: &now,
		Poster: PosterState{
			OriginalShouldCropPoster: &shouldCrop,
		},
	}
	clone := orig.Clone()

	// Modify clone's pointers
	later := now.Add(time.Hour)
	clone.ReleaseDate = &later
	*clone.Poster.OriginalShouldCropPoster = false

	// Verify originals unchanged
	if !orig.ReleaseDate.Equal(now) {
		t.Errorf("original ReleaseDate changed: got %v, want %v", *orig.ReleaseDate, now)
	}
	if *orig.Poster.OriginalShouldCropPoster != true {
		t.Errorf("original OriginalShouldCropPoster changed: got %v, want true", *orig.Poster.OriginalShouldCropPoster)
	}
}

func TestMovieClone_SliceFields_Independent(t *testing.T) {
	t.Parallel()
	orig := &Movie{
		Actresses:    []Actress{{FirstName: "Yui"}, {FirstName: "Aoi"}},
		Genres:       []Genre{{Name: "Action"}, {Name: "Drama"}},
		Screenshots:  []string{"ss1.jpg", "ss2.jpg"},
		Translations: []MovieTranslation{{Language: "en"}, {Language: "ja"}},
	}
	clone := orig.Clone()

	// Append to clone's slices
	clone.Actresses = append(clone.Actresses, Actress{FirstName: "New"})
	clone.Genres = append(clone.Genres, Genre{Name: "New"})
	clone.Screenshots = append(clone.Screenshots, "new.jpg")
	clone.Translations = append(clone.Translations, MovieTranslation{Language: "zh"})

	// Verify originals unchanged
	if len(orig.Actresses) != 2 {
		t.Errorf("original Actresses length: got %d, want 2", len(orig.Actresses))
	}
	if len(orig.Genres) != 2 {
		t.Errorf("original Genres length: got %d, want 2", len(orig.Genres))
	}
	if len(orig.Screenshots) != 2 {
		t.Errorf("original Screenshots length: got %d, want 2", len(orig.Screenshots))
	}
	if len(orig.Translations) != 2 {
		t.Errorf("original Translations length: got %d, want 2", len(orig.Translations))
	}
}

func TestMovieClone_NestedSliceFields_Independent(t *testing.T) {
	t.Parallel()
	orig := &Movie{
		Actresses: []Actress{
			{FirstName: "Yui", Translations: []ActressTranslation{
				{Language: "en", FirstName: "Yui"},
				{Language: "ja", FirstName: "結衣"},
			}},
		},
		Genres: []Genre{
			{Name: "Action", Translations: []GenreTranslation{
				{Language: "en", Name: "Action"},
				{Language: "ja", Name: "アクション"},
			}},
		},
	}
	clone := orig.Clone()

	// Modify nested translations in clone
	clone.Actresses[0].Translations = append(clone.Actresses[0].Translations, ActressTranslation{Language: "zh"})
	clone.Genres[0].Translations = append(clone.Genres[0].Translations, GenreTranslation{Language: "zh"})

	// Verify originals unchanged
	if len(orig.Actresses[0].Translations) != 2 {
		t.Errorf("original Actresses[0].Translations length: got %d, want 2", len(orig.Actresses[0].Translations))
	}
	if len(orig.Genres[0].Translations) != 2 {
		t.Errorf("original Genres[0].Translations length: got %d, want 2", len(orig.Genres[0].Translations))
	}
}

func TestMovieClone_NilFields(t *testing.T) {
	t.Parallel()
	orig := &Movie{
		ContentID:    "ABCD-123",
		Actresses:    nil,
		Genres:       nil,
		Screenshots:  nil,
		Translations: nil,
		ReleaseDate:  nil,
		Poster: PosterState{
			OriginalShouldCropPoster: nil,
		},
	}
	clone := orig.Clone()
	if clone == nil {
		t.Fatal("Clone() returned nil for non-nil receiver")
	}
	if clone.ContentID != "ABCD-123" {
		t.Errorf("ContentID: got %q, want %q", clone.ContentID, "ABCD-123")
	}
	if clone.Actresses != nil {
		t.Errorf("Actresses: got %v, want nil", clone.Actresses)
	}
	if clone.Genres != nil {
		t.Errorf("Genres: got %v, want nil", clone.Genres)
	}
	if clone.Screenshots != nil {
		t.Errorf("Screenshots: got %v, want nil", clone.Screenshots)
	}
	if clone.Translations != nil {
		t.Errorf("Translations: got %v, want nil", clone.Translations)
	}
	if clone.ReleaseDate != nil {
		t.Errorf("ReleaseDate: got %v, want nil", clone.ReleaseDate)
	}
	if clone.Poster.OriginalShouldCropPoster != nil {
		t.Errorf("OriginalShouldCropPoster: got %v, want nil", clone.Poster.OriginalShouldCropPoster)
	}
}

func TestMovieClone_EmptySlices(t *testing.T) {
	t.Parallel()
	orig := &Movie{
		ContentID:    "ABCD-123",
		Actresses:    []Actress{},
		Genres:       []Genre{},
		Screenshots:  []string{},
		Translations: []MovieTranslation{},
	}
	clone := orig.Clone()
	if clone == nil {
		t.Fatal("Clone() returned nil for non-nil receiver")
	}
	if clone.Actresses == nil {
		t.Error("Actresses: got nil, want empty slice")
	} else if len(clone.Actresses) != 0 {
		t.Errorf("Actresses: got length %d, want 0", len(clone.Actresses))
	}
	if clone.Genres == nil {
		t.Error("Genres: got nil, want empty slice")
	} else if len(clone.Genres) != 0 {
		t.Errorf("Genres: got length %d, want 0", len(clone.Genres))
	}
	if clone.Screenshots == nil {
		t.Error("Screenshots: got nil, want empty slice")
	} else if len(clone.Screenshots) != 0 {
		t.Errorf("Screenshots: got length %d, want 0", len(clone.Screenshots))
	}
	if clone.Translations == nil {
		t.Error("Translations: got nil, want empty slice")
	} else if len(clone.Translations) != 0 {
		t.Errorf("Translations: got length %d, want 0", len(clone.Translations))
	}
}
