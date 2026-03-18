package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/assert"
)

// TestActressSourceKeyFromModel tests the actressSourceKeyFromModel function
func TestActressSourceKeyFromModel(t *testing.T) {
	tests := []struct {
		name     string
		actress  models.Actress
		expected string
	}{
		{
			name: "DMMID takes precedence",
			actress: models.Actress{
				DMMID:        12345,
				JapaneseName: "田中愛",
				FirstName:    "Ai",
				LastName:     "Tanaka",
			},
			expected: "dmmid:12345",
		},
		{
			name: "Japanese name when no DMMID",
			actress: models.Actress{
				DMMID:        0,
				JapaneseName: "田中愛",
				FirstName:    "Ai",
				LastName:     "Tanaka",
			},
			expected: "name:田中愛",
		},
		{
			name: "FirstName LastName when no Japanese name",
			actress: models.Actress{
				DMMID:        0,
				JapaneseName: "",
				FirstName:    "Ai",
				LastName:     "Tanaka",
			},
			expected: "name:ai tanaka",
		},
		{
			name: "LastName FirstName when FirstName LastName is empty",
			actress: models.Actress{
				DMMID:        0,
				JapaneseName: "",
				FirstName:    "",
				LastName:     "Tanaka",
			},
			expected: "name:tanaka",
		},
		{
			name: "Empty string when all names are empty",
			actress: models.Actress{
				DMMID:        0,
				JapaneseName: "",
				FirstName:    "",
				LastName:     "",
			},
			expected: "",
		},
		{
			name: "Whitespace normalization",
			actress: models.Actress{
				DMMID:        0,
				JapaneseName: "  田中 愛  ",
				FirstName:    "",
				LastName:     "",
			},
			expected: "name:田中 愛",
		},
		{
			name: "Lowercase normalization",
			actress: models.Actress{
				DMMID:        0,
				JapaneseName: "",
				FirstName:    "AI",
				LastName:     "TANAKA",
			},
			expected: "name:ai tanaka",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := actressSourceKeyFromModel(tt.actress)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestActressSourceKeysFromInfo tests the actressSourceKeysFromInfo function
func TestActressSourceKeysFromInfo(t *testing.T) {
	tests := []struct {
		name     string
		info     models.ActressInfo
		expected []string
	}{
		{
			name: "DMMID only",
			info: models.ActressInfo{
				DMMID:        12345,
				JapaneseName: "",
				FirstName:    "",
				LastName:     "",
			},
			expected: []string{"dmmid:12345"},
		},
		{
			name: "Multiple keys from different fields",
			info: models.ActressInfo{
				DMMID:        12345,
				JapaneseName: "田中愛",
				FirstName:    "Ai",
				LastName:     "Tanaka",
			},
			expected: []string{
				"dmmid:12345",
				"name:田中愛",
				"name:ai tanaka",
				"name:tanaka ai",
			},
		},
		{
			name: "Multiple name formats create different keys",
			info: models.ActressInfo{
				DMMID:        12345,
				JapaneseName: "田中愛",
				FirstName:    "田中",
				LastName:     "愛",
			},
			expected: []string{
				"dmmid:12345",
				"name:田中愛",
				"name:田中 愛",
				"name:愛 田中",
			},
		},
		{
			name: "Empty keys filtered out",
			info: models.ActressInfo{
				DMMID:        0,
				JapaneseName: "",
				FirstName:    "",
				LastName:     "",
			},
			expected: []string{},
		},
		{
			name: "Whitespace handling",
			info: models.ActressInfo{
				DMMID:        0,
				JapaneseName: "  田中愛  ",
				FirstName:    "  Ai  ",
				LastName:     "  Tanaka  ",
			},
			expected: []string{
				"name:田中愛",
				"name:ai tanaka",
				"name:tanaka ai",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := actressSourceKeysFromInfo(tt.info)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeNameForKey tests the normalizeNameForKey function
func TestNormalizeNameForKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple name",
			input:    "John Doe",
			expected: "john doe",
		},
		{
			name:     "Extra whitespace",
			input:    "  John   Doe  ",
			expected: "john doe",
		},
		{
			name:     "Tabs and newlines",
			input:    "John\tDoe\n",
			expected: "john doe",
		},
		{
			name:     "Uppercase to lowercase",
			input:    "JOHN DOE",
			expected: "john doe",
		},
		{
			name:     "Japanese characters",
			input:    "田中愛",
			expected: "田中愛",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "Mixed script",
			input:    "田中 John",
			expected: "田中 john",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeNameForKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBuildActressSourcesFromScrapeResults tests buildActressSourcesFromScrapeResults
func TestBuildActressSourcesFromScrapeResults(t *testing.T) {
	tests := []struct {
		name             string
		results          []*models.ScraperResult
		resolvedPriority map[string][]string
		customPriority   []string
		actresses        []models.Actress
		expected         map[string]string
	}{
		{
			name:    "Empty results",
			results: []*models.ScraperResult{},
			actresses: []models.Actress{
				{JapaneseName: "Test Actress"},
			},
			expected: nil,
		},
		{
			name:      "Empty actresses",
			results:   []*models.ScraperResult{{Source: "r18dev"}},
			actresses: []models.Actress{},
			expected:  nil,
		},
		{
			name: "Single result with matching actress",
			results: []*models.ScraperResult{
				{
					Source: "r18dev",
					Actresses: []models.ActressInfo{
						{
							FirstName: "Ai",
							LastName:  "Tanaka",
						},
					},
				},
			},
			actresses: []models.Actress{
				{
					FirstName: "Ai",
					LastName:  "Tanaka",
				},
			},
			expected: map[string]string{
				"name:ai tanaka": "r18dev",
			},
		},
		{
			name: "Multiple results with priority",
			results: []*models.ScraperResult{
				{
					Source: "r18dev",
					Actresses: []models.ActressInfo{
						{
							FirstName: "Ai",
							LastName:  "Tanaka",
						},
					},
				},
				{
					Source: "dmm",
					Actresses: []models.ActressInfo{
						{
							FirstName: "Ai",
							LastName:  "Tanaka",
						},
					},
				},
			},
			resolvedPriority: map[string][]string{
				"Actress": {"r18dev", "dmm"},
			},
			actresses: []models.Actress{
				{
					FirstName: "Ai",
					LastName:  "Tanaka",
				},
			},
			expected: map[string]string{
				"name:ai tanaka": "r18dev",
			},
		},
		{
			name: "DMMID matching",
			results: []*models.ScraperResult{
				{
					Source: "r18dev",
					Actresses: []models.ActressInfo{
						{
							DMMID:     12345,
							FirstName: "Other",
							LastName:  "Actress",
						},
					},
				},
			},
			actresses: []models.Actress{
				{
					DMMID:     12345,
					FirstName: "Ai",
					LastName:  "Tanaka",
				},
			},
			expected: map[string]string{
				"dmmid:12345": "r18dev",
			},
		},
		{
			name: "No match found",
			results: []*models.ScraperResult{
				{
					Source: "r18dev",
					Actresses: []models.ActressInfo{
						{
							FirstName: "Other",
							LastName:  "Actress",
						},
					},
				},
			},
			actresses: []models.Actress{
				{
					FirstName: "Ai",
					LastName:  "Tanaka",
				},
			},
			expected: nil,
		},
		{
			name: "Japanese name matching",
			results: []*models.ScraperResult{
				{
					Source: "r18dev",
					Actresses: []models.ActressInfo{
						{
							JapaneseName: "田中愛",
						},
					},
				},
			},
			actresses: []models.Actress{
				{
					JapaneseName: "田中愛",
				},
			},
			expected: map[string]string{
				"name:田中愛": "r18dev",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildActressSourcesFromScrapeResults(
				tt.results,
				tt.resolvedPriority,
				tt.customPriority,
				tt.actresses,
			)

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestBuildActressSourcesFromCachedMovie tests buildActressSourcesFromCachedMovie
func TestBuildActressSourcesFromCachedMovie(t *testing.T) {
	tests := []struct {
		name     string
		movie    *models.Movie
		expected map[string]string
	}{
		{
			name:     "Nil movie",
			movie:    nil,
			expected: nil,
		},
		{
			name:     "Empty actresses",
			movie:    &models.Movie{},
			expected: nil,
		},
		{
			name: "Single actress with source",
			movie: &models.Movie{
				SourceName: "r18dev",
				Actresses: []models.Actress{
					{
						FirstName: "Ai",
						LastName:  "Tanaka",
					},
				},
			},
			expected: map[string]string{
				"name:ai tanaka": "r18dev",
			},
		},
		{
			name: "Multiple actresses",
			movie: &models.Movie{
				SourceName: "dmm",
				Actresses: []models.Actress{
					{
						FirstName: "Ai",
						LastName:  "Tanaka",
					},
					{
						FirstName: "Yui",
						LastName:  "Nakamura",
					},
				},
			},
			expected: map[string]string{
				"name:ai tanaka":    "dmm",
				"name:yui nakamura": "dmm",
			},
		},
		{
			name: "Empty source name defaults to scraper",
			movie: &models.Movie{
				SourceName: "",
				Actresses: []models.Actress{
					{
						FirstName: "Ai",
						LastName:  "Tanaka",
					},
				},
			},
			expected: map[string]string{
				"name:ai tanaka": "scraper",
			},
		},
		{
			name: "Actress with DMMID",
			movie: &models.Movie{
				SourceName: "r18dev",
				Actresses: []models.Actress{
					{
						DMMID:     12345,
						FirstName: "Ai",
						LastName:  "Tanaka",
					},
				},
			},
			expected: map[string]string{
				"dmmid:12345": "r18dev",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildActressSourcesFromCachedMovie(tt.movie)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestApplyActressMergeProvenance tests applyActressMergeProvenance
func TestApplyActressMergeProvenance(t *testing.T) {
	tests := []struct {
		name              string
		actressSources    map[string]string
		provenance        map[string]nfo.DataSource
		actresses         []models.Actress
		expected          map[string]string
		expectedContains  map[string]string // Partial match expected
		skipMutationCheck bool
	}{
		{
			name:           "Empty actresses",
			actressSources: map[string]string{"name:test": "r18dev"},
			provenance:     map[string]nfo.DataSource{"Actresses": {Source: "nfo"}},
			actresses:      []models.Actress{},
			expected:       map[string]string{"name:test": "r18dev"},
		},
		{
			name:           "Empty provenance",
			actressSources: map[string]string{"name:test": "r18dev"},
			provenance:     map[string]nfo.DataSource{},
			actresses:      []models.Actress{{FirstName: "Test"}},
			expected:       map[string]string{"name:test": "r18dev"},
		},
		{
			name:           "No Actresses in provenance",
			actressSources: map[string]string{"name:test": "r18dev"},
			provenance:     map[string]nfo.DataSource{"Title": {Source: "nfo"}},
			actresses:      []models.Actress{{FirstName: "Test"}},
			expected:       map[string]string{"name:test": "r18dev"},
		},
		{
			name:           "Empty source in provenance",
			actressSources: map[string]string{"name:test": "r18dev"},
			provenance:     map[string]nfo.DataSource{"Actresses": {Source: ""}},
			actresses:      []models.Actress{{FirstName: "Test"}},
			expected:       map[string]string{"name:test": "r18dev"},
		},
		{
			name:           "Nil actressSources",
			actressSources: nil,
			provenance:     map[string]nfo.DataSource{"Actresses": {Source: "nfo"}},
			actresses:      []models.Actress{{FirstName: "Test"}},
			expected:       map[string]string{"name:test": "nfo"},
		},
		{
			name:           "Apply provenance source - keeps existing scraper",
			actressSources: map[string]string{"name:test": "r18dev"},
			provenance:     map[string]nfo.DataSource{"Actresses": {Source: "nfo"}},
			actresses:      []models.Actress{{FirstName: "Test"}},
			// Function keeps existing scraper-specific attribution
			expected: map[string]string{"name:test": "r18dev"},
		},
		{
			name:              "Apply provenance source - overwrites when existing is scraper",
			actressSources:    map[string]string{"name:test": "scraper"},
			provenance:        map[string]nfo.DataSource{"Actresses": {Source: "nfo"}},
			actresses:         []models.Actress{{FirstName: "Test"}},
			expected:          map[string]string{"name:test": "nfo"},
			skipMutationCheck: true,
		},
		{
			name:           "Apply provenance source - preserves when existing is nfo",
			actressSources: map[string]string{"name:test": "nfo"},
			provenance:     map[string]nfo.DataSource{"Actresses": {Source: "nfo"}},
			actresses:      []models.Actress{{FirstName: "Test"}},
			expected:       map[string]string{"name:test": "nfo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of input to verify it's not mutated
			inputSources := make(map[string]string)
			for k, v := range tt.actressSources {
				inputSources[k] = v
			}

			result := applyActressMergeProvenance(tt.actressSources, tt.provenance, tt.actresses)

			if tt.expected != nil {
				assert.Equal(t, tt.expected, result)
			}

			if tt.expectedContains != nil {
				for k, v := range tt.expectedContains {
					assert.Equal(t, v, result[k])
				}
			}

			// Verify input was not mutated (except when it was nil or skipMutationCheck is true)
			if tt.actressSources != nil && !tt.skipMutationCheck {
				for k, v := range inputSources {
					if _, exists := result[k]; exists {
						assert.Equal(t, v, result[k])
					}
				}
			}
		})
	}
}
