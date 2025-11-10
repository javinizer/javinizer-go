package nfo

import (
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeMovieMetadata_BothNil(t *testing.T) {
	result, err := MergeMovieMetadata(nil, nil, PreferScraper)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestMergeMovieMetadata_ScraperOnly(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "Test Movie",
		Description: "Description",
	}

	result, err := MergeMovieMetadata(scraped, nil, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, scraped, result.Merged)
	assert.Equal(t, 3, result.Stats.FromScraper)
	assert.Equal(t, 3, result.Stats.TotalFields)
	assert.Contains(t, result.Provenance, "ID")
	assert.Equal(t, "scraper", result.Provenance["ID"].Source)
}

func TestMergeMovieMetadata_NFOOnly(t *testing.T) {
	nfo := &models.Movie{
		ID:          "IPX-456",
		Title:       "NFO Movie",
		Description: "NFO Description",
	}

	result, err := MergeMovieMetadata(nil, nfo, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, nfo, result.Merged)
	assert.Equal(t, 3, result.Stats.FromNFO)
	assert.Equal(t, 3, result.Stats.TotalFields)
	assert.Contains(t, result.Provenance, "ID")
	assert.Equal(t, "nfo", result.Provenance["ID"].Source)
}

func TestMergeMovieMetadata_PreferScraper(t *testing.T) {
	releaseDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "Scraped Title",
		Description: "Scraped Description",
		Director:    "Scraped Director",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		RatingScore: 8.5,
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
		},
		Genres: []models.Genre{
			{Name: "Drama"},
		},
	}

	nfo := &models.Movie{
		ID:          "IPX-123",
		Title:       "NFO Title",
		Description: "", // Empty in NFO
		Maker:       "NFO Maker",
		Label:       "NFO Label",
		RatingScore: 7.0,
		Actresses: []models.Actress{
			{FirstName: "Ai", LastName: "Sayama"},
		},
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	merged := result.Merged

	// Scraper data should win for conflicting fields
	assert.Equal(t, "Scraped Title", merged.Title)
	assert.Equal(t, "Scraped Director", merged.Director)
	assert.Equal(t, 8.5, merged.RatingScore)
	assert.Len(t, merged.Actresses, 1)
	assert.Equal(t, "Yui", merged.Actresses[0].FirstName)

	// NFO data should be used for fields where scraper is empty
	assert.Equal(t, "Scraped Description", merged.Description)
	assert.Equal(t, "NFO Maker", merged.Maker)
	assert.Equal(t, "NFO Label", merged.Label)

	// Check provenance
	assert.Equal(t, "scraper", result.Provenance["Title"].Source)
	assert.Equal(t, "scraper", result.Provenance["Description"].Source)
	assert.Equal(t, "nfo", result.Provenance["Maker"].Source)

	// Check stats
	assert.Greater(t, result.Stats.FromScraper, 0)
	assert.Greater(t, result.Stats.FromNFO, 0)
	assert.Greater(t, result.Stats.ConflictsResolved, 0)
}

func TestMergeMovieMetadata_PreferNFO(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "Scraped Title",
		Description: "Scraped Description",
	}

	nfo := &models.Movie{
		ID:    "IPX-123",
		Title: "NFO Title",
		Maker: "NFO Maker",
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferNFO)
	require.NoError(t, err)
	require.NotNil(t, result)

	merged := result.Merged

	// NFO data should win for conflicting fields
	assert.Equal(t, "NFO Title", merged.Title)

	// Scraper data should be used where NFO is empty
	assert.Equal(t, "Scraped Description", merged.Description)

	// NFO-only field
	assert.Equal(t, "NFO Maker", merged.Maker)

	// Check provenance
	assert.Equal(t, "nfo", result.Provenance["Title"].Source)
	assert.Equal(t, "scraper", result.Provenance["Description"].Source)
	assert.Equal(t, "nfo", result.Provenance["Maker"].Source)
}

func TestMergeMovieMetadata_MergeArrays_Actresses(t *testing.T) {
	scraped := &models.Movie{
		ID: "IPX-123",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
			{FirstName: "Ai", LastName: "Sayama"},
		},
	}

	nfo := &models.Movie{
		ID: "IPX-123",
		Actresses: []models.Actress{
			{FirstName: "Ai", LastName: "Sayama"}, // Duplicate
			{FirstName: "Tia", LastName: "Bejean"},
		},
	}

	result, err := MergeMovieMetadata(scraped, nfo, MergeArrays)
	require.NoError(t, err)
	require.NotNil(t, result)

	merged := result.Merged

	// Should have 3 actresses (Yui, Ai, Tia) - deduplicated
	assert.Len(t, merged.Actresses, 3)

	actressNames := make(map[string]bool)
	for _, actress := range merged.Actresses {
		key := actress.FirstName + " " + actress.LastName
		actressNames[key] = true
	}

	assert.True(t, actressNames["Yui Hatano"])
	assert.True(t, actressNames["Ai Sayama"])
	assert.True(t, actressNames["Tia Bejean"])

	// Check provenance
	assert.Equal(t, "merged", result.Provenance["Actresses"].Source)
	assert.Equal(t, 1, result.Stats.MergedArrays)
}

func TestMergeMovieMetadata_MergeArrays_Genres(t *testing.T) {
	scraped := &models.Movie{
		ID: "IPX-123",
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
		},
	}

	nfo := &models.Movie{
		ID: "IPX-123",
		Genres: []models.Genre{
			{Name: "Romance"}, // Duplicate
			{Name: "Comedy"},
		},
	}

	result, err := MergeMovieMetadata(scraped, nfo, MergeArrays)
	require.NoError(t, err)
	require.NotNil(t, result)

	merged := result.Merged

	// Should have 3 genres (Drama, Romance, Comedy) - deduplicated
	assert.Len(t, merged.Genres, 3)

	genreNames := make(map[string]bool)
	for _, genre := range merged.Genres {
		genreNames[genre.Name] = true
	}

	assert.True(t, genreNames["Drama"])
	assert.True(t, genreNames["Romance"])
	assert.True(t, genreNames["Comedy"])

	// Check provenance
	assert.Equal(t, "merged", result.Provenance["Genres"].Source)
}

func TestMergeMovieMetadata_MergeArrays_Screenshots(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		Screenshots: []string{"url1.jpg", "url2.jpg"},
	}

	nfo := &models.Movie{
		ID:          "IPX-123",
		Screenshots: []string{"url2.jpg", "url3.jpg"}, // url2.jpg is duplicate
	}

	result, err := MergeMovieMetadata(scraped, nfo, MergeArrays)
	require.NoError(t, err)
	require.NotNil(t, result)

	merged := result.Merged

	// Should have 3 screenshots - deduplicated
	assert.Len(t, merged.Screenshots, 3)
	assert.Contains(t, merged.Screenshots, "url1.jpg")
	assert.Contains(t, merged.Screenshots, "url2.jpg")
	assert.Contains(t, merged.Screenshots, "url3.jpg")

	// Check provenance
	assert.Equal(t, "merged", result.Provenance["Screenshots"].Source)
}

func TestMergeMovieMetadata_EmptyFields(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "",
		Description: "",
	}

	nfo := &models.Movie{
		ID:          "IPX-123",
		Title:       "",
		Description: "",
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	merged := result.Merged

	// All empty fields should be empty
	assert.Equal(t, "IPX-123", merged.ID) // ID is not empty
	assert.Equal(t, "", merged.Title)
	assert.Equal(t, "", merged.Description)

	// Check provenance for empty fields
	assert.Equal(t, "empty", result.Provenance["Title"].Source)
	assert.Equal(t, "empty", result.Provenance["Description"].Source)

	// Stats should reflect empty fields
	assert.Greater(t, result.Stats.EmptyFields, 0)
}

func TestMergeMovieMetadata_DateFields(t *testing.T) {
	scrapedDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	nfoDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	scraped := &models.Movie{
		ID:          "IPX-123",
		ReleaseDate: &scrapedDate,
	}

	nfo := &models.Movie{
		ID:          "IPX-123",
		ReleaseDate: &nfoDate,
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Scraper date should win
	assert.Equal(t, scrapedDate, *result.Merged.ReleaseDate)
	assert.Equal(t, "scraper", result.Provenance["ReleaseDate"].Source)
}

func TestMergeMovieMetadata_IntFields(t *testing.T) {
	scraped := &models.Movie{
		ID:      "IPX-123",
		Runtime: 120,
	}

	nfo := &models.Movie{
		ID:      "IPX-123",
		Runtime: 90,
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Scraper runtime should win
	assert.Equal(t, 120, result.Merged.Runtime)
	assert.Equal(t, "scraper", result.Provenance["Runtime"].Source)
}

func TestMergeMovieMetadata_FloatFields(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		RatingScore: 8.5,
	}

	nfo := &models.Movie{
		ID:          "IPX-123",
		RatingScore: 7.0,
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Scraper rating should win
	assert.Equal(t, 8.5, result.Merged.RatingScore)
	assert.Equal(t, "scraper", result.Provenance["RatingScore"].Source)
}

func TestMergeMovieMetadata_BoolFields(t *testing.T) {
	scraped := &models.Movie{
		ID:               "IPX-123",
		ShouldCropPoster: true,
	}

	nfo := &models.Movie{
		ID:               "IPX-123",
		ShouldCropPoster: false,
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Scraper bool should win
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "scraper", result.Provenance["ShouldCropPoster"].Source)
}

func TestMergeMovieMetadata_PartialOverlap(t *testing.T) {
	releaseDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "Scraped Title",
		Description: "Scraped Description",
		ReleaseDate: &releaseDate,
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
		},
	}

	nfo := &models.Movie{
		ID:      "IPX-123",
		Title:   "NFO Title",
		Maker:   "NFO Maker",
		Label:   "NFO Label",
		Runtime: 90,
		Genres: []models.Genre{
			{Name: "Drama"},
		},
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)
	require.NotNil(t, result)

	merged := result.Merged

	// Scraped fields should be present
	assert.Equal(t, "Scraped Title", merged.Title)
	assert.Equal(t, "Scraped Description", merged.Description)
	assert.NotNil(t, merged.ReleaseDate)
	assert.Len(t, merged.Actresses, 1)

	// NFO-only fields should be present
	assert.Equal(t, "NFO Maker", merged.Maker)
	assert.Equal(t, "NFO Label", merged.Label)
	assert.Equal(t, 90, merged.Runtime)
	assert.Len(t, merged.Genres, 1)

	// Check stats
	assert.Greater(t, result.Stats.FromScraper, 0)
	assert.Greater(t, result.Stats.FromNFO, 0)
	assert.Greater(t, result.Stats.TotalFields, 0)
}

func TestActressKey(t *testing.T) {
	tests := []struct {
		name    string
		actress models.Actress
		want    string
	}{
		{
			name:    "JapaneseName priority (most consistent across sources)",
			actress: models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", DMMID: 123456},
			want:    "jp:波多野結衣",
		},
		{
			name:    "JapaneseName without DMMID",
			actress: models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"},
			want:    "jp:波多野結衣",
		},
		{
			name:    "DMMID fallback (no Japanese name)",
			actress: models.Actress{FirstName: "Yui", LastName: "Hatano", DMMID: 123456},
			want:    "dmm:123456",
		},
		{
			name:    "Romanized name only (no DMMID, no Japanese)",
			actress: models.Actress{FirstName: "Yui", LastName: "Hatano"},
			want:    "name:yui|hatano",
		},
		{
			name:    "Only first name",
			actress: models.Actress{FirstName: "Madonna"},
			want:    "name:madonna|",
		},
		{
			name:    "Only Japanese name",
			actress: models.Actress{JapaneseName: "波多野結衣"},
			want:    "jp:波多野結衣",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actressKey(tt.actress)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMergeMovieMetadata_ProvenanceConfidence(t *testing.T) {
	scraped := &models.Movie{
		ID:    "IPX-123",
		Title: "Test",
	}

	nfo := &models.Movie{
		ID:    "IPX-123",
		Maker: "NFO Maker",
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)

	// Check that provenance has confidence values
	assert.Equal(t, 1.0, result.Provenance["Title"].Confidence)
	assert.Equal(t, 1.0, result.Provenance["Maker"].Confidence)
}

func TestMergeMovieMetadata_StatsAccuracy(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "Scraped",
		Description: "Desc",
		Actresses: []models.Actress{
			{FirstName: "Yui"},
		},
	}

	nfo := &models.Movie{
		ID:    "IPX-123",
		Title: "NFO",
		Maker: "Maker",
		Genres: []models.Genre{
			{Name: "Drama"},
		},
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)

	// ID: scraper (non-empty)
	// Title: scraper (conflict resolved)
	// Description: scraper
	// Maker: nfo
	// Actresses: scraper
	// Genres: nfo

	// TotalFields now counts all non-empty fields in merged result (more accurate)
	assert.Equal(t, countNonEmptyFields(result.Merged), result.Stats.TotalFields)
	assert.Greater(t, result.Stats.FromScraper, 0)
	assert.Greater(t, result.Stats.FromNFO, 0)
	assert.Greater(t, result.Stats.ConflictsResolved, 0)
}

// Tests for expert-recommended fixes

func TestMergeMovieMetadata_CreatedAt_NewerWins(t *testing.T) {
	olderTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newerTime := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		scrapedCreated  time.Time
		nfoCreated      time.Time
		expectedCreated time.Time
	}{
		{
			name:            "Scraper newer",
			scrapedCreated:  newerTime,
			nfoCreated:      olderTime,
			expectedCreated: newerTime,
		},
		{
			name:            "NFO newer",
			scrapedCreated:  olderTime,
			nfoCreated:      newerTime,
			expectedCreated: newerTime,
		},
		{
			name:            "Scraper zero, use NFO",
			scrapedCreated:  time.Time{},
			nfoCreated:      olderTime,
			expectedCreated: olderTime,
		},
		{
			name:            "NFO zero, use scraper",
			scrapedCreated:  newerTime,
			nfoCreated:      time.Time{},
			expectedCreated: newerTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraped := &models.Movie{
				ID:        "IPX-123",
				CreatedAt: tt.scrapedCreated,
			}
			nfo := &models.Movie{
				ID:        "IPX-123",
				CreatedAt: tt.nfoCreated,
			}

			result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedCreated, result.Merged.CreatedAt)
		})
	}
}

func TestMakeProvenanceMap_NilInput(t *testing.T) {
	// Should not panic on nil input
	provenance := makeProvenanceMap(nil, "test")
	assert.Empty(t, provenance)
}

func TestCountNonEmptyFields_NilInput(t *testing.T) {
	// Should not panic on nil input
	count := countNonEmptyFields(nil)
	assert.Equal(t, 0, count)
}

func TestMergeMovieMetadata_ProvenanceLastUpdated(t *testing.T) {
	updatedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	scraped := &models.Movie{
		ID:        "IPX-123",
		Title:     "Test",
		UpdatedAt: updatedTime,
	}

	result, err := MergeMovieMetadata(scraped, nil, PreferScraper)
	require.NoError(t, err)

	// Check that LastUpdated is populated in provenance
	titleProvenance := result.Provenance["Title"]
	require.NotNil(t, titleProvenance.LastUpdated)
	assert.Equal(t, updatedTime, *titleProvenance.LastUpdated)
}

func TestMergeActresses_NormalizedDeduplication(t *testing.T) {
	scraped := &models.Movie{
		ID: "IPX-123",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
			{FirstName: "Ai", LastName: "Sayama"},
		},
	}

	nfo := &models.Movie{
		ID: "IPX-123",
		Actresses: []models.Actress{
			{FirstName: "YUI", LastName: "HATANO"},    // Case variant
			{FirstName: " Ai ", LastName: " Sayama "}, // Whitespace variant
			{FirstName: "Tia", LastName: "Bejean"},
		},
	}

	result, err := MergeMovieMetadata(scraped, nfo, MergeArrays)
	require.NoError(t, err)

	// Should have 3 actresses (case/whitespace variants deduplicated)
	assert.Len(t, result.Merged.Actresses, 3)

	// Verify the unique actresses
	actressNames := make(map[string]bool)
	for _, actress := range result.Merged.Actresses {
		key := strings.ToLower(actress.FirstName + " " + actress.LastName)
		actressNames[key] = true
	}

	assert.True(t, actressNames["yui hatano"])
	assert.True(t, actressNames["ai sayama"] || actressNames[" ai   sayama "])
	assert.True(t, actressNames["tia bejean"])
}

func TestMergeGenres_NormalizedDeduplication(t *testing.T) {
	scraped := &models.Movie{
		ID: "IPX-123",
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
		},
	}

	nfo := &models.Movie{
		ID: "IPX-123",
		Genres: []models.Genre{
			{Name: "DRAMA"},     // Case variant
			{Name: " Romance "}, // Whitespace variant
			{Name: "Comedy"},
		},
	}

	result, err := MergeMovieMetadata(scraped, nfo, MergeArrays)
	require.NoError(t, err)

	// Should have 3 genres (case/whitespace variants deduplicated)
	assert.Len(t, result.Merged.Genres, 3)

	genreNames := make(map[string]bool)
	for _, genre := range result.Merged.Genres {
		genreNames[strings.ToLower(strings.TrimSpace(genre.Name))] = true
	}

	assert.True(t, genreNames["drama"])
	assert.True(t, genreNames["romance"])
	assert.True(t, genreNames["comedy"])
}

func TestMergeScreenshots_NormalizedDeduplication(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		Screenshots: []string{"https://example.com/shot1.jpg", "https://example.com/shot2.jpg"},
	}

	nfo := &models.Movie{
		ID: "IPX-123",
		Screenshots: []string{
			"https://example.com/shot1.jpg/",  // Trailing slash variant
			" https://example.com/shot2.jpg ", // Whitespace variant
			"https://example.com/shot3.jpg",
		},
	}

	result, err := MergeMovieMetadata(scraped, nfo, MergeArrays)
	require.NoError(t, err)

	// Should have 3 screenshots (trailing slash/whitespace variants deduplicated)
	assert.Len(t, result.Merged.Screenshots, 3)

	// Verify unique screenshots
	screenshotSet := make(map[string]bool)
	for _, url := range result.Merged.Screenshots {
		normalized := strings.TrimSpace(strings.TrimSuffix(url, "/"))
		screenshotSet[normalized] = true
	}

	assert.True(t, screenshotSet["https://example.com/shot1.jpg"])
	assert.True(t, screenshotSet["https://example.com/shot2.jpg"])
	assert.True(t, screenshotSet["https://example.com/shot3.jpg"])
}

func TestActressKey_Normalization(t *testing.T) {
	tests := []struct {
		name        string
		actress1    models.Actress
		actress2    models.Actress
		shouldMatch bool
	}{
		{
			name:        "Case variants should match",
			actress1:    models.Actress{FirstName: "Yui", LastName: "Hatano"},
			actress2:    models.Actress{FirstName: "yui", LastName: "hatano"},
			shouldMatch: true,
		},
		{
			name:        "Whitespace variants should match",
			actress1:    models.Actress{FirstName: "Yui", LastName: "Hatano"},
			actress2:    models.Actress{FirstName: " Yui ", LastName: " Hatano "},
			shouldMatch: true,
		},
		{
			name:        "Different names should not match",
			actress1:    models.Actress{FirstName: "Yui", LastName: "Hatano"},
			actress2:    models.Actress{FirstName: "Ai", LastName: "Sayama"},
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key1 := actressKey(tt.actress1)
			key2 := actressKey(tt.actress2)

			if tt.shouldMatch {
				assert.Equal(t, key1, key2)
			} else {
				assert.NotEqual(t, key1, key2)
			}
		})
	}
}

func TestMergeMovieMetadata_TotalFieldsConsistency(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "Test",
		Description: "Desc",
	}

	nfo := &models.Movie{
		ID:    "IPX-123",
		Maker: "Maker",
	}

	result, err := MergeMovieMetadata(scraped, nfo, PreferScraper)
	require.NoError(t, err)

	// TotalFields should match the count of non-empty fields in merged result
	expectedCount := countNonEmptyFields(result.Merged)
	assert.Equal(t, expectedCount, result.Stats.TotalFields)
}
