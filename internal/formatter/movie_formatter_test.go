package formatter_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/formatter"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestWriteMovie(t *testing.T) {
	releaseDate := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-123",
		ContentID:   "ipx00123",
		Title:       "Sample Movie",
		Description: "This is a long enough description to verify that the description section is printed separately from the metadata table.",
		ReleaseDate: &releaseDate,
		Runtime:     125,
		Director:    "Director Test",
		Maker:       "Maker Test",
		Label:       "Label Test",
		Series:      "Series Test",
		RatingScore: 8.7,
		RatingVotes: 210,
		Poster:      models.PosterState{CoverURL: "https://images.example.com/cover.jpg", PosterURL: "https://images.example.com/poster.jpg"},
		TrailerURL:  "https://images.example.com/trailer.mp4",
		Screenshots: []string{
			"https://images.example.com/shot1.jpg",
			"https://images.example.com/shot2.jpg",
		},
		Actresses: []models.Actress{{
			DMMID:        1234,
			FirstName:    "Jane",
			LastName:     "Doe",
			JapaneseName: "花子",
			ThumbURL:     "https://images.example.com/thumb.jpg",
		}},
		Genres:       []models.Genre{{Name: "Drama"}, {Name: "Romance"}},
		Translations: []models.MovieTranslation{{Language: "en", SourceName: "r18dev"}, {Language: "zh", SourceName: "javdb"}},
		SourceName:   "r18dev",
	}
	results := []*models.ScraperResult{{Source: "r18dev", SourceURL: "https://r18dev.example.com/IPX-123"}}

	var buf bytes.Buffer
	formatter.WriteMovie(&buf, movie, results)
	output := buf.String()

	checks := []string{
		"ID",
		"ContentID",
		"Title",
		"ReleaseDate",
		"Runtime",
		"Director",
		"Maker",
		"Label",
		"Series",
		"Rating",
		"Actresses (1)",
		"Doe Jane (花子) - ID: 1234",
		"Thumb: https://images.example.com/thumb.jpg",
		"Genres",
		"Drama, Romance",
		"Translations",
		"English (r18dev), Chinese (javdb)",
		"Sources",
		"Source URLs:",
		"r18dev       : https://r18dev.example.com/IPX-123",
		"Media URLs:",
		"Cover URL    : https://images.example.com/cover.jpg",
		"Poster URL   : https://images.example.com/poster.jpg",
		"Trailer URL  : https://images.example.com/trailer.mp4",
		"Screenshots  : 2 total",
		"Description:",
		"the description section is printed separately",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Fatalf("expected output to contain %q\nfull output:\n%s", check, output)
		}
	}
}

func TestWriteMovie_Minimal(t *testing.T) {
	movie := &models.Movie{ID: "MIN-001", Title: "Minimal"}

	var buf bytes.Buffer
	formatter.WriteMovie(&buf, movie, nil)
	output := buf.String()

	if !strings.Contains(output, "ID") || !strings.Contains(output, "Minimal") {
		t.Fatalf("unexpected minimal output:\n%s", output)
	}
	for _, absent := range []string{"Source URLs:", "Media URLs:", "Description:"} {
		if strings.Contains(output, absent) {
			t.Fatalf("did not expect %q in minimal output:\n%s", absent, output)
		}
	}
}

func TestWriteMovie_NilInput(t *testing.T) {
	var buf bytes.Buffer
	formatter.WriteMovie(&buf, nil, nil)
	assert.Empty(t, buf.String())
}

func TestWriteMovie_JapaneseNameOnly(t *testing.T) {
	movie := &models.Movie{
		ID:    "ABC-456",
		Title: "Test Movie",
		Actresses: []models.Actress{{
			JapaneseName: "波多野結衣",
		}},
	}

	var buf bytes.Buffer
	formatter.WriteMovie(&buf, movie, nil)
	output := buf.String()

	// The Japanese name should appear exactly once (not duplicated as "波多野結衣 (波多野結衣)")
	if strings.Count(output, "波多野結衣") != 1 {
		t.Fatalf("expected Japanese name to appear exactly once, got %d occurrences\nfull output:\n%s",
			strings.Count(output, "波多野結衣"), output)
	}
	if strings.Contains(output, "波多野結衣 (波多野結衣)") {
		t.Fatalf("Japanese name should not be duplicated in output\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "波多野結衣") {
		t.Fatalf("expected output to contain Japanese name\nfull output:\n%s", output)
	}
}

func TestWriteMovie_RatingZeroVotes(t *testing.T) {
	movie := &models.Movie{
		ID:          "DEF-789",
		Title:       "Test Movie",
		RatingScore: 8.7,
		RatingVotes: 0,
	}

	var buf bytes.Buffer
	formatter.WriteMovie(&buf, movie, nil)
	output := buf.String()

	// Should show rating without vote count (no "0 votes")
	if !strings.Contains(output, "8.7/10") {
		t.Fatalf("expected output to contain rating score\nfull output:\n%s", output)
	}
	if strings.Contains(output, "0 votes") {
		t.Fatalf("should not show '0 votes' when RatingVotes is zero\nfull output:\n%s", output)
	}
}
