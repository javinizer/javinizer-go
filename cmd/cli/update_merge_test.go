package main

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/assert"
)

// TestApplyPresetIntegration tests the preset application logic used in runUpdate
func TestApplyPresetIntegration(t *testing.T) {
	tests := []struct {
		name           string
		preset         string
		scalarStrategy string
		arrayStrategy  string
		wantScalar     string
		wantArray      string
		wantErr        bool
		description    string
	}{
		{
			name:           "Conservative preset overrides individual strategies",
			preset:         "conservative",
			scalarStrategy: "prefer-scraper",
			arrayStrategy:  "replace",
			wantScalar:     "preserve-existing",
			wantArray:      "merge",
			wantErr:        false,
			description:    "Conservative preset should result in preserve-existing + merge",
		},
		{
			name:           "Gap-fill preset overrides individual strategies",
			preset:         "gap-fill",
			scalarStrategy: "prefer-scraper",
			arrayStrategy:  "replace",
			wantScalar:     "fill-missing-only",
			wantArray:      "merge",
			wantErr:        false,
			description:    "Gap-fill preset should result in fill-missing-only + merge",
		},
		{
			name:           "Aggressive preset overrides individual strategies",
			preset:         "aggressive",
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
			wantScalar:     "prefer-scraper",
			wantArray:      "replace",
			wantErr:        false,
			description:    "Aggressive preset should result in prefer-scraper + replace",
		},
		{
			name:           "Empty preset uses individual strategies",
			preset:         "",
			scalarStrategy: "preserve-existing",
			arrayStrategy:  "merge",
			wantScalar:     "preserve-existing",
			wantArray:      "merge",
			wantErr:        false,
			description:    "Empty preset should pass through individual strategies unchanged",
		},
		{
			name:           "Invalid preset returns error",
			preset:         "invalid-preset",
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
			wantScalar:     "prefer-nfo",
			wantArray:      "merge",
			wantErr:        true,
			description:    "Invalid preset should return error",
		},
		{
			name:           "Preset with empty individual strategies",
			preset:         "conservative",
			scalarStrategy: "",
			arrayStrategy:  "",
			wantScalar:     "preserve-existing",
			wantArray:      "merge",
			wantErr:        false,
			description:    "Preset should work even with empty individual strategies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This replicates the logic from runUpdate lines 1110-1118
			var err error
			resultScalar := tt.scalarStrategy
			resultArray := tt.arrayStrategy

			if tt.preset != "" {
				resultScalar, resultArray, err = nfo.ApplyPreset(tt.preset, resultScalar, resultArray)
			}

			if tt.wantErr {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.wantScalar, resultScalar, "scalar strategy mismatch: %s", tt.description)
				assert.Equal(t, tt.wantArray, resultArray, "array strategy mismatch: %s", tt.description)
			}
		})
	}
}

// TestScalarStrategyParsing tests parsing of scalar strategy strings
func TestScalarStrategyParsing(t *testing.T) {
	tests := []struct {
		name             string
		strategyStr      string
		expectedStrategy nfo.MergeStrategy
		description      string
	}{
		{
			name:             "prefer-nfo strategy",
			strategyStr:      "prefer-nfo",
			expectedStrategy: nfo.PreferNFO,
			description:      "Should parse prefer-nfo correctly",
		},
		{
			name:             "prefer-scraper strategy",
			strategyStr:      "prefer-scraper",
			expectedStrategy: nfo.PreferScraper,
			description:      "Should parse prefer-scraper correctly",
		},
		{
			name:             "preserve-existing strategy",
			strategyStr:      "preserve-existing",
			expectedStrategy: nfo.PreserveExisting,
			description:      "Should parse preserve-existing correctly",
		},
		{
			name:             "fill-missing-only strategy",
			strategyStr:      "fill-missing-only",
			expectedStrategy: nfo.FillMissingOnly,
			description:      "Should parse fill-missing-only correctly",
		},
		{
			name:             "case insensitive - PREFER-NFO",
			strategyStr:      "PREFER-NFO",
			expectedStrategy: nfo.PreferNFO,
			description:      "Parsing should be case-insensitive",
		},
		{
			name:             "case insensitive - PrEsErVe-ExIsTiNg",
			strategyStr:      "PrEsErVe-ExIsTiNg",
			expectedStrategy: nfo.PreserveExisting,
			description:      "Mixed case should work",
		},
		{
			name:             "default to prefer-nfo on empty",
			strategyStr:      "",
			expectedStrategy: nfo.PreferNFO,
			description:      "Empty strategy should default to prefer-nfo",
		},
		{
			name:             "default to prefer-nfo on unknown",
			strategyStr:      "invalid-strategy",
			expectedStrategy: nfo.PreferNFO,
			description:      "Unknown strategy should default to prefer-nfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nfo.ParseScalarStrategy(tt.strategyStr)
			assert.Equal(t, tt.expectedStrategy, result, tt.description)
		})
	}
}

// TestArrayStrategyParsing tests parsing of array strategy strings
func TestArrayStrategyParsing(t *testing.T) {
	tests := []struct {
		name          string
		strategyStr   string
		expectedMerge bool
		description   string
	}{
		{
			name:          "merge strategy",
			strategyStr:   "merge",
			expectedMerge: true,
			description:   "Should parse merge correctly",
		},
		{
			name:          "replace strategy",
			strategyStr:   "replace",
			expectedMerge: false,
			description:   "Should parse replace correctly",
		},
		{
			name:          "case insensitive - MERGE",
			strategyStr:   "MERGE",
			expectedMerge: true,
			description:   "Parsing should be case-insensitive",
		},
		{
			name:          "case insensitive - RePlAcE",
			strategyStr:   "RePlAcE",
			expectedMerge: false,
			description:   "Mixed case should work",
		},
		{
			name:          "default to merge on empty",
			strategyStr:   "",
			expectedMerge: true,
			description:   "Empty strategy should default to merge",
		},
		{
			name:          "default to merge on unknown",
			strategyStr:   "invalid-strategy",
			expectedMerge: true,
			description:   "Unknown strategy should default to merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nfo.ParseArrayStrategy(tt.strategyStr)
			assert.Equal(t, tt.expectedMerge, result, tt.description)
		})
	}
}

// TestPresetDefinitions validates the exact preset mappings
func TestPresetDefinitions(t *testing.T) {
	tests := []struct {
		name       string
		preset     string
		wantScalar string
		wantArray  string
	}{
		{
			name:       "Conservative preset definition",
			preset:     "conservative",
			wantScalar: "preserve-existing",
			wantArray:  "merge",
		},
		{
			name:       "Gap-fill preset definition",
			preset:     "gap-fill",
			wantScalar: "fill-missing-only",
			wantArray:  "merge",
		},
		{
			name:       "Aggressive preset definition",
			preset:     "aggressive",
			wantScalar: "prefer-scraper",
			wantArray:  "replace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultScalar, resultArray, err := nfo.ApplyPreset(tt.preset, "", "")
			assert.NoError(t, err)
			assert.Equal(t, tt.wantScalar, resultScalar, "Scalar strategy mismatch for preset %s", tt.preset)
			assert.Equal(t, tt.wantArray, resultArray, "Array strategy mismatch for preset %s", tt.preset)
		})
	}
}

// TestPresetPrecedence ensures presets override individual strategy flags
func TestPresetPrecedence(t *testing.T) {
	// Test that preset always overrides individual strategies
	individualStrategies := []struct {
		scalar string
		array  string
	}{
		{"prefer-scraper", "replace"},
		{"prefer-nfo", "merge"},
		{"preserve-existing", "merge"},
		{"fill-missing-only", "replace"},
	}

	for _, strat := range individualStrategies {
		t.Run("conservative_overrides_"+strat.scalar+"_"+strat.array, func(t *testing.T) {
			resultScalar, resultArray, err := nfo.ApplyPreset("conservative", strat.scalar, strat.array)
			assert.NoError(t, err)
			assert.Equal(t, "preserve-existing", resultScalar, "Conservative preset should override scalar strategy")
			assert.Equal(t, "merge", resultArray, "Conservative preset should override array strategy")
		})
	}
}

// TestTwoParameterArchitecture validates the two-parameter design
func TestTwoParameterArchitecture(t *testing.T) {
	t.Run("Independent scalar and array strategies", func(t *testing.T) {
		// Scalar and array strategies should be independently configurable
		scalarOptions := []string{"prefer-nfo", "prefer-scraper", "preserve-existing", "fill-missing-only"}
		arrayOptions := []string{"merge", "replace"}

		for _, scalar := range scalarOptions {
			for _, array := range arrayOptions {
				scalarEnum := nfo.ParseScalarStrategy(scalar)
				arrayBool := nfo.ParseArrayStrategy(array)

				// Scalar enum should be valid
				assert.GreaterOrEqual(t, int(scalarEnum), 0, "Scalar strategy %s should parse successfully", scalar)

				// Array bool should match expected value
				expectedMerge := (array == "merge")
				assert.Equal(t, expectedMerge, arrayBool, "Array strategy %s should parse to merge=%v", array, expectedMerge)
			}
		}
	})

	t.Run("Preset applies to both parameters", func(t *testing.T) {
		// Presets should set both scalar and array strategies
		presets := []string{"conservative", "gap-fill", "aggressive"}

		for _, preset := range presets {
			scalar, array, err := nfo.ApplyPreset(preset, "", "")
			assert.NoError(t, err, "Preset %s should apply successfully", preset)
			assert.NotEmpty(t, scalar, "Preset %s should set scalar strategy", preset)
			assert.NotEmpty(t, array, "Preset %s should set array strategy", preset)
		}
	})
}

// TestNewStrategyBehavior validates the new strategy options
func TestNewStrategyBehavior(t *testing.T) {
	t.Run("PreserveExisting enum exists", func(t *testing.T) {
		strategy := nfo.ParseScalarStrategy("preserve-existing")
		assert.Equal(t, nfo.PreserveExisting, strategy)
	})

	t.Run("FillMissingOnly enum exists", func(t *testing.T) {
		strategy := nfo.ParseScalarStrategy("fill-missing-only")
		assert.Equal(t, nfo.FillMissingOnly, strategy)
	})

	t.Run("New strategies are distinct from existing ones", func(t *testing.T) {
		preferNFO := nfo.ParseScalarStrategy("prefer-nfo")
		preferScraper := nfo.ParseScalarStrategy("prefer-scraper")
		preserveExisting := nfo.ParseScalarStrategy("preserve-existing")
		fillMissingOnly := nfo.ParseScalarStrategy("fill-missing-only")

		// All four should be distinct enum values
		assert.NotEqual(t, preferNFO, preferScraper)
		assert.NotEqual(t, preferNFO, preserveExisting)
		assert.NotEqual(t, preferNFO, fillMissingOnly)
		assert.NotEqual(t, preferScraper, preserveExisting)
		assert.NotEqual(t, preferScraper, fillMissingOnly)
		assert.NotEqual(t, preserveExisting, fillMissingOnly)
	})
}
