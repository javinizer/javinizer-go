package dmm

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractJSONLD verifies JSON-LD extraction from HTML
func TestExtractJSONLD(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		expectNil   bool
		description string
	}{
		{
			name: "valid Product JSON-LD",
			html: `
<!DOCTYPE html>
<html>
<head>
	<script type="application/ld+json">
	{
		"@context": "http://schema.org",
		"@type": "Product",
		"name": "Test Movie",
		"description": "Test description",
		"sku": "ipx00535"
	}
	</script>
</head>
<body></body>
</html>`,
			expectNil:   false,
			description: "Should extract valid Product JSON-LD",
		},
		{
			name: "non-Product JSON-LD (VideoObject)",
			html: `
<!DOCTYPE html>
<html>
<head>
	<script type="application/ld+json">
	{
		"@context": "http://schema.org",
		"@type": "VideoObject",
		"name": "Test Video"
	}
	</script>
</head>
<body></body>
</html>`,
			expectNil:   true,
			description: "Should return nil for non-Product types",
		},
		{
			name: "invalid JSON",
			html: `
<!DOCTYPE html>
<html>
<head>
	<script type="application/ld+json">
	{ invalid json
	</script>
</head>
<body></body>
</html>`,
			expectNil:   true,
			description: "Should return nil for invalid JSON",
		},
		{
			name: "no JSON-LD script",
			html: `
<!DOCTYPE html>
<html>
<head>
</head>
<body>No JSON-LD here</body>
</html>`,
			expectNil:   true,
			description: "Should return nil when no JSON-LD present",
		},
		{
			name: "multiple JSON-LD scripts (first is Product)",
			html: `
<!DOCTYPE html>
<html>
<head>
	<script type="application/ld+json">
	{
		"@context": "http://schema.org",
		"@type": "Product",
		"name": "First Product"
	}
	</script>
	<script type="application/ld+json">
	{
		"@context": "http://schema.org",
		"@type": "Organization",
		"name": "Some Org"
	}
	</script>
</head>
<body></body>
</html>`,
			expectNil:   false,
			description: "Should return first Product JSON-LD when multiple scripts present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			result := extractJSONLD(doc)

			if tt.expectNil {
				assert.Nil(t, result, tt.description)
			} else {
				assert.NotNil(t, result, tt.description)
				assert.Equal(t, "Product", result.Type)
			}
		})
	}
}

// TestGetImagesFromJSONLD verifies image extraction from JSON-LD image field
func TestGetImagesFromJSONLD(t *testing.T) {
	tests := []struct {
		name          string
		imageField    interface{}
		expectedCount int
		description   string
	}{
		{
			name:          "single image as string",
			imageField:    "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
			expectedCount: 1,
			description:   "Should extract single image URL from string",
		},
		{
			name: "array of images",
			imageField: []interface{}{
				"https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
				"https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg",
				"https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-2.jpg",
			},
			expectedCount: 3,
			description:   "Should extract all images from array",
		},
		{
			name:          "empty string",
			imageField:    "",
			expectedCount: 0,
			description:   "Should return empty array for empty string",
		},
		{
			name:          "empty array",
			imageField:    []interface{}{},
			expectedCount: 0,
			description:   "Should return empty array for empty array input",
		},
		{
			name: "array with empty strings",
			imageField: []interface{}{
				"https://pics.dmm.co.jp/image1.jpg",
				"",
				"https://pics.dmm.co.jp/image2.jpg",
			},
			expectedCount: 2,
			description:   "Should skip empty strings in array",
		},
		{
			name:          "nil value",
			imageField:    nil,
			expectedCount: 0,
			description:   "Should return empty array for nil",
		},
		{
			name:          "unsupported type (number)",
			imageField:    12345,
			expectedCount: 0,
			description:   "Should return empty array for unsupported types",
		},
		{
			name: "mixed array with non-string elements",
			imageField: []interface{}{
				"https://pics.dmm.co.jp/image1.jpg",
				123,
				"https://pics.dmm.co.jp/image2.jpg",
				nil,
			},
			expectedCount: 2,
			description:   "Should only extract string elements from mixed array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getImagesFromJSONLD(tt.imageField)
			assert.Len(t, result, tt.expectedCount, tt.description)

			// Verify all results are non-empty strings
			for _, img := range result {
				assert.NotEmpty(t, img)
				assert.Contains(t, img, "http")
			}
		})
	}
}

// TestParseReleaseDateFromJSONLD verifies release date parsing
func TestParseReleaseDateFromJSONLD(t *testing.T) {
	tests := []struct {
		name         string
		uploadDate   string
		expectNil    bool
		expectedDate string
		description  string
	}{
		{
			name:         "valid ISO 8601 date",
			uploadDate:   "2024-01-15",
			expectNil:    false,
			expectedDate: "2024-01-15",
			description:  "Should parse valid ISO 8601 date",
		},
		{
			name:        "invalid date format",
			uploadDate:  "01/15/2024",
			expectNil:   true,
			description: "Should return nil for non-ISO 8601 format",
		},
		{
			name:        "empty string",
			uploadDate:  "",
			expectNil:   true,
			description: "Should return nil for empty string",
		},
		{
			name:        "invalid date",
			uploadDate:  "2024-13-32",
			expectNil:   true,
			description: "Should return nil for invalid date values",
		},
		{
			name:         "leap year date",
			uploadDate:   "2024-02-29",
			expectNil:    false,
			expectedDate: "2024-02-29",
			description:  "Should handle leap year dates",
		},
		{
			name:        "incomplete date",
			uploadDate:  "2024-01",
			expectNil:   true,
			description: "Should return nil for incomplete date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseReleaseDateFromJSONLD(tt.uploadDate)

			if tt.expectNil {
				assert.Nil(t, result, tt.description)
			} else {
				require.NotNil(t, result, tt.description)
				assert.Equal(t, tt.expectedDate, result.Format("2006-01-02"))
			}
		})
	}
}

// TestExtractMetadataFromJSONLD verifies complete metadata extraction
func TestExtractMetadataFromJSONLD(t *testing.T) {
	tests := []struct {
		name         string
		html         string
		expectedKeys []string
		checkValues  map[string]interface{}
		description  string
	}{
		{
			name: "complete Product JSON-LD",
			html: `
<!DOCTYPE html>
<html>
<head>
	<script type="application/ld+json">
	{
		"@context": "http://schema.org",
		"@type": "Product",
		"name": "Test Movie Title",
		"description": "Test movie description",
		"sku": "ipx00535",
		"brand": {
			"@type": "Brand",
			"name": "Test Studio"
		},
		"image": [
			"https://pics.dmm.co.jp/cover.jpg",
			"https://pics.dmm.co.jp/screenshot1.jpg",
			"https://pics.dmm.co.jp/screenshot2.jpg"
		],
		"subjectOf": {
			"@type": "VideoObject",
			"name": "Test Video",
			"contentUrl": "https://cc3001.dmm.co.jp/litevideo/sample.mp4",
			"uploadDate": "2024-01-15",
			"genre": ["Drama", "Romance", "Action"]
		},
		"aggregateRating": {
			"@type": "AggregateRating",
			"ratingValue": 4.5,
			"ratingCount": 100
		}
	}
	</script>
</head>
<body></body>
</html>`,
			expectedKeys: []string{"title", "description", "content_id", "maker", "cover_url", "screenshots", "trailer_url", "release_date", "genres", "rating_value", "rating_count"},
			checkValues: map[string]interface{}{
				"title":        "Test Movie Title",
				"description":  "Test movie description",
				"content_id":   "ipx00535",
				"maker":        "Test Studio",
				"rating_value": 9.0, // 4.5 * 2
				"rating_count": 100,
			},
			description: "Should extract all metadata fields from complete JSON-LD",
		},
		{
			name: "minimal Product JSON-LD",
			html: `
<!DOCTYPE html>
<html>
<head>
	<script type="application/ld+json">
	{
		"@context": "http://schema.org",
		"@type": "Product",
		"name": "Minimal Movie"
	}
	</script>
</head>
<body></body>
</html>`,
			expectedKeys: []string{"title"},
			checkValues: map[string]interface{}{
				"title": "Minimal Movie",
			},
			description: "Should handle minimal JSON-LD with only title",
		},
		{
			name: "no JSON-LD",
			html: `
<!DOCTYPE html>
<html>
<head></head>
<body>No JSON-LD</body>
</html>`,
			expectedKeys: []string{},
			checkValues:  map[string]interface{}{},
			description:  "Should return empty metadata when no JSON-LD present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			metadata := extractMetadataFromJSONLD(doc)

			// Check expected keys are present
			for _, key := range tt.expectedKeys {
				assert.Contains(t, metadata, key, "Should contain key: "+key)
			}

			// Check specific values
			for key, expectedValue := range tt.checkValues {
				actualValue := metadata[key]
				switch expected := expectedValue.(type) {
				case string:
					assert.Equal(t, expected, actualValue, "Mismatch for key: "+key)
				case float64:
					assert.Equal(t, expected, actualValue, "Mismatch for key: "+key)
				case int:
					assert.Equal(t, expected, actualValue, "Mismatch for key: "+key)
				}
			}
		})
	}
}

// TestGetStringFromMetadata verifies safe string retrieval from metadata map
func TestGetStringFromMetadata(t *testing.T) {
	metadata := map[string]interface{}{
		"title":     "Test Title",
		"number":    123,
		"empty":     "",
		"nil_value": nil,
	}

	tests := []struct {
		name        string
		key         string
		expected    string
		description string
	}{
		{
			name:        "existing string key",
			key:         "title",
			expected:    "Test Title",
			description: "Should retrieve existing string value",
		},
		{
			name:        "non-string value",
			key:         "number",
			expected:    "",
			description: "Should return empty string for non-string value",
		},
		{
			name:        "empty string value",
			key:         "empty",
			expected:    "",
			description: "Should return empty string for empty string value",
		},
		{
			name:        "missing key",
			key:         "missing",
			expected:    "",
			description: "Should return empty string for missing key",
		},
		{
			name:        "nil value",
			key:         "nil_value",
			expected:    "",
			description: "Should return empty string for nil value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringFromMetadata(metadata, tt.key)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestGetStringSliceFromMetadata verifies safe []string retrieval from metadata map
func TestGetStringSliceFromMetadata(t *testing.T) {
	metadata := map[string]interface{}{
		"genres":      []string{"Drama", "Romance"},
		"empty_slice": []string{},
		"number":      123,
		"nil_value":   nil,
	}

	tests := []struct {
		name        string
		key         string
		expected    []string
		description string
	}{
		{
			name:        "existing string slice",
			key:         "genres",
			expected:    []string{"Drama", "Romance"},
			description: "Should retrieve existing string slice",
		},
		{
			name:        "empty string slice",
			key:         "empty_slice",
			expected:    []string{},
			description: "Should retrieve empty string slice",
		},
		{
			name:        "non-slice value",
			key:         "number",
			expected:    nil,
			description: "Should return nil for non-slice value",
		},
		{
			name:        "missing key",
			key:         "missing",
			expected:    nil,
			description: "Should return nil for missing key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringSliceFromMetadata(metadata, tt.key)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestGetTimeFromMetadata verifies safe *time.Time retrieval from metadata map
func TestGetTimeFromMetadata(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	metadata := map[string]interface{}{
		"release_date": &testTime,
		"string_value": "2024-01-15",
		"nil_value":    nil,
	}

	tests := []struct {
		name        string
		key         string
		expectNil   bool
		expected    *time.Time
		description string
	}{
		{
			name:        "existing time value",
			key:         "release_date",
			expectNil:   false,
			expected:    &testTime,
			description: "Should retrieve existing time value",
		},
		{
			name:        "non-time value",
			key:         "string_value",
			expectNil:   true,
			description: "Should return nil for non-time value",
		},
		{
			name:        "missing key",
			key:         "missing",
			expectNil:   true,
			description: "Should return nil for missing key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTimeFromMetadata(metadata, tt.key)

			if tt.expectNil {
				assert.Nil(t, result, tt.description)
			} else {
				require.NotNil(t, result, tt.description)
				assert.Equal(t, tt.expected.Unix(), result.Unix())
			}
		})
	}
}

// TestGetFloat64FromMetadata verifies safe float64 retrieval from metadata map
func TestGetFloat64FromMetadata(t *testing.T) {
	metadata := map[string]interface{}{
		"rating":      9.5,
		"int_value":   10,
		"string_num":  "8.5",
		"string_text": "not a number",
		"nil_value":   nil,
	}

	tests := []struct {
		name        string
		key         string
		expected    float64
		description string
	}{
		{
			name:        "existing float64 value",
			key:         "rating",
			expected:    9.5,
			description: "Should retrieve existing float64 value",
		},
		{
			name:        "int value",
			key:         "int_value",
			expected:    10.0,
			description: "Should convert int to float64",
		},
		{
			name:        "string number",
			key:         "string_num",
			expected:    8.5,
			description: "Should parse numeric string to float64",
		},
		{
			name:        "non-numeric string",
			key:         "string_text",
			expected:    0.0,
			description: "Should return 0 for non-numeric string",
		},
		{
			name:        "missing key",
			key:         "missing",
			expected:    0.0,
			description: "Should return 0 for missing key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFloat64FromMetadata(metadata, tt.key)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestGetIntFromMetadata verifies safe int retrieval from metadata map
func TestGetIntFromMetadata(t *testing.T) {
	metadata := map[string]interface{}{
		"votes":       100,
		"float_value": 99.9,
		"string_num":  "85",
		"string_text": "not a number",
		"nil_value":   nil,
	}

	tests := []struct {
		name        string
		key         string
		expected    int
		description string
	}{
		{
			name:        "existing int value",
			key:         "votes",
			expected:    100,
			description: "Should retrieve existing int value",
		},
		{
			name:        "float64 value",
			key:         "float_value",
			expected:    99,
			description: "Should convert float64 to int",
		},
		{
			name:        "string number",
			key:         "string_num",
			expected:    85,
			description: "Should parse numeric string to int",
		},
		{
			name:        "non-numeric string",
			key:         "string_text",
			expected:    0,
			description: "Should return 0 for non-numeric string",
		},
		{
			name:        "missing key",
			key:         "missing",
			expected:    0,
			description: "Should return 0 for missing key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntFromMetadata(metadata, tt.key)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}
