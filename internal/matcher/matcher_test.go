// Package matcher tests demonstrate the canonical table-driven test pattern for javinizer-go.
//
// This file serves as the reference implementation with 76 test cases showing best practices.
// For the standardized template and documentation, see internal/testutil/template.go.
//
// Key patterns demonstrated:
//   - Multiple test functions, each testing a specific aspect
//   - Table-driven structure with name/input/want/wantErr fields
//   - Comprehensive edge case coverage (real-world filenames, unicode, etc.)
//   - Clear subtest naming for easy debugging
//   - Proper use of t.Run() for parallel execution support
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
		{"1PON date-based format", "020326_001-1PON.mp4", "020326_001-1PON", 0, false, true},
		{"10MU date-based format", "020326_01-10MU.mp4", "020326_01-10MU", 0, false, true},
		{"CARIB date-based format", "123025-001-CARIB.mp4", "123025-001-CARIB", 0, false, true},

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

		// Amateur IDs (no hyphen, 4-6 letter prefixes via conservative heuristic)
		{"Amateur oreco", "oreco183.mp4", "ORECO183", 0, false, true},
		{"Amateur luxu", "luxu456.mp4", "LUXU456", 0, false, true},
		{"Amateur siro", "siro789.mp4", "SIRO789", 0, false, true},
		{"Amateur with title", "oreco183 Beautiful Girl.mp4", "ORECO183", 0, false, true},
		{"Amateur maan", "maan321.mp4", "MAAN321", 0, false, true},
		// Note: 3-letter IDs (cap, ntk, ara) are now treated as standard by conservative heuristic
		{"Cap 3 letters matches", "cap123.mp4", "CAP123", 0, false, true}, // Matches but normalizes with padding
		{"Ntk 3 letters matches", "ntk456.mp4", "NTK456", 0, false, true}, // Matches but normalizes with padding
		{"Ara 3 letters matches", "ara789.mp4", "ARA789", 0, false, true}, // Matches but normalizes with padding

		// TokyoHot-style IDs (short prefix, no hyphen)
		{"TokyoHot N prefix no hyphen", "N1234.mp4", "N1234", 0, false, true},
		{"TokyoHot N prefix hyphen", "N-1234.mp4", "N-1234", 0, false, true},
		{"TokyoHot 2-letter no hyphen", "AB567.mp4", "AB567", 0, false, true},
		{"TokyoHot KEED hyphen", "KEED-528.mp4", "KEED-528", 0, false, true},

		// DMM h_<digits> prefix format
		{"DMM h_ prefix", "h_1472smkcx003.mp4", "H_1472SMKCX003", 0, false, true},
		{"DMM h_ prefix san", "h_796san167.mp4", "H_796SAN167", 0, false, true},
		{"DMM h_ prefix with title", "[Test] h_1472smkcx003 [720p].mkv", "H_1472SMKCX003", 0, false, true},
		{"DMM h_ prefix uppercase", "H_1472SMKCX003.mp4", "H_1472SMKCX003", 0, false, true},

		// Edge cases
		{"No match", "random_movie.mp4", "", 0, false, false},
		{"Only numbers", "12345.mp4", "", 0, false, false},
		{"Invalid format", "ABC_123.mp4", "", 0, false, false},
		// Note: Generic patterns may match, but will fail during DMM search (acceptable behavior)
		{"Generic scene001 matched", "scene001.mp4", "SCENE001", 0, false, true}, // Matcher is lenient, DMM search will filter
		{"video1080 matched", "video1080.mp4", "VIDEO1080", 0, false, true},      // Matcher is lenient, DMM search will filter
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
		{"020326_001-1PON", "020326_001-1PON"},
		{"020326_01-10MU", "020326_01-10MU"},
		{"123025-001-CARIB", "123025-001-CARIB"},
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

		// TokyoHot real-world filenames
		{"N-1234.mp4", "N-1234"},
		{"N1234.mp4", "N1234"},
		{"KEED-528.mp4", "KEED-528"},

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
		name            string
		filename        string
		expectedID      string
		expectedPart    int
		isMultiPart     bool
		expectedPattern string
	}{
		// Letter suffixes - ambiguous, require directory validation
		// IsMultiPart starts as false, becomes true after ValidateMultipartInDirectory
		{"Letter A", "IPX-535-A.mp4", "IPX-535", 1, false, PatternLetter},
		{"Letter B", "IPX-535-B.mp4", "IPX-535", 2, false, PatternLetter},
		{"Letter C", "IPX-535-C.mp4", "IPX-535", 3, false, PatternLetter},
		{"Lowercase letter", "IPX-535-a.mp4", "IPX-535", 1, false, PatternLetter},

		// Numeric suffixes - explicit, always multipart
		{"pt1", "IPX-535-pt1.mp4", "IPX-535", 1, true, PatternExplicit},
		{"pt2", "IPX-535-pt2.mp4", "IPX-535", 2, true, PatternExplicit},
		{"part1", "IPX-535-part1.mp4", "IPX-535", 1, true, PatternExplicit},
		{"part2", "IPX-535-part2.mp4", "IPX-535", 2, true, PatternExplicit},
		{"Double digit", "IPX-535-pt10.mp4", "IPX-535", 10, true, PatternExplicit},

		// No suffix - single part
		{"No suffix", "IPX-535.mp4", "IPX-535", 0, false, PatternNone},
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

			if result.MultipartPattern != tc.expectedPattern {
				t.Errorf("Expected MultipartPattern %s, got %s", tc.expectedPattern, result.MultipartPattern)
			}
		})
	}
}

// TestMatcher_FC2Formats tests FC2-PPV format matching
func TestMatcher_FC2Formats(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name        string
		filename    string
		shouldMatch bool
		expectedID  string
	}{
		// FC2 format - FC2 has a number so doesn't match [A-Za-z]+ pattern
		// But PPV-123456 does match (all letters)
		{"FC2-PPV standard", "FC2-PPV-123456.mp4", true, "PPV-123456"},
		{"FC2 without PPV doesn't match", "FC2-123456.mp4", false, ""}, // FC2 has number, doesn't match
		// With word boundaries, FC2PPV123456 should NOT match partially
		{"FC2 no hyphen doesn't match", "FC2PPV123456.mp4", false, ""}, // Doesn't match (not on word boundary)
		// If the filename contains a standard JAV ID, it will match that first
		{"FC2 with standard ID", "FC2-IPX-535.mp4", true, "IPX-535"},
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
			} else {
				if result != nil {
					t.Errorf("Expected no match for %s, got ID %s", tc.filename, result.ID)
				}
			}
		})
	}
}

// TestMatcher_ComplexFilenames tests filenames with complex metadata
func TestMatcher_ComplexFilenames(t *testing.T) {
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
		// Multiple brackets and metadata
		{"Resolution and codec", "IPX-535 [1080p] [H264] [AAC].mp4", "IPX-535"},
		{"Studio and resolution", "[Studio Name] IPX-535 [1080p].mp4", "IPX-535"},
		{"Year and metadata", "IPX-535 - Title Name (2024) [1080p].mp4", "IPX-535"},
		{"Multiple tags", "[Tag1][Tag2]IPX-535[Tag3][Tag4].mp4", "IPX-535"},

		// Special characters (periods don't work as separators - need hyphens)
		{"With underscores around ID", "IPX-535_Title_Name.mp4", "IPX-535"},
		{"Mixed separators", "IPX-535_Title.Name [1080p].mp4", "IPX-535"},
		{"Parentheses", "(IPX-535) Title Name.mp4", "IPX-535"},

		// Unicode and international characters
		{"Japanese title", "IPX-535 日本語タイトル.mp4", "IPX-535"},
		{"Chinese title", "IPX-535 中文标题.mp4", "IPX-535"},
		{"Korean title", "IPX-535 한국어 제목.mp4", "IPX-535"},
		{"Mixed unicode", "IPX-535 タイトル Title 标题.mp4", "IPX-535"},

		// Very long filenames
		{"Long title", "IPX-535 " + strings.Repeat("Very Long Title ", 20) + ".mp4", "IPX-535"},
		{"Long metadata prefix", strings.Repeat("[Tag]", 50) + "IPX-535.mp4", "IPX-535"},
		{"Long metadata suffix", "IPX-535" + strings.Repeat(" [Tag]", 50) + ".mp4", "IPX-535"},

		// Scene numbers and versions
		{"Scene number", "IPX-535-Scene-1.mp4", "IPX-535"},
		{"Version number", "IPX-535-v2.mp4", "IPX-535"},
		{"Uncensored tag", "IPX-535-uncensored.mp4", "IPX-535"},
		{"Leak tag", "IPX-535-leak.mp4", "IPX-535"},

		// Multiple potential IDs (should match first)
		{"Two IDs", "IPX-535 and ABC-123.mp4", "IPX-535"},
		{"ID in title", "IPX-535 Title with ABC-123 mentioned.mp4", "IPX-535"},
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

// TestMatcher_EdgeCaseIDs tests edge cases in ID patterns
func TestMatcher_EdgeCaseIDs(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name        string
		filename    string
		shouldMatch bool
		expectedID  string
	}{
		// Valid variations
		{"Short studio code", "AB-123.mp4", true, "AB-123"},
		{"Long studio code", "STARS-123.mp4", true, "STARS-123"},
		{"With Z suffix", "IPX-535Z.mp4", true, "IPX-535Z"},
		{"With E suffix", "IPX-535E.mp4", true, "IPX-535E"},

		// The builtin pattern is quite lenient and accepts these
		{"Studio single letter accepted", "A-123.mp4", true, "A-123"},
		{"Number single digit accepted", "IPX-1.mp4", true, "IPX-1"},
		{"Number two digits accepted", "IPX-12.mp4", true, "IPX-12"},
		{"Short number but valid", "TEST-99.mp4", true, "TEST-99"},

		// These truly don't match
		{"Only letters", "ABCDEF.mp4", false, ""},
		{"Only numbers", "123456.mp4", false, ""},
		{"Missing number", "IPX-.mp4", false, ""},
		{"Missing studio", "-535.mp4", false, ""},

		// Ambiguous cases
		{"Looks like year", "2024-01.mp4", false, ""}, // Studio is numbers
		{"Version number", "v1-234.mp4", false, ""},   // v1 is not valid (lowercase letter + number)

		// Lenient pattern now matches these (will fail during DMM search, which is acceptable)
		{"IPX535 no hyphen now matches", "IPX535.mp4", true, "IPX535"}, // Generic pattern catches it
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
			} else {
				if result != nil {
					t.Errorf("Expected no match for %s, got ID %s", tc.filename, result.ID)
				}
			}
		})
	}
}

// TestMatcher_CustomRegexPriority tests that custom regex takes priority over builtin
func TestMatcher_CustomRegexPriority(t *testing.T) {
	testCases := []struct {
		name           string
		regexPattern   string
		filename       string
		expectedID     string
		expectedSource string
		shouldError    bool
	}{
		{
			name:           "Custom matches, use custom",
			regexPattern:   `(?i)([A-Z]{3}-\d{3})`,
			filename:       "IPX-535.mp4",
			expectedID:     "IPX-535",
			expectedSource: "regex",
		},
		{
			name:           "Custom doesn't match, fallback to builtin",
			regexPattern:   `(?i)([A-Z]{3}-\d{3})`,
			filename:       "AB-123.mp4", // Only 2 letters
			expectedID:     "AB-123",
			expectedSource: "builtin",
		},
		{
			name:           "Custom match without capture group falls back to builtin",
			regexPattern:   `(?i)[A-Z]{3}-\d{3}`,
			filename:       "IPX-535.mp4",
			expectedID:     "IPX-535",
			expectedSource: "builtin",
		},
		{
			name:         "Invalid regex pattern",
			regexPattern: `[invalid(`,
			shouldError:  true,
		},
		{
			name:           "Custom pattern matches different format",
			regexPattern:   `(?i)(FC2-PPV-\d+)`,
			filename:       "FC2-PPV-123456.mp4",
			expectedID:     "FC2-PPV-123456",
			expectedSource: "regex",
		},
		{
			name:           "Empty custom pattern uses builtin",
			regexPattern:   "",
			filename:       "IPX-535.mp4",
			expectedID:     "IPX-535",
			expectedSource: "builtin",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.MatchingConfig{
				RegexEnabled: true,
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
				t.Errorf("Expected MatchedBy %s, got %s", tc.expectedSource, result.MatchedBy)
			}
		})
	}
}

// TestMatcher_NilAndEmptyInputs tests handling of nil and empty inputs
func TestMatcher_NilAndEmptyInputs(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	t.Run("Empty filename", func(t *testing.T) {
		file := scanner.FileInfo{
			Name:      "",
			Extension: "",
		}
		result := matcher.MatchFile(file)
		if result != nil {
			t.Errorf("Expected no match for empty filename, got ID %s", result.ID)
		}
	})

	t.Run("Filename with only extension", func(t *testing.T) {
		file := scanner.FileInfo{
			Name:      ".mp4",
			Extension: ".mp4",
		}
		result := matcher.MatchFile(file)
		if result != nil {
			t.Errorf("Expected no match for extension-only filename, got ID %s", result.ID)
		}
	})

	t.Run("Match with empty slice", func(t *testing.T) {
		results := matcher.Match([]scanner.FileInfo{})
		if len(results) != 0 {
			t.Errorf("Expected 0 results for empty slice, got %d", len(results))
		}
	})

	t.Run("Match with nil slice", func(t *testing.T) {
		results := matcher.Match(nil)
		if len(results) != 0 {
			t.Errorf("Expected 0 results for nil slice, got %d", len(results))
		}
	})

	t.Run("MatchString with empty string", func(t *testing.T) {
		result := matcher.MatchString("")
		if result != "" {
			t.Errorf("Expected empty result for empty string, got %s", result)
		}
	})

	t.Run("MatchString with whitespace only", func(t *testing.T) {
		result := matcher.MatchString("   \t\n   ")
		if result != "" {
			t.Errorf("Expected empty result for whitespace-only string, got %s", result)
		}
	})
}

// TestMatcher_CaseNormalization tests that IDs are normalized to uppercase
func TestMatcher_CaseNormalization(t *testing.T) {
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
		{"ipx-535.mp4", "IPX-535"},
		{"IPX-535.mp4", "IPX-535"},
		{"IpX-535.mp4", "IPX-535"},
		{"iPx-535.mp4", "IPX-535"},
		{"ipX-535.mp4", "IPX-535"},
		{"abc-123.mp4", "ABC-123"},
		{"AbC-123.mp4", "ABC-123"},
		{"SSIS-001z.mp4", "SSIS-001Z"}, // Suffix also uppercase
		{"t28-567.mp4", "T28-567"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			file := scanner.FileInfo{
				Name:      tc.input,
				Extension: ".mp4",
			}

			result := matcher.MatchFile(file)
			if result == nil {
				t.Fatalf("Expected match for %s, got nil", tc.input)
			}

			if result.ID != tc.expected {
				t.Errorf("Expected ID %s, got %s", tc.expected, result.ID)
			}
		})
	}
}

// TestMatcher_SpecialStudioCodes tests special studio code patterns
func TestMatcher_SpecialStudioCodes(t *testing.T) {
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
		// T28 format (special case with number in studio code)
		{"T28 standard", "T28-567.mp4", "T28-567"},
		{"T28 lowercase", "t28-567.mp4", "T28-567"},
		{"T28 with title", "T28-567 Title.mp4", "T28-567"},

		// Standard studio codes of various lengths
		{"2 letter studio", "AB-1234.mp4", "AB-1234"},
		{"3 letter studio", "IPX-535.mp4", "IPX-535"},
		{"4 letter studio", "SSIS-123.mp4", "SSIS-123"},
		{"5 letter studio", "STARS-123.mp4", "STARS-123"},

		// With suffix variations
		{"PRED with E", "PRED-123E.mp4", "PRED-123E"},
		{"SSIS with Z", "SSIS-001Z.mp4", "SSIS-001Z"},
		{"STARS with E", "STARS-123E.mp4", "STARS-123E"},
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

// TestMatcher_PartSuffixEdgeCases tests edge cases in part suffix detection
func TestMatcher_PartSuffixEdgeCases(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name            string
		filename        string
		expectedID      string
		expectedPart    int
		expectedSuffix  string
		expectedMulti   bool
		expectedPattern string
	}{
		// Uppercase PT/PART - explicit patterns, always multipart
		{"Uppercase PT", "IPX-535-PT1.mp4", "IPX-535", 1, "-pt1", true, PatternExplicit},
		{"Uppercase PART", "IPX-535-PART1.mp4", "IPX-535", 1, "-part1", true, PatternExplicit},

		// Mixed case - explicit patterns
		{"Mixed case pt", "IPX-535-Pt1.mp4", "IPX-535", 1, "-pt1", true, PatternExplicit},
		{"Mixed case part", "IPX-535-Part1.mp4", "IPX-535", 1, "-part1", true, PatternExplicit},

		// Letter suffixes - ambiguous, require directory validation
		// Note: IsMultiPart is false until ValidateMultipartInDirectory is called
		{"Letter D", "IPX-535-D.mp4", "IPX-535", 4, "-D", false, PatternLetter},
		{"Letter Z", "IPX-535-Z.mp4", "IPX-535", 26, "-Z", false, PatternLetter},
		{"Lowercase z", "IPX-535-z.mp4", "IPX-535", 26, "-Z", false, PatternLetter},

		// Part 0 doesn't exist (returns 0 for invalid)
		{"Part 0 not valid", "IPX-535-pt0.mp4", "IPX-535", 0, "", false, PatternNone},

		// Double digit parts - explicit patterns
		{"Part 11", "IPX-535-pt11.mp4", "IPX-535", 11, "-pt11", true, PatternExplicit},
		{"Part 99", "IPX-535-pt99.mp4", "IPX-535", 99, "-pt99", true, PatternExplicit},

		// Parts with extra text (the regex is flexible and still detects these)
		{"Part with text after", "IPX-535-part1-extra.mp4", "IPX-535", 1, "-part1", true, PatternExplicit},
		{"Letter with text after", "IPX-535-A-extra.mp4", "IPX-535", 0, "", false, PatternNone}, // Extra text prevents letter detection

		// Note: Letter suffixes now require directory validation before being treated as multipart.
		// ABC-123A will have PartNumber=1 and PartSuffix="-A" but IsMultiPart=false until validated.
		// This helps prevent false positives for single files with subtitle markers (-C for Chinese).
		{"ID ending in letter ABC-123A (letter pattern - needs validation)", "ABC-123A.mp4", "ABC-123", 1, "-A", false, PatternLetter},
		{"ID with E suffix IPX-535E (correct: E is part of ID)", "IPX-535E.mp4", "IPX-535E", 0, "", false, PatternNone}, // E is part of ID (matched by regex)
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

			if result.PartSuffix != tc.expectedSuffix {
				t.Errorf("Expected part suffix %q, got %q", tc.expectedSuffix, result.PartSuffix)
			}

			if result.IsMultiPart != tc.expectedMulti {
				t.Errorf("Expected IsMultiPart %v, got %v", tc.expectedMulti, result.IsMultiPart)
			}

			if result.MultipartPattern != tc.expectedPattern {
				t.Errorf("Expected MultipartPattern %s, got %s", tc.expectedPattern, result.MultipartPattern)
			}
		})
	}
}

// TestMatcher_RegressionCases tests specific regression cases from real-world usage
func TestMatcher_RegressionCases(t *testing.T) {
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
		// Real filenames from issue reports (if any)
		{"Complex real filename 1", "[ThZu.Cc]ipx-535-C.mp4", "IPX-535"},
		{"Complex real filename 2", "IPX-535 Sakura Momo Beautiful Day 1080p.mp4", "IPX-535"},
		{"Complex real filename 3", "[HD][JAV]IPX-535[720p][H264].mkv", "IPX-535"},

		// Edge cases that might have caused bugs
		{"Hyphen in title", "IPX-535 Title-With-Hyphens.mp4", "IPX-535"},
		{"Numbers in title", "IPX-535 Title 123 456.mp4", "IPX-535"},
		{"Similar ID pattern in title", "IPX-535 featuring ABC-999.mp4", "IPX-535"},
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

// TestValidateMultipartInDirectory tests the directory context validation for letter-based multipart patterns
func TestValidateMultipartInDirectory(t *testing.T) {
	testCases := []struct {
		name          string
		results       []MatchResult
		expectedMulti []bool // Expected IsMultiPart after validation for each result
		desc          string
	}{
		{
			name: "single file with letter suffix - NOT multipart",
			results: []MatchResult{
				{
					ID:               "ABW-121",
					PartNumber:       3,
					PartSuffix:       "-C",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/ABW-121-C.mp4"},
				},
			},
			expectedMulti: []bool{false},
			desc:          "Single file with -C suffix (Chinese subtitles) should NOT be treated as multipart",
		},
		{
			name: "multiple letter-pattern files same ID - IS multipart",
			results: []MatchResult{
				{
					ID:               "ABW-121",
					PartNumber:       1,
					PartSuffix:       "-A",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/ABW-121-A.mp4"},
				},
				{
					ID:               "ABW-121",
					PartNumber:       2,
					PartSuffix:       "-B",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/ABW-121-B.mp4"},
				},
			},
			expectedMulti: []bool{true, true},
			desc:          "Two files with same ID and letter suffixes should be detected as multipart",
		},
		{
			name: "explicit pattern stays multipart regardless of sibling count",
			results: []MatchResult{
				{
					ID:               "IPX-535",
					PartNumber:       1,
					PartSuffix:       "-pt1",
					MultipartPattern: PatternExplicit,
					IsMultiPart:      true,
					File:             scanner.FileInfo{Path: "/videos/IPX-535-pt1.mp4"},
				},
			},
			expectedMulti: []bool{true},
			desc:          "Explicit patterns (pt1, part2, etc.) are always multipart",
		},
		{
			name: "mixed patterns - only letter patterns validated",
			results: []MatchResult{
				{
					ID:               "IPX-535",
					PartNumber:       1,
					PartSuffix:       "-pt1",
					MultipartPattern: PatternExplicit,
					IsMultiPart:      true,
					File:             scanner.FileInfo{Path: "/videos/IPX-535-pt1.mp4"},
				},
				{
					ID:               "ABW-121",
					PartNumber:       3,
					PartSuffix:       "-C",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/ABW-121-C.mp4"},
				},
			},
			expectedMulti: []bool{true, false},
			desc:          "Explicit multipart stays multipart, lone letter pattern stays single",
		},
		{
			name: "different directories - separate validation",
			results: []MatchResult{
				{
					ID:               "ABW-121",
					PartNumber:       1,
					PartSuffix:       "-A",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/dir1/ABW-121-A.mp4"},
				},
				{
					ID:               "ABW-121",
					PartNumber:       2,
					PartSuffix:       "-B",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/dir2/ABW-121-B.mp4"},
				},
			},
			expectedMulti: []bool{false, false},
			desc:          "Same ID but different directories should NOT be grouped as multipart",
		},
		{
			name: "three parts with letter suffixes",
			results: []MatchResult{
				{
					ID:               "MDB-087",
					PartNumber:       1,
					PartSuffix:       "-A",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/MDB-087-A.mp4"},
				},
				{
					ID:               "MDB-087",
					PartNumber:       2,
					PartSuffix:       "-B",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/MDB-087-B.mp4"},
				},
				{
					ID:               "MDB-087",
					PartNumber:       3,
					PartSuffix:       "-C",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/MDB-087-C.mp4"},
				},
			},
			expectedMulti: []bool{true, true, true},
			desc:          "Three letter-pattern files with same ID should all be multipart",
		},
		{
			name:          "empty results",
			results:       []MatchResult{},
			expectedMulti: []bool{},
			desc:          "Empty input should return empty output",
		},
		{
			name: "letter and explicit patterns mixed for same ID",
			results: []MatchResult{
				{
					ID:               "IPX-535",
					PartNumber:       1,
					PartSuffix:       "-A",
					MultipartPattern: PatternLetter,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/IPX-535-A.mp4"},
				},
				{
					ID:               "IPX-535",
					PartNumber:       2,
					PartSuffix:       "-pt2",
					MultipartPattern: PatternExplicit,
					IsMultiPart:      true,
					File:             scanner.FileInfo{Path: "/videos/IPX-535-pt2.mp4"},
				},
			},
			expectedMulti: []bool{false, true},
			desc:          "Mixed: explicit stays true, single letter stays false (needs 2+ letter files)",
		},
		{
			name: "no pattern files unchanged",
			results: []MatchResult{
				{
					ID:               "IPX-535",
					PartNumber:       0,
					PartSuffix:       "",
					MultipartPattern: PatternNone,
					IsMultiPart:      false,
					File:             scanner.FileInfo{Path: "/videos/IPX-535.mp4"},
				},
			},
			expectedMulti: []bool{false},
			desc:          "Files with no pattern should remain single-part",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			validated := ValidateMultipartInDirectory(tc.results)

			if len(validated) != len(tc.expectedMulti) {
				t.Fatalf("Expected %d results, got %d", len(tc.expectedMulti), len(validated))
			}

			for i, expected := range tc.expectedMulti {
				if validated[i].IsMultiPart != expected {
					t.Errorf("Result %d: expected IsMultiPart=%v, got %v (%s)",
						i, expected, validated[i].IsMultiPart, tc.desc)
				}
			}
		})
	}
}

// TestValidateMultipartInDirectory_DoesNotModifyInput verifies that the function doesn't modify the input slice
func TestValidateMultipartInDirectory_DoesNotModifyInput(t *testing.T) {
	input := []MatchResult{
		{
			ID:               "ABW-121",
			PartNumber:       1,
			PartSuffix:       "-A",
			MultipartPattern: PatternLetter,
			IsMultiPart:      false,
			File:             scanner.FileInfo{Path: "/videos/ABW-121-A.mp4"},
		},
		{
			ID:               "ABW-121",
			PartNumber:       2,
			PartSuffix:       "-B",
			MultipartPattern: PatternLetter,
			IsMultiPart:      false,
			File:             scanner.FileInfo{Path: "/videos/ABW-121-B.mp4"},
		},
	}

	// Store original values
	originalMulti0 := input[0].IsMultiPart
	originalMulti1 := input[1].IsMultiPart

	_ = ValidateMultipartInDirectory(input)

	// Verify input wasn't modified
	if input[0].IsMultiPart != originalMulti0 {
		t.Errorf("Input[0].IsMultiPart was modified: expected %v, got %v", originalMulti0, input[0].IsMultiPart)
	}
	if input[1].IsMultiPart != originalMulti1 {
		t.Errorf("Input[1].IsMultiPart was modified: expected %v, got %v", originalMulti1, input[1].IsMultiPart)
	}
}

// TestValidateMultipartInDirectory_RealWorldScenario tests the main use case: Chinese subtitle files
func TestValidateMultipartInDirectory_RealWorldScenario(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	// Scenario: User has ABW-121-C.mp4 where -C means Chinese subtitles, NOT part 3
	files := []scanner.FileInfo{
		{Name: "ABW-121-C.mp4", Extension: ".mp4", Path: "/videos/ABW-121-C.mp4"},
	}

	// Match files
	results := matcher.Match(files)
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// Before validation: should have letter pattern detected but NOT be multipart
	if results[0].MultipartPattern != PatternLetter {
		t.Errorf("Expected MultipartPattern %s, got %s", PatternLetter, results[0].MultipartPattern)
	}
	if results[0].IsMultiPart != false {
		t.Errorf("Expected IsMultiPart=false before validation, got true")
	}

	// After validation: should still NOT be multipart (single file)
	validated := ValidateMultipartInDirectory(results)
	if validated[0].IsMultiPart != false {
		t.Errorf("Expected IsMultiPart=false after validation, got true")
	}
}

// TestValidateMultipartInDirectory_ActualMultipart tests genuine multipart detection
func TestValidateMultipartInDirectory_ActualMultipart(t *testing.T) {
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}

	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	// Scenario: User has ABW-121-A.mp4 and ABW-121-B.mp4 - these are genuine multipart
	files := []scanner.FileInfo{
		{Name: "ABW-121-A.mp4", Extension: ".mp4", Path: "/videos/ABW-121-A.mp4"},
		{Name: "ABW-121-B.mp4", Extension: ".mp4", Path: "/videos/ABW-121-B.mp4"},
	}

	// Match files
	results := matcher.Match(files)
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// Before validation: should have letter pattern detected but NOT be multipart
	for i, r := range results {
		if r.MultipartPattern != PatternLetter {
			t.Errorf("Result %d: expected MultipartPattern %s, got %s", i, PatternLetter, r.MultipartPattern)
		}
		if r.IsMultiPart != false {
			t.Errorf("Result %d: expected IsMultiPart=false before validation, got true", i)
		}
	}

	// After validation: should be multipart (multiple files with same ID)
	validated := ValidateMultipartInDirectory(results)
	for i, r := range validated {
		if r.IsMultiPart != true {
			t.Errorf("Result %d: expected IsMultiPart=true after validation, got false", i)
		}
	}
}
