package nfo

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/models"
)

// TestNFOGenerationEndToEnd tests the complete NFO generation workflow
func TestNFOGenerationEndToEnd(t *testing.T) {
	// Create a realistic movie with all fields
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:            "IPX-535",
		ContentID:     "ipx00535",
		Title:         "Beautiful Day with Sakura",
		OriginalTitle: "桜と過ごす美しい一日",
		Description:   "A wonderful story about a beautiful day spent with Sakura Momo. The cinematography is exceptional and the performances are outstanding.",
		ReleaseDate:   &releaseDate,
		Runtime:       120,
		Director:      "Yamada Taro",
		Maker:         "IdeaPocket",
		Label:         "IP Premium",
		Series:        "Beautiful Days",
		Poster:        models.PosterState{CoverURL: "https://example.com/covers/ipx535.jpg"},
		TrailerURL:    "https://example.com/trailers/ipx535.mp4",
		Screenshots: []string{
			"https://example.com/screenshots/ipx535-1.jpg",
			"https://example.com/screenshots/ipx535-2.jpg",
			"https://example.com/screenshots/ipx535-3.jpg",
		},
		RatingScore: 8.7,
		RatingVotes: 250,
		Actresses: []models.Actress{
			{
				FirstName:    "Momo",
				LastName:     "Sakura",
				JapaneseName: "桜空もも",
				ThumbURL:     "https://example.com/actresses/sakura-momo.jpg",
			},
		},
		Genres: []models.Genre{
			{Name: "Beautiful Girl"},
			{Name: "Featured Actress"},
			{Name: "Digital Mosaic"},
		},
	}

	// Create temp directory for output
	tmpDir := t.TempDir()

	// Test with default config
	t.Run("Default Config", func(t *testing.T) {
		gen := NewGenerator(afero.NewOsFs(), defaultConfig())

		err := gen.Generate(context.Background(), movie, tmpDir, "", "", nil)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		// Verify file was created
		nfoPath := filepath.Join(tmpDir, "IPX-535.nfo")
		if _, err := os.Stat(nfoPath); os.IsNotExist(err) {
			t.Fatalf("NFO file was not created at %s", nfoPath)
		}

		// Read and parse the NFO
		content, err := os.ReadFile(nfoPath)
		if err != nil {
			t.Fatalf("Failed to read NFO: %v", err)
		}

		var parsed Movie
		if err := xml.Unmarshal(content, &parsed); err != nil {
			t.Fatalf("Failed to parse NFO XML: %v", err)
		}

		// Verify all fields
		verifyNFOContent(t, &parsed, movie, defaultConfig())
	})

	// Test with custom config
	t.Run("Custom Config - Japanese Names", func(t *testing.T) {
		cfg := &Config{
			FirstNameOrder:     false,
			ActressLanguageJA:  true,
			UnknownActressText: "不明",
			FilenameTemplate:   "<ID> - <TITLE>.nfo",
			IncludeFanart:      true,
			IncludeTrailer:     true,
			RatingSource:       "javinizer",
		}
		gen := NewGenerator(afero.NewOsFs(), cfg)

		tmpDir2 := t.TempDir()
		err := gen.Generate(context.Background(), movie, tmpDir2, "", "", nil)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		// Verify custom filename
		expectedName := "IPX-535 - Beautiful Day with Sakura.nfo"
		nfoPath := filepath.Join(tmpDir2, expectedName)
		if _, err := os.Stat(nfoPath); os.IsNotExist(err) {
			// List files for debugging
			files, _ := os.ReadDir(tmpDir2)
			t.Logf("Files in directory:")
			for _, f := range files {
				t.Logf("  - %s", f.Name())
			}
			t.Fatalf("NFO file was not created at %s", nfoPath)
		}

		// Parse and verify
		content, err := os.ReadFile(nfoPath)
		if err != nil {
			t.Fatalf("Failed to read NFO: %v", err)
		}

		var parsed Movie
		if err := xml.Unmarshal(content, &parsed); err != nil {
			t.Fatalf("Failed to parse NFO XML: %v", err)
		}

		// Verify Japanese name is used
		if len(parsed.Actors) > 0 {
			if parsed.Actors[0].Name != "桜空もも" {
				t.Errorf("Expected Japanese name '桜空もも', got '%s'", parsed.Actors[0].Name)
			}
		}

		// Verify rating source
		if len(parsed.Ratings.Rating) > 0 {
			if parsed.Ratings.Rating[0].Name != "javinizer" {
				t.Errorf("Expected rating source 'javinizer', got '%s'", parsed.Ratings.Rating[0].Name)
			}
		}
	})

	// Test with minimal config (no fanart, no trailer)
	t.Run("Minimal Config", func(t *testing.T) {
		cfg := &Config{
			FirstNameOrder:       true,
			ActressLanguageJA:    false,
			UnknownActressText:   "Unknown",
			FilenameTemplate:     "<ID>.nfo",
			IncludeStreamDetails: false,
			IncludeFanart:        false,
			IncludeTrailer:       false,
			RatingSource:         "themoviedb",
		}
		gen := NewGenerator(afero.NewOsFs(), cfg)

		tmpDir3 := t.TempDir()
		err := gen.Generate(context.Background(), movie, tmpDir3, "", "", nil)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		nfoPath := filepath.Join(tmpDir3, "IPX-535.nfo")
		content, err := os.ReadFile(nfoPath)
		if err != nil {
			t.Fatalf("Failed to read NFO: %v", err)
		}

		var parsed Movie
		if err := xml.Unmarshal(content, &parsed); err != nil {
			t.Fatalf("Failed to parse NFO XML: %v", err)
		}

		// Verify fanart and trailer are not included
		if parsed.Fanart != nil {
			t.Error("Fanart should not be included with IncludeFanart=false")
		}
		if parsed.Trailer != "" {
			t.Error("Trailer should not be included with IncludeTrailer=false")
		}
	})
}

// TestConfigDirectConstruction tests direct Config struct construction
func TestConfigDirectConstruction(t *testing.T) {
	nfoCfg := &Config{
		FirstNameOrder:       false,
		ActressLanguageJA:    true,
		UnknownActressText:   "不明",
		FilenameTemplate:     "<ID> [<STUDIO>].nfo",
		ActressAsTag:         true,
		IncludeStreamDetails: true,
		IncludeFanart:        false,
		IncludeTrailer:       false,
		RatingSource:         "custom",
	}

	if nfoCfg.FirstNameOrder != false {
		t.Error("FirstNameOrder not set correctly")
	}
	if nfoCfg.ActressLanguageJA != true {
		t.Error("ActressLanguageJA not set correctly")
	}
	if nfoCfg.UnknownActressText != "不明" {
		t.Errorf("UnknownActressText not set correctly: got %s", nfoCfg.UnknownActressText)
	}
	if nfoCfg.FilenameTemplate != "<ID> [<STUDIO>].nfo" {
		t.Errorf("FilenameTemplate not set correctly: got %s", nfoCfg.FilenameTemplate)
	}
	if nfoCfg.IncludeStreamDetails != true {
		t.Error("IncludeStreamDetails not set correctly")
	}
	if nfoCfg.IncludeFanart != false {
		t.Error("IncludeFanart not set correctly")
	}
	if nfoCfg.IncludeTrailer != false {
		t.Error("IncludeTrailer not set correctly")
	}
	if nfoCfg.RatingSource != "custom" {
		t.Errorf("RatingSource not set correctly: got %s", nfoCfg.RatingSource)
	}
}

// TestNFOFromMovie tests generating NFO from a Movie model
func TestNFOFromMovie(t *testing.T) {
	releaseDate := time.Date(2021, 3, 15, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Test Movie from Scraper",
		Description: "Description from scraper",
		ReleaseDate: &releaseDate,
		Runtime:     130,
		Director:    "Test Director",
		Maker:       "Test Maker",
		Label:       "Test Label",
		Series:      "Test Series",
		RatingScore: 9.2,
		RatingVotes: 150,
		Actresses: []models.Actress{
			{
				FirstName:    "Test",
				LastName:     "Actress",
				JapaneseName: "テスト女優",
				ThumbURL:     "https://example.com/test.jpg",
			},
		},
		Genres: []models.Genre{{Name: "Genre1"}, {Name: "Genre2"}},
		Poster: models.PosterState{CoverURL: "https://example.com/cover.jpg"},
		Screenshots: []string{
			"https://example.com/screenshot1.jpg",
			"https://example.com/screenshot2.jpg",
		},
		TrailerURL: "https://example.com/trailer.mp4",
	}

	gen := NewGenerator(afero.NewOsFs(), defaultConfig())
	tmpDir := t.TempDir()

	err := gen.Generate(context.Background(), movie, tmpDir, "", "", nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify file exists
	nfoPath := filepath.Join(tmpDir, "IPX-535.nfo")
	if _, err := os.Stat(nfoPath); os.IsNotExist(err) {
		t.Fatalf("NFO file was not created")
	}

	// Parse and verify
	content, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("Failed to read NFO: %v", err)
	}

	var parsed Movie
	if err := xml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("Failed to parse NFO XML: %v", err)
	}

	// Verify key fields
	if parsed.ID != "IPX-535" {
		t.Errorf("ID mismatch: got %s", parsed.ID)
	}
	if parsed.Title != "Test Movie from Scraper" {
		t.Errorf("Title mismatch: got %s", parsed.Title)
	}
	if len(parsed.Actors) != 1 {
		t.Fatalf("Expected 1 actor, got %d", len(parsed.Actors))
	}
	if parsed.Actors[0].Name != "Test Actress" {
		t.Errorf("Actor name mismatch: got %s", parsed.Actors[0].Name)
	}
}

// TestXMLFormatting verifies the XML output is properly formatted
func TestXMLFormatting(t *testing.T) {
	releaseDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "TEST-001",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
	}

	gen := NewGenerator(afero.NewOsFs(), defaultConfig())
	tmpDir := t.TempDir()

	err := gen.Generate(context.Background(), movie, tmpDir, "", "", nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Read the raw content
	nfoPath := filepath.Join(tmpDir, "TEST-001.nfo")
	content, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("Failed to read NFO: %v", err)
	}

	contentStr := string(content)

	// Verify XML declaration
	if !strings.HasPrefix(contentStr, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>") {
		t.Error("NFO should start with XML declaration")
	}

	// Verify indentation (should have indented tags)
	if !strings.Contains(contentStr, "\n  <") {
		t.Error("NFO should be properly indented")
	}

	// Verify root element
	if !strings.Contains(contentStr, "<movie>") {
		t.Error("NFO should have <movie> root element")
	}

	// Verify closing tag
	if !strings.HasSuffix(strings.TrimSpace(contentStr), "</movie>") {
		t.Error("NFO should end with </movie>")
	}
}

// verifyNFOContent is a helper to verify NFO content matches the movie
func verifyNFOContent(t *testing.T, nfo *Movie, movie *models.Movie, cfg *Config) {
	t.Helper()

	// Basic fields
	if nfo.ID != movie.ID {
		t.Errorf("ID mismatch: got %s, want %s", nfo.ID, movie.ID)
	}
	if nfo.Title != movie.Title {
		t.Errorf("Title mismatch: got %s, want %s", nfo.Title, movie.Title)
	}
	if nfo.OriginalTitle != movie.OriginalTitle {
		t.Errorf("OriginalTitle mismatch: got %s, want %s", nfo.OriginalTitle, movie.OriginalTitle)
	}
	if nfo.Plot != movie.Description {
		t.Errorf("Plot mismatch: got %s, want %s", nfo.Plot, movie.Description)
	}

	// Date fields
	if movie.ReleaseDate != nil {
		expectedDate := movie.ReleaseDate.Format("2006-01-02")
		if nfo.ReleaseDate != expectedDate {
			t.Errorf("ReleaseDate mismatch: got %s, want %s", nfo.ReleaseDate, expectedDate)
		}
		if nfo.Year != movie.ReleaseDate.Year() {
			t.Errorf("Year mismatch: got %d, want %d", nfo.Year, movie.ReleaseDate.Year())
		}
	}

	// Runtime
	if nfo.Runtime != movie.Runtime {
		t.Errorf("Runtime mismatch: got %d, want %d", nfo.Runtime, movie.Runtime)
	}

	// Production info
	if nfo.Director != movie.Director {
		t.Errorf("Director mismatch: got %s, want %s", nfo.Director, movie.Director)
	}
	if nfo.Studio != movie.Maker {
		t.Errorf("Studio mismatch: got %s, want %s", nfo.Studio, movie.Maker)
	}
	if nfo.Maker != movie.Maker {
		t.Errorf("Maker mismatch: got %s, want %s", nfo.Maker, movie.Maker)
	}
	if nfo.Label != movie.Label {
		t.Errorf("Label mismatch: got %s, want %s", nfo.Label, movie.Label)
	}
	if nfo.Set != movie.Series {
		t.Errorf("Set mismatch: got %s, want %s", nfo.Set, movie.Series)
	}

	// Rating
	if movie.RatingScore > 0 {
		if len(nfo.Ratings.Rating) == 0 {
			t.Error("Rating should be present")
		} else {
			if nfo.Ratings.Rating[0].Value != movie.RatingScore {
				t.Errorf("Rating value mismatch: got %f, want %f", nfo.Ratings.Rating[0].Value, movie.RatingScore)
			}
			if nfo.Ratings.Rating[0].Votes != movie.RatingVotes {
				t.Errorf("Rating votes mismatch: got %d, want %d", nfo.Ratings.Rating[0].Votes, movie.RatingVotes)
			}
		}
	}

	// Actresses
	if len(movie.Actresses) > 0 {
		if len(nfo.Actors) != len(movie.Actresses) {
			t.Errorf("Actors count mismatch: got %d, want %d", len(nfo.Actors), len(movie.Actresses))
		}
	}

	// Genres
	if len(movie.Genres) > 0 {
		if len(nfo.Genres) != len(movie.Genres) {
			t.Errorf("Genres count mismatch: got %d, want %d", len(nfo.Genres), len(movie.Genres))
		}
	}

	// Media
	if movie.Poster.CoverURL != "" {
		if len(nfo.Thumb) == 0 {
			t.Error("Thumb should be present when CoverURL is set")
		}
	}

	if cfg.IncludeFanart && len(movie.Screenshots) > 0 {
		if nfo.Fanart == nil {
			t.Error("Fanart should be present when screenshots exist")
		} else if len(nfo.Fanart.Thumbs) != len(movie.Screenshots) {
			t.Errorf("Fanart thumbs count mismatch: got %d, want %d", len(nfo.Fanart.Thumbs), len(movie.Screenshots))
		}
	}

	if cfg.IncludeTrailer && movie.TrailerURL != "" {
		if nfo.Trailer != movie.TrailerURL {
			t.Errorf("Trailer mismatch: got %s, want %s", nfo.Trailer, movie.TrailerURL)
		}
	}
}

// TestMultipleActresses tests NFO generation with multiple actresses
func TestMultipleActresses(t *testing.T) {
	releaseDate := time.Date(2020, 5, 20, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "TEST-MULTI",
		Title:       "Multiple Actresses Test",
		ReleaseDate: &releaseDate,
		Actresses: []models.Actress{
			{FirstName: "First", LastName: "Actress", ThumbURL: "url1"},
			{FirstName: "Second", LastName: "Actress", ThumbURL: "url2"},
			{FirstName: "Third", LastName: "Actress", ThumbURL: "url3"},
		},
	}

	gen := NewGenerator(afero.NewOsFs(), defaultConfig())
	nfo := gen.movieToNFO(context.Background(), movie, "", nil)

	if len(nfo.Actors) != 3 {
		t.Fatalf("Expected 3 actors, got %d", len(nfo.Actors))
	}

	// Verify order is preserved
	for i, actor := range nfo.Actors {
		if actor.Order != i {
			t.Errorf("Actor %d has wrong order: got %d, want %d", i, actor.Order, i)
		}
	}
}
