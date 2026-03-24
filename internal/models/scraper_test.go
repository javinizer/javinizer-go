package models

// All tests in this package are safe for parallel execution (no shared state).
// Pure validation logic with no database writes or global config modifications.
// Reference: Architecture Decision 8 (concurrent testing with -race flag)

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Embedded golden files for JSON marshaling tests
//
//go:embed testdata/scraper_result_r18dev.json.golden
var scraperResultR18DevGolden []byte

//go:embed testdata/scraper_result_dmm.json.golden
var scraperResultDMMGolden []byte

//go:embed testdata/scraper_result_minimal.json.golden
var scraperResultMinimalGolden []byte

// ScraperResultValidationError represents a validation error for test purposes
type ScraperResultValidationError struct {
	Field   string
	Message string
}

func (e *ScraperResultValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateScraperResult validates a ScraperResult struct for test purposes
// This is a test helper following the pattern from movie_test.go and actress_test.go
// Note: This is test-only validation. Actual model validation should be in Epic 3
func validateScraperResult(sr *ScraperResult) error {
	// Required fields validation
	if sr.ID == "" {
		return &ScraperResultValidationError{Field: "ID", Message: "cannot be empty"}
	}
	if sr.Title == "" {
		return &ScraperResultValidationError{Field: "Title", Message: "cannot be empty"}
	}
	if sr.Source == "" {
		return &ScraperResultValidationError{Field: "Source", Message: "cannot be empty"}
	}

	// Source field must match known scraper names
	validSources := map[string]bool{"r18dev": true, "dmm": true, "mock": true, "test": true}
	if !validSources[sr.Source] {
		return &ScraperResultValidationError{Field: "Source", Message: "invalid scraper name"}
	}

	// Validate image URLs if present
	if sr.PosterURL != "" && !strings.HasPrefix(sr.PosterURL, "http") {
		return &ScraperResultValidationError{Field: "PosterURL", Message: "must be valid HTTP/HTTPS URL or empty"}
	}
	if sr.CoverURL != "" && !strings.HasPrefix(sr.CoverURL, "http") {
		return &ScraperResultValidationError{Field: "CoverURL", Message: "must be valid HTTP/HTTPS URL or empty"}
	}

	// Runtime must be non-negative
	if sr.Runtime < 0 {
		return &ScraperResultValidationError{Field: "Runtime", Message: "cannot be negative"}
	}

	return nil
}

// ActressInfoValidationError represents a validation error for ActressInfo
type ActressInfoValidationError struct {
	Field   string
	Message string
}

func (e *ActressInfoValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateActressInfo validates an ActressInfo struct for test purposes
func validateActressInfo(ai *ActressInfo) error {
	// At least one name field must be populated
	if ai.JapaneseName == "" && ai.FirstName == "" && ai.LastName == "" {
		return &ActressInfoValidationError{Field: "Names", Message: "at least one name field required"}
	}

	// DMMID must be non-negative (0 means not set)
	if ai.DMMID < 0 {
		return &ActressInfoValidationError{Field: "DMMID", Message: "cannot be negative"}
	}

	// ThumbURL validation if present
	if ai.ThumbURL != "" && !strings.HasPrefix(ai.ThumbURL, "http") {
		return &ActressInfoValidationError{Field: "ThumbURL", Message: "must be valid HTTP/HTTPS URL or empty"}
	}

	return nil
}

// TestScraperResultCreation tests ScraperResult struct creation with various field configurations (AC-2.5.1, AC-2.5.4)
func TestScraperResultCreation(t *testing.T) {
	releaseDate := time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		builder func() *ScraperResult
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid ScraperResult with full metadata",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source:        "r18dev",
					SourceURL:     "https://r18.dev/videos/vod/movies/detail/-/id=ipx00123/",
					Language:      "en",
					ID:            "IPX-123",
					ContentID:     "ipx00123",
					Title:         "Test Movie Full",
					OriginalTitle: "テストムービー",
					Description:   "A comprehensive test movie with all fields populated",
					ReleaseDate:   &releaseDate,
					Runtime:       120,
					Director:      "Test Director",
					Maker:         "Test Studio",
					Label:         "Test Label",
					Series:        "Test Series",
					Rating:        &Rating{Score: 4.5, Votes: 123},
					Actresses: []ActressInfo{
						{DMMID: 123456, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", ThumbURL: "https://example.com/thumb1.jpg"},
						{DMMID: 789012, FirstName: "Ai", LastName: "Hoshina", JapaneseName: "星名愛", ThumbURL: "https://example.com/thumb2.jpg"},
					},
					Genres:           []string{"Drama", "Romance"},
					PosterURL:        "https://example.com/poster.jpg",
					CoverURL:         "https://example.com/cover.jpg",
					ShouldCropPoster: true,
					ScreenshotURL:    []string{"https://example.com/screenshot1.jpg", "https://example.com/screenshot2.jpg"},
					TrailerURL:       "https://example.com/trailer.mp4",
				}
			},
			wantErr: false,
		},
		{
			name: "valid ScraperResult with partial metadata",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source:      "dmm",
					ID:          "IPX-456",
					Title:       "Test Movie Partial",
					ReleaseDate: &releaseDate,
					Actresses: []ActressInfo{
						{DMMID: 111222, JapaneseName: "テスト女優"},
					},
					Genres: []string{"Action"},
				}
			},
			wantErr: false,
		},
		{
			name: "valid ScraperResult with minimal required fields",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source: "r18dev",
					ID:     "IPX-789",
					Title:  "Test Movie Minimal",
				}
			},
			wantErr: false,
		},
		{
			name: "invalid ScraperResult missing ID",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source: "r18dev",
					ID:     "",
					Title:  "Test Movie",
				}
			},
			wantErr: true,
			errMsg:  "ID: cannot be empty",
		},
		{
			name: "invalid ScraperResult missing Title",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source: "r18dev",
					ID:     "IPX-999",
					Title:  "",
				}
			},
			wantErr: true,
			errMsg:  "Title: cannot be empty",
		},
		{
			name: "invalid ScraperResult missing Source",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source: "",
					ID:     "IPX-888",
					Title:  "Test Movie",
				}
			},
			wantErr: true,
			errMsg:  "Source: cannot be empty",
		},
		{
			name: "valid ScraperResult with very long title (1000+ chars)",
			builder: func() *ScraperResult {
				longTitle := strings.Repeat("Very Long Title ", 100) // 1600 chars
				return &ScraperResult{
					Source: "r18dev",
					ID:     "IPX-LONG",
					Title:  longTitle,
				}
			},
			wantErr: false,
		},
		{
			name: "valid ScraperResult with empty Actresses array",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source:    "dmm",
					ID:        "IPX-NO-ACTRESS",
					Title:     "Movie with no actress data",
					Actresses: []ActressInfo{}, // Empty array is valid
				}
			},
			wantErr: false,
		},
		{
			name: "valid ScraperResult with nil ReleaseDate",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source:      "r18dev",
					ID:          "IPX-NO-DATE",
					Title:       "Movie with unknown release date",
					ReleaseDate: nil, // Nil date is valid (date unknown)
				}
			},
			wantErr: false,
		},
		{
			name: "valid ScraperResult with zero Runtime",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source:  "dmm",
					ID:      "IPX-NO-RUNTIME",
					Title:   "Movie with unknown runtime",
					Runtime: 0, // Zero runtime is valid (runtime unknown)
				}
			},
			wantErr: false,
		},
		{
			name: "valid ScraperResult with special characters in Title",
			builder: func() *ScraperResult {
				return &ScraperResult{
					Source: "r18dev",
					ID:     "IPX-UNICODE",
					Title:  "Test Movie 【特殊文字】 <HTML> & Symbols ♥♦♣♠",
				}
			},
			wantErr: false,
		},
		{
			name: "valid ScraperResult with multiple genres (50+)",
			builder: func() *ScraperResult {
				manyGenres := make([]string, 60)
				for i := 0; i < 60; i++ {
					manyGenres[i] = "Genre" + strings.Repeat("_", i+1)
				}
				return &ScraperResult{
					Source: "dmm",
					ID:     "IPX-MANY-GENRES",
					Title:  "Movie with many genres",
					Genres: manyGenres,
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.builder()

			// Validate ScraperResult
			err := validateScraperResult(result)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)

			// Additional field-specific assertions for valid cases
			assert.NotEmpty(t, result.Source, "Source should not be empty")
			assert.NotEmpty(t, result.ID, "ID should not be empty")
			assert.NotEmpty(t, result.Title, "Title should not be empty")
		})
	}
}

// TestScraperResultJSONMarshal tests ScraperResult JSON marshaling (AC-2.5.2)
func TestScraperResultJSONMarshal(t *testing.T) {
	releaseDate := time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		scraperResult  *ScraperResult
		expectedFields map[string]bool // Fields that must exist in JSON
	}{
		{
			name: "marshal full ScraperResult to JSON",
			scraperResult: &ScraperResult{
				Source:        "r18dev",
				SourceURL:     "https://r18.dev/test",
				Language:      "en",
				ID:            "IPX-123",
				ContentID:     "ipx00123",
				Title:         "Test Movie",
				OriginalTitle: "テストムービー",
				Description:   "Test description",
				ReleaseDate:   &releaseDate,
				Runtime:       120,
				Director:      "Test Director",
				Maker:         "Test Studio",
				Rating:        &Rating{Score: 4.5, Votes: 100},
				Actresses: []ActressInfo{
					{DMMID: 123456, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"},
				},
				Genres:        []string{"Drama", "Romance"},
				PosterURL:     "https://example.com/poster.jpg",
				CoverURL:      "https://example.com/cover.jpg",
				ScreenshotURL: []string{"https://example.com/screenshot1.jpg"},
			},
			expectedFields: map[string]bool{
				"source":       true,
				"id":           true,
				"title":        true,
				"actresses":    true,
				"genres":       true,
				"release_date": true,
				"rating":       true,
			},
		},
		{
			name: "marshal ScraperResult with nested ActressInfo array",
			scraperResult: &ScraperResult{
				Source: "dmm",
				ID:     "IPX-456",
				Title:  "Test Nested",
				Actresses: []ActressInfo{
					{DMMID: 111, FirstName: "First", LastName: "Actor", JapaneseName: "最初"},
					{DMMID: 222, FirstName: "Second", LastName: "Actor", JapaneseName: "二番目"},
					{DMMID: 333, JapaneseName: "三番目"}, // Only Japanese name
				},
			},
			expectedFields: map[string]bool{
				"actresses": true,
			},
		},
		{
			name: "empty arrays serialize as [] not null",
			scraperResult: &ScraperResult{
				Source:        "test",
				ID:            "TEST-EMPTY",
				Title:         "Empty Arrays Test",
				Actresses:     []ActressInfo{}, // Empty array
				Genres:        []string{},      // Empty array
				ScreenshotURL: []string{},      // Empty array
			},
			expectedFields: map[string]bool{
				"actresses":       true,
				"genres":          true,
				"screenshot_urls": true,
			},
		},
		{
			name: "nil ReleaseDate pointer handles correctly",
			scraperResult: &ScraperResult{
				Source:      "test",
				ID:          "TEST-NIL-DATE",
				Title:       "Nil Date Test",
				ReleaseDate: nil, // Nil pointer
			},
			expectedFields: map[string]bool{
				"release_date": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Marshal to JSON
			jsonData, err := json.Marshal(tt.scraperResult)
			require.NoError(t, err, "JSON marshaling should not error")

			// Unmarshal to map to check structure
			var jsonMap map[string]interface{}
			err = json.Unmarshal(jsonData, &jsonMap)
			require.NoError(t, err, "Unmarshaling to map should not error")

			// Verify expected fields exist
			for field := range tt.expectedFields {
				assert.Contains(t, jsonMap, field, "JSON should contain field: %s", field)
			}

			// Verify empty arrays are [] not null
			if tt.name == "empty arrays serialize as [] not null" {
				actresses := jsonMap["actresses"]
				assert.NotNil(t, actresses, "actresses should not be null")
				actressesArray, ok := actresses.([]interface{})
				assert.True(t, ok, "actresses should be an array")
				assert.Empty(t, actressesArray, "actresses array should be empty")

				genres := jsonMap["genres"]
				assert.NotNil(t, genres, "genres should not be null")
				genresArray, ok := genres.([]interface{})
				assert.True(t, ok, "genres should be an array")
				assert.Empty(t, genresArray, "genres array should be empty")
			}

			// Verify timestamp format for release_date (ISO 8601)
			if tt.scraperResult.ReleaseDate != nil {
				releaseDate, ok := jsonMap["release_date"].(string)
				assert.True(t, ok, "release_date should be a string")
				assert.Contains(t, releaseDate, "2023-05-15", "release_date should contain correct date")
				assert.Contains(t, releaseDate, "T", "release_date should be ISO 8601 format with T separator")
			}

			// Verify nested ActressInfo structure
			if len(tt.scraperResult.Actresses) > 0 {
				actresses, ok := jsonMap["actresses"].([]interface{})
				assert.True(t, ok, "actresses should be an array")
				assert.Equal(t, len(tt.scraperResult.Actresses), len(actresses), "actresses array length should match")

				if len(actresses) > 0 {
					firstActress, ok := actresses[0].(map[string]interface{})
					assert.True(t, ok, "actress should be an object")
					assert.Contains(t, firstActress, "dmm_id", "actress should have dmm_id field")
				}
			}
		})
	}
}

// TestScraperResultJSONUnmarshal tests ScraperResult JSON unmarshaling (AC-2.5.2)
func TestScraperResultJSONUnmarshal(t *testing.T) {
	tests := []struct {
		name       string
		jsonString string
		wantErr    bool
		validate   func(*testing.T, *ScraperResult)
	}{
		{
			name:       "unmarshal valid JSON to ScraperResult",
			jsonString: `{"source":"r18dev","id":"IPX-123","title":"Test Movie","actresses":[{"dmm_id":123456,"japanese_name":"波多野結衣"}],"genres":["Drama"]}`,
			wantErr:    false,
			validate: func(t *testing.T, sr *ScraperResult) {
				assert.Equal(t, "r18dev", sr.Source)
				assert.Equal(t, "IPX-123", sr.ID)
				assert.Equal(t, "Test Movie", sr.Title)
				assert.Len(t, sr.Actresses, 1)
				assert.Equal(t, 123456, sr.Actresses[0].DMMID)
				assert.Len(t, sr.Genres, 1)
			},
		},
		{
			name:       "unmarshal with missing fields uses zero values",
			jsonString: `{"source":"dmm","id":"IPX-456","title":"Minimal"}`,
			wantErr:    false,
			validate: func(t *testing.T, sr *ScraperResult) {
				assert.Equal(t, "dmm", sr.Source)
				assert.Equal(t, "IPX-456", sr.ID)
				assert.Equal(t, "Minimal", sr.Title)
				assert.Nil(t, sr.ReleaseDate, "missing ReleaseDate should be nil")
				assert.Equal(t, 0, sr.Runtime, "missing Runtime should be zero")
				assert.Empty(t, sr.Actresses, "missing actresses should be empty array")
			},
		},
		{
			name:       "unmarshal invalid JSON returns error",
			jsonString: `{"source":"invalid" "missing comma"}`,
			wantErr:    true,
			validate:   nil,
		},
		{
			name:       "unmarshal with extra JSON fields ignored gracefully",
			jsonString: `{"source":"r18dev","id":"IPX-789","title":"Extra Fields","extra_field":"should_be_ignored","another_extra":123}`,
			wantErr:    false,
			validate: func(t *testing.T, sr *ScraperResult) {
				assert.Equal(t, "r18dev", sr.Source)
				assert.Equal(t, "IPX-789", sr.ID)
				assert.Equal(t, "Extra Fields", sr.Title)
				// Extra fields are ignored, struct should be valid
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var result ScraperResult
			err := json.Unmarshal([]byte(tt.jsonString), &result)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, &result)
			}
		})
	}
}

// TestScraperResultGoldenFiles validates serialization against golden files (AC-2.5.2)
func TestScraperResultGoldenFiles(t *testing.T) {
	tests := []struct {
		name       string
		goldenData []byte
		validate   func(*testing.T, *ScraperResult)
	}{
		{
			name:       "r18dev golden file",
			goldenData: scraperResultR18DevGolden,
			validate: func(t *testing.T, sr *ScraperResult) {
				assert.Equal(t, "r18dev", sr.Source)
				assert.Equal(t, "IPX-123", sr.ID)
				assert.NotEmpty(t, sr.Title)
				assert.NotNil(t, sr.ReleaseDate)
				assert.Greater(t, len(sr.Actresses), 0, "r18dev should have actresses")
			},
		},
		{
			name:       "dmm golden file",
			goldenData: scraperResultDMMGolden,
			validate: func(t *testing.T, sr *ScraperResult) {
				assert.Equal(t, "dmm", sr.Source)
				assert.Equal(t, "IPX-123", sr.ID)
				assert.NotEmpty(t, sr.Title)
			},
		},
		{
			name:       "minimal golden file",
			goldenData: scraperResultMinimalGolden,
			validate: func(t *testing.T, sr *ScraperResult) {
				assert.Equal(t, "test", sr.Source)
				assert.Equal(t, "IPX-123", sr.ID)
				assert.Equal(t, "Minimal Test Movie", sr.Title)
				assert.Nil(t, sr.ReleaseDate, "minimal should have nil release date")
				assert.Empty(t, sr.Actresses, "minimal should have empty actresses array")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var result ScraperResult
			err := json.Unmarshal(tt.goldenData, &result)
			require.NoError(t, err, "Golden file should unmarshal successfully")

			if tt.validate != nil {
				tt.validate(t, &result)
			}

			// Marshal back and verify it's valid JSON
			jsonData, err := json.Marshal(&result)
			require.NoError(t, err, "Remarshaling should work")
			assert.NotEmpty(t, jsonData)
		})
	}
}

// TestScraperInterfaceCompliance tests Scraper interface contract (AC-2.5.3)
func TestScraperInterfaceCompliance(t *testing.T) {
	// Import the mocks package for testing
	// This test validates the Scraper interface using mockery-generated MockScraper

	// Note: This is a demonstration of interface compliance testing
	// The actual MockScraper is in internal/mocks/Scraper.go

	t.Run("interface contract validation", func(t *testing.T) {
		t.Parallel()
		// Verify the Scraper interface exists and has correct method signatures
		var _ Scraper = (*mockScraperForTest)(nil)
	})
}

// mockScraperForTest is a minimal test implementation to verify interface compliance
// This demonstrates that any struct with Name(), Search(), GetURL(), IsEnabled() satisfies the interface
type mockScraperForTest struct{}

func (m *mockScraperForTest) Name() string                             { return "mock" }
func (m *mockScraperForTest) Search(id string) (*ScraperResult, error) { return nil, nil }
func (m *mockScraperForTest) GetURL(id string) (string, error)         { return "", nil }
func (m *mockScraperForTest) IsEnabled() bool                          { return true }

// TestActressInfoValidation tests ActressInfo struct validation (AC-2.5.5)
func TestActressInfoValidation(t *testing.T) {
	tests := []struct {
		name    string
		actress *ActressInfo
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid ActressInfo with all fields",
			actress: &ActressInfo{
				DMMID:        123456,
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
				ThumbURL:     "https://example.com/thumb.jpg",
			},
			wantErr: false,
		},
		{
			name: "valid ActressInfo with JapaneseName only",
			actress: &ActressInfo{
				DMMID:        789012,
				JapaneseName: "テスト女優",
				ThumbURL:     "",
			},
			wantErr: false,
		},
		{
			name: "valid ActressInfo with FirstName/LastName only",
			actress: &ActressInfo{
				DMMID:     111222,
				FirstName: "Test",
				LastName:  "Actress",
			},
			wantErr: false,
		},
		{
			name: "invalid ActressInfo all name fields empty",
			actress: &ActressInfo{
				DMMID:        333444,
				FirstName:    "",
				LastName:     "",
				JapaneseName: "",
			},
			wantErr: true,
			errMsg:  "Names: at least one name field required",
		},
		{
			name: "valid ActressInfo with DMMID present",
			actress: &ActressInfo{
				DMMID:        555666,
				JapaneseName: "DMMID Present",
			},
			wantErr: false,
		},
		{
			name: "valid ActressInfo with DMMID absent (zero)",
			actress: &ActressInfo{
				DMMID:        0, // Zero means not set
				JapaneseName: "No DMMID",
			},
			wantErr: false,
		},
		{
			name: "valid ActressInfo with valid HTTP ThumbURL",
			actress: &ActressInfo{
				JapaneseName: "Valid URL",
				ThumbURL:     "https://example.com/thumb.jpg",
			},
			wantErr: false,
		},
		{
			name: "valid ActressInfo with empty ThumbURL",
			actress: &ActressInfo{
				JapaneseName: "Empty ThumbURL",
				ThumbURL:     "",
			},
			wantErr: false,
		},
		{
			name: "invalid ActressInfo with invalid ThumbURL",
			actress: &ActressInfo{
				JapaneseName: "Invalid URL",
				ThumbURL:     "not-a-valid-url",
			},
			wantErr: true,
			errMsg:  "ThumbURL: must be valid HTTP/HTTPS URL or empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateActressInfo(tt.actress)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

// TestActressInfoJSONMarshal tests ActressInfo JSON marshaling (AC-2.5.5)
func TestActressInfoJSONMarshal(t *testing.T) {
	tests := []struct {
		name    string
		actress *ActressInfo
	}{
		{
			name: "marshal ActressInfo with all fields",
			actress: &ActressInfo{
				DMMID:        123456,
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
				ThumbURL:     "https://example.com/thumb.jpg",
			},
		},
		{
			name: "marshal ActressInfo with optional fields missing",
			actress: &ActressInfo{
				DMMID:        0, // Zero DMMID
				JapaneseName: "テスト女優",
				ThumbURL:     "", // Empty ThumbURL
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Marshal to JSON
			jsonData, err := json.Marshal(tt.actress)
			require.NoError(t, err, "JSON marshaling should not error")

			// Unmarshal back
			var unmarshaled ActressInfo
			err = json.Unmarshal(jsonData, &unmarshaled)
			require.NoError(t, err, "JSON unmarshaling should not error")

			// Verify round-trip consistency
			assert.Equal(t, tt.actress.DMMID, unmarshaled.DMMID)
			assert.Equal(t, tt.actress.FirstName, unmarshaled.FirstName)
			assert.Equal(t, tt.actress.LastName, unmarshaled.LastName)
			assert.Equal(t, tt.actress.JapaneseName, unmarshaled.JapaneseName)
			assert.Equal(t, tt.actress.ThumbURL, unmarshaled.ThumbURL)

			// Verify DMMID is preserved (critical for deduplication)
			if tt.actress.DMMID > 0 {
				assert.Equal(t, tt.actress.DMMID, unmarshaled.DMMID, "DMMID must be preserved for deduplication")
			}
		})
	}
}

// TestActressInfoFullNameMethod tests ActressInfo.FullName() method (AC-2.5.5)
func TestActressInfoFullNameMethod(t *testing.T) {
	tests := []struct {
		name         string
		actress      *ActressInfo
		expectedName string
	}{
		{
			name: "full name with first and last",
			actress: &ActressInfo{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			expectedName: "Hatano Yui",
		},
		{
			name: "first name only",
			actress: &ActressInfo{
				FirstName:    "Yui",
				LastName:     "",
				JapaneseName: "結衣",
			},
			expectedName: "Yui",
		},
		{
			name: "japanese name fallback",
			actress: &ActressInfo{
				FirstName:    "",
				LastName:     "",
				JapaneseName: "波多野結衣",
			},
			expectedName: "波多野結衣",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fullName := tt.actress.FullName()
			assert.Equal(t, tt.expectedName, fullName)
		})
	}
}

// TestFieldSpecificValidation tests field-specific validation rules (AC-2.5.4, AC-2.5.6)
func TestFieldSpecificValidation(t *testing.T) {
	tests := []struct {
		name    string
		result  *ScraperResult
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid source field (r18dev)",
			result: &ScraperResult{
				Source: "r18dev",
				ID:     "IPX-123",
				Title:  "Test",
			},
			wantErr: false,
		},
		{
			name: "valid source field (dmm)",
			result: &ScraperResult{
				Source: "dmm",
				ID:     "IPX-456",
				Title:  "Test",
			},
			wantErr: false,
		},
		{
			name: "invalid source field (unknown scraper)",
			result: &ScraperResult{
				Source: "unknown",
				ID:     "IPX-789",
				Title:  "Test",
			},
			wantErr: true,
			errMsg:  "Source: invalid scraper name",
		},
		{
			name: "valid image URLs (HTTPS)",
			result: &ScraperResult{
				Source:    "r18dev",
				ID:        "IPX-URL",
				Title:     "Test URLs",
				PosterURL: "https://example.com/poster.jpg",
				CoverURL:  "https://example.com/cover.jpg",
			},
			wantErr: false,
		},
		{
			name: "valid empty image URLs",
			result: &ScraperResult{
				Source:    "dmm",
				ID:        "IPX-EMPTY",
				Title:     "Empty URLs",
				PosterURL: "",
				CoverURL:  "",
			},
			wantErr: false,
		},
		{
			name: "invalid PosterURL",
			result: &ScraperResult{
				Source:    "r18dev",
				ID:        "IPX-BAD-POSTER",
				Title:     "Bad Poster URL",
				PosterURL: "not-a-url",
			},
			wantErr: true,
			errMsg:  "PosterURL: must be valid HTTP/HTTPS URL or empty",
		},
		{
			name: "invalid CoverURL",
			result: &ScraperResult{
				Source:   "r18dev",
				ID:       "IPX-BAD-COVER",
				Title:    "Bad Cover URL",
				CoverURL: "ftp://invalid.com/cover.jpg",
			},
			wantErr: true,
			errMsg:  "CoverURL: must be valid HTTP/HTTPS URL or empty",
		},
		{
			name: "valid positive runtime",
			result: &ScraperResult{
				Source:  "dmm",
				ID:      "IPX-RUNTIME",
				Title:   "Test Runtime",
				Runtime: 120,
			},
			wantErr: false,
		},
		{
			name: "valid zero runtime (unknown)",
			result: &ScraperResult{
				Source:  "r18dev",
				ID:      "IPX-NO-RUNTIME",
				Title:   "Unknown Runtime",
				Runtime: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateScraperResult(tt.result)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestScraperResultNormalizeMediaURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "pics dmm ps to pl",
			input:  "https://pics.dmm.co.jp/mono/movie/adult/aldn560/aldn560ps.jpg",
			expect: "https://pics.dmm.co.jp/mono/movie/adult/aldn560/aldn560pl.jpg",
		},
		{
			name:   "awsimgsrc co jp ps to pl",
			input:  "https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535ps.jpg",
			expect: "https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535pl.jpg",
		},
		{
			name:   "awsimgsrc com ps to pl",
			input:  "https://awsimgsrc.dmm.com/dig/video/ipx00535/ipx00535ps.jpg",
			expect: "https://awsimgsrc.dmm.com/dig/video/ipx00535/ipx00535pl.jpg",
		},
		{
			name:   "keeps query string",
			input:  "https://pics.dmm.co.jp/mono/movie/adult/aldn560/aldn560ps.jpg?foo=bar",
			expect: "https://pics.dmm.co.jp/mono/movie/adult/aldn560/aldn560pl.jpg?foo=bar",
		},
		{
			name:   "non dmm url unchanged",
			input:  "https://images.example.com/aldn560ps.jpg",
			expect: "https://images.example.com/aldn560ps.jpg",
		},
		{
			name:   "already pl unchanged",
			input:  "https://pics.dmm.co.jp/mono/movie/adult/aldn560/aldn560pl.jpg",
			expect: "https://pics.dmm.co.jp/mono/movie/adult/aldn560/aldn560pl.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := &ScraperResult{
				PosterURL: tt.input,
				CoverURL:  tt.input,
			}
			result.NormalizeMediaURLs()
			assert.Equal(t, tt.expect, result.PosterURL)
			assert.Equal(t, tt.expect, result.CoverURL)
		})
	}
}

func TestScraperResultNormalizeMediaURLs_NilReceiver(t *testing.T) {
	t.Parallel()

	var result *ScraperResult
	assert.NotPanics(t, func() {
		result.NormalizeMediaURLs()
	})
}
