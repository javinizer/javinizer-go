package nfo

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestMovieToNFO(t *testing.T) {
	gen := NewGenerator(DefaultConfig())
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	movie := &models.Movie{
		ID:            "IPX-535",
		ContentID:     "ipx00535",
		Title:         "Test Movie Title",
		OriginalTitle: "テストムービー",
		Description:   "This is a test movie description.",
		ReleaseDate:   &releaseDate,
		Runtime:       120,
		Director:      "Test Director",
		Maker:         "Test Studio",
		Label:         "Test Label",
		Series:        "Test Series",
		CoverURL:      "https://example.com/cover.jpg",
		TrailerURL:    "https://example.com/trailer.mp4",
		Screenshots: []string{
			"https://example.com/screenshot1.jpg",
			"https://example.com/screenshot2.jpg",
		},
		RatingScore: 8.5,
		RatingVotes: 100,
		Actresses: []models.Actress{
			{
				FirstName:    "Momo",
				LastName:     "Sakura",
				JapaneseName: "桜空もも",
				ThumbURL:     "https://example.com/actress1.jpg",
			},
			{
				FirstName: "Test",
				LastName:  "Actress",
				ThumbURL:  "https://example.com/actress2.jpg",
			},
		},
		Genres: []models.Genre{
			{Name: "Genre1"},
			{Name: "Genre2"},
			{Name: "Genre3"},
		},
	}

	nfo := gen.MovieToNFO(movie, "")

	// Verify basic fields
	if nfo.ID != "IPX-535" {
		t.Errorf("Expected ID 'IPX-535', got '%s'", nfo.ID)
	}
	if nfo.Title != "Test Movie Title" {
		t.Errorf("Expected Title 'Test Movie Title', got '%s'", nfo.Title)
	}
	if nfo.OriginalTitle != "テストムービー" {
		t.Errorf("Expected OriginalTitle 'テストムービー', got '%s'", nfo.OriginalTitle)
	}
	if nfo.Plot != "This is a test movie description." {
		t.Errorf("Expected Plot 'This is a test movie description.', got '%s'", nfo.Plot)
	}

	// Verify date fields
	if nfo.Year != 2020 {
		t.Errorf("Expected Year 2020, got %d", nfo.Year)
	}
	if nfo.ReleaseDate != "2020-09-13" {
		t.Errorf("Expected ReleaseDate '2020-09-13', got '%s'", nfo.ReleaseDate)
	}
	if nfo.Premiered != "2020-09-13" {
		t.Errorf("Expected Premiered '2020-09-13', got '%s'", nfo.Premiered)
	}

	// Verify runtime
	if nfo.Runtime != 120 {
		t.Errorf("Expected Runtime 120, got %d", nfo.Runtime)
	}

	// Verify production info
	if nfo.Director != "Test Director" {
		t.Errorf("Expected Director 'Test Director', got '%s'", nfo.Director)
	}
	if nfo.Studio != "Test Studio" {
		t.Errorf("Expected Studio 'Test Studio', got '%s'", nfo.Studio)
	}
	if nfo.Maker != "Test Studio" {
		t.Errorf("Expected Maker 'Test Studio', got '%s'", nfo.Maker)
	}
	if nfo.Label != "Test Label" {
		t.Errorf("Expected Label 'Test Label', got '%s'", nfo.Label)
	}
	if nfo.Set != "Test Series" {
		t.Errorf("Expected Set 'Test Series', got '%s'", nfo.Set)
	}

	// Verify unique IDs
	if len(nfo.UniqueID) != 1 {
		t.Errorf("Expected 1 UniqueID, got %d", len(nfo.UniqueID))
	} else {
		if nfo.UniqueID[0].Type != "contentid" {
			t.Errorf("Expected UniqueID type 'contentid', got '%s'", nfo.UniqueID[0].Type)
		}
		if nfo.UniqueID[0].Value != "ipx00535" {
			t.Errorf("Expected UniqueID value 'ipx00535', got '%s'", nfo.UniqueID[0].Value)
		}
		if !nfo.UniqueID[0].Default {
			t.Error("Expected UniqueID to be default")
		}
	}

	// Verify rating
	if len(nfo.Ratings.Rating) != 1 {
		t.Errorf("Expected 1 Rating, got %d", len(nfo.Ratings.Rating))
	} else {
		if nfo.Ratings.Rating[0].Value != 8.5 {
			t.Errorf("Expected Rating value 8.5, got %f", nfo.Ratings.Rating[0].Value)
		}
		if nfo.Ratings.Rating[0].Votes != 100 {
			t.Errorf("Expected Rating votes 100, got %d", nfo.Ratings.Rating[0].Votes)
		}
	}

	// Verify actresses
	if len(nfo.Actors) != 2 {
		t.Errorf("Expected 2 Actors, got %d", len(nfo.Actors))
	} else {
		if nfo.Actors[0].Name != "Momo Sakura" {
			t.Errorf("Expected Actor name 'Momo Sakura', got '%s'", nfo.Actors[0].Name)
		}
		if nfo.Actors[0].Order != 0 {
			t.Errorf("Expected Actor order 0, got %d", nfo.Actors[0].Order)
		}
		if nfo.Actors[0].Thumb != "https://example.com/actress1.jpg" {
			t.Errorf("Expected Actor thumb URL, got '%s'", nfo.Actors[0].Thumb)
		}
	}

	// Verify genres
	if len(nfo.Genres) != 3 {
		t.Errorf("Expected 3 Genres, got %d", len(nfo.Genres))
	}

	// Verify thumb
	if len(nfo.Thumb) != 1 {
		t.Errorf("Expected 1 Thumb, got %d", len(nfo.Thumb))
	} else {
		if nfo.Thumb[0].Aspect != "poster" {
			t.Errorf("Expected Thumb aspect 'poster', got '%s'", nfo.Thumb[0].Aspect)
		}
		if nfo.Thumb[0].Value != "https://example.com/cover.jpg" {
			t.Errorf("Expected Thumb value 'https://example.com/cover.jpg', got '%s'", nfo.Thumb[0].Value)
		}
	}

	// Verify fanart
	if nfo.Fanart == nil {
		t.Error("Expected Fanart to be present")
	} else if len(nfo.Fanart.Thumbs) != 2 {
		t.Errorf("Expected 2 Fanart thumbs, got %d", len(nfo.Fanart.Thumbs))
	}

	// Verify trailer
	if nfo.Trailer != "https://example.com/trailer.mp4" {
		t.Errorf("Expected Trailer 'https://example.com/trailer.mp4', got '%s'", nfo.Trailer)
	}
}

func TestActressNameFormatting(t *testing.T) {
	tests := []struct {
		name         string
		config       *Config
		actress      models.Actress
		expectedName string
	}{
		{
			name: "FirstName LastName order",
			config: &Config{
				ActorFirstNameOrder: true,
				ActorJapaneseNames:  false,
			},
			actress: models.Actress{
				FirstName: "Momo",
				LastName:  "Sakura",
			},
			expectedName: "Momo Sakura",
		},
		{
			name: "LastName FirstName order",
			config: &Config{
				ActorFirstNameOrder: false,
				ActorJapaneseNames:  false,
			},
			actress: models.Actress{
				FirstName: "Momo",
				LastName:  "Sakura",
			},
			expectedName: "Sakura Momo",
		},
		{
			name: "Japanese name preferred",
			config: &Config{
				ActorFirstNameOrder: true,
				ActorJapaneseNames:  true,
			},
			actress: models.Actress{
				FirstName:    "Momo",
				LastName:     "Sakura",
				JapaneseName: "桜空もも",
			},
			expectedName: "桜空もも",
		},
		{
			name: "Unknown actress",
			config: &Config{
				ActorFirstNameOrder: true,
				ActorJapaneseNames:  false,
				UnknownActress:      "Unknown",
			},
			actress: models.Actress{
				FirstName: "",
				LastName:  "",
			},
			expectedName: "Unknown",
		},
		{
			name: "Only first name",
			config: &Config{
				ActorFirstNameOrder: true,
				ActorJapaneseNames:  false,
			},
			actress: models.Actress{
				FirstName: "Momo",
				LastName:  "",
			},
			expectedName: "Momo",
		},
		{
			name: "Only last name",
			config: &Config{
				ActorFirstNameOrder: true,
				ActorJapaneseNames:  false,
			},
			actress: models.Actress{
				FirstName: "",
				LastName:  "Sakura",
			},
			expectedName: "Sakura",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(tt.config)
			result := gen.formatActressName(tt.actress)
			if result != tt.expectedName {
				t.Errorf("Expected '%s', got '%s'", tt.expectedName, result)
			}
		})
	}
}

func TestWriteNFO(t *testing.T) {
	gen := NewGenerator(DefaultConfig())

	// Create a test NFO structure
	nfo := &Movie{
		Title:   "Test Movie",
		ID:      "TEST-001",
		Runtime: 120,
		Year:    2020,
	}

	// Create temp directory
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test.nfo")

	// Write NFO
	err := gen.WriteNFO(nfo, outputPath)
	if err != nil {
		t.Fatalf("WriteNFO failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("NFO file was not created")
	}

	// Read and verify content
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read NFO file: %v", err)
	}

	// Verify XML structure
	var parsed Movie
	err = xml.Unmarshal(content, &parsed)
	if err != nil {
		t.Fatalf("Failed to parse NFO XML: %v", err)
	}

	if parsed.Title != "Test Movie" {
		t.Errorf("Expected Title 'Test Movie', got '%s'", parsed.Title)
	}
	if parsed.ID != "TEST-001" {
		t.Errorf("Expected ID 'TEST-001', got '%s'", parsed.ID)
	}
	if parsed.Runtime != 120 {
		t.Errorf("Expected Runtime 120, got %d", parsed.Runtime)
	}
	if parsed.Year != 2020 {
		t.Errorf("Expected Year 2020, got %d", parsed.Year)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.ActorFirstNameOrder {
		t.Error("Expected ActorFirstNameOrder to be true by default")
	}
	if cfg.ActorJapaneseNames {
		t.Error("Expected ActorJapaneseNames to be false by default")
	}
	if cfg.UnknownActress != "Unknown" {
		t.Errorf("Expected UnknownActress to be 'Unknown', got '%s'", cfg.UnknownActress)
	}
	if cfg.NFOFilenameTemplate != "<ID>.nfo" {
		t.Errorf("Expected NFOFilenameTemplate to be '<ID>.nfo', got '%s'", cfg.NFOFilenameTemplate)
	}
	if !cfg.IncludeFanart {
		t.Error("Expected IncludeFanart to be true by default")
	}
	if !cfg.IncludeTrailer {
		t.Error("Expected IncludeTrailer to be true by default")
	}
}

func TestNFOWithoutOptionalFields(t *testing.T) {
	gen := NewGenerator(DefaultConfig())

	// Movie with minimal data
	movie := &models.Movie{
		ID:    "TEST-001",
		Title: "Minimal Movie",
	}

	nfo := gen.MovieToNFO(movie, "")

	if nfo.ID != "TEST-001" {
		t.Errorf("Expected ID 'TEST-001', got '%s'", nfo.ID)
	}
	if nfo.Title != "Minimal Movie" {
		t.Errorf("Expected Title 'Minimal Movie', got '%s'", nfo.Title)
	}

	// Verify optional fields are empty/zero
	if nfo.Year != 0 {
		t.Errorf("Expected Year 0, got %d", nfo.Year)
	}
	if nfo.Runtime != 0 {
		t.Errorf("Expected Runtime 0, got %d", nfo.Runtime)
	}
	if len(nfo.Actors) != 0 {
		t.Errorf("Expected 0 Actors, got %d", len(nfo.Actors))
	}
	if len(nfo.Genres) != 0 {
		t.Errorf("Expected 0 Genres, got %d", len(nfo.Genres))
	}
}

func TestGenerateWithTemplate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NFOFilenameTemplate = "<ID> - <TITLE>.nfo"
	gen := NewGenerator(cfg)

	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
	}

	// Create temp directory
	tmpDir := t.TempDir()

	// Generate NFO (no part suffix for single file)
	err := gen.Generate(movie, tmpDir, "", "")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify file was created with correct name
	expectedPath := filepath.Join(tmpDir, "IPX-535 - Test Movie.nfo")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected file '%s' was not created", expectedPath)

		// List files in directory for debugging
		files, _ := os.ReadDir(tmpDir)
		t.Log("Files in directory:")
		for _, f := range files {
			t.Logf("  - %s", f.Name())
		}
	}
}

func TestGenerator_GenerateMultiPart(t *testing.T) {
	// Test NFO generation with part suffixes for multi-part files
	cfg := &Config{
		ActorFirstNameOrder:  true,
		ActorJapaneseNames:   false,
		UnknownActress:       "Unknown",
		NFOFilenameTemplate:  "<ID>.nfo",
		PerFile:              true, // Enable per-file NFO generation
		IncludeStreamDetails: false,
		IncludeFanart:        true,
		IncludeTrailer:       true,
		DefaultRatingSource:  "themoviedb",
	}

	gen := NewGenerator(cfg)

	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
	}

	tmpDir := t.TempDir()

	// Generate NFO for part 1
	err := gen.Generate(movie, tmpDir, "-pt1", "")
	if err != nil {
		t.Fatalf("Generate part 1 failed: %v", err)
	}

	// Generate NFO for part 2
	err = gen.Generate(movie, tmpDir, "-pt2", "")
	if err != nil {
		t.Fatalf("Generate part 2 failed: %v", err)
	}

	// Verify both files were created
	part1Path := filepath.Join(tmpDir, "IPX-535-pt1.nfo")
	if _, err := os.Stat(part1Path); os.IsNotExist(err) {
		// List files in directory for debugging
		files, _ := os.ReadDir(tmpDir)
		t.Log("Files in directory:")
		for _, f := range files {
			t.Logf("  - %s", f.Name())
		}
		t.Errorf("Expected file '%s' was not created", part1Path)
	}

	part2Path := filepath.Join(tmpDir, "IPX-535-pt2.nfo")
	if _, err := os.Stat(part2Path); os.IsNotExist(err) {
		t.Errorf("Expected file '%s' was not created", part2Path)
	}

	// Verify content is identical (both should contain same metadata)
	content1, err := os.ReadFile(part1Path)
	if err != nil {
		t.Fatalf("Failed to read part 1: %v", err)
	}

	content2, err := os.ReadFile(part2Path)
	if err != nil {
		t.Fatalf("Failed to read part 2: %v", err)
	}

	if string(content1) != string(content2) {
		t.Error("Part 1 and Part 2 NFO content should be identical")
	}
}

func TestIncludeOriginalPath(t *testing.T) {
	tests := []struct {
		name                string
		includeOriginalPath bool
		originalFileName    string
		expectedPath        string
	}{
		{
			name:                "Config enabled with filename",
			includeOriginalPath: true,
			originalFileName:    "ipx-535.mp4",
			expectedPath:        "ipx-535.mp4",
		},
		{
			name:                "Config disabled",
			includeOriginalPath: false,
			originalFileName:    "ipx-535.mp4",
			expectedPath:        "",
		},
		{
			name:                "Config enabled but empty filename",
			includeOriginalPath: true,
			originalFileName:    "",
			expectedPath:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ActorFirstNameOrder:  true,
				ActorJapaneseNames:   false,
				UnknownActress:       "Unknown",
				NFOFilenameTemplate:  "<ID>.nfo",
				IncludeOriginalPath:  tt.includeOriginalPath,
				IncludeStreamDetails: false,
				IncludeFanart:        true,
				IncludeTrailer:       true,
				DefaultRatingSource:  "themoviedb",
			}

			gen := NewGenerator(cfg)

			movie := &models.Movie{
				ID:               "IPX-535",
				Title:            "Test Movie",
				OriginalFileName: tt.originalFileName,
			}

			nfo := gen.MovieToNFO(movie, "")

			if nfo.OriginalPath != tt.expectedPath {
				t.Errorf("Expected OriginalPath '%s', got '%s'", tt.expectedPath, nfo.OriginalPath)
			}
		})
	}
}

func TestOriginalPathInXML(t *testing.T) {
	// Test that originalpath is properly included in XML output
	cfg := &Config{
		ActorFirstNameOrder:  true,
		ActorJapaneseNames:   false,
		UnknownActress:       "Unknown",
		NFOFilenameTemplate:  "<ID>.nfo",
		IncludeOriginalPath:  true,
		IncludeStreamDetails: false,
		IncludeFanart:        true,
		IncludeTrailer:       true,
		DefaultRatingSource:  "themoviedb",
	}

	gen := NewGenerator(cfg)

	movie := &models.Movie{
		ID:               "IPX-535",
		Title:            "Test Movie",
		OriginalFileName: "ipx-535-original.mp4",
	}

	tmpDir := t.TempDir()

	err := gen.Generate(movie, tmpDir, "", "")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Read the generated NFO file
	nfoPath := filepath.Join(tmpDir, "IPX-535.nfo")
	content, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("Failed to read NFO file: %v", err)
	}

	// Verify XML contains originalpath
	var parsed Movie
	err = xml.Unmarshal(content, &parsed)
	if err != nil {
		t.Fatalf("Failed to parse NFO XML: %v", err)
	}

	if parsed.OriginalPath != "ipx-535-original.mp4" {
		t.Errorf("Expected OriginalPath 'ipx-535-original.mp4' in XML, got '%s'", parsed.OriginalPath)
	}
}

func TestActressAsTag(t *testing.T) {
	tests := []struct {
		name          string
		actressAsTag  bool
		actresses     []models.Actress
		existingTags  []string
		expectedTags  []string
		firstNameOrder bool
	}{
		{
			name:         "Config enabled with actresses",
			actressAsTag: true,
			actresses: []models.Actress{
				{FirstName: "Momo", LastName: "Sakura"},
				{FirstName: "Test", LastName: "Actress"},
			},
			existingTags:   []string{},
			expectedTags:   []string{"Momo Sakura", "Test Actress"},
			firstNameOrder: true,
		},
		{
			name:         "Config disabled",
			actressAsTag: false,
			actresses: []models.Actress{
				{FirstName: "Momo", LastName: "Sakura"},
			},
			existingTags:   []string{},
			expectedTags:   []string{},
			firstNameOrder: true,
		},
		{
			name:         "Deduplication - actress already in tags",
			actressAsTag: true,
			actresses: []models.Actress{
				{FirstName: "Momo", LastName: "Sakura"},
				{FirstName: "Test", LastName: "Actress"},
			},
			existingTags:   []string{},
			expectedTags:   []string{"Momo Sakura", "Test Actress"},
			firstNameOrder: true,
		},
		{
			name:           "Empty actress list",
			actressAsTag:   true,
			actresses:      []models.Actress{},
			existingTags:   []string{},
			expectedTags:   []string{},
			firstNameOrder: true,
		},
		{
			name:         "Unknown actress filtered out",
			actressAsTag: true,
			actresses: []models.Actress{
				{FirstName: "", LastName: ""},
				{FirstName: "Known", LastName: "Actress"},
			},
			existingTags:   []string{},
			expectedTags:   []string{"Known Actress"},
			firstNameOrder: true,
		},
		{
			name:         "Japanese names when enabled",
			actressAsTag: true,
			actresses: []models.Actress{
				{FirstName: "Momo", LastName: "Sakura", JapaneseName: "桜空もも"},
			},
			existingTags:   []string{},
			expectedTags:   []string{"Momo Sakura"},
			firstNameOrder: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ActorFirstNameOrder:  tt.firstNameOrder,
				ActorJapaneseNames:   false,
				UnknownActress:       "Unknown",
				NFOFilenameTemplate:  "<ID>.nfo",
				ActressAsTag:         tt.actressAsTag,
				IncludeStreamDetails: false,
				IncludeFanart:        true,
				IncludeTrailer:       true,
				DefaultRatingSource:  "themoviedb",
			}

			gen := NewGenerator(cfg)

			movie := &models.Movie{
				ID:        "IPX-535",
				Title:     "Test Movie",
				Actresses: tt.actresses,
			}

			nfo := gen.MovieToNFO(movie, "")

			if len(nfo.Tags) != len(tt.expectedTags) {
				t.Errorf("Expected %d tags, got %d tags: %v", len(tt.expectedTags), len(nfo.Tags), nfo.Tags)
			}

			// Check each expected tag is present
			for _, expectedTag := range tt.expectedTags {
				found := false
				for _, tag := range nfo.Tags {
					if tag == expectedTag {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected tag '%s' not found in tags: %v", expectedTag, nfo.Tags)
				}
			}
		})
	}
}

func TestActressAsTagInXML(t *testing.T) {
	// Test that actress names are properly included in XML as tags
	cfg := &Config{
		ActorFirstNameOrder:  true,
		ActorJapaneseNames:   false,
		UnknownActress:       "Unknown",
		NFOFilenameTemplate:  "<ID>.nfo",
		ActressAsTag:         true,
		IncludeStreamDetails: false,
		IncludeFanart:        true,
		IncludeTrailer:       true,
		DefaultRatingSource:  "themoviedb",
	}

	gen := NewGenerator(cfg)

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test Movie",
		Actresses: []models.Actress{
			{FirstName: "Momo", LastName: "Sakura"},
			{FirstName: "Test", LastName: "Actress"},
		},
	}

	tmpDir := t.TempDir()

	err := gen.Generate(movie, tmpDir, "", "")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Read the generated NFO file
	nfoPath := filepath.Join(tmpDir, "IPX-535.nfo")
	content, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("Failed to read NFO file: %v", err)
	}

	// Verify XML contains tags
	var parsed Movie
	err = xml.Unmarshal(content, &parsed)
	if err != nil {
		t.Fatalf("Failed to parse NFO XML: %v", err)
	}

	expectedTags := []string{"Momo Sakura", "Test Actress"}
	if len(parsed.Tags) != len(expectedTags) {
		t.Errorf("Expected %d tags, got %d: %v", len(expectedTags), len(parsed.Tags), parsed.Tags)
	}

	for _, expectedTag := range expectedTags {
		found := false
		for _, tag := range parsed.Tags {
			if tag == expectedTag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected tag '%s' not found in XML tags: %v", expectedTag, parsed.Tags)
		}
	}
}

func TestStaticNFOFields(t *testing.T) {
	cfg := &Config{
		ActorFirstNameOrder: true,
		DefaultRatingSource: "themoviedb",
		StaticTags:          []string{"JAV", "Japanese"},
		StaticTagline:       "Brought to you by Javinizer",
		StaticCredits:       []string{"Scraped by Javinizer", "Organized automatically"},
	}

	gen := NewGenerator(cfg)

	movie := &models.Movie{
		ID:    "IPX-001",
		Title: "Test Movie",
	}

	nfo := gen.MovieToNFO(movie, "")

	// Verify static tags were added
	assert.Contains(t, nfo.Tags, "JAV")
	assert.Contains(t, nfo.Tags, "Japanese")
	assert.Len(t, nfo.Tags, 2)

	// Verify static tagline
	assert.Equal(t, "Brought to you by Javinizer", nfo.Tagline)

	// Verify static credits
	assert.Equal(t, "Scraped by Javinizer, Organized automatically", nfo.Credits)
}

func TestStaticTagsWithActressAsTag(t *testing.T) {
	cfg := &Config{
		ActorFirstNameOrder: true,
		DefaultRatingSource: "themoviedb",
		ActressAsTag:        true,
		StaticTags:          []string{"JAV", "Collection"},
	}

	gen := NewGenerator(cfg)

	movie := &models.Movie{
		ID:    "IPX-001",
		Title: "Test Movie",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
		},
	}

	nfo := gen.MovieToNFO(movie, "")

	// Verify both actress name and static tags are present
	assert.Contains(t, nfo.Tags, "Yui Hatano")
	assert.Contains(t, nfo.Tags, "JAV")
	assert.Contains(t, nfo.Tags, "Collection")
	assert.Len(t, nfo.Tags, 3)
}

func TestStaticFieldsEmpty(t *testing.T) {
	cfg := &Config{
		ActorFirstNameOrder: true,
		DefaultRatingSource: "themoviedb",
		// No static fields configured
	}

	gen := NewGenerator(cfg)

	movie := &models.Movie{
		ID:    "IPX-001",
		Title: "Test Movie",
	}

	nfo := gen.MovieToNFO(movie, "")

	// Verify no static fields were added
	assert.Empty(t, nfo.Tags)
	assert.Empty(t, nfo.Tagline)
	assert.Empty(t, nfo.Credits)
}
