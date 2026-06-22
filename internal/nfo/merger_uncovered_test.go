package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeStrategy_String(t *testing.T) {
	tests := []struct {
		s    MergeStrategy
		want string
	}{
		{PreferScraper, "prefer-scraper"},
		{PreferNFO, "prefer-nfo"},
		{MergeArrays, "merge-arrays"},
		{PreserveExisting, "preserve-existing"},
		{FillMissingOnly, "fill-missing-only"},
		{MergeStrategy("custom"), "custom"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.s.String())
	}
}

func TestApplyPresetTyped(t *testing.T) {
	t.Run("empty preset returns original values", func(t *testing.T) {
		s, a, err := ApplyPresetTyped("", PreferScraper, false)
		require.NoError(t, err)
		assert.Equal(t, PreferScraper, s)
		assert.False(t, a)
	})

	t.Run("conservative preset", func(t *testing.T) {
		s, a, err := ApplyPresetTyped("conservative", PreferScraper, false)
		require.NoError(t, err)
		assert.Equal(t, PreserveExisting, s)
		assert.True(t, a)
	})

	t.Run("gap-fill preset", func(t *testing.T) {
		s, a, err := ApplyPresetTyped("gap-fill", PreferNFO, true)
		require.NoError(t, err)
		assert.Equal(t, FillMissingOnly, s)
		assert.True(t, a)
	})

	t.Run("aggressive preset", func(t *testing.T) {
		s, a, err := ApplyPresetTyped("aggressive", PreferNFO, true)
		require.NoError(t, err)
		assert.Equal(t, PreferScraper, s)
		assert.False(t, a)
	})

	t.Run("case-insensitive preset", func(t *testing.T) {
		s, a, err := ApplyPresetTyped("Conservative", PreferScraper, false)
		require.NoError(t, err)
		assert.Equal(t, PreserveExisting, s)
		assert.True(t, a)
	})

	t.Run("invalid preset returns error", func(t *testing.T) {
		_, _, err := ApplyPresetTyped("unknown", PreferNFO, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid preset")
	})
}

func TestMergeMovieMetadataWithOptions_CriticalFields(t *testing.T) {
	now := time.Now()

	t.Run("critical field ID falls back to NFO when scraper empty", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "abc123", ID: "", Title: "title", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "abc123", ID: "IPX-001", Title: "title", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
		require.NoError(t, err)
		assert.Equal(t, "IPX-001", result.Merged.ID)
	})

	t.Run("critical field ContentID falls back to NFO when scraper empty", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "", ID: "IPX-001", Title: "title", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "nfo123", ID: "IPX-001", Title: "title", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
		require.NoError(t, err)
		assert.Equal(t, "nfo123", result.Merged.ContentID)
	})

	t.Run("critical field Title falls back to NFO when scraper empty", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "NFO Title", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
		require.NoError(t, err)
		assert.Equal(t, "NFO Title", result.Merged.Title)
	})

	t.Run("both sources empty for critical field uses fallback", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "", ID: "", Title: "", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "", ID: "", Title: "", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
		require.NoError(t, err)
		assert.Contains(t, result.Merged.ID, "Unknown")
	})
}

func TestMergeMovieMetadataWithOptions_NilInputsExtra(t *testing.T) {
	t.Run("both nil returns error", func(t *testing.T) {
		_, err := MergeMovieMetadataWithOptions(nil, nil, PreferNFO, true)
		assert.Error(t, err)
	})

	t.Run("nil scraped uses NFO", func(t *testing.T) {
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Test", UpdatedAt: time.Now()}
		result, err := MergeMovieMetadataWithOptions(nil, nfo, PreferNFO, true)
		require.NoError(t, err)
		assert.Equal(t, "IPX-001", result.Merged.ID)
		assert.Equal(t, 0, result.Stats.FromScraper)
		assert.GreaterOrEqual(t, result.Stats.FromNFO, 1)
	})

	t.Run("nil NFO uses scraped", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-002", Title: "Test", UpdatedAt: time.Now()}
		result, err := MergeMovieMetadataWithOptions(scraped, nil, PreferScraper, true)
		require.NoError(t, err)
		assert.Equal(t, "IPX-002", result.Merged.ID)
		assert.GreaterOrEqual(t, result.Stats.FromScraper, 1)
		assert.Equal(t, 0, result.Stats.FromNFO)
	})
}

func TestMergeMovieMetadataWithOptions_Strategies(t *testing.T) {
	now := time.Now()

	t.Run("PreferScraper uses scraper value", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Scraper", Maker: "SMaker", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "NFO", Maker: "NMaker", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
		require.NoError(t, err)
		assert.Equal(t, "Scraper", result.Merged.Title)
		assert.Equal(t, "SMaker", result.Merged.Maker)
	})

	t.Run("PreferNFO uses NFO value", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Scraper", Maker: "SMaker", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "NFO", Maker: "NMaker", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
		require.NoError(t, err)
		assert.Equal(t, "NFO", result.Merged.Title)
		assert.Equal(t, "NMaker", result.Merged.Maker)
	})

	t.Run("PreserveExisting keeps NFO when both have values", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Scraper", Maker: "SMaker", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "NFO", Maker: "NMaker", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreserveExisting, false)
		require.NoError(t, err)
		assert.Equal(t, "NFO", result.Merged.Title)
	})

	t.Run("FillMissingOnly fills only empty fields", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Scraper", Maker: "SMaker", Label: "", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "NFO", Maker: "", Label: "NLabel", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, FillMissingOnly, false)
		require.NoError(t, err)
		// Maker: NFO empty, scraped has value -> fill from scraped
		assert.Equal(t, "SMaker", result.Merged.Maker)
		// Label: scraped empty, NFO has value -> fill from NFO
		assert.Equal(t, "NLabel", result.Merged.Label)
	})

	t.Run("MergeArrays combines arrays from both sources", func(t *testing.T) {
		scraped := &models.Movie{
			ContentID: "abc", ID: "IPX-001", Title: "Test",
			Genres:    []models.Genre{{Name: "Action"}, {Name: "Drama"}},
			UpdatedAt: now,
		}
		nfo := &models.Movie{
			ContentID: "abc", ID: "IPX-001", Title: "Test",
			Genres:    []models.Genre{{Name: "Comedy"}, {Name: "Drama"}},
			UpdatedAt: now,
		}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
		require.NoError(t, err)
		// Should deduplicate "Drama"
		assert.Len(t, result.Merged.Genres, 3)
		genreNames := make([]string, len(result.Merged.Genres))
		for i, g := range result.Merged.Genres {
			genreNames[i] = g.Name
		}
		assert.Contains(t, genreNames, "Action")
		assert.Contains(t, genreNames, "Drama")
		assert.Contains(t, genreNames, "Comedy")
	})
}

func TestMergeMovieMetadataWithOptions_Timestamps(t *testing.T) {
	t.Run("uses scraped CreatedAt when newer", func(t *testing.T) {
		t1 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		t2 := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Test", CreatedAt: t2, UpdatedAt: t2}
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Test", CreatedAt: t1, UpdatedAt: t1}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
		require.NoError(t, err)
		assert.Equal(t, t2, result.Merged.CreatedAt)
	})

	t.Run("uses NFO CreatedAt when scraped is zero", func(t *testing.T) {
		t1 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Test", CreatedAt: time.Time{}, UpdatedAt: t1}
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Test", CreatedAt: t1, UpdatedAt: t1}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
		require.NoError(t, err)
		assert.Equal(t, t1, result.Merged.CreatedAt)
	})
}

func TestMergeMovieMetadataWithOptions_Provenance(t *testing.T) {
	now := time.Now()

	t.Run("provenance tracks data sources", func(t *testing.T) {
		scraped := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "Scraper", Maker: "SMaker", UpdatedAt: now}
		nfo := &models.Movie{ContentID: "abc", ID: "IPX-001", Title: "NFO", Maker: "NMaker", UpdatedAt: now}
		result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
		require.NoError(t, err)
		assert.Equal(t, "scraper", result.Provenance["Title"].Source)
		assert.Equal(t, "scraper", result.Provenance["Maker"].Source)
	})
}

func TestParseScalarStrategyExtra(t *testing.T) {
	tests := []struct {
		input string
		want  MergeStrategy
		err   bool
	}{
		{"", PreferNFO, false},
		{"prefer-scraper", PreferScraper, false},
		{"prefer-nfo", PreferNFO, false},
		{"merge-arrays", MergeArrays, false},
		{"preserve-existing", PreserveExisting, false},
		{"fill-missing-only", FillMissingOnly, false},
		{"PREFER-NFO", PreferNFO, false},
		{"invalid", PreferNFO, true},
	}
	for _, tt := range tests {
		got, err := ParseScalarStrategy(tt.input)
		if tt.err {
			assert.Error(t, err, "input=%q", tt.input)
		} else {
			assert.NoError(t, err, "input=%q", tt.input)
			assert.Equal(t, tt.want, got, "input=%q", tt.input)
		}
	}
}
