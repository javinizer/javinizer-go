package organizer

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrganizerTemplate_LongTitles tests title truncation for very long titles
// Covers AC-3.5.1: Very long title handling
func TestOrganizerTemplate_LongTitles(t *testing.T) {
	tests := []struct {
		name           string
		titleLength    int
		maxTitleLength int
		shouldTruncate bool
	}{
		{
			name:           "normal length title",
			titleLength:    50,
			maxTitleLength: 100,
			shouldTruncate: false,
		},
		{
			name:           "title with room for prefix",
			titleLength:    80,
			maxTitleLength: 100,
			shouldTruncate: false,
		},
		{
			name:           "exceeds max length",
			titleLength:    150,
			maxTitleLength: 100,
			shouldTruncate: true,
		},
		{
			name:           "very long title 300 chars",
			titleLength:    300,
			maxTitleLength: 80,
			shouldTruncate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourcePath := "/temp/test.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
			require.NoError(t, err)

			// Generate a long title with words
			longTitle := strings.Repeat("Beautiful Day Movie Title ", tt.titleLength/20)
			if len(longTitle) < tt.titleLength {
				longTitle += strings.Repeat("X", tt.titleLength-len(longTitle))
			}
			longTitle = longTitle[:tt.titleLength]

			cfg := &config.OutputConfig{
				FolderFormat:   "<ID> - <TITLE>",
				FileFormat:     "<ID>",
				RenameFile:     true,
				MoveSubtitles:  false,
				MaxTitleLength: tt.maxTitleLength,
			}
			org := NewOrganizer(fs, cfg)

			movie := testutil.NewMovieBuilder().
				WithID("IPX-123").
				WithTitle(longTitle).
				Build()

			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      "test.mp4",
					Extension: ".mp4",
				},
				ID: "IPX-123",
			}

			plan, err := org.Plan(match, movie, "/movies", false)
			require.NoError(t, err)

			// Extract folder name from path
			folderName := filepath.Base(plan.TargetDir)

			if tt.shouldTruncate {
				// Folder name should be shorter than the original title
				assert.Less(t, len(folderName), len("IPX-123 - "+longTitle),
					"Folder name should be truncated")
				assert.False(t, strings.HasSuffix(folderName, "."),
					"Folder name should not end with a trailing period")
			} else {
				// Should not be truncated
				expectedFolder := "IPX-123 - " + longTitle
				assert.Equal(t, expectedFolder, folderName,
					"Title should not be truncated when under max length")
			}
		})
	}
}

// TestOrganizerTemplate_CustomFunctions tests template modifier functions
// Covers AC-3.5.2: Custom function tests
func TestOrganizerTemplate_CustomFunctions(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		movieSetup     func() *testutil.MovieBuilder
		expectedOutput string
		description    string
	}{
		{
			name:     "UPPER modifier on ID",
			template: "<ID:UPPER> - <TITLE>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("ipx-123").
					WithTitle("Beautiful Day")
			},
			expectedOutput: "IPX-123 - Beautiful Day",
			description:    "UPPER modifier should convert ID to uppercase",
		},
		{
			name:     "LOWER modifier on ID",
			template: "<ID:LOWER> - <TITLE>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithTitle("Test Movie")
			},
			expectedOutput: "ipx-123 - Test Movie",
			description:    "LOWER modifier should convert ID to lowercase",
		},
		{
			name:     "date formatting YYYY-MM-DD",
			template: "<ID> - <RELEASEDATE:YYYY-MM-DD>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithReleaseDate(time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC))
			},
			expectedOutput: "IPX-123 - 2023-06-15",
			description:    "Date format should use custom pattern",
		},
		{
			name:     "date formatting YYYYMMDD",
			template: "<ID>-<RELEASEDATE:YYYYMMDD>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("SSIS-888").
					WithReleaseDate(time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC))
			},
			expectedOutput: "SSIS-888-20240105",
			description:    "Compact date format should work",
		},
		{
			name:     "actresses with custom delimiter",
			template: "<ID> - <ACTRESSES: & >",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithActresses([]string{"Momo Sakura", "Yua Mikami", "Rara Anzai"})
			},
			expectedOutput: "IPX-123 - Momo Sakura & Yua Mikami & Rara Anzai",
			description:    "Custom delimiter should join actresses with ampersand",
		},
		{
			name:     "actresses with pipe delimiter",
			template: "<ACTRESSES:|>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("TEST-001").
					WithActresses([]string{"First", "Second"})
			},
			expectedOutput: "First-Second", // Pipe sanitized to hyphen
			description:    "Pipe delimiter should be sanitized",
		},
		{
			name:     "genres with semicolon delimiter",
			template: "<GENRES:; >",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("IPX-999").
					WithGenres([]string{"Solowork", "Beautiful Girl", "Slender"})
			},
			expectedOutput: "Solowork; Beautiful Girl; Slender",
			description:    "Genres should use custom semicolon delimiter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourcePath := "/temp/test.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
			require.NoError(t, err)

			cfg := &config.OutputConfig{
				FolderFormat:  tt.template,
				FileFormat:    "<ID>",
				RenameFile:    true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg)

			movie := tt.movieSetup().Build()
			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      "test.mp4",
					Extension: ".mp4",
				},
				ID: movie.ID,
			}

			plan, err := org.Plan(match, movie, "/movies", false)
			require.NoError(t, err)

			folderName := filepath.Base(plan.TargetDir)
			assert.Equal(t, tt.expectedOutput, folderName, tt.description)
		})
	}
}

// TestOrganizerTemplate_MultipleActresses tests array formatting
// Covers AC-3.5.2: Array formatting with multiple actresses
func TestOrganizerTemplate_MultipleActresses(t *testing.T) {
	tests := []struct {
		name           string
		actresses      []string
		template       string
		expectedOutput string
	}{
		{
			name:           "single actress default delimiter",
			actresses:      []string{"Momo Sakura"},
			template:       "<ACTRESSES>",
			expectedOutput: "Momo Sakura",
		},
		{
			name:           "two actresses default delimiter",
			actresses:      []string{"Momo Sakura", "Yua Mikami"},
			template:       "<ACTRESSES>",
			expectedOutput: "Momo Sakura, Yua Mikami",
		},
		{
			name:           "three actresses default delimiter",
			actresses:      []string{"First", "Second", "Third"},
			template:       "<ACTRESSES>",
			expectedOutput: "First, Second, Third",
		},
		{
			name:           "two actresses with slash",
			actresses:      []string{"Actress One", "Actress Two"},
			template:       "<ACTRESSES: / >",
			expectedOutput: "Actress One _ Actress Two", // Slash sanitized
		},
		{
			name:           "three actresses with ampersand",
			actresses:      []string{"A", "B", "C"},
			template:       "<ACTRESSES: & >",
			expectedOutput: "A & B & C",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourcePath := "/temp/test.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
			require.NoError(t, err)

			cfg := &config.OutputConfig{
				FolderFormat:  tt.template,
				FileFormat:    "<ID>",
				RenameFile:    true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg)

			movie := testutil.NewMovieBuilder().
				WithID("TEST-001").
				WithActresses(tt.actresses).
				Build()

			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      "test.mp4",
					Extension: ".mp4",
				},
				ID: "TEST-001",
			}

			plan, err := org.Plan(match, movie, "/movies", false)
			require.NoError(t, err)

			folderName := filepath.Base(plan.TargetDir)
			assert.Equal(t, tt.expectedOutput, folderName,
				"Actresses should be formatted with correct delimiter")
		})
	}
}

// TestOrganizerTemplate_FilenameTemplates tests template rendering in file names
// Covers AC-3.5.2: Template functions in file path generation
func TestOrganizerTemplate_FilenameTemplates(t *testing.T) {
	tests := []struct {
		name           string
		folderTemplate string
		fileTemplate   string
		movieSetup     func() *testutil.MovieBuilder
		expectedFile   string
	}{
		{
			name:           "file with ID and title",
			folderTemplate: "<ID>",
			fileTemplate:   "<ID> - <TITLE>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithTitle("Movie Title")
			},
			expectedFile: "IPX-123 - Movie Title.mp4",
		},
		{
			name:           "file with uppercase ID",
			folderTemplate: "<ID>",
			fileTemplate:   "<ID:UPPER>-<TITLE>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("ssis-999").
					WithTitle("Test Movie")
			},
			expectedFile: "SSIS-999-Test Movie.mp4",
		},
		{
			name:           "file with year",
			folderTemplate: "<ID>",
			fileTemplate:   "<ID>_<YEAR>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("TEST-001").
					WithReleaseDate(time.Date(2023, 12, 25, 0, 0, 0, 0, time.UTC))
			},
			expectedFile: "TEST-001_2023.mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourcePath := "/temp/original.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
			require.NoError(t, err)

			cfg := &config.OutputConfig{
				FolderFormat:  tt.folderTemplate,
				FileFormat:    tt.fileTemplate,
				RenameFile:    true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg)

			movie := tt.movieSetup().Build()
			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      "original.mp4",
					Extension: ".mp4",
				},
				ID: movie.ID,
			}

			plan, err := org.Plan(match, movie, "/movies", false)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedFile, plan.TargetFile,
				"File name should use template correctly")
		})
	}
}

// TestOrganizerTemplate_PathLengthValidation tests max path length enforcement
// Covers AC-3.5.1: Very long title handling with path limits
func TestOrganizerTemplate_PathLengthValidation(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/temp/test.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	// Create a very long title that would exceed path limits
	veryLongTitle := strings.Repeat("This is a very long movie title with many words ", 10)

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID> - <TITLE>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MaxPathLength: 200, // Set path limit
	}
	org := NewOrganizer(fs, cfg)

	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithTitle(veryLongTitle).
		Build()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "test.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)

	// Path should be truncated to fit within limit
	assert.LessOrEqual(t, len(plan.TargetPath), 200,
		"Target path should respect MaxPathLength limit")

	// Should still be a valid path
	assert.NotEmpty(t, plan.TargetPath, "Target path should not be empty")
}

// TestOrganizerTemplate_EdgeCases tests corner cases and boundary conditions
// Covers AC-3.5.1 and AC-3.5.3: Edge case handling
func TestOrganizerTemplate_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		movieSetup  func() *testutil.MovieBuilder
		shouldError bool
		description string
	}{
		{
			name:     "empty title field",
			template: "<ID> - <TITLE>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithTitle("") // Explicitly empty
			},
			shouldError: false,
			description: "Empty title should not cause error",
		},
		{
			name:     "all fields empty except ID",
			template: "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("MINIMAL-001")
				// All optional fields missing
			},
			shouldError: false,
			description: "Missing optional fields should not error",
		},
		{
			name:     "whitespace in title",
			template: "<ID> - <TITLE>",
			movieSetup: func() *testutil.MovieBuilder {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithTitle("   Spaces   Everywhere   ")
			},
			shouldError: false,
			description: "Whitespace should be handled gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourcePath := "/temp/test.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
			require.NoError(t, err)

			cfg := &config.OutputConfig{
				FolderFormat:  tt.template,
				FileFormat:    "<ID>",
				RenameFile:    true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg)

			movie := tt.movieSetup().Build()
			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      "test.mp4",
					Extension: ".mp4",
				},
				ID: movie.ID,
			}

			plan, err := org.Plan(match, movie, "/movies", false)

			if tt.shouldError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.NotNil(t, plan, "Plan should be generated")
			}
		})
	}
}
