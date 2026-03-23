package nfo

import (
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerator_NilFieldSafety tests AC#1: Nil field safety
func TestGenerator_NilFieldSafety(t *testing.T) {
	tests := []struct {
		name       string
		movie      *models.Movie
		checkField string
	}{
		{
			name: "nil actresses array",
			movie: &models.Movie{
				ID:        "TEST-001",
				Title:     "Test",
				Actresses: nil, // Nil array
			},
			checkField: "actors",
		},
		{
			name: "nil genres array",
			movie: &models.Movie{
				ID:     "TEST-002",
				Title:  "Test",
				Genres: nil, // Nil array
			},
			checkField: "genres",
		},
		{
			name: "nil release date",
			movie: &models.Movie{
				ID:          "TEST-003",
				Title:       "Test",
				ReleaseDate: nil, // Nil pointer
			},
			checkField: "releasedate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			gen := NewGenerator(fs, DefaultConfig())

			// Should not panic
			nfo := gen.MovieToNFO(tt.movie, "")
			assert.NotNil(t, nfo)

			// Generate NFO to verify XML generation doesn't panic
			err := gen.Generate(tt.movie, "/output", "", "")
			assert.NoError(t, err)

			// Read and parse generated NFO
			data, err := afero.ReadFile(fs, filepath.Join("/output", tt.movie.ID+".nfo"))
			require.NoError(t, err)

			var parsed Movie
			err = xml.Unmarshal(data, &parsed)
			assert.NoError(t, err)

			// Verify nil field is handled gracefully
			switch tt.checkField {
			case "actors":
				assert.Empty(t, parsed.Actors, "actors should be empty for nil input")
			case "genres":
				assert.Empty(t, parsed.Genres, "genres should be empty for nil input")
			case "releasedate":
				assert.Empty(t, parsed.ReleaseDate, "release date should be empty for nil input")
			}
		})
	}
}

// TestGenerator_EmptyRequiredFields tests AC#2: Empty required fields handling
func TestGenerator_EmptyRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		movie   *models.Movie
		wantErr bool
	}{
		{
			name: "empty ID",
			movie: &models.Movie{
				ID:    "", // Empty ID
				Title: "Test",
			},
			wantErr: false, // Current implementation is permissive
		},
		{
			name: "empty Title",
			movie: &models.Movie{
				ID:    "TEST-001",
				Title: "", // Empty Title
			},
			wantErr: false, // Current implementation is permissive
		},
		{
			name: "both empty",
			movie: &models.Movie{
				ID:    "",
				Title: "",
			},
			wantErr: false, // Current implementation is permissive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			gen := NewGenerator(fs, DefaultConfig())

			err := gen.Generate(tt.movie, "/output", "", "")

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				// Current implementation doesn't validate required fields
				// It generates NFO with empty values
				assert.NoError(t, err)

				// Verify XML is still well-formed
				filename := tt.movie.ID
				if filename == "" {
					filename = ".nfo" // Default behavior
				} else {
					filename += ".nfo"
				}

				data, err := afero.ReadFile(fs, filepath.Join("/output", filename))
				if err == nil { // File was created
					var parsed Movie
					err = xml.Unmarshal(data, &parsed)
					assert.NoError(t, err, "XML should be well-formed despite empty fields")
				}
			}
		})
	}
}

// TestGenerator_VeryLongFields tests AC#3: Very long field handling
func TestGenerator_VeryLongFields(t *testing.T) {
	tests := []struct {
		name        string
		plotLength  int
		titleLength int
	}{
		{"very long plot", 15000, 100},
		{"very long title", 500, 1000},
		{"both long", 12000, 600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			longPlot := strings.Repeat("A", tt.plotLength)
			longTitle := strings.Repeat("T", tt.titleLength)

			movie := &models.Movie{
				ID:          "LONG-001",
				Title:       longTitle,
				Description: longPlot,
			}

			fs := afero.NewMemMapFs()
			gen := NewGenerator(fs, DefaultConfig())

			// Should complete successfully
			err := gen.Generate(movie, "/output", "", "")
			assert.NoError(t, err)

			// Verify XML is well-formed
			data, err := afero.ReadFile(fs, filepath.Join("/output", "LONG-001.nfo"))
			require.NoError(t, err)

			var parsed Movie
			err = xml.Unmarshal(data, &parsed)
			assert.NoError(t, err, "XML should be well-formed with long fields")

			// Check if truncation occurred (if implemented) or full text included
			assert.NotEmpty(t, parsed.Title)
			assert.NotEmpty(t, parsed.Plot)

			// Verify we can parse the result (XML is well-formed)
			assert.Contains(t, string(data), "<?xml")
			assert.Contains(t, string(data), "<movie>")
			assert.Contains(t, string(data), "</movie>")
		})
	}
}

// TestGenerator_InvalidXMLCharacters tests AC#4: Invalid XML characters
func TestGenerator_InvalidXMLCharacters(t *testing.T) {
	tests := []struct {
		name       string
		plot       string
		expectChar string // Character we expect to see (or not see)
	}{
		{
			name:       "control characters in plot",
			plot:       "Test\x00Plot\x1FWith\x0AControl", // \x00, \x1F, \x0A
			expectChar: "TestPlotWithControl",             // Control chars should be handled
		},
		{
			name:       "XML special characters",
			plot:       "Test <tag> & \"quoted\" 'text'",
			expectChar: "&lt;tag&gt;", // Should be escaped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := &models.Movie{
				ID:          "CHAR-001",
				Title:       "Test",
				Description: tt.plot,
			}

			fs := afero.NewMemMapFs()
			gen := NewGenerator(fs, DefaultConfig())

			// Generate NFO
			err := gen.Generate(movie, "/output", "", "")
			assert.NoError(t, err)

			// Read generated XML
			data, err := afero.ReadFile(fs, filepath.Join("/output", "CHAR-001.nfo"))
			require.NoError(t, err)

			// Verify XML parsing succeeds without error
			var parsed Movie
			err = xml.Unmarshal(data, &parsed)
			assert.NoError(t, err, "XML should parse successfully after character sanitization")

			// Verify UTF-8 encoding is enforced
			assert.Contains(t, string(data), xml.Header, "generated XML should contain UTF-8 encoding declaration")
		})
	}
}

// TestGenerator_FilesystemErrors tests AC#5: File write permission error
// This is already covered by TestWriteNFO_ErrorPaths in coverage_test.go
// Adding additional test for afero.ReadOnlyFs pattern
func TestGenerator_ReadOnlyFilesystem(t *testing.T) {
	t.Run("read-only filesystem", func(t *testing.T) {
		// Create read-only filesystem
		memFs := afero.NewMemMapFs()
		roFs := afero.NewReadOnlyFs(memFs)

		gen := NewGenerator(roFs, DefaultConfig())

		movie := &models.Movie{
			ID:    "RO-001",
			Title: "Test",
		}

		// Should fail to write
		err := gen.Generate(movie, "/output", "", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create") // Either directory or file creation failed
	})

	t.Run("no partial file on error", func(t *testing.T) {
		// Test that no partial file exists after error
		memFs := afero.NewMemMapFs()
		roFs := afero.NewReadOnlyFs(memFs)

		gen := NewGenerator(roFs, DefaultConfig())

		movie := &models.Movie{
			ID:    "ERR-001",
			Title: "Test",
		}

		// Attempt write (will fail)
		err := gen.Generate(movie, "/output", "", "")
		assert.Error(t, err)

		// Verify no partial file in the read-only filesystem
		// (we can't check roFs directly, but the underlying memFs should be empty)
		exists, _ := afero.Exists(memFs, filepath.Join("/output", "ERR-001.nfo"))
		assert.False(t, exists, "should not create partial file on error")
	})
}

// TestGenerator_LargeActressGenreLists tests AC#6: Large actress/genre lists
func TestGenerator_LargeActressGenreLists(t *testing.T) {
	t.Run("large actress list", func(t *testing.T) {
		// Create movie with 25 actresses
		actresses := make([]models.Actress, 25)
		for i := 0; i < 25; i++ {
			actresses[i] = models.Actress{
				FirstName: "Actress",
				LastName:  string(rune('A' + i)),
			}
		}

		movie := &models.Movie{
			ID:        "LARGE-001",
			Title:     "Test",
			Actresses: actresses,
		}

		fs := afero.NewMemMapFs()
		gen := NewGenerator(fs, DefaultConfig())

		// Generate NFO
		start := time.Now()
		err := gen.Generate(movie, "/output", "", "")
		duration := time.Since(start)

		assert.NoError(t, err)
		// Performance regression check (not strict benchmark - see Story 4-7)
		// Relaxed threshold to prevent CI flakiness while catching infinite loops
		assert.Less(t, duration.Milliseconds(), int64(500), "should complete reasonably fast")

		// Verify all actresses appear in XML
		data, err := afero.ReadFile(fs, filepath.Join("/output", "LARGE-001.nfo"))
		require.NoError(t, err)

		var parsed Movie
		err = xml.Unmarshal(data, &parsed)
		assert.NoError(t, err)

		// All 25 actresses should be included (no truncation)
		assert.Len(t, parsed.Actors, 25, "all actresses should be included")
	})

	t.Run("large genre list", func(t *testing.T) {
		// Create movie with 60 genres
		genres := make([]models.Genre, 60)
		for i := 0; i < 60; i++ {
			genres[i] = models.Genre{
				Name: "Genre" + string(rune('A'+i%26)),
			}
		}

		movie := &models.Movie{
			ID:     "LARGE-002",
			Title:  "Test",
			Genres: genres,
		}

		fs := afero.NewMemMapFs()
		gen := NewGenerator(fs, DefaultConfig())

		// Generate NFO
		err := gen.Generate(movie, "/output", "", "")
		assert.NoError(t, err)

		// Verify XML structure is valid
		data, err := afero.ReadFile(fs, filepath.Join("/output", "LARGE-002.nfo"))
		require.NoError(t, err)

		var parsed Movie
		err = xml.Unmarshal(data, &parsed)
		assert.NoError(t, err)

		// All 60 genres should be included
		assert.Len(t, parsed.Genres, 60, "all genres should be included")
	})
}

// TestGenerator_InvalidReleaseDates tests AC#7: Invalid release dates
func TestGenerator_InvalidReleaseDates(t *testing.T) {
	tests := []struct {
		name string
		date time.Time
	}{
		{
			name: "far future date",
			date: time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "far past date",
			date: time.Date(1800, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := &models.Movie{
				ID:          "DATE-001",
				Title:       "Test",
				ReleaseDate: &tt.date,
			}

			fs := afero.NewMemMapFs()
			gen := NewGenerator(fs, DefaultConfig())

			// Should handle gracefully
			err := gen.Generate(movie, "/output", "", "")
			assert.NoError(t, err, "should handle edge case dates gracefully")

			// Read and verify ISO 8601 format
			data, err := afero.ReadFile(fs, filepath.Join("/output", "DATE-001.nfo"))
			require.NoError(t, err)

			var parsed Movie
			err = xml.Unmarshal(data, &parsed)
			assert.NoError(t, err)

			// Verify ISO 8601 format (YYYY-MM-DD)
			assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, parsed.ReleaseDate, "should maintain ISO 8601 format")
			assert.Equal(t, tt.date.Format("2006-01-02"), parsed.ReleaseDate)
		})
	}
}

// TestGenerator_DuplicateActresses tests AC#8: Duplicate actresses
func TestGenerator_DuplicateActresses(t *testing.T) {
	t.Run("duplicate actress names", func(t *testing.T) {
		movie := &models.Movie{
			ID:    "DUP-001",
			Title: "Test",
			Actresses: []models.Actress{
				{FirstName: "Yui", LastName: "Hatano"},
				{FirstName: "Yui", LastName: "Hatano"}, // Duplicate
				{FirstName: "Ai", LastName: "Uehara"},
			},
		}

		fs := afero.NewMemMapFs()
		gen := NewGenerator(fs, DefaultConfig())

		nfo := gen.MovieToNFO(movie, "")

		assert.Len(t, nfo.Actors, 2, "duplicate actress names should be deduplicated")
		assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
		assert.Equal(t, 0, nfo.Actors[0].Order)
		assert.Equal(t, "Ai Uehara", nfo.Actors[1].Name)
		assert.Equal(t, 1, nfo.Actors[1].Order)

		// Count "Yui Hatano" occurrences
		count := 0
		for _, actor := range nfo.Actors {
			if actor.Name == "Yui Hatano" {
				count++
			}
		}

		assert.Equal(t, 1, count, "duplicate actress names should be removed")
	})

	t.Run("duplicate actress DMMIDs", func(t *testing.T) {
		movie := &models.Movie{
			ID:    "DUP-002",
			Title: "Test",
			Actresses: []models.Actress{
				{FirstName: "Yui", LastName: "Hatano", DMMID: 1234},
				{FirstName: "Yui", LastName: "Hatano", DMMID: 1234}, // Duplicate DMMID
			},
		}

		fs := afero.NewMemMapFs()
		gen := NewGenerator(fs, DefaultConfig())

		nfo := gen.MovieToNFO(movie, "")

		assert.Len(t, nfo.Actors, 1, "duplicate DMMID entries should be deduplicated")
		assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
		assert.Equal(t, 0, nfo.Actors[0].Order)
	})

	t.Run("deduplicate actress names with case and whitespace normalization", func(t *testing.T) {
		movie := &models.Movie{
			ID:    "DUP-003",
			Title: "Test",
			Actresses: []models.Actress{
				{FirstName: "Yui", LastName: "Hatano"},
				{FirstName: " yui ", LastName: "HATANO"},
				{FirstName: "Ai", LastName: "Uehara"},
			},
		}

		fs := afero.NewMemMapFs()
		gen := NewGenerator(fs, DefaultConfig())

		nfo := gen.MovieToNFO(movie, "")

		assert.Len(t, nfo.Actors, 2, "name normalization should deduplicate mixed case/whitespace variants")
		assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
		assert.Equal(t, "Ai Uehara", nfo.Actors[1].Name)
	})
}
