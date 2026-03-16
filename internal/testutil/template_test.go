package testutil_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/stretchr/testify/assert"
)

// Example 1: Simple Validation with Inline Data
//
// This demonstrates the table-driven pattern for simple string validation.
// Use inline data when inputs and outputs are primitives or small structs.
//
// Why this pattern:
//   - Input: string (simple primitive)
//   - Output: bool (simple primitive)
//   - Test cases: Multiple validation scenarios
//   - Data fits inline without cluttering test
func TestValidateMovieID(t *testing.T) {
	// Test function that validates movie ID format
	// Returns true if ID matches pattern, false otherwise
	validateID := func(id string) (bool, error) {
		if id == "" {
			return false, assert.AnError
		}
		// Simple validation: must contain a hyphen and be uppercase
		if len(id) < 5 || id[0] < 'A' || id[0] > 'Z' {
			return false, nil
		}
		for _, c := range id {
			if c == '-' {
				return true, nil
			}
		}
		return false, nil
	}

	// Table-driven test structure with inline data
	tests := []struct {
		name    string // Test case name for t.Run()
		id      string // Input: movie ID to validate
		want    bool   // Expected: validation result
		wantErr bool   // Expected: should error occur?
	}{
		{
			name:    "valid standard ID",
			id:      "IPX-123",
			want:    true,
			wantErr: false,
		},
		{
			name:    "valid with prefix",
			id:      "H_1234ABC-567",
			want:    true,
			wantErr: false,
		},
		{
			name:    "empty string",
			id:      "",
			want:    false,
			wantErr: true, // Empty input causes error
		},
		{
			name:    "no hyphen",
			id:      "IPX535",
			want:    false,
			wantErr: false,
		},
		{
			name:    "lowercase start",
			id:      "ipx-123",
			want:    false,
			wantErr: false,
		},
	}

	// Standard for loop with t.Run() for subtests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateID(tt.id)

			// Check error condition first (early return pattern)
			if tt.wantErr {
				assert.Error(t, err)
				return // Early return prevents checking 'got' on error path
			}

			// Success path: verify no error and output matches
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Example 2: Entity Transformation with Builder Pattern
//
// This demonstrates table-driven tests with complex domain entities using builders.
// Use builders when inputs/outputs are domain models (Movie, Actress).
//
// Why this pattern:
//   - Input: *models.Movie (complex entity with many fields)
//   - Output: string (simple, but derived from complex input)
//   - Test cases: Different movie configurations
//   - Builder provides fluent API and sensible defaults
func TestFormatMovieName(t *testing.T) {
	// Test function that formats movie name from Movie entity
	formatName := func(movie *models.Movie) string {
		if movie == nil {
			return ""
		}
		if movie.Maker != "" {
			return movie.ID + " [" + movie.Maker + "] - " + movie.Title
		}
		return movie.ID + " - " + movie.Title
	}

	// Table-driven test using builders for complex entities
	tests := []struct {
		name  string        // Test case name
		movie *models.Movie // Input: Movie entity (built with testutil.MovieBuilder)
		want  string        // Expected: formatted name
	}{
		{
			name: "movie with all fields",
			// Use builder pattern for complex entity creation
			movie: testutil.NewMovieBuilder().
				WithID("IPX-123").
				WithTitle("Test Movie").
				WithStudio("IdeaPocket").
				Build(),
			want: "IPX-123 [IdeaPocket] - Test Movie",
		},
		{
			name: "movie missing studio",
			movie: testutil.NewMovieBuilder().
				WithID("IPX-456").
				WithTitle("Another Movie").
				// No WithStudio() call - Maker will be empty
				Build(),
			want: "IPX-456 - Another Movie",
		},
		{
			name: "movie with minimal data",
			// Builder provides sensible defaults (ID="IPX-123", Title="Test Movie")
			movie: testutil.NewMovieBuilder().Build(),
			want:  "IPX-123 - Test Movie",
		},
		{
			name:  "nil movie",
			movie: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatName(tt.movie)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Example 3: Large Output Comparison with Golden Files
//
// This demonstrates table-driven tests with golden files for large text outputs.
// Use golden files when output is large text/HTML/JSON/XML.
//
// Why this pattern:
//   - Input: *models.Movie (complex entity)
//   - Output: string (large XML output)
//   - Test cases: Different movie configurations
//   - Golden files keep test clean and provide snapshot testing
//
// Note: This is a simplified example. In real tests, you would use actual
// golden files in testdata/*.golden directory.
func TestGenerateMovieSummary(t *testing.T) {
	// Test function that generates a detailed movie summary
	// In real code, this might generate NFO XML or JSON
	generateSummary := func(movie *models.Movie) (string, error) {
		if movie == nil {
			return "", assert.AnError
		}
		// Simplified summary (in real code this could be complex XML/JSON)
		summary := "Movie: " + movie.Title + "\n"
		summary += "ID: " + movie.ID + "\n"
		if movie.Maker != "" {
			summary += "Studio: " + movie.Maker + "\n"
		}
		if len(movie.Actresses) > 0 {
			summary += "Actresses:\n"
			for _, actress := range movie.Actresses {
				summary += "  - " + actress.FirstName + "\n"
			}
		}
		return summary, nil
	}

	// Table-driven test with golden file pattern
	// In production code, you would use testutil.LoadGoldenFile(t, tt.goldenFile)
	tests := []struct {
		name    string        // Test case name
		movie   *models.Movie // Input: Movie entity
		want    string        // Expected: summary text (would be golden file in real test)
		wantErr bool
	}{
		{
			name: "complete movie with all fields",
			movie: testutil.NewMovieBuilder().
				WithID("IPX-123").
				WithTitle("Complete Movie").
				WithStudio("IdeaPocket").
				WithActresses([]string{"Actress One", "Actress Two"}).
				Build(),
			// In real tests, replace this with: goldenFile: "complete_summary.txt.golden"
			// and use: expected := testutil.LoadGoldenFile(t, tt.goldenFile)
			want: "Movie: Complete Movie\n" +
				"ID: IPX-123\n" +
				"Studio: IdeaPocket\n" +
				"Actresses:\n" +
				"  - Actress One\n" +
				"  - Actress Two\n",
			wantErr: false,
		},
		{
			name: "minimal movie with required fields only",
			movie: testutil.NewMovieBuilder().
				WithID("IPX-456").
				WithTitle("Minimal Movie").
				Build(),
			want: "Movie: Minimal Movie\n" +
				"ID: IPX-456\n",
			wantErr: false,
		},
		{
			name:    "nil movie returns error",
			movie:   nil,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateSummary(tt.movie)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)

			// In real golden file tests, you would use:
			// expected := testutil.LoadGoldenFile(t, tt.goldenFile)
			// assert.Equal(t, string(expected), got)
			//
			// To create/update golden files (manual step during test development):
			// err := testutil.UpdateGoldenFile(tt.goldenFile, []byte(got))
			// require.NoError(t, err)
		})
	}
}

// Additional Pattern: Table-Driven with Custom Struct Fields
//
// Sometimes you need additional fields beyond name/input/want/wantErr.
// This example shows how to extend the test struct.
func TestComplexScenario(t *testing.T) {
	// Extended test struct with additional context fields
	tests := []struct {
		name        string // Required: test case name
		input       string // Input data
		setupMocks  bool   // Additional: whether to set up mocks
		validateAll bool   // Additional: comprehensive validation flag
		want        bool   // Expected output
		wantErr     bool   // Expected error
	}{
		{
			name:        "full validation with mocks",
			input:       "test-input",
			setupMocks:  true,
			validateAll: true,
			want:        true,
			wantErr:     false,
		},
		{
			name:        "quick validation without mocks",
			input:       "quick-test",
			setupMocks:  false,
			validateAll: false,
			want:        true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Example test logic
			got := len(tt.input) > 0
			want := tt.want

			assert.Equal(t, want, got)
		})
	}
}
