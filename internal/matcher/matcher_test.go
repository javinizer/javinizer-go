package matcher

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

func TestMatcher_MatchFile(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
		RegexPattern: "",
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name          string
		filename      string
		expectedID    string
		expectedPart  string
		expectedMulti bool
		shouldMatch   bool
	}{
		// Standard formats
		{"Standard ID", "IPX-535.mp4", "IPX-535", "", false, true},
		{"With hyphen", "ABC-123.mkv", "ABC-123", "", false, true},
		{"With Z suffix", "IPX-535Z.mp4", "IPX-535Z", "", false, true},
		{"With E suffix", "IPX-535E.mp4", "IPX-535E", "", false, true},
		{"T28 format", "T28-123.mp4", "T28-123", "", false, true},

		// Multi-part files
		{"Multi-part CD1", "IPX-535-pt1.mp4", "IPX-535", "1", true, true},
		{"Multi-part CD2", "IPX-535-pt2.mp4", "IPX-535", "2", true, true},
		{"Multi-part CD10", "IPX-535-pt10.mp4", "IPX-535", "10", true, true},

		// With extra text
		{"With title", "IPX-535 Beautiful Day.mp4", "IPX-535", "", false, true},
		{"With brackets", "[ThZu.Cc]IPX-535.mp4", "IPX-535", "", false, true},
		{"With metadata", "IPX-535 [1080p].mp4", "IPX-535", "", false, true},

		// Case variations
		{"Lowercase", "ipx-535.mp4", "IPX-535", "", false, true},
		{"Mixed case", "IpX-535.mp4", "IPX-535", "", false, true},

		// Edge cases
		{"No match", "random_movie.mp4", "", "", false, false},
		{"Only numbers", "12345.mp4", "", "", false, false},
		{"Invalid format", "ABC_123.mp4", "", "", false, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			file := scanner.FileInfo{
				Name:      tc.filename,
				Extension: ".mp4",
			}

			result := matcher.MatchFile(file)

			if tc.shouldMatch {
				if result == nil {
					t.Fatalf("Expected match for %s, got nil", tc.filename)
				}

				if result.ID != tc.expectedID {
					t.Errorf("Expected ID %s, got %s", tc.expectedID, result.ID)
				}

				if result.PartNumber != tc.expectedPart {
					t.Errorf("Expected part %s, got %s", tc.expectedPart, result.PartNumber)
				}

				if result.IsMultiPart != tc.expectedMulti {
					t.Errorf("Expected IsMultiPart %v, got %v", tc.expectedMulti, result.IsMultiPart)
				}

				if result.MatchedBy != "builtin" {
					t.Errorf("Expected MatchedBy 'builtin', got %s", result.MatchedBy)
				}
			} else {
				if result != nil {
					t.Errorf("Expected no match for %s, got ID %s", tc.filename, result.ID)
				}
			}
		})
	}
}

func TestMatcher_CustomRegex(t *testing.T) {
	// Custom regex that only matches 3-letter prefixes
	// Note: If custom regex doesn't match, it falls back to builtin pattern
	cfg := &config.MatchingConfig{
		RegexEnabled: true,
		RegexPattern: `([A-Z]{3}-\d+)`,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		filename       string
		expectedID     string
		expectedSource string // "regex" or "builtin"
	}{
		{"IPX-535.mp4", "IPX-535", "regex"},   // Matches custom regex
		{"ABC-123.mp4", "ABC-123", "regex"},   // Matches custom regex
		{"T28-123.mp4", "T28-123", "builtin"}, // Falls back to builtin (T28 not 3 letters)
		{"ABCD-123.mp4", "BCD-123", "regex"},  // Custom regex matches BCD-123 from ABCD-123
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			file := scanner.FileInfo{
				Name:      tc.filename,
				Extension: ".mp4",
			}

			result := matcher.MatchFile(file)

			if result == nil {
				t.Fatalf("Expected match for %s, got nil", tc.filename)
			}

			if result.ID != tc.expectedID {
				t.Errorf("Expected ID %s, got %s", tc.expectedID, result.ID)
			}

			if result.MatchedBy != tc.expectedSource {
				t.Errorf("Expected MatchedBy '%s', got '%s'", tc.expectedSource, result.MatchedBy)
			}
		})
	}
}

func TestMatcher_Match(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	files := []scanner.FileInfo{
		{Name: "IPX-535.mp4", Extension: ".mp4"},
		{Name: "ABC-123.mkv", Extension: ".mkv"},
		{Name: "random_file.mp4", Extension: ".mp4"},
		{Name: "DEF-456-pt1.mp4", Extension: ".mp4"},
		{Name: "DEF-456-pt2.mp4", Extension: ".mp4"},
	}

	results := matcher.Match(files)

	// Should match 4 files (all except random_file.mp4)
	expectedCount := 4
	if len(results) != expectedCount {
		t.Errorf("Expected %d matches, got %d", expectedCount, len(results))
	}

	// Verify IDs
	expectedIDs := map[string]int{
		"IPX-535": 1,
		"ABC-123": 1,
		"DEF-456": 2, // Two parts
	}

	for id, expectedCount := range expectedIDs {
		count := 0
		for _, result := range results {
			if result.ID == id {
				count++
			}
		}

		if count != expectedCount {
			t.Errorf("Expected %d files with ID %s, got %d", expectedCount, id, count)
		}
	}
}

func TestMatcher_MatchString(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		input    string
		expected string
	}{
		{"IPX-535", "IPX-535"},
		{"IPX-535 Beautiful Day", "IPX-535"},
		{"[ThZu.Cc]IPX-535", "IPX-535"},
		{"abc-123", "ABC-123"}, // Uppercase conversion
		{"no match here", ""},
		{"", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := matcher.MatchString(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestGroupByID(t *testing.T) {
	results := []MatchResult{
		{ID: "IPX-535", PartNumber: ""},
		{ID: "ABC-123", PartNumber: ""},
		{ID: "IPX-535", PartNumber: "1"},
		{ID: "IPX-535", PartNumber: "2"},
		{ID: "DEF-456", PartNumber: ""},
	}

	grouped := GroupByID(results)

	if len(grouped) != 3 {
		t.Errorf("Expected 3 groups, got %d", len(grouped))
	}

	if len(grouped["IPX-535"]) != 3 {
		t.Errorf("Expected 3 files for IPX-535, got %d", len(grouped["IPX-535"]))
	}

	if len(grouped["ABC-123"]) != 1 {
		t.Errorf("Expected 1 file for ABC-123, got %d", len(grouped["ABC-123"]))
	}

	if len(grouped["DEF-456"]) != 1 {
		t.Errorf("Expected 1 file for DEF-456, got %d", len(grouped["DEF-456"]))
	}
}

func TestFilterMultiPart(t *testing.T) {
	results := []MatchResult{
		{ID: "IPX-535", IsMultiPart: false},
		{ID: "ABC-123", IsMultiPart: true, PartNumber: "1"},
		{ID: "ABC-123", IsMultiPart: true, PartNumber: "2"},
		{ID: "DEF-456", IsMultiPart: false},
	}

	filtered := FilterMultiPart(results)

	expectedCount := 2
	if len(filtered) != expectedCount {
		t.Errorf("Expected %d multi-part files, got %d", expectedCount, len(filtered))
	}

	for _, result := range filtered {
		if !result.IsMultiPart {
			t.Errorf("FilterMultiPart returned non-multi-part file: %s", result.ID)
		}
	}
}

func TestFilterSinglePart(t *testing.T) {
	results := []MatchResult{
		{ID: "IPX-535", IsMultiPart: false},
		{ID: "ABC-123", IsMultiPart: true, PartNumber: "1"},
		{ID: "ABC-123", IsMultiPart: true, PartNumber: "2"},
		{ID: "DEF-456", IsMultiPart: false},
	}

	filtered := FilterSinglePart(results)

	expectedCount := 2
	if len(filtered) != expectedCount {
		t.Errorf("Expected %d single-part files, got %d", expectedCount, len(filtered))
	}

	for _, result := range filtered {
		if result.IsMultiPart {
			t.Errorf("FilterSinglePart returned multi-part file: %s", result.ID)
		}
	}
}

func TestMatcher_InvalidRegex(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: true,
		RegexPattern: `[invalid(regex`,
	}

	_, err := NewMatcher(cfg)
	if err == nil {
		t.Error("Expected error for invalid regex, got nil")
	}
}

func TestMatcher_RealWorldFilenames(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		filename   string
		expectedID string
	}{
		// Real-world examples
		{"[ThZu.Cc]ipx-535.mp4", "IPX-535"},
		{"IPX-535 Sakura Momo 1080p.mp4", "IPX-535"},
		{"[HD]ABC-123[720p].mkv", "ABC-123"},
		{"xyz-999-C.mp4", "XYZ-999"},
		{"PRED-123E Exclusive Beauty.mp4", "PRED-123E"},
		{"SSIS-001Z Special Edition.mp4", "SSIS-001Z"},
		{"T28-567 Student Edition.mp4", "T28-567"},

		// With additional metadata
		{"IPX-535 [FHD][MP4]", "IPX-535"},
		{"ABC-123 (2020) [1080p]", "ABC-123"},

		// Multi-disc
		{"IPX-535-pt1 Disc1.mp4", "IPX-535"},
		{"IPX-535-pt2 Disc2.mp4", "IPX-535"},
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			file := scanner.FileInfo{
				Name:      tc.filename,
				Extension: ".mp4",
			}

			result := matcher.MatchFile(file)

			if result == nil {
				t.Fatalf("Expected match for %s, got nil", tc.filename)
			}

			if result.ID != tc.expectedID {
				t.Errorf("Expected ID %s, got %s", tc.expectedID, result.ID)
			}
		})
	}
}
