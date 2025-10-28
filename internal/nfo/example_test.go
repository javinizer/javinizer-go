package nfo_test

import (
	"fmt"
	"os"
	"time"

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
		Rating: &models.Rating{
			Score: 8.5,
			Votes: 100,
		},
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
	gen := nfo.NewGenerator(nfo.DefaultConfig())

	// Generate NFO file
	tmpDir := os.TempDir()
	err := gen.Generate(movie, tmpDir)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("NFO generated successfully")
	// Output: NFO generated successfully
}

// ExampleGenerator_MovieToNFO demonstrates converting a Movie to NFO structure
func ExampleGenerator_MovieToNFO() {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Beautiful Day",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Maker:       "IdeaPocket",
	}

	gen := nfo.NewGenerator(nfo.DefaultConfig())
	nfoMovie := gen.MovieToNFO(movie)

	fmt.Printf("ID: %s\n", nfoMovie.ID)
	fmt.Printf("Title: %s\n", nfoMovie.Title)
	fmt.Printf("Year: %d\n", nfoMovie.Year)
	fmt.Printf("Runtime: %d\n", nfoMovie.Runtime)
	fmt.Printf("Studio: %s\n", nfoMovie.Studio)

	// Output:
	// ID: IPX-535
	// Title: Beautiful Day
	// Year: 2020
	// Runtime: 120
	// Studio: IdeaPocket
}

// ExampleConfigFromAppConfig demonstrates config conversion
func ExampleConfigFromAppConfig() {
	// Application config would typically come from config.yaml
	appCfg := &struct {
		FilenameTemplate     string
		FirstNameOrder       bool
		ActressLanguageJA    bool
		UnknownActressText   string
		IncludeFanart        bool
		IncludeTrailer       bool
		RatingSource         string
		IncludeStreamDetails bool
	}{
		FilenameTemplate:     "<ID>.nfo",
		FirstNameOrder:       true,
		ActressLanguageJA:    false,
		UnknownActressText:   "Unknown",
		IncludeFanart:        true,
		IncludeTrailer:       true,
		RatingSource:         "themoviedb",
		IncludeStreamDetails: false,
	}

	fmt.Printf("Filename template: %s\n", appCfg.FilenameTemplate)
	fmt.Printf("Use Japanese names: %v\n", appCfg.ActressLanguageJA)
	fmt.Printf("Include fanart: %v\n", appCfg.IncludeFanart)

	// Output:
	// Filename template: <ID>.nfo
	// Use Japanese names: false
	// Include fanart: true
}
