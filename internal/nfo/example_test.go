package nfo_test

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
)

// ExampleGenerator_Generate demonstrates how to generate an NFO file
func ExampleGenerator_Generate() {
	// Create a movie with metadata
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Beautiful Day",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Yamada Taro",
		Maker:       "IdeaPocket",
		Label:       "IP Premium",
		Series:      "Beautiful Days",
		RatingScore: 8.5,
		RatingVotes: 100,
		Actresses: []models.Actress{
			{
				FirstName: "Momo",
				LastName:  "Sakura",
			},
		},
		Genres: []models.Genre{
			{Name: "Beautiful Girl"},
			{Name: "Featured Actress"},
		},
	}

	// Create generator with default config
	gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{
		FirstNameOrder:     true,
		UnknownActressText: "Unknown",
		UnknownActressMode: models.UnknownActressModeSkip,
		FilenameTemplate:   "<ID>.nfo",
		IncludeFanart:      true,
		IncludeTrailer:     true,
		RatingSource:       "themoviedb",
	})

	// Generate NFO file (no part suffix for single file)
	tmpDir := os.TempDir()
	err := gen.Generate(context.Background(), movie, tmpDir, "", "", nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("NFO generated successfully")
	// Output: NFO generated successfully
}

// ExampleConfig demonstrates constructing an nfo.Config struct
func ExampleConfig() {
	cfg := &nfo.Config{
		FilenameTemplate:     "<ID>.nfo",
		FirstNameOrder:       true,
		ActressLanguageJA:    false,
		UnknownActressText:   "Unknown",
		IncludeFanart:        true,
		IncludeTrailer:       true,
		RatingSource:         "themoviedb",
		IncludeStreamDetails: false,
	}

	fmt.Printf("Filename template: %s\n", cfg.FilenameTemplate)
	fmt.Printf("Use Japanese names: %v\n", cfg.ActressLanguageJA)
	fmt.Printf("Include fanart: %v\n", cfg.IncludeFanart)

	// Output:
	// Filename template: <ID>.nfo
	// Use Japanese names: false
	// Include fanart: true
}
