package template

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContextFromMovie(t *testing.T) {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		movie *models.Movie
		want  *Context
	}{
		{
			name: "Complete movie with all fields",
			movie: &models.Movie{
				ID:            "IPX-535",
				ContentID:     "ipx00535",
				Title:         "Test Movie Title",
				OriginalTitle: "テストムービータイトル",
				ReleaseDate:   &releaseDate,
				Runtime:       120,
				Director:      "Test Director",
				Maker:         "Test Studio",
				Label:         "Test Label",
				Series:        "Test Series",
				RatingScore:   4.5,
				Description:   "Test description",
				CoverURL:      "https://example.com/cover.jpg",
				TrailerURL:    "https://example.com/trailer.mp4",
				Actresses: []models.Actress{
					{FirstName: "Sakura", LastName: "Momo"},
					{FirstName: "Yui", LastName: "Hatano"},
				},
				Genres: []models.Genre{
					{Name: "Drama"},
					{Name: "Romance"},
				},
				OriginalFileName: "original_file.mp4",
			},
			want: &Context{
				ID:               "IPX-535",
				ContentID:        "ipx00535",
				Title:            "Test Movie Title",
				OriginalTitle:    "テストムービータイトル",
				ReleaseDate:      &releaseDate,
				Runtime:          120,
				Director:         "Test Director",
				Maker:            "Test Studio",
				Label:            "Test Label",
				Series:           "Test Series",
				Rating:           4.5,
				Description:      "Test description",
				CoverURL:         "https://example.com/cover.jpg",
				TrailerURL:       "https://example.com/trailer.mp4",
				Actresses:        []string{"Momo Sakura", "Hatano Yui"},
				Genres:           []string{"Drama", "Romance"},
				FirstName:        "Sakura",
				LastName:         "Momo",
				OriginalFilename: "original_file.mp4",
			},
		},
		{
			name: "Minimal movie with only required fields",
			movie: &models.Movie{
				ID:    "ABC-123",
				Title: "Minimal Movie",
			},
			want: &Context{
				ID:    "ABC-123",
				Title: "Minimal Movie",
			},
		},
		{
			name: "Movie with no actresses",
			movie: &models.Movie{
				ID:        "IPX-001",
				Title:     "No Actress Movie",
				Actresses: []models.Actress{},
			},
			want: &Context{
				ID:    "IPX-001",
				Title: "No Actress Movie",
			},
		},
		{
			name: "Movie with single actress",
			movie: &models.Movie{
				ID:    "IPX-001",
				Title: "Single Actress",
				Actresses: []models.Actress{
					{FirstName: "Test", LastName: "Actress"},
				},
			},
			want: &Context{
				ID:        "IPX-001",
				Title:     "Single Actress",
				Actresses: []string{"Actress Test"},
				FirstName: "Test",
				LastName:  "Actress",
			},
		},
		{
			name: "Movie with Japanese actress names",
			movie: &models.Movie{
				ID:    "IPX-001",
				Title: "Japanese Names",
				Actresses: []models.Actress{
					{JapaneseName: "波多野結衣"},
					{FirstName: "Ai", LastName: "Uehara", JapaneseName: "上原亜衣"},
				},
			},
			want: &Context{
				ID:        "IPX-001",
				Title:     "Japanese Names",
				Actresses: []string{"波多野結衣", "Uehara Ai"},
				FirstName: "",
				LastName:  "",
			},
		},
		{
			name: "Movie with empty genres",
			movie: &models.Movie{
				ID:     "IPX-001",
				Title:  "No Genres",
				Genres: []models.Genre{},
			},
			want: &Context{
				ID:    "IPX-001",
				Title: "No Genres",
			},
		},
		{
			name: "Movie with zero rating",
			movie: &models.Movie{
				ID:          "IPX-001",
				Title:       "Zero Rating",
				RatingScore: 0,
			},
			want: &Context{
				ID:     "IPX-001",
				Title:  "Zero Rating",
				Rating: 0,
			},
		},
		{
			name: "Movie with nil release date",
			movie: &models.Movie{
				ID:          "IPX-001",
				Title:       "No Date",
				ReleaseDate: nil,
			},
			want: &Context{
				ID:          "IPX-001",
				Title:       "No Date",
				ReleaseDate: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewContextFromMovie(tt.movie)

			assert.Equal(t, tt.want.ID, got.ID)
			assert.Equal(t, tt.want.ContentID, got.ContentID)
			assert.Equal(t, tt.want.Title, got.Title)
			assert.Equal(t, tt.want.OriginalTitle, got.OriginalTitle)
			assert.Equal(t, tt.want.ReleaseDate, got.ReleaseDate)
			assert.Equal(t, tt.want.Runtime, got.Runtime)
			assert.Equal(t, tt.want.Director, got.Director)
			assert.Equal(t, tt.want.Maker, got.Maker)
			assert.Equal(t, tt.want.Label, got.Label)
			assert.Equal(t, tt.want.Series, got.Series)
			assert.Equal(t, tt.want.Rating, got.Rating)
			assert.Equal(t, tt.want.Description, got.Description)
			assert.Equal(t, tt.want.CoverURL, got.CoverURL)
			assert.Equal(t, tt.want.TrailerURL, got.TrailerURL)
			assert.Equal(t, tt.want.OriginalFilename, got.OriginalFilename)
			assert.Equal(t, tt.want.FirstName, got.FirstName)
			assert.Equal(t, tt.want.LastName, got.LastName)
			assert.Equal(t, tt.want.Actresses, got.Actresses)
			assert.Equal(t, tt.want.Genres, got.Genres)
		})
	}
}

func TestNewContextFromScraperResult(t *testing.T) {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		result *models.ScraperResult
		want   *Context
	}{
		{
			name: "Complete scraper result with all fields",
			result: &models.ScraperResult{
				Source:        "r18dev",
				ID:            "IPX-535",
				ContentID:     "ipx00535",
				Title:         "Test Movie Title",
				OriginalTitle: "テストムービータイトル",
				ReleaseDate:   &releaseDate,
				Runtime:       120,
				Director:      "Test Director",
				Maker:         "Test Studio",
				Label:         "Test Label",
				Series:        "Test Series",
				Rating: &models.Rating{
					Score: 4.5,
					Votes: 100,
				},
				Description: "Test description",
				CoverURL:    "https://example.com/cover.jpg",
				TrailerURL:  "https://example.com/trailer.mp4",
				Actresses: []models.ActressInfo{
					{FirstName: "Sakura", LastName: "Momo"},
					{FirstName: "Yui", LastName: "Hatano"},
				},
				Genres: []string{"Drama", "Romance"},
			},
			want: &Context{
				ID:            "IPX-535",
				ContentID:     "ipx00535",
				Title:         "Test Movie Title",
				OriginalTitle: "テストムービータイトル",
				ReleaseDate:   &releaseDate,
				Runtime:       120,
				Director:      "Test Director",
				Maker:         "Test Studio",
				Label:         "Test Label",
				Series:        "Test Series",
				Rating:        4.5,
				Description:   "Test description",
				CoverURL:      "https://example.com/cover.jpg",
				TrailerURL:    "https://example.com/trailer.mp4",
				Actresses:     []string{"Momo Sakura", "Hatano Yui"},
				Genres:        []string{"Drama", "Romance"},
				FirstName:     "Sakura",
				LastName:      "Momo",
			},
		},
		{
			name: "Minimal scraper result",
			result: &models.ScraperResult{
				Source: "r18dev",
				ID:     "ABC-123",
				Title:  "Minimal Result",
			},
			want: &Context{
				ID:    "ABC-123",
				Title: "Minimal Result",
			},
		},
		{
			name: "Scraper result with nil rating",
			result: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "No Rating",
				Rating: nil,
			},
			want: &Context{
				ID:     "IPX-001",
				Title:  "No Rating",
				Rating: 0,
			},
		},
		{
			name: "Scraper result with zero rating score",
			result: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Zero Rating",
				Rating: &models.Rating{Score: 0, Votes: 10},
			},
			want: &Context{
				ID:     "IPX-001",
				Title:  "Zero Rating",
				Rating: 0,
			},
		},
		{
			name: "Scraper result with empty actresses",
			result: &models.ScraperResult{
				Source:    "r18dev",
				ID:        "IPX-001",
				Title:     "No Actresses",
				Actresses: []models.ActressInfo{},
			},
			want: &Context{
				ID:    "IPX-001",
				Title: "No Actresses",
			},
		},
		{
			name: "Scraper result with single actress",
			result: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Single Actress",
				Actresses: []models.ActressInfo{
					{FirstName: "Test", LastName: "Actress"},
				},
			},
			want: &Context{
				ID:        "IPX-001",
				Title:     "Single Actress",
				Actresses: []string{"Actress Test"},
				FirstName: "Test",
				LastName:  "Actress",
			},
		},
		{
			name: "Scraper result with Japanese actress name",
			result: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Japanese Name",
				Actresses: []models.ActressInfo{
					{JapaneseName: "波多野結衣"},
				},
			},
			want: &Context{
				ID:        "IPX-001",
				Title:     "Japanese Name",
				Actresses: []string{"波多野結衣"},
				FirstName: "",
				LastName:  "",
			},
		},
		{
			name: "Scraper result with empty genres",
			result: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "No Genres",
				Genres: []string{},
			},
			want: &Context{
				ID:     "IPX-001",
				Title:  "No Genres",
				Genres: []string{},
			},
		},
		{
			name: "Scraper result with nil genres",
			result: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Nil Genres",
				Genres: nil,
			},
			want: &Context{
				ID:     "IPX-001",
				Title:  "Nil Genres",
				Genres: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewContextFromScraperResult(tt.result)

			assert.Equal(t, tt.want.ID, got.ID)
			assert.Equal(t, tt.want.ContentID, got.ContentID)
			assert.Equal(t, tt.want.Title, got.Title)
			assert.Equal(t, tt.want.OriginalTitle, got.OriginalTitle)
			assert.Equal(t, tt.want.ReleaseDate, got.ReleaseDate)
			assert.Equal(t, tt.want.Runtime, got.Runtime)
			assert.Equal(t, tt.want.Director, got.Director)
			assert.Equal(t, tt.want.Maker, got.Maker)
			assert.Equal(t, tt.want.Label, got.Label)
			assert.Equal(t, tt.want.Series, got.Series)
			assert.Equal(t, tt.want.Rating, got.Rating)
			assert.Equal(t, tt.want.Description, got.Description)
			assert.Equal(t, tt.want.CoverURL, got.CoverURL)
			assert.Equal(t, tt.want.TrailerURL, got.TrailerURL)
			assert.Equal(t, tt.want.FirstName, got.FirstName)
			assert.Equal(t, tt.want.LastName, got.LastName)
			assert.Equal(t, tt.want.Actresses, got.Actresses)
			assert.Equal(t, tt.want.Genres, got.Genres)
		})
	}
}

func TestClone(t *testing.T) {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		ctx  *Context
	}{
		{
			name: "Complete context with all fields",
			ctx: &Context{
				ID:               "IPX-535",
				ContentID:        "ipx00535",
				Title:            "Test Movie",
				OriginalTitle:    "テストムービー",
				ReleaseDate:      &releaseDate,
				Runtime:          120,
				Director:         "Test Director",
				Actresses:        []string{"Actress 1", "Actress 2"},
				FirstName:        "Test",
				LastName:         "Actress",
				Maker:            "Test Studio",
				Label:            "Test Label",
				Series:           "Test Series",
				Genres:           []string{"Genre 1", "Genre 2", "Genre 3"},
				OriginalFilename: "original.mp4",
				VideoFilePath:    "/path/to/video.mp4",
				Index:            5,
				Rating:           4.5,
				Description:      "Test description",
				CoverURL:         "https://example.com/cover.jpg",
				TrailerURL:       "https://example.com/trailer.mp4",
				GroupActress:     true,
			},
		},
		{
			name: "Minimal context",
			ctx: &Context{
				ID:    "ABC-123",
				Title: "Minimal",
			},
		},
		{
			name: "Context with empty slices",
			ctx: &Context{
				ID:        "IPX-001",
				Title:     "Empty Slices",
				Actresses: []string{},
				Genres:    []string{},
			},
		},
		{
			name: "Context with nil slices",
			ctx: &Context{
				ID:        "IPX-001",
				Title:     "Nil Slices",
				Actresses: nil,
				Genres:    nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clone := tt.ctx.Clone()

			// Verify all fields are equal
			assert.Equal(t, tt.ctx.ID, clone.ID)
			assert.Equal(t, tt.ctx.ContentID, clone.ContentID)
			assert.Equal(t, tt.ctx.Title, clone.Title)
			assert.Equal(t, tt.ctx.OriginalTitle, clone.OriginalTitle)
			assert.Equal(t, tt.ctx.ReleaseDate, clone.ReleaseDate)
			assert.Equal(t, tt.ctx.Runtime, clone.Runtime)
			assert.Equal(t, tt.ctx.Director, clone.Director)
			assert.Equal(t, tt.ctx.FirstName, clone.FirstName)
			assert.Equal(t, tt.ctx.LastName, clone.LastName)
			assert.Equal(t, tt.ctx.Maker, clone.Maker)
			assert.Equal(t, tt.ctx.Label, clone.Label)
			assert.Equal(t, tt.ctx.Series, clone.Series)
			assert.Equal(t, tt.ctx.OriginalFilename, clone.OriginalFilename)
			assert.Equal(t, tt.ctx.VideoFilePath, clone.VideoFilePath)
			assert.Equal(t, tt.ctx.Index, clone.Index)
			assert.Equal(t, tt.ctx.Rating, clone.Rating)
			assert.Equal(t, tt.ctx.Description, clone.Description)
			assert.Equal(t, tt.ctx.CoverURL, clone.CoverURL)
			assert.Equal(t, tt.ctx.TrailerURL, clone.TrailerURL)
			assert.Equal(t, tt.ctx.GroupActress, clone.GroupActress)

			// Verify slices are equal
			assert.Equal(t, tt.ctx.Actresses, clone.Actresses)
			assert.Equal(t, tt.ctx.Genres, clone.Genres)

			// Verify deep copy: modifying clone should not affect original
			if len(clone.Actresses) > 0 {
				originalFirst := tt.ctx.Actresses[0]
				clone.Actresses[0] = "Modified Actress"
				assert.Equal(t, originalFirst, tt.ctx.Actresses[0], "Original should not be modified")
				assert.Equal(t, "Modified Actress", clone.Actresses[0], "Clone should be modified")
			}

			if len(clone.Genres) > 0 {
				originalFirst := tt.ctx.Genres[0]
				clone.Genres[0] = "Modified Genre"
				assert.Equal(t, originalFirst, tt.ctx.Genres[0], "Original should not be modified")
				assert.Equal(t, "Modified Genre", clone.Genres[0], "Clone should be modified")
			}

			// Verify that appending to clone does not affect original
			if clone.Actresses != nil {
				originalLen := len(tt.ctx.Actresses)
				clone.Actresses = append(clone.Actresses, "New Actress")
				assert.Equal(t, originalLen, len(tt.ctx.Actresses), "Original slice length should not change")
			}

			if clone.Genres != nil {
				originalLen := len(tt.ctx.Genres)
				clone.Genres = append(clone.Genres, "New Genre")
				assert.Equal(t, originalLen, len(tt.ctx.Genres), "Original slice length should not change")
			}
		})
	}
}

func TestSanitizeFolderPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Backslash converted to underscore",
			input: "Test\\Folder\\Path",
			want:  "Test_Folder_Path",
		},
		{
			name:  "Forward slash converted to underscore",
			input: "Test/Folder/Path",
			want:  "Test_Folder_Path",
		},
		{
			name:  "Mixed slashes converted to underscores",
			input: "Test\\Folder/SubFolder\\Final",
			want:  "Test_Folder_SubFolder_Final",
		},
		{
			name:  "Colon replaced",
			input: "Test: Folder",
			want:  "Test - Folder",
		},
		{
			name:  "Asterisk removed",
			input: "Test* Folder",
			want:  "Test Folder",
		},
		{
			name:  "Question mark removed",
			input: "Test? Folder",
			want:  "Test Folder",
		},
		{
			name:  "Quotes replaced",
			input: `Test "Folder"`,
			want:  "Test 'Folder'",
		},
		{
			name:  "Angle brackets replaced",
			input: "Test<Folder>",
			want:  "Test(Folder)",
		},
		{
			name:  "Pipe replaced",
			input: "Test|Folder",
			want:  "Test-Folder",
		},
		{
			name:  "Complex path with multiple special chars",
			input: `2020\Test Studio\IPX-535: "Test Movie" <HD>`,
			want:  `2020_Test Studio_IPX-535 - 'Test Movie' (HD)`,
		},
		{
			name:  "Windows absolute path",
			input: `C:\Users\Test\Videos\Movie.mp4`,
			want:  `C -_Users_Test_Videos_Movie.mp4`,
		},
		{
			name:  "Unix absolute path",
			input: `/home/test/videos/movie.mp4`,
			want:  `_home_test_videos_movie.mp4`,
		},
		{
			name:  "Empty string",
			input: "",
			want:  "",
		},
		{
			name:  "Only special characters",
			input: `\:*?"<>|`,
			want:  `_ -'()-`,
		},
		{
			name:  "Unicode characters preserved",
			input: "テスト/フォルダ/パス",
			want:  "テスト_フォルダ_パス",
		},
		{
			name:  "Mixed English and Japanese with special chars",
			input: `Test\テスト: "Movie" 映画`,
			want:  `Test_テスト - 'Movie' 映画`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFolderPath(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeFolderPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMediaInfo(t *testing.T) {
	t.Run("Returns cached mediainfo if available", func(t *testing.T) {
		// Pre-populate cached mediainfo to verify caching behavior
		cachedInfo := &mediainfo.VideoInfo{
			Height: 1080,
			Width:  1920,
		}

		ctx := &Context{
			ID:              "TEST-001",
			cachedMediaInfo: cachedInfo,
		}

		// First call should return the cached info
		info1 := ctx.GetMediaInfo()
		require.NotNil(t, info1, "Should return cached mediainfo")
		assert.Equal(t, 1080, info1.Height)
		assert.Equal(t, 1920, info1.Width)

		// Second call should return the same cached instance (pointer equality)
		info2 := ctx.GetMediaInfo()
		require.NotNil(t, info2, "Should return cached mediainfo")
		assert.Same(t, info1, info2, "Should return the same cached instance")
		assert.Equal(t, 1080, info2.Height)
	})

	t.Run("Returns nil when VideoFilePath is empty", func(t *testing.T) {
		ctx := &Context{
			ID:            "TEST-001",
			VideoFilePath: "",
		}

		info := ctx.GetMediaInfo()
		assert.Nil(t, info, "Should return nil when VideoFilePath is empty")
	})

	t.Run("Returns nil when video file does not exist", func(t *testing.T) {
		ctx := &Context{
			ID:            "TEST-001",
			VideoFilePath: "/nonexistent/path/to/video.mp4",
		}

		info := ctx.GetMediaInfo()
		assert.Nil(t, info, "Should return nil when video file does not exist")
	})
}

func TestContextFieldPreservation(t *testing.T) {
	// Test that NewContextFromMovie preserves all fields correctly
	releaseDate := time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)

	movie := &models.Movie{
		ID:               "TEST-123",
		ContentID:        "test00123",
		Title:            "Test Title",
		OriginalTitle:    "Original Title",
		ReleaseDate:      &releaseDate,
		Runtime:          150,
		Director:         "Director Name",
		Maker:            "Studio Name",
		Label:            "Label Name",
		Series:           "Series Name",
		Description:      "Long description",
		RatingScore:      3.8,
		CoverURL:         "https://example.com/cover.jpg",
		TrailerURL:       "https://example.com/trailer.mp4",
		OriginalFileName: "test_file.mp4",
		Actresses: []models.Actress{
			{FirstName: "First", LastName: "Last"},
		},
		Genres: []models.Genre{
			{Name: "Genre1"},
		},
	}

	ctx := NewContextFromMovie(movie)

	// Verify all scalar fields
	assert.Equal(t, movie.ID, ctx.ID)
	assert.Equal(t, movie.ContentID, ctx.ContentID)
	assert.Equal(t, movie.Title, ctx.Title)
	assert.Equal(t, movie.OriginalTitle, ctx.OriginalTitle)
	assert.Equal(t, movie.ReleaseDate, ctx.ReleaseDate)
	assert.Equal(t, movie.Runtime, ctx.Runtime)
	assert.Equal(t, movie.Director, ctx.Director)
	assert.Equal(t, movie.Maker, ctx.Maker)
	assert.Equal(t, movie.Label, ctx.Label)
	assert.Equal(t, movie.Series, ctx.Series)
	assert.Equal(t, movie.Description, ctx.Description)
	assert.Equal(t, movie.RatingScore, ctx.Rating)
	assert.Equal(t, movie.CoverURL, ctx.CoverURL)
	assert.Equal(t, movie.TrailerURL, ctx.TrailerURL)
	assert.Equal(t, movie.OriginalFileName, ctx.OriginalFilename)

	// Verify transformed fields
	require.Len(t, ctx.Actresses, 1)
	assert.Equal(t, "Last First", ctx.Actresses[0])
	assert.Equal(t, "First", ctx.FirstName)
	assert.Equal(t, "Last", ctx.LastName)

	require.Len(t, ctx.Genres, 1)
	assert.Equal(t, "Genre1", ctx.Genres[0])
}

func TestContextFieldPreservationFromScraperResult(t *testing.T) {
	// Test that NewContextFromScraperResult preserves all fields correctly
	releaseDate := time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)

	result := &models.ScraperResult{
		Source:        "r18dev",
		ID:            "TEST-123",
		ContentID:     "test00123",
		Title:         "Test Title",
		OriginalTitle: "Original Title",
		ReleaseDate:   &releaseDate,
		Runtime:       150,
		Director:      "Director Name",
		Maker:         "Studio Name",
		Label:         "Label Name",
		Series:        "Series Name",
		Description:   "Long description",
		Rating:        &models.Rating{Score: 3.8, Votes: 50},
		CoverURL:      "https://example.com/cover.jpg",
		TrailerURL:    "https://example.com/trailer.mp4",
		Actresses: []models.ActressInfo{
			{FirstName: "First", LastName: "Last"},
		},
		Genres: []string{"Genre1"},
	}

	ctx := NewContextFromScraperResult(result)

	// Verify all scalar fields
	assert.Equal(t, result.ID, ctx.ID)
	assert.Equal(t, result.ContentID, ctx.ContentID)
	assert.Equal(t, result.Title, ctx.Title)
	assert.Equal(t, result.OriginalTitle, ctx.OriginalTitle)
	assert.Equal(t, result.ReleaseDate, ctx.ReleaseDate)
	assert.Equal(t, result.Runtime, ctx.Runtime)
	assert.Equal(t, result.Director, ctx.Director)
	assert.Equal(t, result.Maker, ctx.Maker)
	assert.Equal(t, result.Label, ctx.Label)
	assert.Equal(t, result.Series, ctx.Series)
	assert.Equal(t, result.Description, ctx.Description)
	assert.Equal(t, result.Rating.Score, ctx.Rating)
	assert.Equal(t, result.CoverURL, ctx.CoverURL)
	assert.Equal(t, result.TrailerURL, ctx.TrailerURL)

	// Verify transformed fields
	require.Len(t, ctx.Actresses, 1)
	assert.Equal(t, "Last First", ctx.Actresses[0])
	assert.Equal(t, "First", ctx.FirstName)
	assert.Equal(t, "Last", ctx.LastName)

	require.Len(t, ctx.Genres, 1)
	assert.Equal(t, "Genre1", ctx.Genres[0])
}
