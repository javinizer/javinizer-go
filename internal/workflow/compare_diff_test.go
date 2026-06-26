package workflow

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestIdentifyDifferencesExtended_NoDifferences(t *testing.T) {
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ContentID:   "ABCD-123",
		ID:          "ABCD-123",
		Title:       "Test Title",
		Description: "A description",
		Director:    "Director Name",
		Maker:       "Studio A",
		Label:       "Label X",
		Series:      "Series 1",
		Runtime:     120,
		ReleaseDate: &date,
		Poster: models.PosterState{
			CoverURL:  "https://example.com/cover.jpg",
			PosterURL: "https://example.com/poster.jpg",
		},
		TrailerURL: "https://example.com/trailer.mp4",
		Actresses: []models.Actress{
			{JapaneseName: "田中麻美"},
			{JapaneseName: "佐藤花子"},
		},
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
		},
	}

	diffs := identifyDifferences(movie, movie, movie)
	assert.Empty(t, diffs, "identical movies should have no differences")
}

func TestIdentifyDifferencesExtended_ScalarFields(t *testing.T) {
	nfo := &models.Movie{Title: "Old Title", Maker: "Old Maker", Label: "Old Label", Series: "Old Series"}
	scraped := &models.Movie{Title: "New Title", Maker: "New Maker", Label: "New Label", Series: "New Series"}
	merged := &models.Movie{Title: "Merged Title", Maker: "Merged Maker", Label: "Merged Label", Series: "Merged Series"}

	diffs := identifyDifferences(nfo, scraped, merged)

	diffMap := map[string]FieldDifference{}
	for _, d := range diffs {
		diffMap[d.Field] = d
	}

	assert.Contains(t, diffMap, "title")
	assert.Equal(t, "Old Title", diffMap["title"].NFOValue)
	assert.Equal(t, "New Title", diffMap["title"].ScrapedValue)
	assert.Equal(t, "Merged Title", diffMap["title"].MergedValue)

	assert.Contains(t, diffMap, "maker")
	assert.Contains(t, diffMap, "label")
	assert.Contains(t, diffMap, "series")
}

func TestIdentifyDifferencesExtended_ReleaseDate(t *testing.T) {
	date1 := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		nfoDate *time.Time
		scrDate *time.Time
		expect  bool
	}{
		{"both nil", nil, nil, false},
		{"both same", &date1, &date1, false},
		{"different dates", &date1, &date2, true},
		{"nil vs non-nil", nil, &date1, true},
		{"non-nil vs nil", &date1, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfo := &models.Movie{ReleaseDate: tt.nfoDate}
			scraped := &models.Movie{ReleaseDate: tt.scrDate}
			merged := &models.Movie{ReleaseDate: tt.nfoDate}

			diffs := identifyDifferences(nfo, scraped, merged)
			found := false
			for _, d := range diffs {
				if d.Field == "release_date" {
					found = true
					break
				}
			}
			assert.Equal(t, tt.expect, found)
		})
	}
}

func TestIdentifyDifferencesExtended_MediaURLs(t *testing.T) {
	nfo := &models.Movie{
		Poster:     models.PosterState{CoverURL: "old-cover.jpg", PosterURL: "old-poster.jpg"},
		TrailerURL: "old-trailer.mp4",
	}
	scraped := &models.Movie{
		Poster:     models.PosterState{CoverURL: "new-cover.jpg", PosterURL: "new-poster.jpg"},
		TrailerURL: "new-trailer.mp4",
	}
	merged := &models.Movie{
		Poster:     models.PosterState{CoverURL: "merged-cover.jpg", PosterURL: "merged-poster.jpg"},
		TrailerURL: "merged-trailer.mp4",
	}

	diffs := identifyDifferences(nfo, scraped, merged)

	diffMap := map[string]FieldDifference{}
	for _, d := range diffs {
		diffMap[d.Field] = d
	}

	assert.Contains(t, diffMap, "cover_url")
	assert.Contains(t, diffMap, "poster_url")
	assert.Contains(t, diffMap, "trailer_url")
}

func TestIdentifyDifferencesExtended_MediaURLsSkippedWhenBothEmpty(t *testing.T) {
	nfo := &models.Movie{}
	scraped := &models.Movie{}
	merged := &models.Movie{}

	diffs := identifyDifferences(nfo, scraped, merged)

	for _, d := range diffs {
		assert.NotEqual(t, "cover_url", d.Field, "empty cover URLs should not produce a diff")
		assert.NotEqual(t, "poster_url", d.Field, "empty poster URLs should not produce a diff")
		assert.NotEqual(t, "trailer_url", d.Field, "empty trailer URLs should not produce a diff")
	}
}

func TestIdentifyDifferencesExtended_Rating(t *testing.T) {
	tests := []struct {
		name   string
		nfo    float64
		scr    float64
		expect bool
	}{
		{"both zero", 0, 0, false},
		{"same non-zero", 7.5, 7.5, false},
		{"different", 7.5, 8.0, true},
		{"zero vs non-zero", 0, 8.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfo := &models.Movie{RatingScore: tt.nfo}
			scraped := &models.Movie{RatingScore: tt.scr}
			merged := &models.Movie{RatingScore: tt.scr}

			diffs := identifyDifferences(nfo, scraped, merged)
			found := false
			for _, d := range diffs {
				if d.Field == "rating" {
					found = true
					break
				}
			}
			assert.Equal(t, tt.expect, found)
		})
	}
}

func TestIdentifyDifferencesExtended_Actresses(t *testing.T) {
	tests := []struct {
		name    string
		nfo     []models.Actress
		scraped []models.Actress
		expect  bool
	}{
		{
			"same actresses",
			[]models.Actress{{JapaneseName: "田中麻美"}, {JapaneseName: "佐藤花子"}},
			[]models.Actress{{JapaneseName: "田中麻美"}, {JapaneseName: "佐藤花子"}},
			false,
		},
		{
			"different actresses",
			[]models.Actress{{JapaneseName: "田中麻美"}},
			[]models.Actress{{JapaneseName: "佐藤花子"}},
			true,
		},
		{
			"different count",
			[]models.Actress{{JapaneseName: "田中麻美"}},
			[]models.Actress{{JapaneseName: "田中麻美"}, {JapaneseName: "佐藤花子"}},
			true,
		},
		{
			"same actresses different order",
			[]models.Actress{{JapaneseName: "佐藤花子"}, {JapaneseName: "田中麻美"}},
			[]models.Actress{{JapaneseName: "田中麻美"}, {JapaneseName: "佐藤花子"}},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfo := &models.Movie{Actresses: tt.nfo}
			scraped := &models.Movie{Actresses: tt.scraped}
			merged := &models.Movie{Actresses: tt.scraped}

			diffs := identifyDifferences(nfo, scraped, merged)
			found := false
			for _, d := range diffs {
				if d.Field == "actresses" {
					found = true
					break
				}
			}
			assert.Equal(t, tt.expect, found)
		})
	}
}

func TestIdentifyDifferencesExtended_Genres(t *testing.T) {
	nfo := &models.Movie{
		Genres: []models.Genre{{Name: "Drama"}, {Name: "Action"}},
	}
	scraped := &models.Movie{
		Genres: []models.Genre{{Name: "Romance"}, {Name: "Drama"}},
	}
	merged := &models.Movie{
		Genres: []models.Genre{{Name: "Romance"}, {Name: "Drama"}},
	}

	diffs := identifyDifferences(nfo, scraped, merged)
	found := false
	for _, d := range diffs {
		if d.Field == "genres" {
			found = true
			// Check that formatted output includes names, not just count
			assert.Contains(t, d.NFOValue.(string), "Drama")
			assert.Contains(t, d.ScrapedValue.(string), "Romance")
			break
		}
	}
	assert.True(t, found, "different genres should produce a diff")
}

func TestIdentifyDifferencesExtended_ContentID(t *testing.T) {
	// ContentID diff only shown when different from ID (meaningful difference)
	nfo := &models.Movie{ContentID: "ABCD-123", ID: "ABCD-123"}
	scraped := &models.Movie{ContentID: "ABCD-456", ID: "ABCD-123"}
	merged := &models.Movie{ContentID: "ABCD-456", ID: "ABCD-123"}

	diffs := identifyDifferences(nfo, scraped, merged)
	found := false
	for _, d := range diffs {
		if d.Field == "content_id" {
			found = true
			break
		}
	}
	assert.True(t, found, "different ContentID should produce a diff when different from ID")
}

func TestActressKey_CompositeStrategy(t *testing.T) {
	tests := []struct {
		name string
		a    models.Actress
		want string
	}{
		{
			"DMMID preferred",
			models.Actress{DMMID: 12345, JapaneseName: "田中麻美", FirstName: "Ami", LastName: "Tanaka"},
			"dmm:12345",
		},
		{
			"JapaneseName when no DMMID",
			models.Actress{JapaneseName: "田中麻美", FirstName: "Ami", LastName: "Tanaka"},
			"ja:田中麻美",
		},
		{
			"FullName for Western-only name",
			models.Actress{FirstName: "Ami", LastName: "Tanaka"},
			"en:Tanaka Ami",
		},
		{
			"empty actress falls back to en:",
			models.Actress{},
			"en:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, actressKey(tt.a))
		})
	}
}

func TestActressSlicesEqual_CompositeKey(t *testing.T) {
	// Western-name-only actresses that previously would false-match under JapaneseName-only comparison
	a := []models.Actress{
		{FirstName: "Ami", LastName: "Tanaka"},
		{FirstName: "Yui", LastName: "Hatano"},
	}
	b := []models.Actress{
		{FirstName: "Ami", LastName: "Tanaka"},
		{FirstName: "Yui", LastName: "Hatano"},
	}
	assert.True(t, actressSlicesEqual(a, b), "Western-named actresses should match by FullName")

	// Different Western-name actresses
	c := []models.Actress{
		{FirstName: "Ami", LastName: "Tanaka"},
		{FirstName: "Sora", LastName: "Aoi"},
	}
	assert.False(t, actressSlicesEqual(a, c), "different Western-named actresses should not match")
}

func TestActressSlicesEqual_MixedIdentifiers(t *testing.T) {
	// One actress matched by DMMID, one by JapaneseName
	a := []models.Actress{
		{DMMID: 100, JapaneseName: "田中麻美"},
		{JapaneseName: "佐藤花子"},
	}
	b := []models.Actress{
		{DMMID: 100, JapaneseName: "田中麻美"}, // matched by DMMID
		{JapaneseName: "佐藤花子"},             // matched by JapaneseName
	}
	assert.True(t, actressSlicesEqual(a, b))

	// DMMID mismatch
	c := []models.Actress{
		{DMMID: 999, JapaneseName: "田中麻美"}, // same JapaneseName but different DMMID
		{JapaneseName: "佐藤花子"},
	}
	assert.False(t, actressSlicesEqual(a, c), "different DMMID should not match even with same JapaneseName")
}

func TestFormatTimePtr(t *testing.T) {
	assert.Equal(t, "<nil>", formatTimePtr(nil))

	date := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, "2024-03-15", formatTimePtr(&date))
}

func TestFormatActressList(t *testing.T) {
	tests := []struct {
		name string
		a    []models.Actress
		want string
	}{
		{"empty", []models.Actress{}, "0 actresses"},
		{"one", []models.Actress{{JapaneseName: "田中麻美"}}, "1 actresses: 田中麻美"},
		{"two", []models.Actress{{JapaneseName: "田中麻美"}, {JapaneseName: "佐藤花子"}}, "2 actresses: 田中麻美, 佐藤花子"},
		{"truncated",
			[]models.Actress{{JapaneseName: "A"}, {JapaneseName: "B"}, {JapaneseName: "C"}, {JapaneseName: "D"}},
			"4 actresses: A, B, C, ..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatActressList(tt.a))
		})
	}
}

func TestFormatGenreList(t *testing.T) {
	tests := []struct {
		name string
		g    []models.Genre
		want string
	}{
		{"empty", []models.Genre{}, "0 genres"},
		{"one", []models.Genre{{Name: "Drama"}}, "1 genres: Drama"},
		{"two", []models.Genre{{Name: "Drama"}, {Name: "Romance"}}, "2 genres: Drama, Romance"},
		{"truncated",
			[]models.Genre{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}},
			"4 genres: A, B, C, ..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatGenreList(tt.g))
		})
	}
}
