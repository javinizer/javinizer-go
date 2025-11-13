package nfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyPreset(t *testing.T) {
	tests := []struct {
		name            string
		preset          string
		scalarStrategy  string
		arrayStrategy   string
		wantScalar      string
		wantArray       string
		wantErr         bool
		wantErrContains string
	}{
		{
			name:           "Conservative preset",
			preset:         "conservative",
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
			wantScalar:     "preserve-existing",
			wantArray:      "merge",
			wantErr:        false,
		},
		{
			name:           "Gap-fill preset",
			preset:         "gap-fill",
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
			wantScalar:     "fill-missing-only",
			wantArray:      "merge",
			wantErr:        false,
		},
		{
			name:           "Aggressive preset",
			preset:         "aggressive",
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
			wantScalar:     "prefer-scraper",
			wantArray:      "replace",
			wantErr:        false,
		},
		{
			name:           "Empty preset (no change)",
			preset:         "",
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
			wantScalar:     "prefer-nfo",
			wantArray:      "merge",
			wantErr:        false,
		},
		{
			name:           "Case insensitive - CONSERVATIVE",
			preset:         "CONSERVATIVE",
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
			wantScalar:     "preserve-existing",
			wantArray:      "merge",
			wantErr:        false,
		},
		{
			name:            "Invalid preset",
			preset:          "invalid-preset",
			scalarStrategy:  "prefer-nfo",
			arrayStrategy:   "merge",
			wantScalar:      "prefer-nfo",
			wantArray:       "merge",
			wantErr:         true,
			wantErrContains: "invalid preset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotScalar, gotArray, err := ApplyPreset(tt.preset, tt.scalarStrategy, tt.arrayStrategy)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					assert.Contains(t, err.Error(), tt.wantErrContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantScalar, gotScalar, "scalar strategy mismatch")
				assert.Equal(t, tt.wantArray, gotArray, "array strategy mismatch")
			}
		})
	}
}

func TestApplyPreset_Integration(t *testing.T) {
	// Test that preset values work with ParseScalarStrategy and ParseArrayStrategy
	tests := []struct {
		name           string
		preset         string
		wantScalarEnum MergeStrategy
		wantArrayMerge bool
	}{
		{
			name:           "Conservative -> PreserveExisting + merge",
			preset:         "conservative",
			wantScalarEnum: PreserveExisting,
			wantArrayMerge: true,
		},
		{
			name:           "Gap-fill -> FillMissingOnly + merge",
			preset:         "gap-fill",
			wantScalarEnum: FillMissingOnly,
			wantArrayMerge: true,
		},
		{
			name:           "Aggressive -> PreferScraper + replace",
			preset:         "aggressive",
			wantScalarEnum: PreferScraper,
			wantArrayMerge: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scalarStr, arrayStr, err := ApplyPreset(tt.preset, "", "")
			require.NoError(t, err)

			gotScalarEnum := ParseScalarStrategy(scalarStr)
			gotArrayMerge := ParseArrayStrategy(arrayStr)

			assert.Equal(t, tt.wantScalarEnum, gotScalarEnum, "scalar enum mismatch")
			assert.Equal(t, tt.wantArrayMerge, gotArrayMerge, "array merge flag mismatch")
		})
	}
}
