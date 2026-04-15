package organizer

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrganizerTemplate_SimplePatterns tests basic template rendering
// Covers AC-3.5.1: Simple template patterns
func TestOrganizerTemplate_SimplePatterns(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		movieSetup     func() *models.Movie
		expectedOutput string
	}{
		{
			name:     "ID only template",
			template: "<ID>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().WithID("IPX-123").Build()
			},
			expectedOutput: "IPX-123",
		},
		{
			name:     "title only template",
			template: "<TITLE>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithTitle("Beautiful Day").
					Build()
			},
			expectedOutput: "Beautiful Day",
		},
		{
			name:     "studio only template",
			template: "<STUDIO>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithStudio("IdeaPocket").
					Build()
			},
			expectedOutput: "IdeaPocket",
		},
		{
			name:     "year only template",
			template: "<YEAR>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithReleaseDate(time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)).
					Build()
			},
			expectedOutput: "2023",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourcePath := "/temp/IPX-123.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
			require.NoError(t, err)

			cfg := &config.OutputConfig{
				FolderFormat:  tt.template,
				FileFormat:    "<ID>",
				RenameFile:    true,
				MoveToFolder:  true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg, nil)

			movie := tt.movieSetup()
			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      "IPX-123.mp4",
					Extension: ".mp4",
				},
				ID: "IPX-123",
			}

			plan, err := org.Plan(match, movie, "/movies", false)
			require.NoError(t, err)

			expectedDir := filepath.Join("/movies", tt.expectedOutput)
			assert.Equal(t, expectedDir, plan.TargetDir,
				"Template %s should render to %s", tt.template, tt.expectedOutput)
		})
	}
}

// TestOrganizerTemplate_ComplexPatterns tests complex multi-tag templates
// Covers AC-3.5.1: Complex template patterns
func TestOrganizerTemplate_ComplexPatterns(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		movieSetup     func() *models.Movie
		expectedOutput string
	}{
		{
			name:     "ID with studio and title",
			template: "<ID> [<STUDIO>] - <TITLE>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithStudio("IdeaPocket").
					WithTitle("Beautiful Day").
					Build()
			},
			expectedOutput: "IPX-123 [IdeaPocket] - Beautiful Day",
		},
		{
			name:     "ID studio title and year",
			template: "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithStudio("IdeaPocket").
					WithTitle("Test Movie").
					WithReleaseDate(time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)).
					Build()
			},
			expectedOutput: "IPX-123 [IdeaPocket] - Test Movie (2023)",
		},
		{
			name:     "title with year and studio",
			template: "<TITLE> (<YEAR>) [<STUDIO>]",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithTitle("Amazing Film").
					WithReleaseDate(time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)).
					WithStudio("Premium Studio").
					Build()
			},
			expectedOutput: "Amazing Film (2024) [Premium Studio]",
		},
		{
			name:     "studio slash year slash ID",
			template: "<STUDIO>/<YEAR>/<ID>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("SSIS-999").
					WithStudio("S1").
					WithReleaseDate(time.Date(2023, 12, 25, 0, 0, 0, 0, time.UTC)).
					Build()
			},
			expectedOutput: "S1_2023_SSIS-999", // Slashes sanitized to underscores
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
				MoveToFolder:  true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg, nil)

			movie := tt.movieSetup()
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

			expectedDir := filepath.Join("/movies", tt.expectedOutput)
			assert.Equal(t, expectedDir, plan.TargetDir,
				"Template should render correctly")
		})
	}
}

// TestOrganizerTemplate_ConditionalLogic tests IF conditional blocks
// Covers AC-3.5.1: Conditional template support
func TestOrganizerTemplate_ConditionalLogic(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		movieSetup     func() *models.Movie
		expectedOutput string
	}{
		{
			name:     "series present shows content",
			template: "<ID><IF:SERIES> - <SERIES></IF>",
			movieSetup: func() *models.Movie {
				movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
				movie.Series = "Premium Series"
				return movie
			},
			expectedOutput: "IPX-123 - Premium Series",
		},
		{
			name:     "series absent hides content",
			template: "<ID><IF:SERIES> - <SERIES></IF>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					Build()
				// No series set
			},
			expectedOutput: "IPX-123",
		},
		{
			name:     "label present shows brackets",
			template: "<ID><IF:LABEL> [<LABEL>]</IF>",
			movieSetup: func() *models.Movie {
				movie := testutil.NewMovieBuilder().WithID("SSIS-888").Build()
				movie.Label = "Gold Label"
				return movie
			},
			expectedOutput: "SSIS-888 [Gold Label]",
		},
		{
			name:     "label absent omits brackets",
			template: "<ID><IF:LABEL> [<LABEL>]</IF>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("SSIS-888").
					Build()
				// No label
			},
			expectedOutput: "SSIS-888",
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
				MoveToFolder:  true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg, nil)

			movie := tt.movieSetup()
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

			expectedDir := filepath.Join("/movies", tt.expectedOutput)
			assert.Equal(t, expectedDir, plan.TargetDir,
				"Conditional should render correctly")
		})
	}
}

// TestOrganizerTemplate_MissingFields tests handling of empty/missing data
// Covers AC-3.5.1: Missing field handling
func TestOrganizerTemplate_MissingFields(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		movieSetup     func() *models.Movie
		expectedOutput string
		description    string
	}{
		{
			name:     "missing studio shows empty",
			template: "<ID> [<STUDIO>] - <TITLE>",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithTitle("Test Movie").
					Build()
				// Studio not set
			},
			expectedOutput: "IPX-123 [] - Test Movie",
			description:    "Empty studio should leave empty brackets",
		},
		{
			name:     "missing year shows empty",
			template: "<ID> - <TITLE> (<YEAR>)",
			movieSetup: func() *models.Movie {
				return testutil.NewMovieBuilder().
					WithID("IPX-123").
					WithTitle("No Date Movie").
					Build()
				// ReleaseDate not set
			},
			expectedOutput: "IPX-123 - No Date Movie ()",
			description:    "Missing year should show empty parentheses",
		},
		{
			name:     "missing all optional fields",
			template: "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			movieSetup: func() *models.Movie {
				movie := testutil.NewMovieBuilder().
					WithID("IPX-999").
					Build()
				// Clear the default title from builder
				movie.Title = ""
				return movie
			},
			expectedOutput: "IPX-999 [] -  ()",
			description:    "All missing fields should show empty placeholders",
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
				MoveToFolder:  true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg, nil)

			movie := tt.movieSetup()
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

			expectedDir := filepath.Join("/movies", tt.expectedOutput)
			assert.Equal(t, expectedDir, plan.TargetDir, tt.description)
		})
	}
}

// TestOrganizerTemplate_SpecialCharacters tests filesystem-unsafe character handling
// Covers AC-3.5.1: Special character sanitization
func TestOrganizerTemplate_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name              string
		titleWithSpecial  string
		expectedSanitized string
		description       string
	}{
		{
			name:              "forward slash",
			titleWithSpecial:  "Love/Hate Story",
			expectedSanitized: "IPX-123 - Love_Hate Story",
			description:       "Forward slash should become underscore (folder path sanitization)",
		},
		{
			name:              "backslash",
			titleWithSpecial:  "Back\\slash Movie",
			expectedSanitized: "IPX-123 - Back_slash Movie",
			description:       "Backslash should become underscore (folder path sanitization)",
		},
		{
			name:              "colon",
			titleWithSpecial:  "Title: Subtitle",
			expectedSanitized: "IPX-123 - Title - Subtitle",
			description:       "Colon should become space-hyphen",
		},
		{
			name:              "asterisk",
			titleWithSpecial:  "Star* Movie",
			expectedSanitized: "IPX-123 - Star Movie",
			description:       "Asterisk should be removed",
		},
		{
			name:              "question mark",
			titleWithSpecial:  "Why? Movie",
			expectedSanitized: "IPX-123 - Why Movie",
			description:       "Question mark should be removed",
		},
		{
			name:              "double quotes",
			titleWithSpecial:  "The \"Best\" Film",
			expectedSanitized: "IPX-123 - The 'Best' Film",
			description:       "Double quotes should become single quotes",
		},
		{
			name:              "angle brackets",
			titleWithSpecial:  "Love <3 Movie",
			expectedSanitized: "IPX-123 - Love (3 Movie",
			description:       "Angle brackets should become parentheses",
		},
		{
			name:              "pipe character",
			titleWithSpecial:  "This|That",
			expectedSanitized: "IPX-123 - This-That",
			description:       "Pipe should become hyphen",
		},
		{
			name:              "multiple special chars",
			titleWithSpecial:  "Love/Hate: The \"Ultimate\" Story?",
			expectedSanitized: "IPX-123 - Love_Hate - The 'Ultimate' Story",
			description:       "Multiple special characters should all be sanitized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourcePath := "/temp/test.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
			require.NoError(t, err)

			cfg := &config.OutputConfig{
				FolderFormat:  "<ID> - <TITLE>",
				FileFormat:    "<ID>",
				RenameFile:    true,
				MoveToFolder:  true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg, nil)

			movie := testutil.NewMovieBuilder().
				WithID("IPX-123").
				WithTitle(tt.titleWithSpecial).
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

			expectedDir := filepath.Join("/movies", tt.expectedSanitized)
			assert.Equal(t, expectedDir, plan.TargetDir, tt.description)
		})
	}
}

// TestOrganizerTemplate_UnicodeHandling tests Japanese/CJK character preservation
// Covers AC-3.5.1: Unicode preservation
func TestOrganizerTemplate_UnicodeHandling(t *testing.T) {
	tests := []struct {
		name           string
		unicodeTitle   string
		expectedOutput string
		description    string
	}{
		{
			name:           "japanese hiragana",
			unicodeTitle:   "あいうえお",
			expectedOutput: "IPX-123 - あいうえお",
			description:    "Hiragana should be preserved",
		},
		{
			name:           "japanese katakana",
			unicodeTitle:   "カタカナ",
			expectedOutput: "IPX-123 - カタカナ",
			description:    "Katakana should be preserved",
		},
		{
			name:           "japanese kanji",
			unicodeTitle:   "美しい日",
			expectedOutput: "IPX-123 - 美しい日",
			description:    "Kanji should be preserved",
		},
		{
			name:           "mixed japanese and english",
			unicodeTitle:   "Beautiful 日本 Day",
			expectedOutput: "IPX-123 - Beautiful 日本 Day",
			description:    "Mixed Japanese/English should be preserved",
		},
		{
			name:           "chinese characters",
			unicodeTitle:   "中文标题",
			expectedOutput: "IPX-123 - 中文标题",
			description:    "Chinese characters should be preserved",
		},
		{
			name:           "korean hangul",
			unicodeTitle:   "한글제목",
			expectedOutput: "IPX-123 - 한글제목",
			description:    "Korean Hangul should be preserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourcePath := "/temp/test.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
			require.NoError(t, err)

			cfg := &config.OutputConfig{
				FolderFormat:  "<ID> - <TITLE>",
				FileFormat:    "<ID>",
				RenameFile:    true,
				MoveToFolder:  true,
				MoveSubtitles: false,
			}
			org := NewOrganizer(fs, cfg, nil)

			movie := testutil.NewMovieBuilder().
				WithID("IPX-123").
				WithTitle(tt.unicodeTitle).
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

			expectedDir := filepath.Join("/movies", tt.expectedOutput)
			assert.Equal(t, expectedDir, plan.TargetDir, tt.description)
		})
	}
}
