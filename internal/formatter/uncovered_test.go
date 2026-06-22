package formatter

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteMovie_GenresTruncationUncovered(t *testing.T) {
	genres := make([]models.Genre, 12)
	for i := range genres {
		genres[i] = models.Genre{Name: fmt.Sprintf("Genre%d", i)}
	}
	movie := &models.Movie{
		ID:     "GEN-001",
		Title:  "Many Genres",
		Genres: genres,
	}

	var buf bytes.Buffer
	WriteMovie(&buf, movie, nil)
	output := buf.String()
	assert.Contains(t, output, "and 4 more")
}

func TestWriteMovie_NoScraperResults_OmitsSourceURLsUncovered(t *testing.T) {
	movie := &models.Movie{
		ID:         "NOSRC-001",
		Title:      "No Sources",
		SourceName: "internal",
	}

	var buf bytes.Buffer
	WriteMovie(&buf, movie, nil)
	output := buf.String()
	assert.NotContains(t, output, "Source URLs:")
	assert.Contains(t, output, "Sources")
}

func TestWriteMovie_PosterSameAsCover_OmitsDuplicateUncovered(t *testing.T) {
	movie := &models.Movie{
		ID:     "DUP-001",
		Title:  "Duplicate URLs",
		Poster: models.PosterState{CoverURL: "https://example.com/img.jpg", PosterURL: "https://example.com/img.jpg"},
	}

	var buf bytes.Buffer
	WriteMovie(&buf, movie, nil)
	output := buf.String()
	// Poster URL should not be shown when it equals cover URL
	count := strings.Count(output, "Poster URL")
	assert.Equal(t, 0, count, "poster URL should be omitted when same as cover")
}

func TestWrapText_Uncovered(t *testing.T) {
	lines := wrapText("hello world foo bar baz", 11)
	assert.Greater(t, len(lines), 1, "long text should be wrapped")

	emptyLines := wrapText("", 80)
	assert.Equal(t, []string{""}, emptyLines)

	zeroWidth := wrapText("hello world", 0)
	assert.Greater(t, len(zeroWidth), 0, "zero width should default to 80")
}

func TestWriteMovie_SingleTranslationUncovered(t *testing.T) {
	movie := &models.Movie{
		ID:    "SINGLE-001",
		Title: "Single Translation",
		Translations: []models.MovieTranslation{
			{Language: "ja", SourceName: "original"},
		},
	}

	var buf bytes.Buffer
	WriteMovie(&buf, movie, nil)
	output := buf.String()
	// Only 1 translation — should not show "Translations" (requires >1)
	assert.NotContains(t, output, "Translations")
}

func TestWriteMovie_ContentTypeIDMatchesID_OmittedUncovered(t *testing.T) {
	movie := &models.Movie{
		ID:        "ABC-001",
		ContentID: "ABC-001",
		Title:     "Same IDs",
	}

	var buf bytes.Buffer
	WriteMovie(&buf, movie, nil)
	output := buf.String()
	assert.NotContains(t, output, "ContentID", "ContentID should be omitted when same as ID")
}

func TestWriteMovie_RatingWithVotesUncovered(t *testing.T) {
	releaseDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "RV-001",
		Title:       "Rated",
		RatingScore: 9.2,
		RatingVotes: 500,
		ReleaseDate: &releaseDate,
	}

	var buf bytes.Buffer
	WriteMovie(&buf, movie, nil)
	output := buf.String()
	require.Contains(t, output, "9.2/10")
	require.Contains(t, output, "500 votes")
}
