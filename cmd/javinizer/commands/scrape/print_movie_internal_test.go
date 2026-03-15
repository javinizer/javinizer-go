package scrape

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	outCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outCh <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-outCh
}

func TestPrintMovie(t *testing.T) {
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
		CoverURL:    "https://images.example.com/cover.jpg",
		PosterURL:   "https://images.example.com/poster.jpg",
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

	output := captureOutput(t, func() {
		printMovie(movie, results)
	})

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

func TestPrintMovie_Minimal(t *testing.T) {
	movie := &models.Movie{ID: "MIN-001", Title: "Minimal"}

	output := captureOutput(t, func() {
		printMovie(movie, nil)
	})

	if !strings.Contains(output, "ID") || !strings.Contains(output, "Minimal") {
		t.Fatalf("unexpected minimal output:\n%s", output)
	}
	for _, absent := range []string{"Source URLs:", "Media URLs:", "Description:"} {
		if strings.Contains(output, absent) {
			t.Fatalf("did not expect %q in minimal output:\n%s", absent, output)
		}
	}
}
