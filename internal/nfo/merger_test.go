package nfo

import (
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
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

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
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

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
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

func TestMergeDateField(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-24 * time.Hour)

	t.Run("both empty returns nil", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		result := mergeDateField("release_date", nil, nil, PreferScraper, stats, provenance, now, earlier)
		assert.Nil(t, result)
		assert.Equal(t, 1, stats.EmptyFields)
	})

	t.Run("scraped empty uses nfo", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		nfoVal := now
		result := mergeDateField("release_date", nil, &nfoVal, PreferScraper, stats, provenance, now, earlier)
		assert.Equal(t, nfoVal, *result)
		assert.Equal(t, 1, stats.FromNFO)
	})

	t.Run("nfo empty uses scraped", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		scrapedVal := now
		result := mergeDateField("release_date", &scrapedVal, nil, PreferScraper, stats, provenance, now, earlier)
		assert.Equal(t, scrapedVal, *result)
		assert.Equal(t, 1, stats.FromScraper)
	})

	t.Run("both present prefer nfo", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		scrapedVal := now
		nfoVal := earlier
		result := mergeDateField("release_date", &scrapedVal, &nfoVal, PreferNFO, stats, provenance, now, earlier)
		assert.Equal(t, nfoVal, *result)
		assert.Equal(t, 1, stats.ConflictsResolved)
		assert.Equal(t, 1, stats.FromNFO)
	})

	t.Run("both present prefer scraper", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		scrapedVal := now
		nfoVal := earlier
		result := mergeDateField("release_date", &scrapedVal, &nfoVal, PreferScraper, stats, provenance, now, earlier)
		assert.Equal(t, scrapedVal, *result)
		assert.Equal(t, 1, stats.ConflictsResolved)
		assert.Equal(t, 1, stats.FromScraper)
	})

	t.Run("both present preserve existing", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		scrapedVal := now
		nfoVal := earlier
		result := mergeDateField("release_date", &scrapedVal, &nfoVal, PreserveExisting, stats, provenance, now, earlier)
		assert.Equal(t, nfoVal, *result)
	})

	t.Run("both present fill missing only", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		scrapedVal := now
		nfoVal := earlier
		result := mergeDateField("release_date", &scrapedVal, &nfoVal, FillMissingOnly, stats, provenance, now, earlier)
		assert.Equal(t, nfoVal, *result)
	})

	t.Run("both present merge arrays", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		scrapedVal := now
		nfoVal := earlier
		result := mergeDateField("release_date", &scrapedVal, &nfoVal, MergeArrays, stats, provenance, now, earlier)
		assert.Equal(t, scrapedVal, *result)
	})

	t.Run("scraped zero value treated as empty", func(t *testing.T) {
		stats := &MergeStats{}
		provenance := make(map[string]DataSource)
		zeroTime := time.Time{}
		nfoVal := now
		result := mergeDateField("release_date", &zeroTime, &nfoVal, PreferScraper, stats, provenance, now, earlier)
		assert.Equal(t, nfoVal, *result)
	})
}
