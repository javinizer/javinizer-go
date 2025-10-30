package matcher

import (
	"strings"
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
		expectedPart  int
		expectedMulti bool
		shouldMatch   bool
	}{
		// Standard formats
		{"Standard ID", "IPX-535.mp4", "IPX-535", 0, false, true},
		{"With hyphen", "ABC-123.mkv", "ABC-123", 0, false, true},
		{"With Z suffix", "IPX-535Z.mp4", "IPX-535Z", 0, false, true},
		{"With E suffix", "IPX-535E.mp4", "IPX-535E", 0, false, true},
		{"T28 format", "T28-123.mp4", "T28-123", 0, false, true},

		// Multi-part files
		{"Multi-part CD1", "IPX-535-pt1.mp4", "IPX-535", 1, true, true},
		{"Multi-part CD2", "IPX-535-pt2.mp4", "IPX-535", 2, true, true},
		{"Multi-part CD10", "IPX-535-pt10.mp4", "IPX-535", 10, true, true},

		// With extra text
		{"With title", "IPX-535 Beautiful Day.mp4", "IPX-535", 0, false, true},
		{"With brackets", "[ThZu.Cc]IPX-535.mp4", "IPX-535", 0, false, true},
		{"With metadata", "IPX-535 [1080p].mp4", "IPX-535", 0, false, true},

		// Case variations
		{"Lowercase", "ipx-535.mp4", "IPX-535", 0, false, true},
		{"Mixed case", "IpX-535.mp4", "IPX-535", 0, false, true},

		// Edge cases
		{"No match", "random_movie.mp4", "", 0, false, false},
		{"Only numbers", "12345.mp4", "", 0, false, false},
		{"Invalid format", "ABC_123.mp4", "", 0, false, false},
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
					t.Errorf("Expected part %d, got %d", tc.expectedPart, result.PartNumber)
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
		{ID: "IPX-535", PartNumber: 0},
		{ID: "ABC-123", PartNumber: 0},
		{ID: "IPX-535", PartNumber: 1},
		{ID: "IPX-535", PartNumber: 2},
		{ID: "DEF-456", PartNumber: 0},
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
		{ID: "ABC-123", IsMultiPart: true, PartNumber: 1},
		{ID: "ABC-123", IsMultiPart: true, PartNumber: 2},
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
		{ID: "ABC-123", IsMultiPart: true, PartNumber: 1},
		{ID: "ABC-123", IsMultiPart: true, PartNumber: 2},
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

// TestMatcher_MatchString_EdgeCases tests additional edge cases for MatchString
func TestMatcher_MatchString_EdgeCases(t *testing.T) {
	testCases := []struct {
		name         string
		regexEnabled bool
		regexPattern string
		input        string
		expected     string
		shouldError  bool
	}{
		{
			name:         "Empty string",
			regexEnabled: false,
			input:        "",
			expected:     "",
		},
		{
			name:         "Only whitespace",
			regexEnabled: false,
			input:        "   ",
			expected:     "",
		},
		{
			name:         "No match pattern",
			regexEnabled: false,
			input:        "just some text",
			expected:     "",
		},
		{
			name:         "Multiple IDs - returns first",
			regexEnabled: false,
			input:        "IPX-535 and ABC-123",
			expected:     "IPX-535",
		},
		{
			name:         "ID at end",
			regexEnabled: false,
			input:        "The movie is IPX-535",
			expected:     "IPX-535",
		},
		{
			name:         "Custom regex enabled - matches",
			regexEnabled: true,
			regexPattern: `([A-Z]{3}-\d+)`,
			input:        "IPX-535",
			expected:     "IPX-535",
		},
		{
			name:         "Custom regex enabled - no match, fallback to builtin",
			regexEnabled: true,
			regexPattern: `([A-Z]{3}-\d+)`,
			input:        "T28-567", // T28 not 3 letters
			expected:     "T28-567",
		},
		{
			name:         "Custom regex with no capture group",
			regexEnabled: true,
			regexPattern: `[A-Z]{3}-\d+`, // No capture group
			input:        "IPX-535",
			expected:     "IPX-535", // Falls back to builtin
		},
		{
			name:         "Case insensitive matching",
			regexEnabled: false,
			input:        "ipx-535",
			expected:     "IPX-535",
		},
		{
			name:         "With special characters",
			regexEnabled: false,
			input:        "[ThZu.Cc]IPX-535(1080p)",
			expected:     "IPX-535",
		},
		{
			name:         "Very long string",
			regexEnabled: false,
			input:        strings.Repeat("text ", 1000) + "IPX-535" + strings.Repeat(" more", 1000),
			expected:     "IPX-535",
		},
		{
			name:         "Unicode characters around ID",
			regexEnabled: false,
			input:        "映画 IPX-535 美しい",
			expected:     "IPX-535",
		},
		{
			name:         "Numbers only",
			regexEnabled: false,
			input:        "123456",
			expected:     "",
		},
		{
			name:         "Letters only",
			regexEnabled: false,
			input:        "ABCDEF",
			expected:     "",
		},
		{
			name:         "Almost valid - missing number",
			regexEnabled: false,
			input:        "IPX-",
			expected:     "",
		},
		{
			name:         "Almost valid - missing studio",
			regexEnabled: false,
			input:        "-535",
			expected:     "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.MatchingConfig{
				RegexEnabled: tc.regexEnabled,
				RegexPattern: tc.regexPattern,
			}

			matcher, err := NewMatcher(cfg)
			if tc.shouldError {
				if err == nil {
					t.Error("Expected error creating matcher, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Failed to create matcher: %v", err)
			}

			result := matcher.MatchString(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q for input %q", tc.expected, result, tc.input)
			}
		})
	}
}

// TestMatcher_EmptyResults tests handling of empty file lists
func TestMatcher_EmptyResults(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	// Empty file list
	results := matcher.Match([]scanner.FileInfo{})
	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty file list, got %d", len(results))
	}

	// Nil file list
	results = matcher.Match(nil)
	if len(results) != 0 {
		t.Errorf("Expected 0 results for nil file list, got %d", len(results))
	}
}

// TestGroupByID_EdgeCases tests edge cases for GroupByID
func TestGroupByID_EdgeCases(t *testing.T) {
	t.Run("Empty results", func(t *testing.T) {
		grouped := GroupByID([]MatchResult{})
		if len(grouped) != 0 {
			t.Errorf("Expected 0 groups for empty results, got %d", len(grouped))
		}
	})

	t.Run("Nil results", func(t *testing.T) {
		grouped := GroupByID(nil)
		if len(grouped) != 0 {
			t.Errorf("Expected 0 groups for nil results, got %d", len(grouped))
		}
	})

	t.Run("Single ID multiple times", func(t *testing.T) {
		results := []MatchResult{
			{ID: "IPX-535"},
			{ID: "IPX-535"},
			{ID: "IPX-535"},
		}
		grouped := GroupByID(results)
		if len(grouped) != 1 {
			t.Errorf("Expected 1 group, got %d", len(grouped))
		}
		if len(grouped["IPX-535"]) != 3 {
			t.Errorf("Expected 3 files in group, got %d", len(grouped["IPX-535"]))
		}
	})
}

// TestFilterMultiPart_EdgeCases tests edge cases for FilterMultiPart
func TestFilterMultiPart_EdgeCases(t *testing.T) {
	t.Run("Empty results", func(t *testing.T) {
		filtered := FilterMultiPart([]MatchResult{})
		if len(filtered) != 0 {
			t.Errorf("Expected 0 filtered results for empty input, got %d", len(filtered))
		}
	})

	t.Run("Nil results", func(t *testing.T) {
		filtered := FilterMultiPart(nil)
		if len(filtered) != 0 {
			t.Errorf("Expected 0 filtered results for nil input, got %d", len(filtered))
		}
	})

	t.Run("All single-part", func(t *testing.T) {
		results := []MatchResult{
			{ID: "IPX-535", IsMultiPart: false},
			{ID: "ABC-123", IsMultiPart: false},
		}
		filtered := FilterMultiPart(results)
		if len(filtered) != 0 {
			t.Errorf("Expected 0 filtered results for all single-part, got %d", len(filtered))
		}
	})

	t.Run("All multi-part", func(t *testing.T) {
		results := []MatchResult{
			{ID: "IPX-535", IsMultiPart: true},
			{ID: "ABC-123", IsMultiPart: true},
		}
		filtered := FilterMultiPart(results)
		if len(filtered) != 2 {
			t.Errorf("Expected 2 filtered results for all multi-part, got %d", len(filtered))
		}
	})
}

// TestFilterSinglePart_EdgeCases tests edge cases for FilterSinglePart
func TestFilterSinglePart_EdgeCases(t *testing.T) {
	t.Run("Empty results", func(t *testing.T) {
		filtered := FilterSinglePart([]MatchResult{})
		if len(filtered) != 0 {
			t.Errorf("Expected 0 filtered results for empty input, got %d", len(filtered))
		}
	})

	t.Run("Nil results", func(t *testing.T) {
		filtered := FilterSinglePart(nil)
		if len(filtered) != 0 {
			t.Errorf("Expected 0 filtered results for nil input, got %d", len(filtered))
		}
	})

	t.Run("All multi-part", func(t *testing.T) {
		results := []MatchResult{
			{ID: "IPX-535", IsMultiPart: true},
			{ID: "ABC-123", IsMultiPart: true},
		}
		filtered := FilterSinglePart(results)
		if len(filtered) != 0 {
			t.Errorf("Expected 0 filtered results for all multi-part, got %d", len(filtered))
		}
	})

	t.Run("All single-part", func(t *testing.T) {
		results := []MatchResult{
			{ID: "IPX-535", IsMultiPart: false},
			{ID: "ABC-123", IsMultiPart: false},
		}
		filtered := FilterSinglePart(results)
		if len(filtered) != 2 {
			t.Errorf("Expected 2 filtered results for all single-part, got %d", len(filtered))
		}
	})
}

// TestMatcher_VariousExtensions tests matching with different file extensions
func TestMatcher_VariousExtensions(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	extensions := []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".m4v"}

	for _, ext := range extensions {
		t.Run(ext, func(t *testing.T) {
			file := scanner.FileInfo{
				Name:      "IPX-535" + ext,
				Extension: ext,
			}

			result := matcher.MatchFile(file)
			if result == nil {
				t.Fatalf("Expected match for extension %s, got nil", ext)
			}

			if result.ID != "IPX-535" {
				t.Errorf("Expected ID IPX-535, got %s", result.ID)
			}
		})
	}
}

// TestMatcher_PathSeparators tests that path separators don't break matching
func TestMatcher_PathSeparators(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name       string
		filename   string
		expectedID string
	}{
		{"With path", "/path/to/IPX-535.mp4", "IPX-535"},
		{"Windows path", "C:\\Videos\\IPX-535.mp4", "IPX-535"},
		{"Relative path", "./videos/IPX-535.mp4", "IPX-535"},
		{"Deep path", "/a/b/c/d/e/IPX-535.mp4", "IPX-535"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

// TestMatcher_LongStudioCodes tests studio codes of varying lengths
func TestMatcher_LongStudioCodes(t *testing.T) {
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
		// 2 letters
		{"AB-123.mp4", "AB-123"},
		// 3 letters
		{"IPX-535.mp4", "IPX-535"},
		// 4 letters
		{"SSIS-001.mp4", "SSIS-001"},
		// 5 letters
		{"STARS-123.mp4", "STARS-123"},
		// Special case: T28
		{"T28-567.mp4", "T28-567"},
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

// TestMatcher_PartSuffixVariations tests various multi-part suffix formats
func TestMatcher_PartSuffixVariations(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name         string
		filename     string
		expectedID   string
		expectedPart int
		isMultiPart  bool
	}{
		// Letter suffixes
		{"Letter A", "IPX-535-A.mp4", "IPX-535", 1, true},
		{"Letter B", "IPX-535-B.mp4", "IPX-535", 2, true},
		{"Letter C", "IPX-535-C.mp4", "IPX-535", 3, true},
		{"Lowercase letter", "IPX-535-a.mp4", "IPX-535", 1, true},

		// Numeric suffixes
		{"pt1", "IPX-535-pt1.mp4", "IPX-535", 1, true},
		{"pt2", "IPX-535-pt2.mp4", "IPX-535", 2, true},
		{"part1", "IPX-535-part1.mp4", "IPX-535", 1, true},
		{"part2", "IPX-535-part2.mp4", "IPX-535", 2, true},
		{"Double digit", "IPX-535-pt10.mp4", "IPX-535", 10, true},

		// No suffix - single part
		{"No suffix", "IPX-535.mp4", "IPX-535", 0, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

			if result.PartNumber != tc.expectedPart {
				t.Errorf("Expected part number %d, got %d", tc.expectedPart, result.PartNumber)
			}

			if result.IsMultiPart != tc.isMultiPart {
				t.Errorf("Expected IsMultiPart %v, got %v", tc.isMultiPart, result.IsMultiPart)
			}
		})
	}
}
